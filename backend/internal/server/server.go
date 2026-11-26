// Package server starts a ReelStash API process in one of three roles:
//
//   - all:   everything in one process (local packages, SQLite, development)
//   - media: the media library API — browsing, playback, sign-in and per-user
//     state. It runs no background jobs; work it asks for (sprites, previews)
//     is queued in the database for the admin server.
//   - admin: the admin API plus every background job, the scanner, schedules,
//     file watching, downloads and plugin bundles.
//
// The media and admin servers share one PostgreSQL database. Notifications
// and plugin changes cross between them over PostgreSQL LISTEN/NOTIFY.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/auth"
	"reelstash/backend/internal/authplugin"
	"reelstash/backend/internal/config"
	"reelstash/backend/internal/database"
	"reelstash/backend/internal/downloads"
	"reelstash/backend/internal/events"
	"reelstash/backend/internal/extprovider"
	"reelstash/backend/internal/housekeeping"
	"reelstash/backend/internal/httpapi"
	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/namer"
	"reelstash/backend/internal/notify"
	"reelstash/backend/internal/organizer"
	"reelstash/backend/internal/plugins"
	"reelstash/backend/internal/provider"
	"reelstash/backend/internal/qbit"
	"reelstash/backend/internal/scanner"
	"reelstash/backend/internal/schedule"
	"reelstash/backend/internal/tmdb"
)

// namerMatcher adapts the namer engine to the scanner's metadata lookup.
type namerMatcher struct{ *namer.Engine }

func (m namerMatcher) Lookup(ctx context.Context, mediaFileID int64) (string, error) {
	result, err := m.MatchFile(ctx, mediaFileID)
	if err != nil {
		return "", err
	}
	return result.Status, nil
}

// shutdownGrace bounds how long a deploy/restart waits for an in-flight scan
// to reach its next per-file checkpoint before the process exits anyway. The
// job itself is reverted to PENDING (not left RUNNING or FAILED) so it
// resumes cleanly on the next boot instead of needing manual DB cleanup.
const shutdownGrace = 20 * time.Second

// pollInterval is how often the admin server looks for jobs the media
// server queued.
const pollInterval = 3 * time.Second

// QbitInit handles `qbit-init [config path]`: it writes the WebUI username
// and password from REELSTASH_QBIT_USERNAME/PASSWORD into qBittorrent's
// config before it starts (the qbittorrent-init Compose service). It reports
// whether args asked for it.
func QbitInit(args []string) bool {
	if len(args) == 0 || args[0] != "qbit-init" {
		return false
	}
	path := "/config/qBittorrent/qBittorrent.conf"
	if len(args) > 1 {
		path = args[1]
	}
	if err := qbit.SeedConfig(path, os.Getenv("REELSTASH_QBIT_USERNAME"), os.Getenv("REELSTASH_QBIT_PASSWORD")); err != nil {
		log.Fatalf("qbit-init: %v", err)
	}
	log.Printf("qbit-init: wrote the WebUI sign-in to %s", path)
	return true
}

