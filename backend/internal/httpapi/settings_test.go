package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"reelstash/backend/internal/config"
	"reelstash/backend/internal/namer"
	"reelstash/backend/internal/provider"
	"reelstash/backend/internal/testdb"
)

// fakeProvider is a metadata source with one secret and one plain setting.
type fakeProvider struct {
	provider.Provider // unimplemented search methods panic if ever called
	id, token, base   string
	failTest          error
	configureErr      error
}

func (f *fakeProvider) ID() string   { return f.id }
func (f *fakeProvider) Name() string { return "Fake " + f.id }
func (f *fakeProvider) Capabilities() provider.Caps {
	return provider.Caps{Media: true, Performer: true}
}
func (f *fakeProvider) Enabled() bool        { return f.token != "" }
func (f *fakeProvider) ImageHosts() []string { return nil }
func (f *fakeProvider) ConfigFields() []provider.Field {
	return []provider.Field{
		{Key: "token", Label: "Token", Secret: true},
		{Key: "baseUrl", Label: "Base URL", Placeholder: "https://x"},
	}
}
func (f *fakeProvider) Configure(cfg map[string]string) error {
	if f.configureErr != nil {
		return f.configureErr
	}
	if v, ok := cfg["token"]; ok {
		f.token = v
	}
	if v, ok := cfg["baseUrl"]; ok {
		f.base = v
	}
	return nil
}
func (f *fakeProvider) Test(ctx context.Context) error { return f.failTest }

func providerRig(t *testing.T) (*harness, *fakeProvider, *fakeProvider) {
	t.Helper()
	a, b := &fakeProvider{id: "alpha"}, &fakeProvider{id: "beta"}
	reg := provider.NewRegistry()
	reg.Register(a)
	reg.Register(b)
	pool := testdb.Isolated(t)
	engine := namer.New(pool, reg, nil, "", t.TempDir())
	return newHarnessOn(t, pool, config.Config{}, engine), a, b
}

func TestProviderAdminMasksSecrets(t *testing.T) {
	h, a, _ := providerRig(t)
	h.expect(h.do(&h.admin, "PUT", "/api/admin/providers/alpha/config", map[string]string{"token": "abcdef123456", "baseUrl": "https://api.test"}), 200)
	if a.token != "abcdef123456" || a.base != "https://api.test" {
		t.Errorf("provider not hot-configured: %+v", a)
	}

	rec := h.do(&h.admin, "GET", "/api/admin/providers", nil)
	if strings.Contains(rec.Body.String(), "abcdef123456") {
		t.Fatalf("the full secret leaked in the admin listing: %s", rec.Body.String())
	}
	out := h.jsonBody(rec, 200)
	var alpha map[string]any
	for _, it := range out["items"].([]any) {
		if m := it.(map[string]any); m["id"] == "alpha" {
			alpha = m
		}
	}
	if alpha == nil || alpha["enabled"] != true {
		t.Fatalf("alpha = %v", alpha)
	}
	for _, f := range alpha["fields"].([]any) {
		fm := f.(map[string]any)
		switch fm["key"] {
		case "token":
			if fm["value"] != "****3456" || fm["set"] != true {
				t.Errorf("token field = %v", fm)
			}
		case "baseUrl":
			if fm["value"] != "https://api.test" {
				t.Errorf("non-secret value should be shown, got %v", fm["value"])
			}
		}
	}

	// A partial update keeps the other stored values.
	h.expect(h.do(&h.admin, "PUT", "/api/admin/providers/alpha/config", map[string]string{"baseUrl": "https://other.test"}), 200)
	if a.token != "abcdef123456" || a.base != "https://other.test" {
		t.Errorf("after partial update: %+v", a)
	}
}

