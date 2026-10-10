package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestSecureRequest(t *testing.T) {
	lan := httptest.NewRequest("GET", "http://media.local:8000/auth/callback", nil)
	if secureRequest(lan) {
		t.Fatal("plain-HTTP LAN request treated as secure")
	}
	tunnel := httptest.NewRequest("GET", "/auth/callback", nil)
	tunnel.Header.Set("X-Forwarded-Proto", "https")
	if !secureRequest(tunnel) {
		t.Fatal("X-Forwarded-Proto https not treated as secure")
	}
	cf := httptest.NewRequest("GET", "/auth/callback", nil)
	cf.Header.Set("Cf-Visitor", `{"scheme":"https"}`)
	if !secureRequest(cf) {
		t.Fatal("Cf-Visitor https not treated as secure")
	}
}
