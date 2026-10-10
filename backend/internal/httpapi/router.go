// Package httpapi wires the ReelStash media and admin routes onto a chi
// router backed by a pgx pool.
package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/authplugin"
	"reelstash/backend/internal/config"
	"reelstash/backend/internal/downloads"
	"reelstash/backend/internal/events"
	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/namer"
	"reelstash/backend/internal/notify"
	"reelstash/backend/internal/plugins"
	"reelstash/backend/internal/scanner"
	"reelstash/backend/internal/schedule"
)

type API struct {
	DB      *pgxpool.Pool
	Cfg     config.Config
	Jobs    *jobs.Runner
	Scanner *scanner.Scanner
	Namer   *namer.Engine
	Hub     *events.Hub
	Notify  *notify.Service
	// Watcher reports file-watching status; nil when watching is not running.
	Watcher *schedule.Watcher
	// Plugins installs and runs plugin bundles; nil when they are unavailable.
	Plugins *plugins.Manager
	// AuthPlugins are the running sign-in plugins; never nil after NewRouter.
	AuthPlugins *authplugin.Registry
	// Downloads is the qBittorrent link; nil when it is not running.
	Downloads *downloads.Service

	throttle *loginThrottle
	// background cancels process-lifetime loops on server shutdown.
	// Nil means the loops run until the process exits.
	background context.Context
}

// Option adjusts the API built by NewRouter.
type Option func(*API)

// WithWatcher gives the API the file watcher whose status the admin pages show.
func WithWatcher(w *schedule.Watcher) Option { return func(a *API) { a.Watcher = w } }

// WithPlugins gives the API the bundle manager and the sign-in plugins it runs.
func WithPlugins(m *plugins.Manager, authPlugins *authplugin.Registry) Option {
	return func(a *API) { a.Plugins, a.AuthPlugins = m, authPlugins }
}

// WithBackground stops the stats flush and jobs feed when ctx is cancelled.
// The server passes its signal context so a graceful shutdown flushes stream
// totals instead of leaving those loops to die with the process.
func WithBackground(ctx context.Context) Option {
	return func(a *API) { a.background = ctx }
}

func NewRouter(db *pgxpool.Pool, cfg config.Config, jobRunner *jobs.Runner, sc *scanner.Scanner, namerEngine *namer.Engine, hub *events.Hub, notifier *notify.Service, opts ...Option) http.Handler {
	api := &API{DB: db, Cfg: cfg, Jobs: jobRunner, Scanner: sc, Namer: namerEngine, Hub: hub, Notify: notifier, throttle: newLoginThrottle()}
	for _, opt := range opts {
		opt(api)
	}
	if api.AuthPlugins == nil {
		api.AuthPlugins = authplugin.NewRegistry()
	}
	bg := api.background
	if bg == nil {
		bg = context.Background()
	}
	go api.runStreamStatsFlush(bg)
	if hub != nil {
		go api.runJobsFeed(bg)
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(logExceptProbes)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", api.handleHealth)
	api.mountAuth(r)
	api.mountAPI(r)

	return r
}

// logExceptProbes is chi's request logger with /healthz left out. The Docker
// healthcheck calls that path every few seconds.
func logExceptProbes(next http.Handler) http.Handler {
	logged := middleware.Logger(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		logged.ServeHTTP(w, r)
	})
}

// mountAPI registers every route under /api. Middleware that applies to the
// whole group lives in mountSession.
func (a *API) mountAPI(r chi.Router) {
	r.Route("/api", func(r chi.Router) {
		a.mountSession(r)
		a.mountMedia(r)
		a.mountCatalog(r)
		a.mountActivity(r)
		a.mountAdmin(r)
	})
}
