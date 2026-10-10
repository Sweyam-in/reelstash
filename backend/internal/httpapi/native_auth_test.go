package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reelstash/backend/internal/auth"
	"reelstash/backend/internal/authplugin"
	"reelstash/backend/internal/config"
	"reelstash/backend/internal/testdb"
)

// freshRig is the router over an empty private schema: no users at all, like a
// brand-new install.
func freshRig(t *testing.T, cfg config.Config, opts ...Option) *harness {
	t.Helper()
	pool := testdb.Isolated(t)
	cfg.PosterDir = t.TempDir()
	h := &harness{t: t, pool: pool, cfg: cfg}
	h.authPlugins = authplugin.NewRegistry()
	h.handler = NewRouter(pool, cfg, nil, nil, nil, nil, nil, append([]Option{WithPlugins(nil, h.authPlugins)}, opts...)...)
	return h
}

func (h *harness) cookieActor(rec interface{ Result() *http.Response }) *actor {
	h.t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return &actor{cookie: c.Value}
		}
	}
	h.t.Fatal("the response set no session cookie")
	return nil
}

func TestFreshInstallOffersToCreateTheFirstAdmin(t *testing.T) {
	h := freshRig(t, config.Config{})
	cfg := h.jsonBody(h.do(nil, "GET", "/auth/config", nil), 200)
	if cfg["needsSetup"] != true || cfg["nativeEnabled"] != false {
		t.Fatalf("fresh config = %v", cfg)
	}

	for name, body := range map[string]map[string]string{
		"short name":     {"username": "ab", "password": "long-enough-pw"},
		"odd characters": {"username": "bad name!", "password": "long-enough-pw"},
		"short password": {"username": "owner", "password": "short"},
	} {
		if rec := h.do(nil, "POST", "/auth/setup", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, rec.Code)
		}
	}

	rec := h.do(nil, "POST", "/auth/setup", map[string]string{"username": "Owner", "password": "long-enough-pw", "displayName": "The Owner"})
	h.expect(rec, http.StatusCreated)
	me := h.jsonBody(h.do(h.cookieActor(rec), "GET", "/api/me", nil), 200)
	if me["role"] != "ADMIN" || me["displayName"] != "The Owner" || me["canChangePassword"] != true {
		t.Errorf("me = %v", me)
	}

	// Setup closes for good once an admin exists.
	h.expect(h.do(nil, "POST", "/auth/setup", map[string]string{"username": "second", "password": "long-enough-pw"}), http.StatusConflict)
	cfg = h.jsonBody(h.do(nil, "GET", "/auth/config", nil), 200)
	if cfg["needsSetup"] != false || cfg["nativeEnabled"] != true {
		t.Errorf("config after setup = %v", cfg)
	}
}

func TestSetupIsClosedWhenAnotherSignInExists(t *testing.T) {
	h := freshRig(t, config.Config{EmergencyLocalAuth: true, EmergencyLocalPassword: "x"})
	if cfg := h.jsonBody(h.do(nil, "GET", "/auth/config", nil), 200); cfg["needsSetup"] != false {
		t.Errorf("config = %v", cfg)
	}
	h.expect(h.do(nil, "POST", "/auth/setup", map[string]string{"username": "owner", "password": "long-enough-pw"}), http.StatusConflict)
	var n int
	h.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM app_user`).Scan(&n)
	if n != 0 {
		t.Errorf("a refused setup still created %d user(s)", n)
	}
}

func TestOnlyOneConcurrentSetupWins(t *testing.T) {
	h := freshRig(t, config.Config{})
	codes := make(chan int, 6)
	for i := 0; i < cap(codes); i++ {
		go func(i int) {
			rec := h.do(nil, "POST", "/auth/setup", map[string]string{"username": "owner" + string(rune('a'+i)), "password": "long-enough-pw"})
			codes <- rec.Code
		}(i)
	}
	created := 0
	for i := 0; i < cap(codes); i++ {
		if <-codes == http.StatusCreated {
			created++
		}
	}
	var admins int
	h.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM app_user WHERE role = 'ADMIN'`).Scan(&admins)
	if created != 1 || admins != 1 {
		t.Errorf("%d setups succeeded and %d admins exist, want 1 and 1", created, admins)
	}
}