func TestProviderConfigValidation(t *testing.T) {
	h, a, _ := providerRig(t)
	if rec := h.do(&h.admin, "PUT", "/api/admin/providers/nope/config", map[string]string{"token": "x"}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown provider: %d", rec.Code)
	}
	if rec := h.do(&h.admin, "PUT", "/api/admin/providers/alpha/config", map[string]string{"bogus": "x"}); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown setting: %d", rec.Code)
	}
	if rec := h.do(&h.admin, "PUT", "/api/admin/providers/alpha/config", "{bad"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad body: %d", rec.Code)
	}
	a.configureErr = context.DeadlineExceeded
	rec := h.do(&h.admin, "PUT", "/api/admin/providers/alpha/config", map[string]string{"token": "x"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("rejected by provider: %d", rec.Code)
	}
	if n := count(t, h.pool, `SELECT COUNT(*) FROM app_setting WHERE key = 'provider.alpha.config'`); n != 0 {
		t.Error("a config the provider rejected must not be persisted")
	}
}

func TestProviderConfigDelete(t *testing.T) {
	h, a, _ := providerRig(t)
	h.expect(h.do(&h.admin, "PUT", "/api/admin/providers/alpha/config", map[string]string{"token": "tok-1234"}), 200)
	out := h.jsonBody(h.do(&h.admin, "DELETE", "/api/admin/providers/alpha/config", nil), 200)
	if out["enabled"] != false || a.token != "" {
		t.Errorf("after delete: %v token=%q", out, a.token)
	}
	if rec := h.do(&h.admin, "DELETE", "/api/admin/providers/nope/config", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown: %d", rec.Code)
	}
}

func TestProviderTestEndpoint(t *testing.T) {
	h, a, _ := providerRig(t)
	if out := h.jsonBody(h.do(&h.admin, "POST", "/api/admin/providers/alpha/test", nil), 200); out["ok"] != true {
		t.Errorf("healthy provider: %v", out)
	}
	a.failTest = context.Canceled
	out := h.jsonBody(h.do(&h.admin, "POST", "/api/admin/providers/alpha/test", nil), 200)
	if out["ok"] != false || out["error"] == "" {
		t.Errorf("failing provider should report ok:false with the error, got %v", out)
	}
	if rec := h.do(&h.admin, "POST", "/api/admin/providers/nope/test", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown: %d", rec.Code)
	}
}

