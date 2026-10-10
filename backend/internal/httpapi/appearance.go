package httpapi

import (
	"context"
	"net/http"
)

// mediaColorThemeSetting is the app_setting row for the media library palette.
// Ids must match COLOR_THEMES in ui/src/colorThemes.js.
const mediaColorThemeSetting = "media_color_theme"

var mediaColorThemeIDs = []string{"grove", "lagoon", "ember", "iris", "aurora"}

func validMediaColorTheme(id string) bool {
	for _, known := range mediaColorThemeIDs {
		if id == known {
			return true
		}
	}
	return false
}

func (a *API) mediaColorTheme(ctx context.Context) string {
	var value string
	err := a.DB.QueryRow(ctx, `SELECT value FROM app_setting WHERE key = $1`, mediaColorThemeSetting).Scan(&value)
	if err != nil || !validMediaColorTheme(value) {
		return mediaColorThemeIDs[0]
	}
	return value
}

func (a *API) getAppearance(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"colorTheme": a.mediaColorTheme(r.Context()),
		"themes":     mediaColorThemeIDs,
	})
}

func (a *API) updateAppearance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ColorTheme string `json:"colorTheme"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validMediaColorTheme(req.ColorTheme) {
		writeError(w, http.StatusBadRequest, "unknown color theme")
		return
	}
	if _, err := a.DB.Exec(r.Context(), `
		INSERT INTO app_setting (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
	`, mediaColorThemeSetting, req.ColorTheme); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the theme")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"colorTheme": req.ColorTheme})
}