// Run starts the API in role and blocks until SIGINT or SIGTERM.
func Run(role httpapi.Role) {
	cfg := config.Load()
	ctx := context.Background()
	runsJobs := role.ServesAdmin()

	db, err := database.Open(ctx, cfg.DatabaseURL, int32(cfg.PgMaxConns))
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer db.Close()
	pool, _ := db.(*pgxpool.Pool)
	if role != httpapi.RoleAll && pool == nil {
		log.Fatalf("the %s server needs PostgreSQL; SQLite installs run everything in one process (reelstash-server)", role)
	}

	if err := migrate(ctx, db, pool); err != nil {
		log.Fatalf("migrate database: %v", err)
	}
	log.Printf("starting the ReelStash %s server", role)

	if cfg.EmergencyLocalAuth {
		log.Printf("WARNING: running with EMERGENCY_LOCAL_AUTH enabled")
	}
	if runsJobs && cfg.AdminUsername != "" && cfg.AdminPassword != "" {
		switch created, err := auth.EnsureAdmin(ctx, db, cfg.AdminUsername, cfg.AdminPassword); {
		case err != nil:
			log.Printf("WARNING: could not create the admin %q from REELSTASH_ADMIN_USERNAME/PASSWORD: %v", cfg.AdminUsername, err)
		case created:
			log.Printf("created the admin account %q from REELSTASH_ADMIN_USERNAME/PASSWORD", cfg.AdminUsername)
		}
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hub := events.New()
	notifier := notify.New(db, hub)
	if role != httpapi.RoleAll {
		events.Bridge(sigCtx, pool, hub, "notification", httpapi.PluginsChanged)
	}

	// SCAN/ORGANIZE and THUMBNAIL run on separate worker pools: a
	// ScanStorageLocation call occupies its worker for the whole scan, so a
	// slow or stalled scan must not be able to starve thumbnail generation
	// (or vice versa) of every available worker. The media server only
	// queues jobs; the admin server runs them.
	var jobRunner, thumbnailJobRunner *jobs.Runner
	if runsJobs {
		jobs.SetChangeHook(hub.Kick)
		jobs.SetFinishHook(func(f jobs.Finished) { go notifier.JobFinished(f) })
		jobRunner = jobs.NewRunner(db, cfg.ScanWorkers)
		thumbnailJobRunner = jobs.NewRunner(db, cfg.ThumbnailWorkers)
	} else {
		jobRunner = jobs.NewQueue(db)
		thumbnailJobRunner = jobs.NewQueue(db)
	}
	sc := scanner.New(db, jobRunner, thumbnailJobRunner, cfg.ThumbnailDir, cfg.ScanFileWorkers)
	sc.SpriteDir, sc.PreviewDir, sc.PosterDir = cfg.SpriteDir, cfg.PreviewDir, cfg.PosterDir

	providers := provider.NewRegistry()
	providers.Register(tmdb.NewProvider(tmdb.New("", "")))
	extprovider.RegisterSaved(ctx, db, providers)
	providers.Bootstrap(ctx, db)

	// Plugin bundles run as child processes and connect to the registries above
	// when they are ready; they are started once the server is up. The media
	// server runs only sign-in plugins, so people can sign in there too.
	authPlugins := authplugin.NewRegistry()
	pluginOpts := plugins.Options{
		Root: cfg.PluginDir, Enabled: cfg.PluginBundles, Store: plugins.DBStore{DB: db},
		Providers: providers, Auth: authPlugins,
	}
	if !runsJobs {
		pluginOpts.Kinds = []plugins.Kind{plugins.KindAuth}
	}
	bundles := plugins.NewManager(pluginOpts)
	if err := bundles.Load(ctx); err != nil {
		log.Printf("could not read the installed plugin bundles: %v", err)
	}
	if !cfg.PluginBundles {
		log.Printf("plugin bundles are switched off (REELSTASH_PLUGIN_BUNDLES=false)")
	}
	namerEngine := namer.New(db, providers, sc, cfg.RenameTemplate, cfg.PosterDir)
	sc.Matcher = namerMatcher{namerEngine}

	opts := []httpapi.Option{
		httpapi.WithRole(role),
		httpapi.WithPlugins(bundles, authPlugins),
		httpapi.WithBackground(sigCtx),
	}
	var background []func(context.Context)
	if runsJobs {
		registerJobs(ctx, cfg, db, jobRunner, sc, namerEngine)
		if err := jobRunner.AdoptOrphans(ctx); err != nil {
			log.Printf("failed to resume pending background jobs: %v", err)
		}
		if err := thumbnailJobRunner.AdoptOrphans(ctx); err != nil {
			log.Printf("failed to resume pending thumbnail jobs: %v", err)
		}
		sc.BackfillRotation(ctx)
		if namerEngine.Enabled() {
			log.Printf("metadata matching enabled (%d provider(s) configured)", len(providers.Enabled(provider.KindMedia)))
		} else {
			log.Printf("metadata matching disabled (add an API key under Metadata → Providers, or install a provider plugin under Plugins)")
		}

		// Problems with recurring scans and file watching reach admins the same way
		// a failed job does: an in-app notification and any subscribed webhooks.
		alert := func(ctx context.Context, title, body string) {
			notifier.Emit(ctx, notify.Event{Kind: notify.JobFailed, Title: title, Body: body, Link: "/admin/automation", AdminOnly: true})
		}
		scheduler := schedule.NewScheduler(db, jobRunner, alert)
		watcher := schedule.NewWatcher(db, jobRunner, alert)

		// Downloads: the live link to qBittorrent. Moves and failures reach
		// admins as notifications and webhooks.
		dl := downloads.New(db, jobRunner, qbit.Config{URL: cfg.QbitURL, Username: cfg.QbitUsername, Password: cfg.QbitPassword},
			func(ctx context.Context, failed bool, title, body string) {
				kind := notify.DownloadMoved
				if failed {
					kind = notify.DownloadFailed
				}
				notifier.Emit(ctx, notify.Event{Kind: kind, Title: title, Body: body, Link: "/downloads", AdminOnly: true})
			})
		opts = append(opts, httpapi.WithWatcher(watcher), httpapi.WithDownloads(dl))

		// Hourly disk capacity readings feed the storage charts and keep each
		// disk's online status current between scans.
		background = append(background,
			func(c context.Context) { sc.RunCapacitySampler(c, time.Hour) },
			scheduler.Run, watcher.Run, dl.Run)
		if role == httpapi.RoleAdmin {
			// Jobs the media server queued (sprites, previews) are picked up here.
			background = append(background,
				func(c context.Context) { jobRunner.PollPending(c, pollInterval) },
				func(c context.Context) { thumbnailJobRunner.PollPending(c, pollInterval) })
		}
	} else {
		// The admin server reports plugin changes; reload the sign-in plugins.
		background = append(background, func(c context.Context) { reloadPluginsOnChange(c, hub, bundles) })
	}

	router := httpapi.NewRouter(db, cfg, jobRunner, sc, namerEngine, hub, notifier, opts...)
	var handler http.Handler = router
	if webDir := os.Getenv("REELSTASH_WEB_DIR"); webDir != "" {
		handler = localWebHandler(router, webDir)
	}

	server := &http.Server{
		Addr:    cfg.Addr,
		Handler: handler,
		// ReadTimeout/ReadHeaderTimeout/IdleTimeout bound slow or stalled
		// clients from holding a connection open indefinitely. WriteTimeout
		// is intentionally left unset: HLS segment transcodes and large
		// direct-play range responses can legitimately take longer than any
		// single fixed cap would allow.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	bundles.StartAll(sigCtx)
	for _, run := range background {
		go run(sigCtx)
	}

	go func() {
		log.Printf("reelstash %s server listening on %s", role, cfg.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server stopped: %v", err)
		}
	}()

	<-sigCtx.Done()
	log.Printf("shutting down: waiting up to %s for in-flight jobs", shutdownGrace)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("http server shutdown: %v", err)
	}
	bundles.Shutdown()
	jobRunner.Shutdown(shutdownGrace)
	thumbnailJobRunner.Shutdown(shutdownGrace)
	log.Printf("shutdown complete")
}

// migrate applies the schema. With PostgreSQL it holds an advisory lock so a
// media and an admin server starting together never migrate at once.
func migrate(ctx context.Context, db database.DB, pool *pgxpool.Pool) error {
	if pool == nil {
		return database.Migrate(ctx, db)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	const lockKey = 0x5265656c // "Reel"
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		return err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, lockKey)
	return database.Migrate(ctx, db)
}

// reloadPluginsOnChange reloads the sign-in plugins whenever another server
// reports a plugin change.
func reloadPluginsOnChange(ctx context.Context, hub *events.Hub, bundles *plugins.Manager) {
	sub, unsubscribe := hub.Subscribe(true)
	defer unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-sub.C:
			if ev.Name != httpapi.PluginsChanged {
				continue
			}
			log.Printf("plugins changed on the admin server; reloading sign-in plugins")
			if err := bundles.Reload(ctx); err != nil {
				log.Printf("reload plugins: %v", err)
			}
		}
	}
}