func TestNativeLogin(t *testing.T) {
	h := freshRig(t, config.Config{})
	ctx := context.Background()
	if _, err := auth.CreateNativeUser(ctx, h.pool, "Alice", "correct horse", "Alice A", "USER"); err != nil {
		t.Fatal(err)
	}
	login := func(user, pw string) int {
		return h.do(nil, "POST", "/auth/native-login", map[string]string{"username": user, "password": pw}).Code
	}

	rec := h.do(nil, "POST", "/auth/native-login", map[string]string{"username": "alice", "password": "correct horse"}) // case-insensitive
	h.expect(rec, 200)
	c := rec.Result().Cookies()[0]
	if !c.HttpOnly || c.Value == "" {
		t.Errorf("cookie = %+v", c)
	}
	if me := h.jsonBody(h.do(&actor{cookie: c.Value}, "GET", "/api/me", nil), 200); me["role"] != "USER" || me["displayName"] != "Alice A" {
		t.Errorf("me = %v", me)
	}

	if login("alice", "wrong") != 401 || login("nobody", "correct horse") != 401 {
		t.Error("a wrong password and an unknown user must both be 401")
	}
	body := h.do(nil, "POST", "/auth/native-login", map[string]string{"username": "nobody", "password": "x"}).Body.String()
	body2 := h.do(nil, "POST", "/auth/native-login", map[string]string{"username": "alice", "password": "x"}).Body.String()
	if body != body2 {
		t.Errorf("the two failures must look the same: %q vs %q", body, body2)
	}

	h.pool.Exec(ctx, `UPDATE app_user SET disabled = true WHERE lower(username) = 'alice'`)
	if login("alice", "correct horse") != 403 {
		t.Error("a disabled account must be refused")
	}
}

func TestNativeLoginIsThrottled(t *testing.T) {
	h := freshRig(t, config.Config{})
	auth.CreateNativeUser(context.Background(), h.pool, "victim", "correct horse", "", "USER")
	login := func(pw string) int {
		return h.do(nil, "POST", "/auth/native-login", map[string]string{"username": "victim", "password": pw}).Code
	}
	for i := 0; i < limitPerPair; i++ {
		if got := login("guess"); got != 401 {
			t.Fatalf("attempt %d: %d, want 401", i, got)
		}
	}
	rec := h.do(nil, "POST", "/auth/native-login", map[string]string{"username": "victim", "password": "correct horse"})
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("after too many failures: %d %v", rec.Code, rec.Header())
	}
}

func TestAdminManagesPasswordAccounts(t *testing.T) {
	h := newIsolatedHarness(t, config.Config{}, nil)
	body := map[string]string{"username": "kid", "password": "kids-password", "role": "USER", "displayName": "Kid"}
	id := h.createdID(&h.admin, "/api/admin/users", body)
	h.expect(h.do(&h.admin, "POST", "/api/admin/users", body), http.StatusConflict)
	for name, bad := range map[string]map[string]string{
		"role missing": {"username": "x-user", "password": "long-enough-pw"},
		"weak":         {"username": "x-user", "password": "123", "role": "USER"},
		"bad name":     {"username": "a b", "password": "long-enough-pw", "role": "USER"},
	} {
		if rec := h.do(&h.admin, "POST", "/api/admin/users", bad); rec.Code != 400 {
			t.Errorf("%s: %d, want 400", name, rec.Code)
		}
	}
	h.expect(h.do(&h.user, "POST", "/api/admin/users", body), http.StatusForbidden)

	rec := h.do(nil, "POST", "/auth/native-login", map[string]string{"username": "kid", "password": "kids-password"})
	h.expect(rec, 200)
	kid := h.cookieActor(rec)

	// The listing tells password accounts apart.
	var found map[string]any
	for _, u := range h.items(h.do(&h.admin, "GET", "/api/admin/users", nil)) {
		if u["username"] == "kid" {
			found = u
		}
	}
	if found == nil || found["passwordLogin"] != true || found["role"] != "USER" {
		t.Errorf("listing = %v", found)
	}
	if strings.Contains(h.do(&h.admin, "GET", "/api/admin/users", nil).Body.String(), "pbkdf2") {
		t.Error("a password hash leaked into the listing")
	}

	// Resetting the password signs that account out everywhere.
	h.expect(h.do(&h.admin, "PUT", "/api/admin/users/"+itoa(id)+"/password", map[string]string{"password": "brand-new-pass"}), 200)
	h.expect(h.do(kid, "GET", "/api/me", nil), http.StatusUnauthorized)
	h.expect(h.do(nil, "POST", "/auth/native-login", map[string]string{"username": "kid", "password": "kids-password"}), http.StatusUnauthorized)
	h.expect(h.do(nil, "POST", "/auth/native-login", map[string]string{"username": "kid", "password": "brand-new-pass"}), 200)
	h.expect(h.do(&h.admin, "PUT", "/api/admin/users/"+itoa(id)+"/password", map[string]string{"password": "short"}), 400)
	// Accounts that sign in elsewhere have no ReelStash password to reset.
	h.expect(h.do(&h.admin, "PUT", "/api/admin/users/"+itoa(h.other.id)+"/password", map[string]string{"password": "long-enough-pw"}), 400)
}

