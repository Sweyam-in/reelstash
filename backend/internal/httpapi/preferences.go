package httpapi

import (
	"net/http"
	"strings"

	"reelstash/backend/internal/lang"
)

type preferencesBody struct {
	AudioLanguage    *string `json:"audioLanguage"`
	SubtitleLanguage *string `json:"subtitleLanguage"`
	SubtitleMode     string  `json:"subtitleMode"`
}

// getPreferences returns the current user's playback language defaults.
func (a *API) getPreferences(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFromContext(r.Context())
	var p preferencesBody
	err := a.DB.QueryRow(r.Context(), `
		SELECT preferred_audio_language, preferred_subtitle_language, subtitle_mode FROM app_user WHERE id = $1
	`, user.ID).Scan(&p.AudioLanguage, &p.SubtitleLanguage, &p.SubtitleMode)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load preferences")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"audioLanguage": p.AudioLanguage, "subtitleLanguage": p.SubtitleLanguage, "subtitleMode": p.SubtitleMode,
		"languages": lang.Options(),
	})
}

// cleanLanguage turns a submitted language into a stored code: empty or null
// clears the preference, anything unrecognisable is refused.
func cleanLanguage(v *string) (*string, bool) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil, true
	}
	code := lang.Normalize(*v)
	if code == "" {
		return nil, false
	}
	return &code, true
}

// updatePreferences saves the current user's playback language defaults:
// which audio language to start in, which subtitles to show, and when.
func (a *API) updatePreferences(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFromContext(r.Context())
	var req preferencesBody
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	audio, ok1 := cleanLanguage(req.AudioLanguage)
	subs, ok2 := cleanLanguage(req.SubtitleLanguage)
	if !ok1 || !ok2 {
		writeError(w, http.StatusBadRequest, "unknown language")
		return
	}
	switch req.SubtitleMode {
	case "off", "on", "foreign":
	case "":
		req.SubtitleMode = "off"
	default:
		writeError(w, http.StatusBadRequest, "subtitleMode must be off, on or foreign")
		return
	}
	if req.SubtitleMode != "off" && subs == nil {
		writeError(w, http.StatusBadRequest, "choose a subtitle language first")
		return
	}
	if _, err := a.DB.Exec(r.Context(), `
		UPDATE app_user SET preferred_audio_language = $2, preferred_subtitle_language = $3, subtitle_mode = $4, updated_at = now() WHERE id = $1
	`, user.ID, audio, subs, req.SubtitleMode); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save preferences")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audioLanguage": audio, "subtitleLanguage": subs, "subtitleMode": req.SubtitleMode})
}
