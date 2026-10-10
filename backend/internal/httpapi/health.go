package httpapi

import (
	"net/http"
	"os/exec"
	"time"

	"reelstash/backend/internal/diskinfo"
)

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := a.DB.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":   "unavailable",
			"database": "unreachable",
		})
		return
	}
	_, ffmpegErr := exec.LookPath("ffmpeg")
	_, ffprobeErr := exec.LookPath("ffprobe")
	ffmpeg := ffmpegErr == nil
	ffprobe := ffprobeErr == nil
	offline := []string{}
	rows, err := a.DB.Query(r.Context(), `SELECT name, mount_path, online FROM storage_location ORDER BY id`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var name, mount string
			var online bool
			if rows.Scan(&name, &mount, &online) != nil {
				continue
			}
			if _, err := diskinfo.HasEntries(mount, 2*time.Second); !online || err != nil {
				offline = append(offline, name)
			}
		}
	}
	status := "ok"
	if !ffmpeg || !ffprobe || len(offline) > 0 {
		status = "degraded"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         status,
		"database":       "ok",
		"ffmpeg":         ffmpeg,
		"ffprobe":        ffprobe,
		"offlineStorage": offline,
	})
}
