package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Role says which part of the API a server process serves.
//
// ReelStash can run as one process (RoleAll: local packages, development,
// SQLite) or as two: a media server for the library app (browsing, playback,
// sign-in and per-user state) and an admin server for the admin app (admin
// routes, metadata editing, files, downloads, plugins and every background
// job). Both read and write the same database.
type Role string

const (
	RoleAll   Role = "all"
	RoleMedia Role = "media"
	RoleAdmin Role = "admin"
)

// ParseRole reads a role name; anything unknown is RoleAll.
func ParseRole(s string) Role {
	switch Role(strings.ToLower(strings.TrimSpace(s))) {
	case RoleMedia:
		return RoleMedia
	case RoleAdmin:
		return RoleAdmin
	}
	return RoleAll
}

// ServesAdmin reports whether the role mounts admin-only routes.
func (r Role) ServesAdmin() bool { return r != RoleMedia }

// WithRole picks which routes the router mounts. The default is RoleAll.
func WithRole(role Role) Option { return func(a *API) { a.Role = role } }

// adminOnly guards an admin action. On the media server it answers 404, so
// the media server has no admin surface at all: admin actions live on the
// admin server, which the admin app talks to.
func (a *API) adminOnly(next http.Handler) http.Handler {
	if a.Role.ServesAdmin() {
		return a.requireAdmin(next)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "this is an admin action; use the ReelStash admin app")
	})
}

// PluginsChanged is the event a server publishes after an admin installs,
// removes, switches, reconfigures or updates a plugin bundle. The media
// server reloads its sign-in plugins when it arrives.
const PluginsChanged = "plugins"

// announcePluginChanges publishes PluginsChanged after a successful change
// to a plugin bundle, so other server processes pick it up.
func (a *API) announcePluginChanges(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || a.Hub == nil {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if rec.status < 300 {
			a.Hub.Publish(PluginsChanged, map[string]string{"id": chi.URLParam(r, "id")}, true)
		}
	})
}
