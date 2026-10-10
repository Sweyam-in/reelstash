package httpapi

import (
	"encoding/json"
	"net/http"
)

// libraryExport is one person's library data, keyed by title uid so it can
// be loaded on another server that has the same files.
type libraryExport struct {
	Version    int              `json:"version"`
	Favorites  []string         `json:"favorites"`
	WatchLater []string         `json:"watchLater"`
	Ratings    []exportRating   `json:"ratings"`
	Playback   []exportPlayback `json:"playback"`
	Playlists  []exportPlaylist `json:"playlists"`
}

type exportRating struct {
	UID    string `json:"uid"`
	Rating int    `json:"rating"`
}

type exportPlayback struct {
	UID             string  `json:"uid"`
	PositionSeconds float64 `json:"positionSeconds"`
	Completed       bool    `json:"completed"`
}

type exportPlaylist struct {
	Name  string         `json:"name"`
	Kind  string         `json:"kind"`
	Rules map[string]any `json:"rules,omitempty"`
	Items []exportItem   `json:"items,omitempty"`
}

type exportItem struct {
	UID      string `json:"uid"`
	Position int    `json:"position"`
}

func (a *API) exportLibrary(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)
	out := libraryExport{Version: 1, Favorites: []string{}, WatchLater: []string{}, Ratings: []exportRating{}, Playback: []exportPlayback{}, Playlists: []exportPlaylist{}}
	collect := func(query string, dest *[]string) error {
		rows, err := a.DB.Query(r.Context(), query, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var uid string
			if err := rows.Scan(&uid); err != nil {
				return err
			}
			*dest = append(*dest, uid)
		}
		return rows.Err()
	}
	if err := collect(`SELECT mi.uid::text FROM media_item_favorite f JOIN media_item mi ON mi.id = f.media_item_id WHERE f.user_id = $1 ORDER BY mi.uid`, &out.Favorites); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to export favorites")
		return
	}
	if err := collect(`SELECT mi.uid::text FROM watch_later w JOIN media_item mi ON mi.id = w.media_item_id WHERE w.user_id = $1 ORDER BY mi.uid`, &out.WatchLater); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to export watch later")
		return
	}
	rows, err := a.DB.Query(r.Context(), `SELECT mi.uid::text, r.rating FROM media_item_rating r JOIN media_item mi ON mi.id = r.media_item_id WHERE r.user_id = $1 ORDER BY mi.uid`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to export ratings")
		return
	}
	for rows.Next() {
		var item exportRating
		if err := rows.Scan(&item.UID, &item.Rating); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "failed to export ratings")
			return
		}
		out.Ratings = append(out.Ratings, item)
	}
	rows.Close()
	rows, err = a.DB.Query(r.Context(), `SELECT mi.uid::text, p.position_seconds, p.completed FROM playback_state p JOIN media_item mi ON mi.id = p.media_item_id WHERE p.user_id = $1 ORDER BY mi.uid`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to export playback")
		return
	}
	for rows.Next() {
		var item exportPlayback
		if err := rows.Scan(&item.UID, &item.PositionSeconds, &item.Completed); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "failed to export playback")
			return
		}
		out.Playback = append(out.Playback, item)
	}
	rows.Close()
	playlists, err := a.DB.Query(r.Context(), `SELECT id, name, kind, rules FROM collection WHERE user_id = $1 ORDER BY name`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to export playlists")
		return
	}
	defer playlists.Close()
	for playlists.Next() {
		var id int64
		var item exportPlaylist
		var rules json.RawMessage
		if err := playlists.Scan(&id, &item.Name, &item.Kind, &rules); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to export playlists")
			return
		}
		if len(rules) > 0 {
			_ = json.Unmarshal(rules, &item.Rules)
		}
		items, err := a.DB.Query(r.Context(), `SELECT mi.uid::text, c.position FROM collection_media_item c JOIN media_item mi ON mi.id = c.media_item_id WHERE c.collection_id = $1 ORDER BY c.position`, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to export playlists")
			return
		}
		item.Items = []exportItem{}
		for items.Next() {
			var entry exportItem
			if err := items.Scan(&entry.UID, &entry.Position); err != nil {
				items.Close()
				writeError(w, http.StatusInternalServerError, "failed to export playlists")
				return
			}
			item.Items = append(item.Items, entry)
		}
		items.Close()
		out.Playlists = append(out.Playlists, item)
	}
	w.Header().Set("Content-Disposition", `attachment; filename="reelstash-library.json"`)
	writeJSON(w, http.StatusOK, out)
}