func TestChangeOwnPassword(t *testing.T) {
	h := freshRig(t, config.Config{})
	auth.CreateNativeUser(context.Background(), h.pool, "pat", "first-password", "", "USER")
	signIn := func(pw string) *actor {
		rec := h.do(nil, "POST", "/auth/native-login", map[string]string{"username": "pat", "password": pw})
		if rec.Code != 200 {
			return nil
		}
		return h.cookieActor(rec)
	}
	here, elsewhere := signIn("first-password"), signIn("first-password")

	h.expect(h.do(here, "PUT", "/api/me/password", map[string]string{"current": "nope", "password": "second-password"}), http.StatusForbidden)
	h.expect(h.do(here, "PUT", "/api/me/password", map[string]string{"current": "first-password", "password": "short"}), 400)
	h.expect(h.do(here, "PUT", "/api/me/password", map[string]string{"current": "first-password", "password": "second-password"}), 200)

	h.expect(h.do(here, "GET", "/api/me", nil), 200)                          // this session stays
	h.expect(h.do(elsewhere, "GET", "/api/me", nil), http.StatusUnauthorized) // the others end
	if signIn("first-password") != nil || signIn("second-password") == nil {
		t.Error("the old password must stop working and the new one must work")
	}
}

func TestPasswordChangeIsRefusedForAccountsWithoutOne(t *testing.T) {
	h := newHarness(t)
	h.expect(h.do(&h.user, "PUT", "/api/me/password", map[string]string{"current": "x", "password": "long-enough-pw"}), 400)
	if me := h.jsonBody(h.do(&h.user, "GET", "/api/me", nil), 200); me["canChangePassword"] != false {
		t.Errorf("me = %v", me)
	}
}

func TestPasswordSignInCanBeTurnedOffOnlyWhenThereIsAnotherWay(t *testing.T) {
	h := newIsolatedHarness(t, config.Config{}, nil)
	auth.CreateNativeUser(context.Background(), h.pool, "pat", "first-password", "", "USER")
	// Password sign-in is the only way in: refuse to lock everyone out.
	h.expect(h.do(&h.admin, "PUT", "/api/admin/auth", map[string]bool{"nativeEnabled": false}), http.StatusConflict)

	// Once a plugin offers another way, it can be turned off.
	h.AuthPluginsFor(t).Register(authplugin.New(authplugin.Manifest{ID: "sso", Name: "SSO", Login: authplugin.Login{Type: authplugin.LoginRedirect}}, "http://x", ""))
	h.expect(h.do(&h.admin, "PUT", "/api/admin/auth", map[string]bool{"nativeEnabled": false}), 200)
	h.expect(h.do(nil, "POST", "/auth/native-login", map[string]string{"username": "pat", "password": "first-password"}), http.StatusForbidden)
	cfg := h.jsonBody(h.do(nil, "GET", "/auth/config", nil), 200)
	if cfg["nativeEnabled"] != false {
		t.Errorf("config = %v", cfg)
	}
	settings := h.jsonBody(h.do(&h.admin, "GET", "/api/admin/auth", nil), 200)
	if settings["nativeEnabled"] != false || len(settings["plugins"].([]any)) != 1 {
		t.Errorf("settings = %v", settings)
	}
	h.expect(h.do(&h.admin, "PUT", "/api/admin/auth", map[string]bool{"nativeEnabled": true}), 200)
}

func TestAdminFromTheEnvironment(t *testing.T) {
	h := freshRig(t, config.Config{})
	ctx := context.Background()
	if created, err := auth.EnsureAdmin(ctx, h.pool, "root", "long-enough-pw"); err != nil || !created {
		t.Fatalf("first: %v %v", created, err)
	}
	// The password changed in the app is not overwritten on the next start.
	var id int64
	h.pool.QueryRow(ctx, `SELECT id FROM app_user WHERE username = 'root'`).Scan(&id)
	if err := auth.SetPassword(ctx, h.pool, id, "changed-in-app", ""); err != nil {
		t.Fatal(err)
	}
	if created, err := auth.EnsureAdmin(ctx, h.pool, "root", "long-enough-pw"); err != nil || created {
		t.Errorf("second: %v %v", created, err)
	}
	h.expect(h.do(nil, "POST", "/auth/native-login", map[string]string{"username": "root", "password": "changed-in-app"}), 200)
	if cfg := h.jsonBody(h.do(nil, "GET", "/auth/config", nil), 200); cfg["needsSetup"] != false {
		t.Errorf("an admin from the environment closes setup: %v", cfg)
	}
}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	return nil
}
