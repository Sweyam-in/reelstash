package notify_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"reelstash/backend/internal/events"
	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/notify"
	"reelstash/backend/internal/testdb"
)

type delivery struct {
	event, signature string
	body             map[string]any
	raw              []byte
}

// receiver is a webhook endpoint that records what it is sent.
type receiver struct {
	*httptest.Server
	mu   sync.Mutex
	got  []delivery
	code int
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	r := &receiver{code: 200}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		d := delivery{event: req.Header.Get("X-ReelStash-Event"), signature: req.Header.Get("X-ReelStash-Signature"), raw: raw}
		json.Unmarshal(raw, &d.body)
		r.mu.Lock()
		r.got = append(r.got, d)
		code := r.code
		r.mu.Unlock()
		w.WriteHeader(code)
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *receiver) deliveries() []delivery {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]delivery(nil), r.got...)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func notifications(t *testing.T, s *notify.Service) []map[string]any {
	t.Helper()
	rows, err := s.DB.Query(context.Background(), `SELECT kind, title, COALESCE(body,''), COALESCE(link,''), admin_only FROM notification ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var kind, title, body, link string
		var admin bool
		rows.Scan(&kind, &title, &body, &link, &admin)
		out = append(out, map[string]any{"kind": kind, "title": title, "body": body, "link": link, "admin": admin})
	}
	return out
}

func newService(t *testing.T) (*notify.Service, *events.Hub) {
	pool := testdb.Isolated(t)
	hub := events.New()
	return notify.New(pool, hub), hub
}

func TestEmitStoresPublishesAndDelivers(t *testing.T) {
	s, hub := newService(t)
	rx := newReceiver(t)
	ctx := context.Background()
	s.DB.Exec(ctx, `INSERT INTO webhook (name, url, secret, events, enabled) VALUES ('all', $1, 's3cret', '{}', true)`, rx.URL)
	admin, stopAdmin := hub.Subscribe(true)
	defer stopAdmin()
	user, stopUser := hub.Subscribe(false)
	defer stopUser()

	s.Emit(ctx, notify.Event{Kind: notify.ScanCompleted, Title: "Scan finished", Body: "Movies: 3 new", Link: "/admin/jobs", AdminOnly: true})

	rows := notifications(t, s)
	if len(rows) != 1 || rows[0]["kind"] != "scan.completed" || rows[0]["title"] != "Scan finished" || rows[0]["admin"] != true || rows[0]["link"] != "/admin/jobs" {
		t.Fatalf("stored = %v", rows)
	}
	select {
	case ev := <-admin.C:
		if ev.Name != "notification" {
			t.Errorf("event name = %q", ev.Name)
		}
	case <-time.After(time.Second):
		t.Error("admin subscriber got no push")
	}
	select {
	case ev := <-user.C:
		t.Errorf("admin-only notification pushed to a regular user: %s", ev.Data)
	case <-time.After(100 * time.Millisecond):
	}

	waitFor(t, "webhook delivery", func() bool { return len(rx.deliveries()) == 1 })
	d := rx.deliveries()[0]
	if d.event != "scan.completed" || d.body["title"] != "Scan finished" {
		t.Errorf("delivery = %+v", d)
	}
	if want := notify.Sign("s3cret", d.raw); d.signature != want {
		t.Errorf("signature = %q, want %q", d.signature, want)
	}
	waitFor(t, "delivery to be recorded", func() bool {
		var status *int
		s.DB.QueryRow(ctx, `SELECT last_status FROM webhook`).Scan(&status)
		return status != nil && *status == 200
	})
}

func TestWebhookEventFilteringAndDisabled(t *testing.T) {
	s, _ := newService(t)
	only, other, off := newReceiver(t), newReceiver(t), newReceiver(t)
	ctx := context.Background()
	s.DB.Exec(ctx, `INSERT INTO webhook (name, url, events, enabled) VALUES ('only-fail', $1, '{job.failed}', true), ('scan', $2, '{scan.completed}', true), ('disabled', $3, '{}', false)`,
		only.URL, other.URL, off.URL)

	s.Emit(ctx, notify.Event{Kind: notify.JobFailed, Title: "Boom"})

	waitFor(t, "matching webhook", func() bool { return len(only.deliveries()) == 1 })
	time.Sleep(150 * time.Millisecond)
	if n := len(other.deliveries()); n != 0 {
		t.Errorf("webhook subscribed to another event got %d deliveries", n)
	}
	if n := len(off.deliveries()); n != 0 {
		t.Errorf("disabled webhook got %d deliveries", n)
	}
}

func TestWebhookUnsignedWithoutSecretAndRecordsFailure(t *testing.T) {
	s, _ := newService(t)
	rx := newReceiver(t)
	rx.code = 500
	ctx := context.Background()
	var id int64
	s.DB.QueryRow(ctx, `INSERT INTO webhook (name, url, events, enabled) VALUES ('n', $1, '{}', true) RETURNING id`, rx.URL).Scan(&id)

	status, err := s.Test(ctx, notify.Webhook{ID: id, Name: "n", URL: rx.URL})
	if err == nil || status != 500 {
		t.Fatalf("Test = %d, %v; want a 500 error", status, err)
	}
	d := rx.deliveries()[0]
	if d.signature != "" {
		t.Errorf("no secret configured, but signature = %q", d.signature)
	}
	if d.event != "webhook.test" {
		t.Errorf("test event = %q", d.event)
	}
	var lastStatus *int
	var lastErr *string
	s.DB.QueryRow(ctx, `SELECT last_status, last_error FROM webhook WHERE id = $1`, id).Scan(&lastStatus, &lastErr)
	if lastStatus == nil || *lastStatus != 500 || lastErr == nil || *lastErr == "" {
		t.Errorf("failure not recorded: %v %v", lastStatus, lastErr)
	}

	// Recovery clears the error.
	rx.mu.Lock()
	rx.code = 204
	rx.mu.Unlock()
	if status, err := s.Test(ctx, notify.Webhook{ID: id, URL: rx.URL}); err != nil || status != 204 {
		t.Fatalf("Test = %d, %v", status, err)
	}
	s.DB.QueryRow(ctx, `SELECT last_status, last_error FROM webhook WHERE id = $1`, id).Scan(&lastStatus, &lastErr)
	if lastErr != nil {
		t.Errorf("last_error should clear after a good delivery, got %q", *lastErr)
	}
}

func TestWebhookUnreachable(t *testing.T) {
	s, _ := newService(t)
	rx := newReceiver(t)
	url := rx.URL
	rx.Close()
	if _, err := s.Test(context.Background(), notify.Webhook{URL: url}); err == nil {
		t.Error("an unreachable endpoint must report an error")
	}
	if _, err := s.Test(context.Background(), notify.Webhook{URL: "http://\x7f"}); err == nil {
		t.Error("an invalid URL must report an error")
	}
}

func TestJobFinishedNotifications(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	var loc int64
	s.DB.QueryRow(ctx, `INSERT INTO storage_location (name, type, mount_path) VALUES ('Movies Disk', 'LOCAL', $1) RETURNING id`, "/mnt/"+uuid.NewString()).Scan(&loc)
	payload, _ := json.Marshal(map[string]any{"storageLocationId": loc})
	res := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

	cases := []struct {
		name string
		f    jobs.Finished
		want *[3]string // kind, title, body; nil = no notification
	}{
		{"scan ok", jobs.Finished{Kind: "SCAN", Status: "COMPLETED", Payload: payload, Result: res(map[string]int{"discovered": 3, "changed": 1, "missing": 2})},
			&[3]string{"scan.completed", "Scan finished", "Movies Disk: 3 new, 1 changed, 2 missing"}},
		{"scan failed", jobs.Finished{Kind: "SCAN", Status: "FAILED", Error: "disk offline", Payload: payload},
			&[3]string{"scan.failed", "Scan failed", "Movies Disk: disk offline"}},
		{"scan without location", jobs.Finished{Kind: "SCAN", Status: "FAILED", Error: "x"},
			&[3]string{"scan.failed", "Scan failed", "x"}},
		{"match ok", jobs.Finished{Kind: "METADATA_MATCH", Status: "COMPLETED", Result: res(map[string]int{"matched": 4, "total": 5})},
			&[3]string{"metadata.matched", "Metadata matching finished", "4 of 5 titles matched"}},
		{"match nothing to do", jobs.Finished{Kind: "METADATA_MATCH", Status: "COMPLETED", Result: res(map[string]int{"total": 0})}, nil},
		{"match failed", jobs.Finished{Kind: "METADATA_MATCH", Status: "FAILED", Error: "no provider"},
			&[3]string{"job.failed", "Metadata matching failed", "no provider"}},
		{"organize ok", jobs.Finished{Kind: "ORGANIZE", Status: "COMPLETED", Result: res(map[string]int{"total": 7})},
			&[3]string{"job.completed", "Organize files finished", ""}},
		{"organize empty", jobs.Finished{Kind: "ORGANIZE", Status: "COMPLETED", Result: res(map[string]int{"total": 0})}, nil},
		{"organize failed", jobs.Finished{Kind: "ORGANIZE", Status: "FAILED", Error: "perm"},
			&[3]string{"job.failed", "Organize files failed", "perm"}},
		{"housekeeping kinds are silent", jobs.Finished{Kind: "THUMBNAIL", Status: "COMPLETED"}, nil},
		{"unknown failing kind is silent", jobs.Finished{Kind: "WHATEVER", Status: "FAILED", Error: "x"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s.DB.Exec(ctx, `DELETE FROM notification`)
			s.JobFinished(c.f)
			rows := notifications(t, s)
			if c.want == nil {
				if len(rows) != 0 {
					t.Fatalf("expected no notification, got %v", rows)
				}
				return
			}
			if len(rows) != 1 {
				t.Fatalf("got %d notifications: %v", len(rows), rows)
			}
			r := rows[0]
			if r["kind"] != c.want[0] || r["title"] != c.want[1] || r["body"] != c.want[2] || r["admin"] != true {
				t.Errorf("notification = %v, want %v (admin-only)", r, c.want)
			}
		})
	}
}

func TestOldNotificationsArePruned(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	s.DB.Exec(ctx, `INSERT INTO notification (kind, title, admin_only, created_at) VALUES ('job.completed', 'ancient', false, now() - interval '61 days')`)
	s.Emit(ctx, notify.Event{Kind: notify.JobCompleted, Title: "fresh"})
	titles := map[string]bool{}
	for _, r := range notifications(t, s) {
		titles[r["title"].(string)] = true
	}
	if titles["ancient"] || !titles["fresh"] {
		t.Errorf("after prune: %v", titles)
	}
}
