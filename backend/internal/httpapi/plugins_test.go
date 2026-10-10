package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"reelstash/backend/internal/auth"
	"reelstash/backend/internal/authplugin"
	"reelstash/backend/internal/config"
	"reelstash/backend/internal/extprovider"
	"reelstash/backend/internal/namer"
	"reelstash/backend/internal/plugins"
	"reelstash/backend/internal/provider"
	"reelstash/backend/internal/testdb"
)

// --- sign-in through an auth plugin ---

// fakeAuthService is an auth plugin: it "authenticates" whoever presents the
// code "ok-<name>".
func fakeAuthService(t *testing.T, loginType string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/manifest", func(w http.ResponseWriter, r *http.Request) {
		write(w, authplugin.Manifest{ID: "fake", Name: "Fake SSO", Login: authplugin.Login{Type: loginType}})
	})
	mux.HandleFunc("/auth/start", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		write(w, map[string]string{"url": "https://idp.test/go?cb=" + url.QueryEscape(b["callbackUrl"]) + "&to=" + url.QueryEscape(b["redirectTo"])})
	})
	mux.HandleFunc("/auth/finish", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Query map[string]string }
		json.NewDecoder(r.Body).Decode(&b)
		code := b.Query["code"]
		switch {
		case strings.HasPrefix(code, "ok-"):
			who := strings.TrimPrefix(code, "ok-")
			role := "USER"
			if who == "boss" {
				role = "ADMIN"
			}
			write(w, authplugin.Result{Identity: &authplugin.Identity{Subject: who, Email: who + "@x.test", DisplayName: strings.ToUpper(who), Role: role}, RedirectTo: b.Query["to"]})
		case code == "denied":
			write(w, authplugin.Result{Error: "not_authorized"})
		case code == "weird":
			write(w, authplugin.Result{Error: "something-else"})
		default:
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		if b["password"] == "pw" {
			write(w, authplugin.Result{Identity: &authplugin.Identity{Subject: b["username"]}})
			return
		}
		write(w, authplugin.Result{Error: "login_failed"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func registerFake(t *testing.T, h *harness, loginType string) {
	t.Helper()
	p, err := authplugin.Discover(context.Background(), fakeAuthService(t, loginType).URL, "")
	if err != nil {
		t.Fatal(err)
	}
	h.AuthPluginsFor(t).Register(p)
}

func TestAuthPluginRedirectSignIn(t *testing.T) {
	h := freshRig(t, config.Config{})
	registerFake(t, h, authplugin.LoginRedirect)

	cfg := h.jsonBody(h.do(nil, "GET", "/auth/config", nil), 200)
	plugs := cfg["plugins"].([]any)
	if len(plugs) != 1 || plugs[0].(map[string]any)["id"] != "fake" || plugs[0].(map[string]any)["label"] != "Continue with Fake SSO" {
		t.Fatalf("config plugins = %v", cfg["plugins"])
	}
	// With a sign-in plugin in place, nobody can claim the first-run admin.
	if cfg["needsSetup"] != false {
		t.Error("setup must be closed when a sign-in plugin exists")
	}

	// Starting sends the browser to the plugin's page, with ReelStash's own
	// callback for this plugin on the host the visitor used.
	rec := h.do(nil, "GET", "/auth/plugin/fake/login?redirect_to=%2Fmovies", nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if loc.Host != "idp.test" || loc.Query().Get("cb") != "http://example.com/auth/plugin/fake/callback" || loc.Query().Get("to") != "/movies" {
		t.Errorf("redirect = %s", loc)
	}
	req := httptest.NewRequest("GET", "/auth/plugin/fake/login", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if l, _ := url.Parse(w.Header().Get("Location")); l.Query().Get("cb") != "https://example.com/auth/plugin/fake/callback" {
		t.Errorf("behind an HTTPS proxy the callback must be https: %s", l)
	}

	// Coming back signs the person in: a user is created under the plugin's
	// namespace, a session starts, and they land on the page they came from.
	rec = h.do(nil, "GET", "/auth/plugin/fake/callback?code=ok-pat&to=%2Fmovies%3Fx%3D1", nil)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/movies?x=1" {
		t.Fatalf("callback: %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	me := h.jsonBody(h.do(h.cookieActor(rec), "GET", "/api/me", nil), 200)
	if me["role"] != "USER" || me["email"] != "pat@x.test" || me["displayName"] != "PAT" || me["canChangePassword"] != false {
		t.Errorf("me = %v", me)
	}
	var subject string
	h.pool.QueryRow(context.Background(), `SELECT auth_subject FROM app_user WHERE email = 'pat@x.test'`).Scan(&subject)
	if subject != "fake:pat" {
		t.Errorf("stored subject = %q, want it namespaced by the plugin", subject)
	}
	// The same person signing in again is the same user.
	h.do(nil, "GET", "/auth/plugin/fake/callback?code=ok-pat", nil)
	var n int
	h.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM app_user WHERE email = 'pat@x.test'`).Scan(&n)
	if n != 1 {
		t.Errorf("signing in twice made %d users", n)
	}

	// A plugin can say who is an admin, and a later USER answer never demotes them.
	rec = h.do(nil, "GET", "/auth/plugin/fake/callback?code=ok-boss", nil)
	if me := h.jsonBody(h.do(h.cookieActor(rec), "GET", "/api/me", nil), 200); me["role"] != "ADMIN" {
		t.Errorf("boss = %v", me)
	}
	h.pool.Exec(context.Background(), `UPDATE app_user SET role = 'ADMIN' WHERE email = 'pat@x.test'`)
	rec = h.do(nil, "GET", "/auth/plugin/fake/callback?code=ok-pat", nil)
	if me := h.jsonBody(h.do(h.cookieActor(rec), "GET", "/api/me", nil), 200); me["role"] != "ADMIN" {
		t.Errorf("a plugin must not demote an admin: %v", me)
	}
}

func TestAuthPluginFailuresNeverMakeASession(t *testing.T) {
	h := freshRig(t, config.Config{})
	registerFake(t, h, authplugin.LoginRedirect)
	for query, want := range map[string]string{
		"code=denied":  "/?auth_error=not_authorized",
		"code=weird":   "/?auth_error=login_failed", // unknown words become login_failed
		"code=garbage": "/?auth_error=login_failed", // the plugin itself errored
	} {
		rec := h.do(nil, "GET", "/auth/plugin/fake/callback?"+query, nil)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != want || sessionCookie(rec) != nil {
			t.Errorf("%s: %d -> %q cookie=%v", query, rec.Code, rec.Header().Get("Location"), sessionCookie(rec))
		}
	}

	h.pool.Exec(context.Background(), `INSERT INTO app_user (auth_subject, role, disabled) VALUES ('fake:off', 'USER', true)`)
	rec := h.do(nil, "GET", "/auth/plugin/fake/callback?code=ok-off", nil)
	if rec.Header().Get("Location") != "/?auth_error=account_disabled" || sessionCookie(rec) != nil {
		t.Errorf("disabled: %q", rec.Header().Get("Location"))
	}

	h.expect(h.do(nil, "GET", "/auth/plugin/missing/login", nil), http.StatusNotFound)
	h.expect(h.do(nil, "GET", "/auth/plugin/missing/callback", nil), http.StatusNotFound)
}

func TestSignInRedirectsStayOnThisSite(t *testing.T) {
	h := freshRig(t, config.Config{})
	registerFake(t, h, authplugin.LoginRedirect)
	for _, evil := range []string{"https://evil.test/", "//evil.test", "/\\evil.test", "javascript:alert(1)", ""} {
		rec := h.do(nil, "GET", "/auth/plugin/fake/login?redirect_to="+url.QueryEscape(evil), nil)
		loc, _ := url.Parse(rec.Header().Get("Location"))
		if loc.Query().Get("to") != "/" {
			t.Errorf("redirect_to=%q reached the plugin as %q", evil, loc.Query().Get("to"))
		}
		// A plugin that echoes a bad address back is not trusted either.
		rec = h.do(nil, "GET", "/auth/plugin/fake/callback?code=ok-x&to="+url.QueryEscape(evil), nil)
		if got := rec.Header().Get("Location"); got != "/" {
			t.Errorf("callback to=%q redirected to %q", evil, got)
		}
	}
}

func TestAuthPluginPasswordSignIn(t *testing.T) {
	h := freshRig(t, config.Config{})
	registerFake(t, h, authplugin.LoginPassword)
	rec := h.do(nil, "POST", "/auth/plugin/fake/login", map[string]string{"username": "lee", "password": "pw"})
	h.expect(rec, 200)
	h.expect(h.do(h.cookieActor(rec), "GET", "/api/me", nil), 200)
	h.expect(h.do(nil, "POST", "/auth/plugin/fake/login", map[string]string{"username": "lee", "password": "no"}), http.StatusUnauthorized)
	// A password plugin has no redirect flow.
	if rec := h.do(nil, "GET", "/auth/plugin/fake/login", nil); rec.Header().Get("Location") != "/?auth_error=login_failed" {
		t.Errorf("redirect on a password plugin: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	for i := 0; i < limitPerPair; i++ {
		h.do(nil, "POST", "/auth/plugin/fake/login", map[string]string{"username": "victim", "password": "x"})
	}
	h.expect(h.do(nil, "POST", "/auth/plugin/fake/login", map[string]string{"username": "victim", "password": "pw"}), http.StatusTooManyRequests)
}

func TestSessionsFromEveryMethodAreTheSame(t *testing.T) {
	h := freshRig(t, config.Config{})
	registerFake(t, h, authplugin.LoginRedirect)
	rec := h.do(nil, "GET", "/auth/plugin/fake/callback?code=ok-sam", nil)
	sam := h.cookieActor(rec)
	h.expect(h.do(sam, "POST", "/auth/logout", nil), 200)
	h.expect(h.do(sam, "GET", "/api/me", nil), http.StatusUnauthorized)
	if _, err := auth.LoadSessionUser(context.Background(), h.pool, sam.cookie); err == nil {
		t.Error("logout must delete the session")
	}
}

// --- Admin → Plugins ---

type bundleRig struct {
	*harness
	mgr *plugins.Manager
}

func newBundleRig(t *testing.T, enabled bool) bundleRig {
	t.Helper()
	pool := testdb.Isolated(t)
	reg := provider.NewRegistry()
	authReg := authplugin.NewRegistry()
	mgr := plugins.NewManager(plugins.Options{Root: t.TempDir(), Enabled: enabled, Store: plugins.DBStore{DB: pool}, Providers: reg, Auth: authReg})
	h := &harness{t: t, pool: pool, authPlugins: authReg}
	cfg := config.Config{PosterDir: t.TempDir()}
	engine := namer.New(pool, reg, nil, "", t.TempDir())
	h.handler = NewRouter(pool, cfg, nil, nil, engine, nil, nil, WithPlugins(mgr, authReg))
	h.admin = h.newActor("ADMIN")
	h.user = h.newActor("USER")
	return bundleRig{h, mgr}
}

func (h *harness) upload(who *actor, path, field string, data []byte) *httptest.ResponseRecorder {
	h.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile(field, "bundle.zip")
	fw.Write(data)
	mw.Close()
	req := httptest.NewRequest("POST", path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: who.cookie})
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func zipBundle(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(content))
	}
	zw.Close()
	return buf.Bytes()
}

func TestPluginsAdminLifecycle(t *testing.T) {
	r := newBundleRig(t, true)
	listing := r.jsonBody(r.do(&r.admin, "GET", "/api/admin/plugins", nil), 200)
	if listing["enabled"] != true || len(listing["items"].([]any)) != 0 {
		t.Fatalf("empty listing = %v", listing)
	}
	if _, offered := listing["catalog"]; offered {
		t.Error("ReelStash ships no plugins of its own; there is nothing to offer")
	}

	// Install an auth bundle by upload.
	bundle := zipBundle(t, map[string]string{
		"plugin.json": `{"id":"sso-test","name":"SSO test","version":"1.0.0","kind":"auth","run":{"command":["python3","server.py"]},
			"config":[{"key":"issuer","label":"Issuer","required":true},{"key":"clientId","label":"Client","required":true},{"key":"clientSecret","label":"Secret","secret":true}]}`,
		"server.py": "",
	})
	item := r.jsonBody(r.upload(&r.admin, "/api/admin/plugins/install", "bundle", bundle), http.StatusCreated)
	if item["id"] != "sso-test" || item["kind"] != "auth" || item["enabled"] != true {
		t.Fatalf("installed = %v", item)
	}
	if missing := item["missing"].([]any); len(missing) != 2 {
		t.Errorf("the bundle needs the issuer and client id: %v", item["missing"])
	}
	if cb := item["callbackUrl"]; cb != "http://example.com/auth/plugin/sso-test/callback" {
		t.Errorf("callbackUrl = %v (register this with the identity provider)", cb)
	}

	// Settings are saved, secrets are masked, unknown keys are refused.
	item = r.jsonBody(r.do(&r.admin, "PUT", "/api/admin/plugins/sso-test/config", map[string]string{
		"issuer": "https://auth.example.test", "clientId": "reelstash", "clientSecret": "s3cr3t-value-9876",
	}), 200)
	if len(item["missing"].([]any)) != 0 {
		t.Errorf("missing after saving = %v", item["missing"])
	}
	raw := r.do(&r.admin, "GET", "/api/admin/plugins", nil).Body.String()
	if strings.Contains(raw, "s3cr3t-value-9876") || !strings.Contains(raw, "****9876") {
		t.Errorf("the secret must be masked in the listing: %s", raw)
	}
	r.expect(r.do(&r.admin, "PUT", "/api/admin/plugins/sso-test/config", map[string]string{"nope": "x"}), http.StatusBadRequest)

	// Off and on, restart, and the process log.
	item = r.jsonBody(r.do(&r.admin, "PUT", "/api/admin/plugins/sso-test/enabled", map[string]bool{"enabled": false}), 200)
	if item["enabled"] != false || item["status"] != "stopped" {
		t.Errorf("after off = %v", item)
	}
	r.expect(r.do(&r.admin, "POST", "/api/admin/plugins/sso-test/restart", nil), 200)
	r.expect(r.do(&r.admin, "GET", "/api/admin/plugins/sso-test/log", nil), 200)
	r.expect(r.do(&r.admin, "GET", "/api/admin/plugins/missing/log", nil), http.StatusNotFound)
	r.expect(r.do(&r.admin, "PUT", "/api/admin/plugins/missing/enabled", map[string]bool{"enabled": true}), http.StatusNotFound)

	// Uninstalling removes it everywhere.
	r.expect(r.do(&r.admin, "DELETE", "/api/admin/plugins/sso-test", nil), 200)
	r.expect(r.do(&r.admin, "DELETE", "/api/admin/plugins/sso-test", nil), http.StatusNotFound)
	if got := r.items(r.do(&r.admin, "GET", "/api/admin/plugins", nil)); len(got) != 0 {
		t.Errorf("still installed: %v", got)
	}
}

func TestPluginsUploadAndRefusals(t *testing.T) {
	r := newBundleRig(t, true)
	good := zipBundle(t, map[string]string{
		"my-plugin/plugin.json": `{"id":"my-plugin","name":"Mine","version":"0.1","kind":"provider","run":{"command":["./run.sh"]}}`,
		"my-plugin/run.sh":      "#!/bin/sh\n",
	})
	item := r.jsonBody(r.upload(&r.admin, "/api/admin/plugins/install", "bundle", good), http.StatusCreated)
	if item["id"] != "my-plugin" || item["providerId"] != "x-my-plugin" {
		t.Errorf("uploaded = %v", item)
	}

	if rec := r.upload(&r.admin, "/api/admin/plugins/install", "bundle", []byte("not a zip")); rec.Code != http.StatusBadRequest {
		t.Errorf("garbage: %d", rec.Code)
	}
	if rec := r.upload(&r.admin, "/api/admin/plugins/install", "other-field", good); rec.Code != http.StatusBadRequest {
		t.Errorf("wrong field: %d", rec.Code)
	}
	if rec := r.upload(&r.user, "/api/admin/plugins/install", "bundle", good); rec.Code != http.StatusForbidden {
		t.Errorf("a regular user must not install code: %d", rec.Code)
	}
	for _, bad := range []string{"", "ftp://x/y.zip", "file:///etc/passwd"} {
		r.expect(r.do(&r.admin, "POST", "/api/admin/plugins/install-url", map[string]string{"url": bad}), http.StatusBadRequest)
	}

	// Removing a provider bundle also drops it from libraries.
	var libID int64
	r.pool.QueryRow(context.Background(), `INSERT INTO library (name, mode) VALUES ('L', 'private') RETURNING id`).Scan(&libID)
	r.pool.Exec(context.Background(), `INSERT INTO library_provider (library_id, provider, position) VALUES ($1, 'x-my-plugin', 0)`, libID)
	r.expect(r.do(&r.admin, "DELETE", "/api/admin/plugins/my-plugin", nil), 200)
	var n int
	r.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM library_provider WHERE provider = 'x-my-plugin'`).Scan(&n)
	if n != 0 {
		t.Error("libraries still list the uninstalled provider")
	}
}

func TestPluginInstallInstallDownloadsFromAnAddress(t *testing.T) {
	r := newBundleRig(t, true)
	data := zipBundle(t, map[string]string{
		"plugin.json": `{"id":"dl","version":"1","kind":"auth","run":{"command":["python3","x.py"]}}`, "x.py": "",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/dl.zip" {
			http.NotFound(w, req)
			return
		}
		w.Write(data)
	}))
	defer srv.Close()
	item := r.jsonBody(r.do(&r.admin, "POST", "/api/admin/plugins/install-url", map[string]string{"url": srv.URL + "/dl.zip"}), http.StatusCreated)
	if item["id"] != "dl" {
		t.Errorf("installed = %v", item)
	}
	r.expect(r.do(&r.admin, "POST", "/api/admin/plugins/install-url", map[string]string{"url": srv.URL + "/missing.zip"}), http.StatusBadGateway)
}

func TestPluginBundlesCanBeSwitchedOffForTheServer(t *testing.T) {
	r := newBundleRig(t, false)
	if listing := r.jsonBody(r.do(&r.admin, "GET", "/api/admin/plugins", nil), 200); listing["enabled"] != false {
		t.Errorf("listing = %v", listing)
	}
	bundle := zipBundle(t, map[string]string{"plugin.json": `{"id":"abc","version":"1","kind":"auth","run":{"command":["x"]}}`})
	if rec := r.upload(&r.admin, "/api/admin/plugins/install", "bundle", bundle); rec.Code != http.StatusForbidden {
		t.Errorf("install with bundles off: %d", rec.Code)
	}
}

func TestPluginsAreUnavailableWithoutAManager(t *testing.T) {
	h := newHarness(t)
	if listing := h.jsonBody(h.do(&h.admin, "GET", "/api/admin/plugins", nil), 200); listing["enabled"] != false {
		t.Errorf("listing = %v", listing)
	}
	h.expect(h.do(&h.admin, "POST", "/api/admin/plugins/install-url", map[string]string{"url": "https://x.test/p.zip"}), http.StatusServiceUnavailable)
}

// --- the provider on/off switch and request log ---

func TestExternalProviderSwitchAndRequestLog(t *testing.T) {
	h, reg := externalRig(t)
	srv := pluginServer(t, "switchy", true)
	h.expect(h.do(&h.admin, "POST", "/api/admin/providers/external", map[string]string{"baseUrl": srv.URL}), http.StatusCreated)
	extprovider.ClearCalls("x-switchy")
	t.Cleanup(func() { extprovider.ClearCalls("x-switchy") })

	// Requests are logged; the newest comes first.
	h.jsonBody(h.do(&h.admin, "POST", "/api/admin/providers/x-switchy/test", nil), 200)
	h.jsonBody(h.do(&h.admin, "POST", "/api/admin/providers/x-switchy/test", nil), 200)
	calls := h.items(h.do(&h.admin, "GET", "/api/admin/providers/external/x-switchy/log", nil))
	if len(calls) < 2 || calls[0]["path"] != "/manifest" || calls[0]["status"] != float64(200) || calls[0]["method"] != "GET" {
		t.Fatalf("calls = %v", calls)
	}
	h.expect(h.do(&h.admin, "DELETE", "/api/admin/providers/external/x-switchy/log", nil), 200)
	if calls := h.items(h.do(&h.admin, "GET", "/api/admin/providers/external/x-switchy/log", nil)); len(calls) != 0 {
		t.Errorf("the log was not cleared: %v", calls)
	}

	// Switched off: still connected and listed, but out of matching.
	h.expect(h.do(&h.admin, "PUT", "/api/admin/providers/external/x-switchy/enabled", map[string]bool{"enabled": false}), 200)
	if len(reg.Enabled(provider.KindMedia)) != 0 {
		t.Error("a switched-off plugin is still used for matching")
	}
	var entry map[string]any
	for _, it := range h.items(h.do(&h.admin, "GET", "/api/admin/providers", nil)) {
		if it["id"] == "x-switchy" {
			entry = it
		}
	}
	if entry == nil || entry["enabled"] != false || entry["switchedOff"] != true || entry["external"] != true {
		t.Errorf("listing = %v", entry)
	}
	// The choice survives a restart.
	fresh := provider.NewRegistry()
	extprovider.RegisterSaved(context.Background(), h.pool, fresh)
	if p, ok := fresh.Get("x-switchy"); !ok || p.Enabled() {
		t.Errorf("after a restart the plugin is %v (found %v), want it present but off", p, ok)
	}
	// Refreshing a switched-off plugin does not switch it on again.
	h.expect(h.do(&h.admin, "POST", "/api/admin/providers/external/x-switchy/refresh", nil), 200)
	if p, _ := reg.Get("x-switchy"); p.Enabled() {
		t.Error("refresh must keep the plugin switched off")
	}

	h.expect(h.do(&h.admin, "PUT", "/api/admin/providers/external/x-switchy/enabled", map[string]bool{"enabled": true}), 200)
	if len(reg.Enabled(provider.KindMedia)) != 1 {
		t.Error("switching on should bring it back")
	}
	h.expect(h.do(&h.admin, "PUT", "/api/admin/providers/external/nope/enabled", map[string]bool{"enabled": true}), http.StatusNotFound)
}

func TestPluginUpdateRoutes(t *testing.T) {
	r := newBundleRig(t, true)
	v := func(version string) []byte {
		return zipBundle(t, map[string]string{"plugin.json": `{"id":"upd-test","version":"` + version + `","kind":"auth","run":{"command":["python3","x.py"]}}`, "x.py": ""})
	}
	v2 := v("1.1.0")
	sum := sha256.Sum256(v2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/dist/index.json":
			fmt.Fprintf(w, `{"plugins":[{"id":"upd-test","latest":"1.1.0","versions":[{"version":"1.1.0","file":"upd-test/upd-test-1.1.0.zip","sha256":"%s"}]}]}`, hex.EncodeToString(sum[:]))
		case "/dist/upd-test/upd-test-1.1.0.zip":
			w.Write(v2)
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()

	item := r.jsonBody(r.upload(&r.admin, "/api/admin/plugins/install", "bundle", v("1.0.0")), http.StatusCreated)
	if item["version"] != "1.0.0" || item["previousVersion"] != nil || item["updateUrl"] != "" {
		t.Fatalf("installed = %v", item)
	}
	// Without a source there is nothing to check; with one, the saved address is shown.
	r.expect(r.do(&r.admin, "POST", "/api/admin/plugins/upd-test/check-update", nil), http.StatusBadGateway)
	r.expect(r.do(&r.admin, "PUT", "/api/admin/plugins/upd-test/update-source", map[string]string{"url": "ftp://x"}), http.StatusBadRequest)
	item = r.jsonBody(r.do(&r.admin, "PUT", "/api/admin/plugins/upd-test/update-source", map[string]string{"url": srv.URL + "/dist/index.json"}), 200)
	if item["updateUrl"] != srv.URL+"/dist/index.json" {
		t.Errorf("updateUrl = %v", item["updateUrl"])
	}
	chk := r.jsonBody(r.do(&r.admin, "POST", "/api/admin/plugins/upd-test/check-update", nil), 200)
	if chk["available"] != true || chk["current"] != "1.0.0" || chk["latest"] != "1.1.0" {
		t.Errorf("check = %v", chk)
	}
	item = r.jsonBody(r.do(&r.admin, "POST", "/api/admin/plugins/upd-test/update", nil), 200)
	if item["version"] != "1.1.0" || item["previousVersion"] != "1.0.0" {
		t.Errorf("update = %v", item)
	}
	if chk := r.jsonBody(r.do(&r.admin, "POST", "/api/admin/plugins/upd-test/check-update", nil), 200); chk["available"] != false {
		t.Errorf("after updating: %v", chk)
	}
	r.expect(r.do(&r.admin, "POST", "/api/admin/plugins/upd-test/update", nil), http.StatusBadGateway)
	r.expect(r.do(&r.admin, "POST", "/api/admin/plugins/missing/check-update", nil), http.StatusNotFound)
	r.expect(r.do(&r.admin, "POST", "/api/admin/plugins/missing/update", nil), http.StatusNotFound)

	// Installing a newer zip by hand reports the upgrade too.
	up := r.jsonBody(r.upload(&r.admin, "/api/admin/plugins/install", "bundle", v("1.2.0")), http.StatusCreated)
	if up["previousVersion"] != "1.1.0" {
		t.Errorf("upload upgrade = %v", up)
	}
}
