package httpapi

import (
	"net/http"
	"testing"

	"reelstash/backend/internal/config"
)

func TestAppearanceTheme(t *testing.T) {
	h := newIsolatedHarness(t, config.Config{}, nil)

	out := h.jsonBody(h.do(&h.admin, "GET", "/api/admin/appearance", nil), http.StatusOK)
	if out["colorTheme"] != "grove" {
		t.Fatalf("default theme = %v", out["colorTheme"])
	}
	themes, _ := out["themes"].([]any)
	if len(themes) < 5 {
		t.Fatalf("themes = %v", themes)
	}

	public := h.jsonBody(h.do(nil, "GET", "/auth/config", nil), http.StatusOK)
	if public["colorTheme"] != "grove" {
		t.Fatalf("public theme = %v", public["colorTheme"])
	}

	h.expect(h.do(&h.user, "PUT", "/api/admin/appearance", map[string]any{"colorTheme": "iris"}), http.StatusForbidden)
	h.expect(h.do(&h.admin, "PUT", "/api/admin/appearance", map[string]any{"colorTheme": "pink"}), http.StatusBadRequest)
	h.expect(h.do(&h.admin, "PUT", "/api/admin/appearance", map[string]any{"colorTheme": "aurora"}), http.StatusOK)

	if got := h.jsonBody(h.do(&h.admin, "GET", "/api/admin/appearance", nil), http.StatusOK)["colorTheme"]; got != "aurora" {
		t.Fatalf("saved theme = %v", got)
	}
	if got := h.jsonBody(h.do(nil, "GET", "/auth/config", nil), http.StatusOK)["colorTheme"]; got != "aurora" {
		t.Fatalf("public theme after save = %v", got)
	}
}
