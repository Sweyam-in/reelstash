package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"reelstash/backend/internal/auth"
	"reelstash/backend/internal/config"
)

// apiRoutes lists every registered /api route with `{param}` filled in.
func apiRoutes(t *testing.T, h http.Handler) [][2]string {
	t.Helper()
	mux, ok := h.(chi.Routes)
	if !ok {
		t.Fatalf("router is %T, not chi.Routes", h)
	}
	var routes [][2]string
	err := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api") {
			return nil
		}
		route = strings.ReplaceAll(route, "/*", "")
		for {
			i := strings.Index(route, "{")
			if i < 0 {
				break
			}
			j := strings.Index(route[i:], "}")
			route = route[:i] + "1" + route[i+j+1:]
		}
		if len(route) > 1 {
			route = strings.TrimSuffix(route, "/")
		}
		routes = append(routes, [2]string{method, route})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) < 100 {
		t.Fatalf("only %d /api routes found; the walk is broken", len(routes))
	}
	return routes
}

// Every /api route refuses a request with no session or a bogus one, before
// any handler code runs.
func TestEveryAPIRouteRequiresASession(t *testing.T) {
	h := newHarness(t)
	for _, rt := range apiRoutes(t, h.handler) {
		rt := rt
		t.Run(rt[0]+" "+rt[1], func(t *testing.T) {
			if rec := h.do(nil, rt[0], rt[1], nil); rec.Code != http.StatusUnauthorized {
				t.Errorf("anonymous: status %d, want 401", rec.Code)
			}
			bogus := actor{cookie: "not-a-session"}
			if rec := h.do(&bogus, rt[0], rt[1], nil); rec.Code != http.StatusUnauthorized {
				t.Errorf("bogus session: status %d, want 401", rec.Code)
			}
		})
	}
}

