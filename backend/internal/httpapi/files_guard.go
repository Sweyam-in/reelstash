package httpapi

import (
	"net/http"

	"reelstash/backend/internal/pathguard"
)

// refuseChange writes 409 and reports true when renaming, moving or deleting
// abs would touch the downloads in-progress folder or a torrent qBittorrent
// is still using.
func (a *API) refuseChange(w http.ResponseWriter, r *http.Request, abs string) bool {
	if blocked, why := pathguard.Load(r.Context(), a.DB).Blocked(abs); blocked {
		writeError(w, http.StatusConflict, why)
		return true
	}
	return false
}

// refuseLocked writes 409 and reports true when abs is inside a locked
// folder, where nothing may be added.
func (a *API) refuseLocked(w http.ResponseWriter, r *http.Request, abs string) bool {
	if pathguard.Load(r.Context(), a.DB).Locked(abs) {
		writeError(w, http.StatusConflict, "this is inside the downloads in-progress folder, which ReelStash never changes")
		return true
	}
	return false
}

// guardMarks says why entries of a listing are protected: "locked" inside
// the in-progress folder, "torrent" for data qBittorrent is using.
func guardMark(g *pathguard.Guard, abs string) string {
	switch {
	case g.Locked(abs):
		return "locked"
	case g.Inside(abs):
		return "torrent"
	}
	return ""
}
