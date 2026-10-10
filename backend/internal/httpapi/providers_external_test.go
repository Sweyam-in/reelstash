package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reelstash/backend/internal/config"
	"reelstash/backend/internal/extprovider"
	"reelstash/backend/internal/namer"
	"reelstash/backend/internal/provider"
	"reelstash/backend/internal/testdb"
)

func pluginServer(t *testing.T, id string, media bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest" {
			json.NewEncoder(w).Encode(extprovider.Manifest{ID: id, Name: "Plugin " + id, Capabilities: extprovider.Capabilities{Media: media, Performer: true}})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func externalRig(t *testing.T) (*harness, *provider.Registry) {
	t.Helper()
	reg := provider.NewRegistry()
	pool := testdb.Isolated(t)
	engine := namer.New(pool, reg, nil, "", t.TempDir())
	return newHarnessOn(t, pool, config.Config{}, engine), reg
}

func TestExternalProviderLifecycle(t *testing.T) {
	h, reg := externalRig(t)
	srv := pluginServer(t, "mydb", true)

	out := h.jsonBody(h.do(&h.admin, "POST", "/api/admin/providers/external", map[string]string{"baseUrl": srv.URL, "token": "tok"}), http.StatusCreated)
	if out["id"] != "x-mydb" || out["name"] != "Plugin mydb" {
		t.Fatalf("created = %v", out)
	}
	if p, ok := reg.Get("x-mydb"); !ok || !p.Enabled() || len(reg.Enabled(provider.KindMedia)) != 1 {
		t.Fatal("the provider is not live in the registry")
	}
	// Saved, so it survives a restart: the spec and the token.
	if specs := extprovider.LoadSpecs(context.Background(), h.pool); len(specs) != 1 || specs[0].BaseURL != srv.URL {
		t.Errorf("saved specs = %+v", specs)
	}
	if cfg := provider.LoadConfig(context.Background(), h.pool, "x-mydb"); cfg["token"] != "tok" {
		t.Errorf("saved config = %v", cfg)
	}

	// It shows in the admin list as external, with the token masked.
	rec := h.do(&h.admin, "GET", "/api/admin/providers", nil)
	if strings.Contains(rec.Body.String(), `"tok"`) {
		t.Error("the token leaked in the listing")
	}
	var found map[string]any
	for _, it := range h.jsonBody(rec, 200)["items"].([]any) {
		if m := it.(map[string]any); m["id"] == "x-mydb" {
			found = m
		}
	}
	if found == nil || found["external"] != true || found["baseUrl"] != srv.URL || found["enabled"] != true {
		t.Errorf("listing entry = %v", found)
	}
	if res := h.jsonBody(h.do(&h.admin, "POST", "/api/admin/providers/x-mydb/test", nil), 200); res["ok"] != true {
		t.Errorf("test = %v", res)
	}

	// Adding it again updates it instead of duplicating it.
	h.expect(h.do(&h.admin, "POST", "/api/admin/providers/external", map[string]string{"baseUrl": srv.URL}), http.StatusOK)
	if len(reg.All()) != 1 || extprovider.LoadSpecs(context.Background(), h.pool)[0].ID != "x-mydb" {
		t.Error("re-adding duplicated the provider")
	}
	if cfg := provider.LoadConfig(context.Background(), h.pool, "x-mydb"); cfg["token"] != "tok" {
		t.Error("re-adding without a token lost the saved token")
	}

	// A library can use it, and removing it takes it out of the library.
	var libID int64
	h.pool.QueryRow(context.Background(), `INSERT INTO library (name, mode) VALUES ('L', 'private') RETURNING id`).Scan(&libID)
	h.pool.Exec(context.Background(), `INSERT INTO library_provider (library_id, provider, position) VALUES ($1, 'x-mydb', 0)`, libID)
	h.expect(h.do(&h.admin, "POST", "/api/admin/providers/external/x-mydb/refresh", nil), http.StatusOK)
	h.expect(h.do(&h.admin, "DELETE", "/api/admin/providers/external/x-mydb", nil), http.StatusOK)
	if _, ok := reg.Get("x-mydb"); ok || len(extprovider.LoadSpecs(context.Background(), h.pool)) != 0 {
		t.Error("the provider is still registered or saved")
	}
	var n int
	h.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM library_provider WHERE provider = 'x-mydb'`).Scan(&n)
	if n != 0 {
		t.Error("libraries still list the removed provider")
	}
	h.expect(h.do(&h.admin, "DELETE", "/api/admin/providers/external/x-mydb", nil), http.StatusNotFound)
}

func TestExternalProviderRefusesBadServicesAndBuiltIns(t *testing.T) {
	h, reg := externalRig(t)
	for _, bad := range []string{"", "ftp://example.test", "http://127.0.0.1:1"} {
		if rec := h.do(&h.admin, "POST", "/api/admin/providers/external", map[string]string{"baseUrl": bad}); rec.Code != http.StatusBadRequest {
			t.Errorf("%q: %d, want 400", bad, rec.Code)
		}
	}
	if len(reg.All()) != 0 {
		t.Error("a failed connection registered something")
	}
	// Only plugins can be removed this way; built-ins cannot.
	reg.Register(&fakeProvider{id: "alpha"})
	h.expect(h.do(&h.admin, "DELETE", "/api/admin/providers/external/alpha", nil), http.StatusNotFound)
	if _, ok := reg.Get("alpha"); !ok {
		t.Error("a built-in provider was removed")
	}
}

func TestExternalProviderRefreshDetectsAnotherService(t *testing.T) {
	h, _ := externalRig(t)
	current := "mydb"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(extprovider.Manifest{ID: current, Name: "P", Capabilities: extprovider.Capabilities{Media: true}})
	}))
	defer srv.Close()
	h.expect(h.do(&h.admin, "POST", "/api/admin/providers/external", map[string]string{"baseUrl": srv.URL}), http.StatusCreated)
	current = "other"
	h.expect(h.do(&h.admin, "POST", "/api/admin/providers/external/x-mydb/refresh", nil), http.StatusConflict)
}

func TestExternalProviderRoutesAreAdminOnly(t *testing.T) {
	h, _ := externalRig(t)
	h.expect(h.do(&h.user, "POST", "/api/admin/providers/external", map[string]string{"baseUrl": "http://x"}), http.StatusForbidden)
	h.expect(h.do(&h.user, "DELETE", "/api/admin/providers/external/x-a", nil), http.StatusForbidden)
}

func TestSavedExternalProvidersComeBackAfterARestart(t *testing.T) {
	h, _ := externalRig(t)
	srv := pluginServer(t, "kept", true)
	h.expect(h.do(&h.admin, "POST", "/api/admin/providers/external", map[string]string{"baseUrl": srv.URL, "token": "t"}), http.StatusCreated)

	fresh := provider.NewRegistry()
	extprovider.RegisterSaved(context.Background(), h.pool, fresh)
	fresh.Bootstrap(context.Background(), h.pool)
	p, ok := fresh.Get("x-kept")
	if !ok || !p.Enabled() {
		t.Fatal("the plugin was not restored")
	}
	if err := p.Test(context.Background()); err != nil {
		t.Errorf("restored plugin test: %v", err)
	}
}