// Everything under /api/admin, plus the admin-only mutations elsewhere, is
// closed to non-admins. The admin gate runs before any handler, so these
// requests never touch data.
func TestAdminRoutesAreForbiddenToUsers(t *testing.T) {
	h := newHarness(t)
	for _, rt := range apiRoutes(t, h.handler) {
		if !strings.HasPrefix(rt[1], "/api/admin") {
			continue
		}
		rt := rt
		t.Run(rt[0]+" "+rt[1], func(t *testing.T) {
			if rec := h.do(&h.user, rt[0], rt[1], nil); rec.Code != http.StatusForbidden {
				t.Errorf("user: status %d, want 403; body %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAdminOnlyMutationsOutsideAdminPrefix(t *testing.T) {
	h := newHarness(t)
	cases := []struct{ method, path string }{
		{"POST", "/api/media"},
		{"POST", "/api/performers"},
		{"PUT", "/api/performers/1"},
		{"DELETE", "/api/performers/1"},
		{"POST", "/api/performers/1/merge"},
		{"GET", "/api/performers/duplicates"},
		{"POST", "/api/studios"},
		{"PUT", "/api/studios/1"},
		{"DELETE", "/api/studios/1"},
		{"POST", "/api/studios/1/merge"},
		{"POST", "/api/series"},
		{"PUT", "/api/series/1"},
		{"DELETE", "/api/series/1"},
		{"POST", "/api/categories"},
		{"PUT", "/api/categories/1"},
		{"DELETE", "/api/categories/1"},
		{"POST", "/api/tags"},
		{"GET", "/api/duplicates"},
		{"POST", "/api/duplicates/1/dismiss"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			if rec := h.do(&h.user, c.method, c.path, nil); rec.Code != http.StatusForbidden {
				t.Errorf("user: status %d, want 403; body %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestDisabledAccountIsRefused(t *testing.T) {
	h := newHarness(t)
	if _, err := h.pool.Exec(t.Context(), `UPDATE app_user SET disabled = true WHERE id = $1`, h.user.id); err != nil {
		t.Fatal(err)
	}
	if rec := h.do(&h.user, "GET", "/api/me", nil); rec.Code != http.StatusForbidden {
		t.Errorf("disabled user: status %d, want 403", rec.Code)
	}
}

func TestExpiredSessionIsRefused(t *testing.T) {
	h := newHarness(t)
	if _, err := h.pool.Exec(t.Context(), `UPDATE web_session SET expires_at = now() - interval '1 minute' WHERE id = $1`, h.user.cookie); err != nil {
		t.Fatal(err)
	}
	if rec := h.do(&h.user, "GET", "/api/me", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("expired session: status %d, want 401", rec.Code)
	}
}

func TestMeReportsRole(t *testing.T) {
	h := newHarness(t)
	me := h.jsonBody(h.do(&h.admin, "GET", "/api/me", nil), 200)
	if me["role"] != "ADMIN" || me["isAdmin"] != true {
		t.Errorf("admin /me = %v", me)
	}
	me = h.jsonBody(h.do(&h.user, "GET", "/api/me", nil), 200)
	if me["role"] != "USER" || me["isAdmin"] != false {
		t.Errorf("user /me = %v", me)
	}
}

func TestHealthz(t *testing.T) {
	h := newHarness(t)
	out := h.jsonBody(h.do(nil, "GET", "/healthz", nil), 200)
	if out["database"] != "ok" || (out["status"] != "ok" && out["status"] != "degraded") {
		t.Errorf("healthz = %v", out)
	}
	if _, ok := out["ffmpeg"]; !ok {
		t.Errorf("healthz missing ffmpeg: %v", out)
	}
}

func TestAuthConfigPublic(t *testing.T) {
	h := newHarnessCfg(t, cfgWith(func(c *config.Config) {
		c.EmergencyLocalAuth = true
		c.MediaURLs = []string{"https://media.example"}
		c.AdminURLs = []string{"https://admin.example"}
	}))
	req := h.do(nil, "GET", "/auth/config", nil)
	out := h.jsonBody(req, 200)
	if _, old := out["ssoEnabled"]; out["localAuthEnabled"] != true || old {
		t.Errorf("auth config = %v", out)
	}
}

func TestLocalLoginFlow(t *testing.T) {
	h := newHarnessCfg(t, cfgWith(func(c *config.Config) {
		c.EmergencyLocalAuth = true
		c.EmergencyLocalPassword = "s3cret-test"
	}))

	if rec := h.do(nil, "POST", "/auth/local-login", map[string]string{"password": "wrong"}); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong password: status %d, want 401", rec.Code)
	}
	if rec := h.do(nil, "POST", "/auth/local-login", "{not json"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad body: status %d, want 400", rec.Code)
	}

	rec := h.do(nil, "POST", "/auth/local-login", map[string]string{"password": "s3cret-test"})
	h.expect(rec, 200)
	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName {
			session = c
		}
	}
	if session == nil || session.Value == "" || !session.HttpOnly {
		t.Fatalf("expected an HttpOnly session cookie, got %v", rec.Result().Cookies())
	}
	if session.Secure {
		t.Error("cookie must not be Secure over plain HTTP")
	}

	who := actor{cookie: session.Value}
	if me := h.jsonBody(h.do(&who, "GET", "/api/me", nil), 200); me["isAdmin"] != true {
		t.Errorf("local login should yield an admin, got %v", me)
	}

	// Logout drops the server-side session, not just the cookie.
	h.expect(h.do(&who, "POST", "/auth/logout", nil), 200)
	if rec := h.do(&who, "GET", "/api/me", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("after logout: status %d, want 401", rec.Code)
	}
}

func TestLocalLoginDisabledByDefault(t *testing.T) {
	h := newHarness(t)
	rec := h.do(nil, "POST", "/auth/local-login", map[string]string{"password": ""})
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503", rec.Code)
	}
}

func TestSignInPluginRoutesAreUnavailableWithoutAPlugin(t *testing.T) {
	h := newHarness(t)
	for p, want := range map[string]int{
		"/auth/login": http.StatusServiceUnavailable, "/auth/callback": http.StatusNotFound,
		"/auth/plugin/oidc-auth/login": http.StatusNotFound, "/auth/plugin/oidc-auth/callback": http.StatusNotFound,
	} {
		if rec := h.do(nil, "GET", p, nil); rec.Code != want {
			t.Errorf("%s: status %d, want %d", p, rec.Code, want)
		}
	}
}

func cfgWith(f func(*config.Config)) config.Config {
	var c config.Config
	f(&c)
	return c
}

// Media sub-routes check title access first, so the admin gate is only
// reachable for a title the user may open.
func TestAdminOnlyMediaRoutesForAccessibleTitle(t *testing.T) {
	f := newLibraryFixture(t)
	id := itoa(f.movieItem)
	for _, c := range []struct{ method, path string }{
		{"PUT", "/api/media/" + id + "/metadata"},
		{"POST", "/api/media/" + id + "/poster"},
		{"POST", "/api/media/" + id + "/poster/frame"},
		{"DELETE", "/api/media/" + id + "/poster"},
		{"POST", "/api/media/" + id + "/backdrop"},
		{"POST", "/api/media/" + id + "/backdrop/frame"},
		{"DELETE", "/api/media/" + id + "/backdrop"},
		{"GET", "/api/media/" + id + "/match"},
		{"POST", "/api/media/" + id + "/match"},
		{"POST", "/api/media/" + id + "/match/unmatch"},
		{"POST", "/api/media/" + id + "/links"},
		{"PUT", "/api/media/" + id + "/series"},
	} {
		if rec := f.do(t, f.userCookie, c.method, c.path, "{}"); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as user: status %d, want 403", c.method, c.path, rec.Code)
		}
	}
	// The same title is readable by that user.
	if rec := f.get(t, f.userCookie, "/api/media/"+id); rec.Code != http.StatusOK {
		t.Errorf("user reading own-library title: status %d", rec.Code)
	}
}