func TestProviderOrderAndMode(t *testing.T) {
	h, _, _ := providerRig(t)
	if rec := h.do(&h.admin, "PUT", "/api/admin/providers/order", map[string]any{"mode": "chaos"}); rec.Code != http.StatusBadRequest {
		t.Errorf("bad mode: %d", rec.Code)
	}
	if rec := h.do(&h.admin, "PUT", "/api/admin/providers/order", map[string]any{"order": []string{"beta", "ghost"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown provider in order: %d", rec.Code)
	}
	h.expect(h.do(&h.admin, "PUT", "/api/admin/providers/order", map[string]any{"order": []string{"beta", "alpha"}, "mode": "best"}), 200)

	out := h.jsonBody(h.do(&h.admin, "GET", "/api/admin/providers", nil), 200)
	if out["mode"] != "best" {
		t.Errorf("mode = %v", out["mode"])
	}
	items := out["items"].([]any)
	if items[0].(map[string]any)["id"] != "beta" {
		t.Errorf("order not applied: %v", items)
	}
	// The user-facing list follows the same order.
	users := h.items(h.do(&h.user, "GET", "/api/providers", nil))
	if len(users) != 2 || users[0]["id"] != "beta" {
		t.Errorf("user provider list = %v", users)
	}
	for _, it := range users {
		if _, leaked := it["fields"]; leaked {
			t.Error("user provider list must not include configuration fields")
		}
	}
}

func TestProvidersWithoutEngine(t *testing.T) {
	h := newIsolatedHarness(t, config.Config{}, nil)
	if items := h.items(h.do(&h.admin, "GET", "/api/providers", nil)); len(items) != 0 {
		t.Errorf("no engine should list nothing, got %v", items)
	}
	if rec := h.do(&h.admin, "PUT", "/api/admin/providers/order", map[string]any{"mode": "best"}); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("order without providers: %d, want 503", rec.Code)
	}
	if rec := h.do(&h.admin, "PUT", "/api/admin/providers/alpha/config", map[string]string{}); rec.Code != http.StatusNotFound {
		t.Errorf("config without providers: %d, want 404", rec.Code)
	}
}

func TestMaskToken(t *testing.T) {
	for in, want := range map[string]string{"": "", "ab": "****", "abcd": "****", "abcde": "****bcde", "0123456789": "****6789"} {
		if got := maskToken(in); got != want {
			t.Errorf("maskToken(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOrganizerSettingsValidationAndTemplate(t *testing.T) {
	h := newIsolatedHarness(t, config.Config{RenameTemplate: "{studio} - {title}"}, nil)
	org := h.organizerBase()
	def := h.jsonBody(h.do(&h.admin, "GET", org+"/settings", nil), 200)
	if def["categorizedDir"] != "categorized" || def["template"] != "{studio} - {title}" || def["renameFiles"] != true {
		t.Errorf("defaults = %v", def)
	}

	good := map[string]any{"categorizedDir": "Sorted", "uncategorizedDir": "Inbox", "renameFiles": false, "writeNfo": true, "writeImages": false, "template": "{date} {title}"}
	put := func(mod func(map[string]any)) *http.Response {
		body := map[string]any{}
		for k, v := range good {
			body[k] = v
		}
		mod(body)
		return h.do(&h.admin, "PUT", org+"/settings", body).Result()
	}
	for name, mod := range map[string]func(map[string]any){
		"empty categorized":       func(m map[string]any) { m["categorizedDir"] = "" },
		"dot dir":                 func(m map[string]any) { m["uncategorizedDir"] = "." },
		"hidden folder":           func(m map[string]any) { m["categorizedDir"] = ".secret" },
		"illegal characters":      func(m map[string]any) { m["categorizedDir"] = "a:b" },
		"nested overlap":          func(m map[string]any) { m["categorizedDir"] = "Inbox/Sorted" },
		"identical":               func(m map[string]any) { m["categorizedDir"] = "inbox" },
		"template without title":  func(m map[string]any) { m["template"] = "{date}" },
		"template with separator": func(m map[string]any) { m["template"] = "{studio}/{title}" },
	} {
		if resp := put(mod); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, resp.StatusCode)
		}
	}
	if resp := put(func(m map[string]any) {}); resp.StatusCode != 200 {
		t.Fatalf("valid settings: %d", resp.StatusCode)
	}
	out := h.jsonBody(h.do(&h.admin, "GET", org+"/settings", nil), 200)
	if out["categorizedDir"] != "Sorted" || out["renameFiles"] != false || out["template"] != "{date} {title}" || out["writeImages"] != false {
		t.Errorf("saved = %v", out)
	}

	// Saving the default template again drops the override.
	h.expect(h.do(&h.admin, "PUT", org+"/settings", map[string]any{"categorizedDir": "Sorted", "uncategorizedDir": "Inbox", "template": "{studio} - {title}"}), 200)
	if n := count(t, h.pool, `SELECT COUNT(*) FROM app_setting WHERE key = 'rename_template'`); n != 0 {
		t.Error("the default template should not be stored as an override")
	}
}

func TestOrganizerRuleLifecycle(t *testing.T) {
	h := newIsolatedHarness(t, config.Config{}, nil)
	org := h.organizerBase()
	studioID := h.createdID(&h.admin, "/api/studios", map[string]string{"name": "Rule Studio"})
	perfID := h.createdID(&h.admin, "/api/performers", map[string]string{"name": "Rule Performer"})

	for _, bad := range []map[string]any{
		{"kind": "bogus", "entityId": studioID},
		{"kind": "studio", "entityId": 0},
		{"kind": "studio"},
	} {
		if rec := h.do(&h.admin, "POST", org+"/rules", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: %d, want 400", bad, rec.Code)
		}
	}
	if rec := h.do(&h.admin, "POST", org+"/rules", map[string]any{"kind": "studio", "entityId": 999999999}); rec.Code != http.StatusNotFound {
		t.Errorf("rule for missing studio: %d, want 404", rec.Code)
	}
	r1 := h.createdID(&h.admin, org+"/rules", map[string]any{"kind": "studio", "entityId": studioID, "folderName": "Studios/Rule"})
	r2 := h.createdID(&h.admin, org+"/rules", map[string]any{"kind": "performer", "entityId": perfID})
	if rec := h.do(&h.admin, "POST", org+"/rules", map[string]any{"kind": "studio", "entityId": studioID}); rec.Code != http.StatusConflict {
		t.Errorf("duplicate rule: %d, want 409", rec.Code)
	}

	items := h.items(h.do(&h.admin, "GET", org+"/rules", nil))
	if len(items) != 2 || int64(items[0]["id"].(float64)) != r1 {
		t.Fatalf("rules = %v", items)
	}
	h.expect(h.do(&h.admin, "PUT", org+"/rules/order", map[string]any{"ids": []int64{r2, r1}}), 200)
	if items = h.items(h.do(&h.admin, "GET", org+"/rules", nil)); int64(items[0]["id"].(float64)) != r2 {
		t.Errorf("reorder not applied: %v", items)
	}
	h.expect(h.do(&h.admin, "PUT", org+"/rules/"+itoa(r1), map[string]any{"folderName": "Renamed", "enabled": false}), 200)
	if rec := h.do(&h.admin, "PUT", org+"/rules/999999999", map[string]any{"folderName": "x"}); rec.Code != http.StatusNotFound {
		t.Errorf("update missing: %d", rec.Code)
	}
	h.expect(h.do(&h.admin, "DELETE", org+"/rules/"+itoa(r1), nil), 200)
	if items = h.items(h.do(&h.admin, "GET", org+"/rules", nil)); len(items) != 1 {
		t.Errorf("after delete: %v", items)
	}
}

// organizerBase creates a library and returns its organizer API prefix.
func (h *harness) organizerBase() string {
	h.t.Helper()
	out := h.jsonBody(h.do(&h.admin, "POST", "/api/admin/libraries", map[string]any{"name": "Organizer Lib", "mode": "private"}), http.StatusOK)
	return "/api/admin/organizer/" + itoa(int64(out["id"].(float64)))
}

func TestAutomaticMatchingSettings(t *testing.T) {
	h, _, _ := providerRig(t)
	autoMatch := func() map[string]any {
		return h.jsonBody(h.do(&h.admin, "GET", "/api/admin/providers", nil), 200)["autoMatch"].(map[string]any)
	}
	// Defaults: performers and studios apply exact matches; titles go to review.
	if got := autoMatch(); got["titles"] != false || got["performers"] != true || got["studios"] != true {
		t.Fatalf("defaults = %v", got)
	}

	// Kinds left out are unchanged.
	h.expect(h.do(&h.admin, "PUT", "/api/admin/providers/order", map[string]any{"autoMatch": map[string]bool{"titles": true, "studios": false}}), 200)
	if got := autoMatch(); got["titles"] != true || got["performers"] != true || got["studios"] != false {
		t.Fatalf("after update = %v", got)
	}

	if rec := h.do(&h.admin, "PUT", "/api/admin/providers/order", map[string]any{"autoMatch": map[string]bool{"series": true}}); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown kind: %d", rec.Code)
	}
	if rec := h.do(&h.user, "PUT", "/api/admin/providers/order", map[string]any{"autoMatch": map[string]bool{"titles": false}}); rec.Code != http.StatusForbidden {
		t.Errorf("non-admin: %d", rec.Code)
	}
}
