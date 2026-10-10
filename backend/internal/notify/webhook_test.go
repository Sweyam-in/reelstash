package notify

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSignMatchesKnownHMAC(t *testing.T) {
	// HMAC-SHA256("key", "The quick brown fox jumps over the lazy dog")
	got := Sign("key", []byte("The quick brown fox jumps over the lazy dog"))
	want := "sha256=f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8"
	if got != want {
		t.Fatalf("Sign = %s, want %s", got, want)
	}
}

func TestBodyShapes(t *testing.T) {
	ev := Event{Kind: ScanCompleted, Title: "Scan finished", Body: "Movies: 3 new"}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	generic, err := Body("https://example.com/hook", ev, at)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(generic, &m); err != nil {
		t.Fatal(err)
	}
	if m["event"] != "scan.completed" || m["title"] != "Scan finished" || m["timestamp"] != "2026-01-02T03:04:05Z" {
		t.Fatalf("unexpected generic payload: %s", generic)
	}

	discord, _ := Body("https://discord.com/api/webhooks/1/abc", ev, at)
	var d map[string]string
	if err := json.Unmarshal(discord, &d); err != nil || d["content"] != "**Scan finished**\nMovies: 3 new" {
		t.Fatalf("unexpected discord payload: %s", discord)
	}

	slack, _ := Body("https://hooks.slack.com/services/x", ev, at)
	var sl map[string]string
	if err := json.Unmarshal(slack, &sl); err != nil || sl["text"] != "*Scan finished*\nMovies: 3 new" {
		t.Fatalf("unexpected slack payload: %s", slack)
	}
}

func TestValidateURL(t *testing.T) {
	for _, ok := range []string{"http://192.168.1.5:8123/api/webhook/x", "https://example.com/a"} {
		if err := ValidateURL(ok); err != nil {
			t.Errorf("%q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "ftp://example.com", "example.com/hook", "javascript:alert(1)", "http://"} {
		if err := ValidateURL(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestKnownKind(t *testing.T) {
	if !KnownKind("scan.failed") || KnownKind("nope") {
		t.Fatal("KnownKind mismatch")
	}
}
