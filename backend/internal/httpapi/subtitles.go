package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"reelstash/backend/internal/lang"
	"reelstash/backend/internal/provider"
	"reelstash/backend/internal/scanner"
	"reelstash/backend/internal/subtitles"
)

// openSubtitlesID keys the OpenSubtitles settings in app_setting. It is not a
// metadata provider, so it is kept apart from the provider registry.
const openSubtitlesID = "subtitles.opensubtitles"

var openSubtitlesFields = []provider.Field{
	{Key: "apiKey", Label: "API key", Secret: true, Placeholder: "From opensubtitles.com → your profile → API consumers"},
	{Key: "username", Label: "Username", Placeholder: "Needed to download"},
	{Key: "password", Label: "Password", Secret: true, Placeholder: "Needed to download"},
	{Key: "languages", Label: "Languages to search", Placeholder: "en, es (empty: your subtitle language)"},
}

// subtitlesBaseURL lets tests point the client at a fake server.
var subtitlesBaseURL = ""

func (a *API) openSubtitles(ctx context.Context) (*subtitles.Client, subtitles.Config, map[string]string) {
	saved := provider.LoadConfig(ctx, a.DB, openSubtitlesID)
	cfg := subtitles.Config{APIKey: saved["apiKey"], Username: saved["username"], Password: saved["password"], BaseURL: subtitlesBaseURL}
	return subtitles.New(cfg), cfg, saved
}

func openSubtitlesStatus(cfg subtitles.Config, saved map[string]string) map[string]any {
	fields := []map[string]any{}
	for _, f := range openSubtitlesFields {
		value := saved[f.Key]
		if f.Secret {
			value = maskToken(value)
		}
		fields = append(fields, map[string]any{
			"key": f.Key, "label": f.Label, "secret": f.Secret, "placeholder": f.Placeholder, "value": value, "set": saved[f.Key] != "",
		})
	}
	return map[string]any{
		"id": "opensubtitles", "name": "OpenSubtitles.com",
		"enabled": cfg.Enabled(), "canDownload": cfg.CanDownload(),
		"languages": subtitles.ParseLanguages(saved["languages"]),
		"fields":    fields,
	}
}

// adminSubtitleProvider shows the OpenSubtitles settings, secrets masked.
func (a *API) adminSubtitleProvider(w http.ResponseWriter, r *http.Request) {
	_, cfg, saved := a.openSubtitles(r.Context())
	writeJSON(w, http.StatusOK, openSubtitlesStatus(cfg, saved))
}

// updateSubtitleProvider merges the submitted fields into the saved settings:
// an absent field is left alone, an empty string clears it.
func (a *API) updateSubtitleProvider(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	allowed := map[string]bool{}
	for _, f := range openSubtitlesFields {
		allowed[f.Key] = true
	}
	merged := provider.LoadConfig(r.Context(), a.DB, openSubtitlesID)
	for k, v := range req {
		if !allowed[k] {
			writeError(w, http.StatusBadRequest, "unknown setting "+k)
			return
		}
		if k == "languages" {
			v = strings.Join(subtitles.ParseLanguages(v), ", ")
		}
		merged[k] = v
	}
	if err := provider.SaveConfig(r.Context(), a.DB, openSubtitlesID, merged); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save settings")
		return
	}
	subtitles.ForgetSessions()
	_, cfg, saved := a.openSubtitles(r.Context())
	writeJSON(w, http.StatusOK, openSubtitlesStatus(cfg, saved))
}

func (a *API) deleteSubtitleProvider(w http.ResponseWriter, r *http.Request) {
	if err := provider.ClearConfig(r.Context(), a.DB, openSubtitlesID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear settings")
		return
	}
	subtitles.ForgetSessions()
	_, cfg, saved := a.openSubtitles(r.Context())
	writeJSON(w, http.StatusOK, openSubtitlesStatus(cfg, saved))
}

// testSubtitleProvider checks the saved key (and account, when saved).
func (a *API) testSubtitleProvider(w http.ResponseWriter, r *http.Request) {
	client, _, _ := a.openSubtitles(r.Context())
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	account, err := client.Test(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "account": account})
}

// subtitleLanguages is what a search asks for when the request names none:
// the saved provider languages, else the admin's own subtitle language.
func (a *API) subtitleLanguages(r *http.Request, saved map[string]string) []string {
	if q := subtitles.ParseLanguages(r.URL.Query().Get("languages")); len(q) > 0 {
		return q
	}
	if l := subtitles.ParseLanguages(saved["languages"]); len(l) > 0 {
		return l
	}
	var pref *string
	_ = a.DB.QueryRow(r.Context(), `SELECT preferred_subtitle_language FROM app_user WHERE id = $1`, currentUserID(r)).Scan(&pref)
	if pref != nil && *pref != "" {
		return []string{*pref}
	}
	return []string{"en"}
}

func subtitleProviderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, subtitles.ErrNotConfigured), errors.Is(err, subtitles.ErrNoAccount):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusBadGateway, err.Error())
	}
}

