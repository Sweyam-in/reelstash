package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/auth"
	"reelstash/backend/internal/authplugin"
	"reelstash/backend/internal/config"
	"reelstash/backend/internal/namer"
	"reelstash/backend/internal/testdb"
)

// harness is the full router over a real Postgres with one admin and two
// regular users (each holding a live session), for black-box HTTP tests.
// It needs REELSTASH_TEST_DATABASE_URL and skips otherwise.
type harness struct {
	t       *testing.T
	pool    *pgxpool.Pool
	handler http.Handler
	cfg     config.Config

	admin, user, other actor

	authPlugins *authplugin.Registry
}

type actor struct {
	id     int64
	cookie string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessCfg(t, config.Config{})
}

func newHarnessCfg(t *testing.T, cfg config.Config) *harness {
	t.Helper()
	_, pool := unmatchAPI(t)
	cfg.PosterDir = t.TempDir()
	h := &harness{t: t, pool: pool, cfg: cfg}
	h.authPlugins = authplugin.NewRegistry()
	h.handler = NewRouter(pool, cfg, nil, nil, nil, nil, nil, WithPlugins(nil, h.authPlugins))
	h.admin = h.newActor("ADMIN")
	h.user = h.newActor("USER")
	h.other = h.newActor("USER")
	return h
}

// newActor creates a user with a valid session and removes both afterwards.
func (h *harness) newActor(role string) actor {
	h.t.Helper()
	ctx := context.Background()
	var a actor
	if err := h.pool.QueryRow(ctx, `INSERT INTO app_user (auth_subject, role, display_name) VALUES ($1, $2, $3) RETURNING id`,
		"h-"+strings.ToLower(role)+"-"+uuid.NewString(), role, role+" tester").Scan(&a.id); err != nil {
		h.t.Fatal(err)
	}
	a.cookie = uuid.NewString()
	if _, err := h.pool.Exec(ctx, `INSERT INTO web_session (id, user_id, expires_at) VALUES ($1, $2, $3)`, a.cookie, a.id, time.Now().Add(time.Hour)); err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() {
		h.pool.Exec(context.Background(), `DELETE FROM app_user WHERE id = $1`, a.id)
	})
	return a
}

// do sends a request as who (nil = anonymous) with an optional JSON body.
func (h *harness) do(who *actor, method, path string, body any) *httptest.ResponseRecorder {
	h.t.Helper()
	var rd *strings.Reader
	switch b := body.(type) {
	case nil:
		rd = strings.NewReader("")
	case string:
		rd = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			h.t.Fatal(err)
		}
		rd = strings.NewReader(string(raw))
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if who != nil {
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: who.cookie})
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

// expect fails unless the response has the wanted status; returns the body.
func (h *harness) expect(rec *httptest.ResponseRecorder, want int) []byte {
	h.t.Helper()
	if rec.Code != want {
		h.t.Fatalf("status = %d, want %d; body: %s", rec.Code, want, rec.Body.String())
	}
	return rec.Body.Bytes()
}

// json decodes the response body into a generic map after checking status.
func (h *harness) jsonBody(rec *httptest.ResponseRecorder, want int) map[string]any {
	h.t.Helper()
	raw := h.expect(rec, want)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		h.t.Fatalf("response is not a JSON object: %v: %s", err, raw)
	}
	return out
}

// createdID POSTs body and returns the new row's id (asserting 201).
func (h *harness) createdID(who *actor, path string, body any) int64 {
	h.t.Helper()
	out := h.jsonBody(h.do(who, http.MethodPost, path, body), http.StatusCreated)
	id, ok := out["id"].(float64)
	if !ok {
		h.t.Fatalf("no id in response: %v", out)
	}
	return int64(id)
}

// items returns the "items" array of a list response.
func (h *harness) items(rec *httptest.ResponseRecorder) []map[string]any {
	h.t.Helper()
	out := h.jsonBody(rec, http.StatusOK)
	raw, _ := out["items"].([]any)
	items := make([]map[string]any, 0, len(raw))
	for _, v := range raw {
		if m, ok := v.(map[string]any); ok {
			items = append(items, m)
		}
	}
	return items
}

// hasID reports whether any item has the numeric id.
func hasID(items []map[string]any, id int64) bool {
	for _, it := range items {
		if v, ok := it["id"].(float64); ok && int64(v) == id {
			return true
		}
	}
	return false
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

// uniq makes a name that never collides across runs on a shared database.
func uniq(prefix string) string { return prefix + " " + uuid.NewString()[:8] }

// newTitle inserts a bare media item (no file, so it belongs to no library
// folder) and deletes it afterwards.
func (h *harness) newTitle(title string) int64 {
	h.t.Helper()
	var id int64
	if err := h.pool.QueryRow(context.Background(), `INSERT INTO media_item (title) VALUES ($1) RETURNING id`, title).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM media_item WHERE id = $1`, id) })
	return id
}

// newIsolatedHarness is newHarness over a private, freshly migrated schema and
// an optional metadata engine. Use it for tests that write global settings
// (provider config, organizer settings) other packages also read.
func newIsolatedHarness(t *testing.T, cfg config.Config, engine *namer.Engine) *harness {
	t.Helper()
	return newHarnessOn(t, testdb.Isolated(t), cfg, engine)
}

func newHarnessOn(t *testing.T, pool *pgxpool.Pool, cfg config.Config, engine *namer.Engine) *harness {
	t.Helper()
	cfg.PosterDir = t.TempDir()
	h := &harness{t: t, pool: pool, cfg: cfg}
	h.authPlugins = authplugin.NewRegistry()
	h.handler = NewRouter(pool, cfg, nil, nil, engine, nil, nil, WithPlugins(nil, h.authPlugins))
	h.admin = h.newActor("ADMIN")
	h.user = h.newActor("USER")
	h.other = h.newActor("USER")
	return h
}

// AuthPluginsFor returns the registry of sign-in plugins behind the harness's router.
func (h *harness) AuthPluginsFor(t *testing.T) *authplugin.Registry {
	t.Helper()
	if h.authPlugins == nil {
		t.Fatal("this harness was not built with a plugin registry; use newPluginRig")
	}
	return h.authPlugins
}

// doReason is do with an X-Admin-Reason header.
func (h *harness) doReason(who *actor, method, path string, body any, reason string) *httptest.ResponseRecorder {
	h.t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	if reason != "" {
		req.Header.Set(reasonHeader, reason)
	}
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: who.cookie})
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}
