// Package notify records in-app notifications, pushes them to connected
// browsers, and delivers them to configured webhooks.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/events"
	"reelstash/backend/internal/jobs"
)

// Kind is the identifier webhooks subscribe to.
type Kind string

const (
	ScanCompleted    Kind = "scan.completed"
	ScanFailed       Kind = "scan.failed"
	MetadataMatched  Kind = "metadata.matched"
	JobCompleted     Kind = "job.completed"
	JobFailed        Kind = "job.failed"
	DownloadMoved    Kind = "download.moved"
	DownloadFailed   Kind = "download.failed"
	webhookTestEvent Kind = "webhook.test"
)

// KindInfo describes an event a webhook can subscribe to.
type KindInfo struct {
	Kind  Kind   `json:"kind"`
	Label string `json:"label"`
}

// Kinds lists every event a webhook can subscribe to.
var Kinds = []KindInfo{
	{ScanCompleted, "Scan finished"},
	{ScanFailed, "Scan failed"},
	{MetadataMatched, "Metadata matching finished"},
	{JobCompleted, "Other job finished"},
	{JobFailed, "Job failed"},
	{DownloadMoved, "Download moved into place"},
	{DownloadFailed, "Download could not be moved"},
}

// Event is one thing worth telling people about.
type Event struct {
	Kind      Kind           `json:"event"`
	Title     string         `json:"title"`
	Body      string         `json:"body,omitempty"`
	Link      string         `json:"link,omitempty"`
	AdminOnly bool           `json:"-"`
	Data      map[string]any `json:"data,omitempty"`
}

type Service struct {
	DB  *pgxpool.Pool
	Hub *events.Hub
}

func New(db *pgxpool.Pool, hub *events.Hub) *Service {
	return &Service{DB: db, Hub: hub}
}

// Row is a stored notification as the API returns it.
type Row struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Body      string    `json:"body,omitempty"`
	Link      string    `json:"link,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Emit stores the notification, pushes it to browsers, and fans it out to webhooks.
func (s *Service) Emit(ctx context.Context, ev Event) {
	var row Row
	err := s.DB.QueryRow(ctx, `
		INSERT INTO notification (kind, title, body, link, admin_only)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5)
		RETURNING id, kind, title, COALESCE(body, ''), COALESCE(link, ''), created_at
	`, string(ev.Kind), ev.Title, ev.Body, ev.Link, ev.AdminOnly).
		Scan(&row.ID, &row.Kind, &row.Title, &row.Body, &row.Link, &row.CreatedAt)
	if err != nil {
		log.Printf("notify: store notification: %v", err)
	} else {
		s.Hub.Publish("notification", row, ev.AdminOnly)
		// Keep the table small: nobody scrolls back further than this.
		s.DB.Exec(ctx, `DELETE FROM notification WHERE created_at < now() - interval '60 days'`)
	}
	s.dispatchWebhooks(ev)
}

// jobTitles names the job kinds that notify; per-item housekeeping kinds
// (thumbnails, single-file matches, ...) are deliberately absent so a big
// scan does not produce hundreds of notifications.
var jobTitles = map[string]string{
	"ORGANIZE":       "Organize files",
	"DUPLICATE_SCAN": "Duplicate scan",
	"PERFORMER_SYNC": "Performer sync",
	"STUDIO_SYNC":    "Studio sync",
	"SPRITE_SCAN":    "Scrub previews",
	"PREVIEW_SCAN":   "Hover previews",
	"THUMBNAIL_FIX":  "Thumbnail repair",
	"REMOVE_MISSING": "Remove missing titles",
	"LINK_RECOVERY":  "Link recovery",
}

// JobFinished turns a finished background job into a notification when it is
// one people care about.
func (s *Service) JobFinished(f jobs.Finished) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result := map[string]any{}
	if len(f.Result) > 0 {
		_ = json.Unmarshal(f.Result, &result)
	}
	num := func(key string) int {
		if v, ok := result[key].(float64); ok {
			return int(v)
		}
		return 0
	}

	switch f.Kind {
	case "SCAN":
		location := s.locationName(ctx, f.Payload)
		if f.Status == "FAILED" {
			s.Emit(ctx, Event{Kind: ScanFailed, Title: "Scan failed", Body: joinNonEmpty(": ", location, f.Error), Link: "/admin/jobs", AdminOnly: true, Data: map[string]any{"error": f.Error, "location": location}})
			return
		}
		body := fmt.Sprintf("%d new, %d changed, %d missing", num("discovered"), num("changed"), num("missing"))
		s.Emit(ctx, Event{Kind: ScanCompleted, Title: "Scan finished", Body: joinNonEmpty(": ", location, body), Link: "/admin/jobs", AdminOnly: true,
			Data: map[string]any{"location": location, "discovered": num("discovered"), "changed": num("changed"), "missing": num("missing")}})
	case "METADATA_MATCH":
		if f.Status == "FAILED" {
			s.Emit(ctx, Event{Kind: JobFailed, Title: "Metadata matching failed", Body: f.Error, Link: "/admin/jobs", AdminOnly: true, Data: map[string]any{"job": f.Kind, "error": f.Error}})
			return
		}
		total := num("total")
		if total == 0 {
			return
		}
		s.Emit(ctx, Event{Kind: MetadataMatched, Title: "Metadata matching finished", Body: fmt.Sprintf("%d of %d titles matched", num("matched"), total), Link: "/admin/review", AdminOnly: true,
			Data: map[string]any{"matched": num("matched"), "total": total, "failed": num("failed")}})
	default:
		title, ok := jobTitles[f.Kind]
		if !ok {
			return
		}
		if f.Status == "FAILED" {
			s.Emit(ctx, Event{Kind: JobFailed, Title: title + " failed", Body: f.Error, Link: "/admin/jobs", AdminOnly: true, Data: map[string]any{"job": f.Kind, "error": f.Error}})
			return
		}
		if total, has := result["total"].(float64); has && total == 0 {
			return // a bulk job that found nothing to do is not news
		}
		s.Emit(ctx, Event{Kind: JobCompleted, Title: title + " finished", Link: "/admin/jobs", AdminOnly: true, Data: map[string]any{"job": f.Kind, "result": result}})
	}
}

func (s *Service) locationName(ctx context.Context, payload json.RawMessage) string {
	var p struct {
		StorageLocationID int64 `json:"storageLocationId"`
	}
	if json.Unmarshal(payload, &p) != nil || p.StorageLocationID == 0 {
		return ""
	}
	var name string
	if err := s.DB.QueryRow(ctx, `SELECT name FROM storage_location WHERE id = $1`, p.StorageLocationID).Scan(&name); err != nil {
		return ""
	}
	return name
}

func joinNonEmpty(sep string, parts ...string) string {
	kept := parts[:0:0]
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}
