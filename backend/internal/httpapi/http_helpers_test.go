package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"reelstash/backend/internal/events"
)

func withPathParam(name, value string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(name, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestPathInt64(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    int64
		wantErr bool
	}{
		{name: "positive", raw: "42", want: 42},
		{name: "zero", raw: "0", want: 0},
		{name: "negative", raw: "-3", want: -3},
		{name: "empty", raw: "", wantErr: true},
		{name: "words", raw: "abc", wantErr: true},
		{name: "trailing junk", raw: "12x", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pathInt64(withPathParam("id", tt.raw), "id")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("pathInt64(%q) = %d, want error", tt.raw, got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("pathInt64(%q) = %d, %v; want %d", tt.raw, got, err, tt.want)
			}
		})
	}
}

func TestParseLimitOffset(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantLimit  int
		wantOffset int
	}{
		{name: "defaults", wantLimit: 30, wantOffset: 0},
		{name: "in range", query: "limit=10&offset=5", wantLimit: 10, wantOffset: 5},
		{name: "limit cap", query: "limit=99999", wantLimit: 30, wantOffset: 0},
		{name: "zero limit ignored", query: "limit=0&offset=2", wantLimit: 30, wantOffset: 2},
		{name: "negative offset ignored", query: "offset=-4", wantLimit: 30, wantOffset: 0},
		{name: "garbage ignored", query: "limit=no&offset=no", wantLimit: 30, wantOffset: 0},
		{name: "max limit", query: "limit=200", wantLimit: 200, wantOffset: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/?"+tt.query, nil)
			limit, offset := parseLimitOffset(r)
			if limit != tt.wantLimit || offset != tt.wantOffset {
				t.Fatalf("parseLimitOffset(%q) = %d, %d; want %d, %d", tt.query, limit, offset, tt.wantLimit, tt.wantOffset)
			}
		})
	}
}

func TestLikeContains(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{in: "plain", want: "%plain%"},
		{in: `100%`, want: `%100\%%`},
		{in: `a_b`, want: `%a\_b%`},
		{in: `a\b`, want: `%a\\b%`},
	}
	for _, tt := range tests {
		if got := likeContains(tt.in); got != tt.want {
			t.Errorf("likeContains(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFuzzyTokens(t *testing.T) {
	if got := fuzzyTokens("  Hello   World "); len(got) != 2 || got[0] != "hello" || got[1] != "world" {
		t.Fatalf("fuzzyTokens = %#v", got)
	}
	if got := fuzzyTokens("one two three four five six seven"); len(got) != 6 {
		t.Fatalf("fuzzyTokens capped at %d tokens", len(got))
	}
}

func TestWriteJSONAndError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusCreated, map[string]any{"id": 7})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	if body := rec.Body.String(); body != "{\"id\":7}\n" {
		t.Fatalf("body = %q", body)
	}

	rec = httptest.NewRecorder()
	writeError(rec, http.StatusBadRequest, "invalid studio id")
	if rec.Code != http.StatusBadRequest || rec.Body.String() != "{\"error\":\"invalid studio id\"}\n" {
		t.Fatalf("error response = %d %q", rec.Code, rec.Body.String())
	}
}

func TestDecodeJSON(t *testing.T) {
	var dest struct {
		Name string `json:"name"`
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"Ada"}`))
	if err := decodeJSON(r, &dest); err != nil || dest.Name != "Ada" {
		t.Fatalf("decodeJSON = %+v, %v", dest, err)
	}
	r = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{`))
	if err := decodeJSON(r, &dest); err == nil {
		t.Fatal("malformed JSON decoded")
	}
}

func TestBackgroundLoopsStopWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := &API{Hub: events.New()}
	done := make(chan struct{})
	go func() {
		a.runJobsFeed(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("jobs feed kept running after its context was cancelled")
	}
}

func TestRouteGroupsStayRegistered(t *testing.T) {
	a := &API{}
	r := chi.NewRouter()
	r.Get("/healthz", http.NotFound)
	a.mountAuth(r)
	a.mountAPI(r)

	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/healthz"},
		{"GET", "/auth/config"},
		{"POST", "/auth/native-login"},
		{"POST", "/auth/device/poll"},
		{"GET", "/auth/plugin/oidc/callback"},
		{"GET", "/api/me"},
		{"PUT", "/api/libraries/default"},
		{"GET", "/api/media"},
		{"GET", "/api/media/1/stream"},
		{"POST", "/api/media/1/match/unmatch"},
		{"GET", "/api/performers/1"},
		{"POST", "/api/performers/1/merge"},
		{"GET", "/api/studios/1/image"},
		{"DELETE", "/api/series/1/media/2"},
		{"POST", "/api/categories"},
		{"GET", "/api/collections/1"},
		{"GET", "/api/search"},
		{"PUT", "/api/playback/1"},
		{"GET", "/api/notifications"},
		{"GET", "/api/files/nfo"},
		{"GET", "/api/downloads"},
		{"POST", "/api/downloads/add"},
		{"GET", "/api/admin/overview"},
		{"POST", "/api/admin/scans/9"},
		{"POST", "/api/admin/organizer/3/run"},
		{"POST", "/api/admin/files/transfer"},
		{"POST", "/api/admin/namer/match/4"},
	}
	for _, rt := range routes {
		if !r.Match(chi.NewRouteContext(), rt.method, rt.path) {
			t.Errorf("missing %s %s", rt.method, rt.path)
		}
	}
}

func TestLogExceptProbesSkipsHealthz(t *testing.T) {
	var logged []string
	prev := middleware.DefaultLogger
	middleware.DefaultLogger = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logged = append(logged, r.URL.Path)
			next.ServeHTTP(w, r)
		})
	}
	t.Cleanup(func() { middleware.DefaultLogger = prev })

	h := logExceptProbes(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/media", nil))
	if len(logged) != 1 || logged[0] != "/api/media" {
		t.Fatalf("logged paths = %v", logged)
	}
}