// searchMediaSubtitles looks a title up on OpenSubtitles, by the file's hash
// and the given text (the title by default).
func (a *API) searchMediaSubtitles(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	src, status, msg := a.resolveMediaFile(r, id)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	client, cfg, saved := a.openSubtitles(r.Context())
	if !cfg.Enabled() {
		subtitleProviderError(w, subtitles.ErrNotConfigured)
		return
	}
	text := strings.TrimSpace(r.URL.Query().Get("q"))
	if text == "" {
		_ = a.DB.QueryRow(r.Context(), `SELECT title FROM media_item WHERE id = $1`, id).Scan(&text)
	}
	q := subtitles.Query{Text: text, Languages: a.subtitleLanguages(r, saved)}
	if r.URL.Query().Get("hash") != "false" {
		if h, err := subtitles.FileHash(src.FullPath); err == nil {
			q.MovieHash = h
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	results, err := client.Search(ctx, q)
	if err != nil {
		subtitleProviderError(w, err)
		return
	}
	for i := range results {
		if code := lang.Normalize(results[i].Language); code != "" {
			results[i].Language = code
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"query": text, "languages": q.Languages, "hashed": q.MovieHash != "", "canDownload": cfg.CanDownload(), "items": results,
	})
}

type subtitleDownloadRequest struct {
	FileID          int64  `json:"fileId"`
	Language        string `json:"language"`
	HearingImpaired bool   `json:"hearingImpaired"`
	ForeignOnly     bool   `json:"foreignPartsOnly"`
}

// downloadMediaSubtitle fetches a subtitle from OpenSubtitles and saves it
// next to the video (Movie.en.srt), where the player, other media servers
// and the organizer all find it.
func (a *API) downloadMediaSubtitle(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	var req subtitleDownloadRequest
	if err := decodeJSON(r, &req); err != nil || req.FileID <= 0 {
		writeError(w, http.StatusBadRequest, "fileId is required")
		return
	}
	src, status, msg := a.resolveMediaFile(r, id)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	client, cfg, _ := a.openSubtitles(r.Context())
	if !cfg.CanDownload() {
		subtitleProviderError(w, subtitles.ErrNoAccount)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	data, _, remaining, err := client.Download(ctx, req.FileID)
	if err != nil {
		subtitleProviderError(w, err)
		return
	}
	var tags []string
	if code := lang.Normalize(req.Language); code != "" {
		tags = append(tags, code)
	}
	if req.HearingImpaired {
		tags = append(tags, "sdh")
	}
	if req.ForeignOnly {
		tags = append(tags, "forced")
	}
	path, err := scanner.WriteSidecar(src.FullPath, tags, ".srt", data)
	if err != nil {
		log.Printf("subtitles: save for file %d: %v", src.FileID, err)
		reason := err.Error()
		var pe *os.PathError
		var le *os.LinkError
		if errors.As(err, &pe) {
			reason = pe.Err.Error()
		} else if errors.As(err, &le) {
			reason = le.Err.Error()
		}
		writeError(w, http.StatusInternalServerError, "could not save the subtitle next to the video: "+reason)
		return
	}
	sidecars := scanner.FindSidecars(src.FullPath)
	_ = scanner.SaveSidecarCount(r.Context(), a.DB, src.FileID, len(sidecars))
	trackID := ""
	for _, sc := range sidecars {
		if sc.Path == path {
			trackID = sc.ID
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"trackId": trackID, "fileName": filepath.Base(path), "remaining": remaining})
}

// deleteMediaSubtitle removes a subtitle file lying next to the video.
// Subtitles inside the video file cannot be removed.
func (a *API) deleteMediaSubtitle(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	track := chi.URLParam(r, "track")
	if !strings.HasPrefix(track, "x") {
		writeError(w, http.StatusBadRequest, "only subtitle files beside the video can be removed")
		return
	}
	src, status, msg := a.resolveMediaFile(r, id)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	sidecars := scanner.FindSidecars(src.FullPath)
	var found *scanner.Sidecar
	for i := range sidecars {
		if sidecars[i].ID == track {
			found = &sidecars[i]
		}
	}
	if found == nil {
		writeError(w, http.StatusNotFound, "subtitle not found")
		return
	}
	if err := os.Remove(found.Path); err != nil && !os.IsNotExist(err) {
		log.Printf("subtitles: remove %s: %v", found.Path, err)
		writeError(w, http.StatusInternalServerError, "could not remove the subtitle file")
		return
	}
	_ = scanner.SaveSidecarCount(r.Context(), a.DB, src.FileID, len(sidecars)-1)
	w.WriteHeader(http.StatusNoContent)
}

// refreshMediaTracks reads the file's tracks again and returns them.
func (a *API) refreshMediaTracks(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	src, status, msg := a.resolveMediaFile(r, id)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	pr, err := scanner.Probe(ctx, src.FullPath)
	if err != nil {
		log.Printf("tracks: re-read file %d: %v", src.FileID, err)
		writeError(w, http.StatusUnprocessableEntity, "could not read the video's tracks")
		return
	}
	if err := scanner.SaveTracks(r.Context(), a.DB, src.FileID, pr.Tracks, true); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save tracks")
		return
	}
	_ = scanner.SaveRotation(r.Context(), a.DB, src.FileID, pr)
	a.mediaTracks(w, r)
}
