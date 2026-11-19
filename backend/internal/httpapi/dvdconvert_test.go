package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestDVDConvertRequiresAScan(t *testing.T) {
	h := newHarness(t)
	root := t.TempDir()
	var disk int64
	if err := h.pool.QueryRow(context.Background(), `INSERT INTO storage_location (name, type, mount_path) VALUES ('dvd', 'LOCAL', $1) RETURNING id`, root).Scan(&disk); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		h.pool.Exec(context.Background(), `DELETE FROM storage_location WHERE id = $1`, disk)
	})

	if rec := h.do(&h.user, http.MethodGet, "/api/admin/dvds", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("user list: %d", rec.Code)
	}
	if rec := h.do(&h.user, http.MethodPost, "/api/admin/dvds/convert", map[string]any{"ids": []any{}}); rec.Code != http.StatusForbidden {
		t.Fatalf("user convert: %d", rec.Code)
	}
	if rec := h.do(&h.admin, http.MethodPost, "/api/admin/dvds/convert", map[string]any{"ids": []any{}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty: %d %s", rec.Code, rec.Body.String())
	}
	var missing int64
	if err := h.pool.QueryRow(context.Background(), `SELECT COALESCE(MAX(id), 0) + 1000000 FROM dvd_disc`).Scan(&missing); err != nil {
		t.Fatal(err)
	}
	if rec := h.do(&h.admin, http.MethodPost, "/api/admin/dvds/convert", map[string]any{"ids": []int64{missing}}); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown: %d %s", rec.Code, rec.Body.String())
	}
	// A folder on disk is not enough. Conversion only accepts a disc a scan recorded.
	if rec := h.do(&h.admin, http.MethodPost, "/api/admin/files/convert", map[string]any{
		"items": []map[string]any{{"storageLocationId": disk, "path": "DADDY_RIDE_ALONG/VIDEO_TS"}},
	}); rec.Code != http.StatusNotFound {
		t.Fatalf("files convert: %d %s", rec.Code, rec.Body.String())
	}

	vts := filepath.Join(root, "DADDY_RIDE_ALONG", "VIDEO_TS")
	if err := os.MkdirAll(vts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vts, "VTS_01_1.VOB"), []byte("title"), 0o644); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := h.pool.QueryRow(context.Background(), `
		INSERT INTO dvd_disc (storage_location_id, relative_path, title_count, file_size)
		VALUES ($1, 'DADDY_RIDE_ALONG/VIDEO_TS', 1, 5) RETURNING id
	`, disk).Scan(&id); err != nil {
		t.Fatal(err)
	}

	rec := h.do(&h.admin, http.MethodGet, "/api/admin/dvds", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			Path   string `json:"path"`
			OnDisk bool   `json:"onDisk"`
			Titles int    `json:"titles"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, item := range body.Items {
		if item.ID != id {
			continue
		}
		found = item.Name == "DADDY_RIDE_ALONG" && item.Path == "DADDY_RIDE_ALONG/VIDEO_TS" && item.OnDisk && item.Titles == 1
	}
	if !found {
		t.Fatalf("list = %+v", body.Items)
	}

	// The test router has no job runner, so a recorded disc is refused before any encode starts.
	if rec := h.do(&h.admin, http.MethodPost, "/api/admin/dvds/convert", map[string]any{"ids": []int64{id}}); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("convert: %d %s", rec.Code, rec.Body.String())
	}
}