// registerJobs connects every background job kind to its handler.
func registerJobs(ctx context.Context, cfg config.Config, db database.DB, jobRunner *jobs.Runner, sc *scanner.Scanner, namerEngine *namer.Engine) {
	sc.RegisterJobs()
	jobRunner.Register("ORGANIZE", func(ctx context.Context, db database.DB, jobID int64, payload json.RawMessage) (any, error) {
		var p struct {
			StorageLocationID int64 `json:"storageLocationId"`
			LibraryID         int64 `json:"libraryId"`
			DryRun            bool  `json:"dryRun"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		return organizer.Run(ctx, db, jobID, p.LibraryID, p.StorageLocationID, p.DryRun, organizer.Options{
			FallbackTemplate: cfg.RenameTemplate, PosterDir: cfg.PosterDir, ThumbnailDir: cfg.ThumbnailDir,
		})
	})
	jobRunner.Register("NAMER", func(ctx context.Context, db database.DB, jobID int64, payload json.RawMessage) (any, error) {
		var p struct {
			MediaFileID int64  `json:"mediaFileId"`
			Provider    string `json:"provider"`
			ExternalID  string `json:"externalId"`
			SceneUUID   string `json:"sceneUUID"` // jobs queued before providers existed
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		if p.MediaFileID == 0 {
			return nil, errors.New("mediaFileId is required")
		}
		if p.ExternalID == "" {
			p.ExternalID = p.SceneUUID
		}
		if p.ExternalID != "" {
			if p.Provider == "" {
				return nil, errors.New("provider is required with externalId")
			}
			return namerEngine.SelectCandidate(ctx, p.MediaFileID, p.Provider, p.ExternalID)
		}
		return namerEngine.MatchFile(ctx, p.MediaFileID)
	})
	jobRunner.Register("LINK_RECOVERY", func(ctx context.Context, db database.DB, jobID int64, payload json.RawMessage) (any, error) {
		return namerEngine.RecoverLinks(ctx, jobID)
	})
	jobRunner.Register("PERFORMER_SYNC", func(ctx context.Context, db database.DB, jobID int64, payload json.RawMessage) (any, error) {
		var p struct {
			RetryUnresolved bool `json:"retryUnresolved"`
		}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &p)
		}
		return namerEngine.SyncPerformers(ctx, jobID, p.RetryUnresolved)
	})
	jobRunner.Register("STUDIO_SYNC", func(ctx context.Context, db database.DB, jobID int64, payload json.RawMessage) (any, error) {
		return namerEngine.SyncStudios(ctx, jobID)
	})
	jobRunner.Register("STUDIO_MATCH", func(ctx context.Context, db database.DB, jobID int64, payload json.RawMessage) (any, error) {
		var p struct {
			StudioID   int64  `json:"studioId"`
			Provider   string `json:"provider"`
			ExternalID string `json:"externalId"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		if p.StudioID == 0 || p.Provider == "" || p.ExternalID == "" {
			return nil, errors.New("studioId, provider and externalId are required")
		}
		return namerEngine.MatchStudio(ctx, p.StudioID, p.Provider, p.ExternalID)
	})
	jobRunner.Register("PERFORMER_MATCH", func(ctx context.Context, db database.DB, jobID int64, payload json.RawMessage) (any, error) {
		var p struct {
			PerformerID int64  `json:"performerId"`
			Provider    string `json:"provider"`
			ExternalID  string `json:"externalId"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		if p.PerformerID == 0 || p.Provider == "" || p.ExternalID == "" {
			return nil, errors.New("performerId, provider and externalId are required")
		}
		return namerEngine.MatchPerformer(ctx, p.PerformerID, p.Provider, p.ExternalID)
	})
	jobRunner.Register(httpapi.FileTransferJobKind, httpapi.FileTransferJob)
	jobRunner.Register(housekeeping.JobKind, housekeeping.New(db, housekeeping.DirsFromConfig(cfg)).Handler())
}
