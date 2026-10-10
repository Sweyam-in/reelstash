package httpapi

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"reelstash/backend/internal/config"
)

func TestPosterArtworkUsesThumbnailWhenPosterFileIsMissing(t *testing.T) {
	thumbDir := t.TempDir()
	h := newHarnessCfg(t, config.Config{ThumbnailDir: thumbDir})
	id := h.newTitle("Missing poster")
	ctx := context.Background()
	var posterID int64
	if err := h.pool.QueryRow(ctx, `INSERT INTO artwork (kind, storage_key, mime_type) VALUES ('POSTER', 'gone.jpg', 'image/jpeg') RETURNING id`).Scan(&posterID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM artwork WHERE id = $1`, posterID) })
	if _, err := h.pool.Exec(ctx, `UPDATE media_item SET poster_image_id = $2 WHERE id = $1`, id, posterID); err != nil {
		t.Fatal(err)
	}
	thumbKey := "frame.jpg"
	if err := os.WriteFile(filepath.Join(thumbDir, thumbKey), []byte("thumb-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(ctx, `INSERT INTO thumbnail (media_item_id, storage_key, preferred) VALUES ($1, $2, true)`, id, thumbKey); err != nil {
		t.Fatal(err)
	}

	rec := h.do(&h.admin, http.MethodGet, "/api/media/"+itoa(id)+"/artwork?kind=poster", nil)
	body := h.expect(rec, http.StatusOK)
	if string(body) != "thumb-bytes" {
		t.Fatalf("body = %q", body)
	}
	if rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
}
