package httpapi

import (
	"net/http"
	"testing"
)

func TestParseRole(t *testing.T) {
	for in, want := range map[string]Role{"": RoleAll, "all": RoleAll, "Media": RoleMedia, " admin ": RoleAdmin, "other": RoleAll} {
		if got := ParseRole(in); got != want {
			t.Errorf("ParseRole(%q) = %q, want %q", in, got, want)
		}
	}
}

// The media server serves browsing and per-user state but no admin action,
// even to an admin: those live on the admin server.
func TestMediaRoleHasNoAdminSurface(t *testing.T) {
	h := newHarness(t)
	h.handler = NewRouter(h.pool, h.cfg, nil, nil, nil, nil, nil, WithPlugins(nil, h.authPlugins), WithRole(RoleMedia))
	id := h.newTitle("Media role title")

	h.expect(h.do(&h.user, http.MethodGet, "/api/media", nil), http.StatusOK)
	h.expect(h.do(&h.user, http.MethodGet, "/api/performers", nil), http.StatusOK)
	h.expect(h.do(&h.user, http.MethodGet, "/api/me", nil), http.StatusOK)

	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/admin/overview"},
		{http.MethodGet, "/api/duplicates"},
		{http.MethodGet, "/api/downloads"},
		{http.MethodGet, "/api/files"},
		{http.MethodPost, "/api/performers"},
		{http.MethodPut, "/api/media/" + itoa(id) + "/metadata"},
		{http.MethodDelete, "/api/media/" + itoa(id) + "/poster"},
	} {
		if rec := h.do(&h.admin, c.method, c.path, map[string]any{}); rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s on the media server = %d, want 404 or 405", c.method, c.path, rec.Code)
		}
	}
}

// The admin server keeps the admin routes and the reads the admin app uses.
func TestAdminRoleServesAdminRoutes(t *testing.T) {
	h := newHarness(t)
	h.handler = NewRouter(h.pool, h.cfg, nil, nil, nil, nil, nil, WithPlugins(nil, h.authPlugins), WithRole(RoleAdmin))
	h.expect(h.do(&h.admin, http.MethodGet, "/api/admin/overview", nil), http.StatusOK)
	h.expect(h.do(&h.user, http.MethodGet, "/api/admin/overview", nil), http.StatusForbidden)
	h.expect(h.do(&h.admin, http.MethodGet, "/api/media", nil), http.StatusOK)
}
