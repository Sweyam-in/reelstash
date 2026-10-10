// Command server runs the ReelStash Go API.
package main

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
	_ "time/tzdata" // lets TZ=Area/City work on the slim runtime image, for cron schedules

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

func main() {
	// `reelstash-server qbit-init [config path]` writes the WebUI username
	// and password from REELSTASH_QBIT_USERNAME/PASSWORD into qBittorrent's
	// config before it starts (the qbittorrent-init Compose service).
	if len(os.Args) > 1 && os.Args[1] == "qbit-init" {
		path := "/config/qBittorrent/qBittorrent.conf"
		if len(os.Args) > 2 {
			path = os.Args[2]
		}
		if err := qbit.SeedConfig(path, os.Getenv("REELSTASH_QBIT_USERNAME"), os.Getenv("REELSTASH_QBIT_PASSWORD")); err != nil {
			log.Fatalf("qbit-init: %v", err)
		}
		log.Printf("qbit-init: wrote the WebUI sign-in to %s", path)
		return
	}

	cfg := config.Load()
	ctx := context.Background()

	pool, err := database.Connect(ctx, cfg.DatabaseURL, int32(cfg.PgMaxConns))
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	if err := database.Migrate(ctx, pool); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	if cfg.EmergencyLocalAuth {
		log.Printf("WARNING: running with EMERGENCY_LOCAL_AUTH enabled")
	}
	log.Printf("people sign in with a ReelStash username and password; add more ways (such as single sign-on) by installing a sign-in plugin under Admin → Plugins")
	if cfg.AdminUsername != "" && cfg.AdminPassword != "" {
		switch created, err := auth.EnsureAdmin(ctx, pool, cfg.AdminUsername, cfg.AdminPassword); {
		case err != nil:
			log.Printf("WARNING: could not create the admin %q from REELSTASH_ADMIN_USERNAME/PASSWORD: %v", cfg.AdminUsername, err)
		case created:
			log.Printf("created the admin account %q from REELSTASH_ADMIN_USERNAME/PASSWORD", cfg.AdminUsername)
		}
	}

	// SCAN/ORGANIZE and THUMBNAIL run on separate worker pools: a
	// ScanStorageLocation call occupies its worker for the whole scan, so a
	// slow or stalled scan must not be able to starve thumbnail generation
	// (or vice versa) of every available worker.
	hub := events.New()
	notifier := notify.New(pool, hub)
	jobs.SetChangeHook(hub.Kick)
	jobs.SetFinishHook(func(f jobs.Finished) { go notifier.JobFinished(f) })

	jobRunner := jobs.NewRunner(pool, cfg.ScanWorkers)
	thumbnailJobRunner := jobs.NewRunner(pool, cfg.ThumbnailWorkers)
	sc := scanner.New(pool, jobRunner, thumbnailJobRunner, cfg.ThumbnailDir, cfg.ScanFileWorkers)
	sc.SpriteDir, sc.PreviewDir, sc.PosterDir = cfg.SpriteDir, cfg.PreviewDir, cfg.PosterDir
	sc.RegisterJobs()
	jobRunner.Register("ORGANIZE", func(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
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

	providers := provider.NewRegistry()
	providers.Register(tmdb.NewProvider(tmdb.New("", "")))
	extprovider.RegisterSaved(ctx, pool, providers)
	providers.Bootstrap(ctx, pool)

	// Plugin bundles run as child processes and connect to the registries above
	// when they are ready; they are started once the server is up.
	authPlugins := authplugin.NewRegistry()
	bundles := plugins.NewManager(plugins.Options{
		Root: cfg.PluginDir, Enabled: cfg.PluginBundles, Store: plugins.DBStore{DB: pool},
		Providers: providers, Auth: authPlugins,
	})
	if err := bundles.Load(ctx); err != nil {
		log.Printf("could not read the installed plugin bundles: %v", err)
	}
	if !cfg.PluginBundles {
		log.Printf("plugin bundles are switched off (REELSTASH_PLUGIN_BUNDLES=false)")
	}
	namerEngine := namer.New(pool, providers, sc, cfg.RenameTemplate, cfg.PosterDir)
	sc.Matcher = namerMatcher{namerEngine}
	if namerEngine.Enabled() {
		log.Printf("metadata matching enabled (%d provider(s) configured)", len(providers.Enabled(provider.KindMedia)))
	} else {
		log.Printf("metadata matching disabled (add an API key under Metadata → Providers, or install a provider plugin under Plugins)")
	}
	jobRunner.Register("NAMER", func(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
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
	jobRunner.Register("LINK_RECOVERY", func(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
		return namerEngine.RecoverLinks(ctx, jobID)
	})
	jobRunner.Register("PERFORMER_SYNC", func(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
		var p struct {
			RetryUnresolved bool `json:"retryUnresolved"`
		}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &p)
		}
		return namerEngine.SyncPerformers(ctx, jobID, p.RetryUnresolved)
	})
	jobRunner.Register("STUDIO_SYNC", func(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
		return namerEngine.SyncStudios(ctx, jobID)
	})
	jobRunner.Register("STUDIO_MATCH", func(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
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
	jobRunner.Register("PERFORMER_MATCH", func(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
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
	jobRunner.Register(housekeeping.JobKind, housekeeping.New(pool, housekeeping.DirsFromConfig(cfg)).Handler())
	if err := jobRunner.AdoptOrphans(ctx); err != nil {
		log.Printf("failed to resume pending background jobs: %v", err)
	}
	if err := thumbnailJobRunner.AdoptOrphans(ctx); err != nil {
		log.Printf("failed to resume pending thumbnail jobs: %v", err)
	}
	sc.BackfillRotation(ctx)

	// Problems with recurring scans and file watching reach admins the same way
	// a failed job does: an in-app notification and any subscribed webhooks.
	alert := func(ctx context.Context, title, body string) {
		notifier.Emit(ctx, notify.Event{Kind: notify.JobFailed, Title: title, Body: body, Link: "/admin/automation", AdminOnly: true})
	}
	scheduler := schedule.NewScheduler(pool, jobRunner, alert)
	watcher := schedule.NewWatcher(pool, jobRunner, alert)

	// Downloads: the live link to qBittorrent. Moves and failures reach
	// admins as notifications and webhooks.
	dl := downloads.New(pool, jobRunner, qbit.Config{URL: cfg.QbitURL, Username: cfg.QbitUsername, Password: cfg.QbitPassword},
		func(ctx context.Context, failed bool, title, body string) {
			kind := notify.DownloadMoved
			if failed {
				kind = notify.DownloadFailed
			}
			notifier.Emit(ctx, notify.Event{Kind: kind, Title: title, Body: body, Link: "/downloads", AdminOnly: true})
		})

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	router := httpapi.NewRouter(pool, cfg, jobRunner, sc, namerEngine, hub, notifier, httpapi.WithWatcher(watcher), httpapi.WithPlugins(bundles, authPlugins), httpapi.WithDownloads(dl), httpapi.WithBackground(sigCtx))

	server := &http.Server{
		Addr:    cfg.Addr,
		Handler: router,
		// ReadTimeout/ReadHeaderTimeout/IdleTimeout bound slow or stalled
		// clients from holding a connection open indefinitely. WriteTimeout
		// is intentionally left unset: HLS segment transcodes and large
		// direct-play range responses can legitimately take longer than any
		// single fixed cap would allow.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Hourly disk capacity readings feed the storage charts and keep each
	// disk's online status current between scans.
	bundles.StartAll(sigCtx)
	go sc.RunCapacitySampler(sigCtx, time.Hour)
	go scheduler.Run(sigCtx)
	go watcher.Run(sigCtx)
	go dl.Run(sigCtx)

	go func() {
		log.Printf("reelstash server listening on %s", cfg.Addr)
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
