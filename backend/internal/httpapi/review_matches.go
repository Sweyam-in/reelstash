package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type matchSuggestion struct {
	MediaItemID int64           `json:"mediaItemId"`
	MediaFileID int64           `json:"mediaFileId"`
	FileName    string          `json:"fileName"`
	Title       string          `json:"title"`
	PosterID    *int64          `json:"posterImageId"`
	Candidates  json.RawMessage `json:"candidates"`
	UpdatedAt   string          `json:"updatedAt"`
}

// listMatchSuggestions returns the titles waiting for a metadata match to be
// chosen, each with the candidates the providers suggested.
func (a *API) listMatchSuggestions(w http.ResponseWriter, r *http.Request) {
	limit, offset := parseLimitOffset(r)
	libraryID, _ := strconv.ParseInt(r.URL.Query().Get("libraryId"), 10, 64)
	var total int
	if err := a.DB.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM media_match_suggestion s JOIN media_item mi ON mi.id = s.media_item_id
		WHERE $1::bigint = 0 OR mi.library_id = $1`, libraryID).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load matches")
		return
	}
	rows, err := a.DB.Query(r.Context(), `
		SELECT s.media_item_id, COALESCE(f.id, 0), COALESCE(f.file_name, ''), mi.title, mi.poster_image_id, s.candidates, s.updated_at::text
		FROM media_match_suggestion s
		JOIN media_item mi ON mi.id = s.media_item_id
		LEFT JOIN LATERAL (
			SELECT id, file_name FROM media_file WHERE media_item_id = mi.id AND available
			ORDER BY height DESC NULLS LAST, file_size DESC LIMIT 1
		) f ON true
		WHERE $3::bigint = 0 OR mi.library_id = $3
		ORDER BY s.updated_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset, libraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load matches")
		return
	}
	defer rows.Close()
	items := []matchSuggestion{}
	for rows.Next() {
		var s matchSuggestion
		if err := rows.Scan(&s.MediaItemID, &s.MediaFileID, &s.FileName, &s.Title, &s.PosterID, &s.Candidates, &s.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read match")
			return
		}
		items = append(items, s)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

// dismissMatchSuggestion says none of the suggested candidates is right: they
// are marked as wrong matches so a later search does not offer them again.
func (a *API) dismissMatchSuggestion(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "mediaItemID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	var raw []byte
	if err := a.DB.QueryRow(r.Context(), `SELECT candidates FROM media_match_suggestion WHERE media_item_id = $1`, id).Scan(&raw); err != nil {
		writeError(w, http.StatusNotFound, "no suggestions for this title")
		return
	}
	var cands []struct {
		Provider   string `json:"provider"`
		ExternalID string `json:"externalId"`
	}
	_ = json.Unmarshal(raw, &cands)
	for _, c := range cands {
		if c.Provider == "" || c.ExternalID == "" {
			continue
		}
		a.DB.Exec(r.Context(), `INSERT INTO media_rejected_match (media_item_id, provider, scene_uuid) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, id, c.Provider, c.ExternalID)
	}
	if _, err := a.DB.Exec(r.Context(), `DELETE FROM media_match_suggestion WHERE media_item_id = $1`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to dismiss")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