func (a *API) importLibrary(w http.ResponseWriter, r *http.Request) {
	var in libraryExport
	if err := decodeJSON(r, &in); err != nil || in.Version != 1 {
		writeError(w, http.StatusBadRequest, "this file is not a ReelStash library export")
		return
	}
	userID := currentUserID(r)
	ctx := r.Context()
	idOf := func(uid string) (int64, bool) {
		var id int64
		err := a.DB.QueryRow(ctx, `SELECT id FROM media_item WHERE uid = $1::uuid`, uid).Scan(&id)
		return id, err == nil
	}
	skipped := 0
	apply := func(uid string) (int64, bool) {
		id, ok := idOf(uid)
		if !ok {
			skipped++
		}
		return id, ok
	}
	for _, uid := range in.Favorites {
		if id, ok := apply(uid); ok {
			_, _ = a.DB.Exec(ctx, `INSERT INTO media_item_favorite (user_id, media_item_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, id)
		}
	}
	for _, uid := range in.WatchLater {
		if id, ok := apply(uid); ok {
			_, _ = a.DB.Exec(ctx, `INSERT INTO watch_later (user_id, media_item_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, id)
		}
	}
	for _, item := range in.Ratings {
		if id, ok := apply(item.UID); ok && item.Rating >= 1 && item.Rating <= 10 {
			_, _ = a.DB.Exec(ctx, `
				INSERT INTO media_item_rating (user_id, media_item_id, rating) VALUES ($1, $2, $3)
				ON CONFLICT (user_id, media_item_id) DO UPDATE SET rating = EXCLUDED.rating, updated_at = now()`, userID, id, item.Rating)
		}
	}
	for _, item := range in.Playback {
		if id, ok := apply(item.UID); ok {
			_, _ = a.DB.Exec(ctx, `
				INSERT INTO playback_state (user_id, media_item_id, position_seconds, completed) VALUES ($1, $2, $3, $4)
				ON CONFLICT (user_id, media_item_id) DO UPDATE SET position_seconds = EXCLUDED.position_seconds, completed = EXCLUDED.completed, updated_at = now()`,
				userID, id, item.PositionSeconds, item.Completed)
		}
	}
	for _, list := range in.Playlists {
		if list.Name == "" || (list.Kind != "MANUAL" && list.Kind != "SMART") {
			continue
		}
		rules, _ := json.Marshal(list.Rules)
		var id int64
		err := a.DB.QueryRow(ctx, `SELECT id FROM collection WHERE user_id = $1 AND name = $2`, userID, list.Name).Scan(&id)
		if err != nil {
			err = a.DB.QueryRow(ctx, `INSERT INTO collection (name, kind, rules, user_id) VALUES ($1, $2, $3, $4) RETURNING id`, list.Name, list.Kind, rules, userID).Scan(&id)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to import a playlist")
			return
		}
		for _, item := range list.Items {
			if mediaID, ok := apply(item.UID); ok {
				_, _ = a.DB.Exec(ctx, `
					INSERT INTO collection_media_item (collection_id, media_item_id, position) VALUES ($1, $2, $3)
					ON CONFLICT (collection_id, media_item_id) DO UPDATE SET position = EXCLUDED.position`, id, mediaID, item.Position)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "skipped": skipped})
}
