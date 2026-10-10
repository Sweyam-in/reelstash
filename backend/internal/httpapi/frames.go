package httpapi

import (
	"math"
	"net/http"
	"strconv"

	"reelstash/backend/internal/scanner"
)

// frameSem bounds concurrent ffmpeg frame grabs so a scrubbing slider cannot
// pile up processes.
var frameSem = make(chan struct{}, 2)

// clampSeconds keeps a requested position inside the video. An unknown
// duration (<= 0) only rules out negative and non-numeric values.
func clampSeconds(t, duration float64) float64 {
	if math.IsNaN(t) || math.IsInf(t, 0) || t < 0 {
		return 0
	}
	if duration > 0 && t > duration-0.1 {
		return math.Max(0, duration-0.1)
	}
	return t
}

func mediaIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := pathInt64(r, "id")
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return 0, false
	}
	return id, true
}

// mediaFrame returns one JPEG frame of the title's video, used by the poster
// picker to preview a position before it is chosen.
func (a *API) mediaFrame(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaIDParam(w, r)
	if !ok {
		return
	}
	src, status, msg := a.resolveMediaFile(r, id)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	if !ffmpegAvailable() {
		writeError(w, http.StatusNotImplemented, "ffmpeg is not installed on the server")
		return
	}
	t, _ := strconv.ParseFloat(r.URL.Query().Get("t"), 64)
	width := 640
	if v, err := strconv.Atoi(r.URL.Query().Get("w")); err == nil && v >= 160 && v <= 1280 {
		width = v
	}

	select {
	case frameSem <- struct{}{}:
		defer func() { <-frameSem }()
	case <-r.Context().Done():
		return
	}
	data, err := scanner.ExtractFrame(r.Context(), src.FullPath, clampSeconds(t, src.Duration), width)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not read a frame there")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(data)
}

// setPosterFromFrame makes a frame of the video the title's poster. With
// {"auto": true} it picks the first frame that is not black or flat;
// otherwise it uses {"seconds": n}. Like an uploaded poster, the result is
// never replaced by a provider match or a rescan.
func (a *API) setPosterFromFrame(w http.ResponseWriter, r *http.Request) {
	a.setArtworkFromFrame(w, r, false)
}

func (a *API) setBackdropFromFrame(w http.ResponseWriter, r *http.Request) {
	a.setArtworkFromFrame(w, r, true)
}

func (a *API) setArtworkFromFrame(w http.ResponseWriter, r *http.Request, backdrop bool) {
	id, ok := mediaIDParam(w, r)
	if !ok {
		return
	}
	var req struct {
		Seconds *float64 `json:"seconds"`
		Auto    bool     `json:"auto"`
	}
	if err := decodeJSON(r, &req); err != nil || (!req.Auto && req.Seconds == nil) {
		writeError(w, http.StatusBadRequest, "choose a position in the video, or ask for the best frame")
		return
	}
	if a.Namer == nil {
		writeError(w, http.StatusServiceUnavailable, "poster storage is not available")
		return
	}
	src, status, msg := a.resolveMediaFile(r, id)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	if !ffmpegAvailable() {
		writeError(w, http.StatusNotImplemented, "ffmpeg is not installed on the server")
		return
	}

	select {
	case frameSem <- struct{}{}:
		defer func() { <-frameSem }()
	case <-r.Context().Done():
		return
	}
	var (
		data    []byte
		seconds float64
		err     error
	)
	if req.Auto {
		data, seconds, err = scanner.PickBestFrame(r.Context(), src.FullPath, src.Duration, 1280)
	} else {
		seconds = clampSeconds(*req.Seconds, src.Duration)
		data, err = scanner.ExtractFrame(r.Context(), src.FullPath, seconds, 1280)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not read a frame there")
		return
	}
	var saveErr error
	if backdrop {
		saveErr = a.Namer.SetMediaBackdropUpload(r.Context(), id, data)
	} else {
		saveErr = a.Namer.SetMediaPosterUpload(r.Context(), id, data)
	}
	if err := saveErr; err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	column, field := "poster_image_id", "posterImageId"
	if backdrop {
		column, field = "backdrop_image_id", "backdropImageId"
	}
	var imageID int64
	_ = a.DB.QueryRow(r.Context(), `SELECT `+column+` FROM media_item WHERE id = $1`, id).Scan(&imageID)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, field: imageID, "seconds": seconds})
}
