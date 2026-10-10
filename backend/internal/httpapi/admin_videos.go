package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"reelstash/backend/internal/scanner"
)

// adminVideo is one row of the admin Videos table: the title plus its best
// file, and where it stands in metadata matching.
type adminVideo struct {
	ID              int64           `json:"id"`
	Title           string          `json:"title"`
	PosterImageID   *int64          `json:"posterImageId,omitempty"`
	StudioName      *string         `json:"studioName,omitempty"`
	SeriesName      *string         `json:"seriesName,omitempty"`
	Performers      []string        `json:"performers"`
	DurationSeconds *float64        `json:"durationSeconds,omitempty"`
	DateAdded       time.Time       `json:"dateAdded"`
	MatchStatus     string          `json:"matchStatus"`
	MetadataSource  string          `json:"metadataSource"`
	Suggestions     int             `json:"suggestions"`
	FileCount       int             `json:"fileCount"`
	File            *adminVideoFile `json:"file,omitempty"`
}

type adminVideoFile struct {
	ID           int64   `json:"id"`
	FileName     string  `json:"fileName"`
	RelativePath string  `json:"relativePath"`
	StorageID    int64   `json:"storageLocationId"`
	StorageName  string  `json:"storageLocationName"`
	Size         int64   `json:"size"`
	Width        *int32  `json:"width,omitempty"`
	Height       *int32  `json:"height,omitempty"`
	VideoCodec   *string `json:"videoCodec,omitempty"`
	Available    bool    `json:"available"`
	// Tracks is nil until the file's tracks or subtitle files were read
	// (TRACK_SCAN, a scan of a new file, or the first time it plays).
	Tracks *adminVideoTracks `json:"tracks,omitempty"`
}

// adminVideoTracks counts a file's audio tracks and the subtitles the player
// can show (text tracks inside the file plus subtitle files beside it).
type adminVideoTracks struct {
	Audio     int `json:"audio"`
	Subtitles int `json:"subtitles"`
	// Image is how many picture-based subtitle tracks (PGS, VobSub) the file
	// has; browsers cannot show them.
	Image int `json:"image"`
}

// adminVideoCounts sizes the status tabs; each ignores the status filter but
// honours search, storage and missing filters.
type adminVideoCounts struct {
	All      int `json:"all"`
	Review   int `json:"review"`
	Filename int `json:"filename"`
	Matched  int `json:"matched"`
	Rejected int `json:"rejected"`
}

var adminVideoStatus = map[string]string{
	"review":   `EXISTS (SELECT 1 FROM media_match_suggestion ms WHERE ms.media_item_id = mi.id)`,
	"filename": `mi.metadata_source = 'FILENAME'`,
	"matched":  `mi.match_status = 'matched'`,
	"rejected": `mi.match_status = 'rejected'`,
}

var adminVideoMissing = map[string]string{
	"performers": `NOT EXISTS (SELECT 1 FROM media_item_performer mip WHERE mip.media_item_id = mi.id)`,
	"studio":     `mi.studio_id IS NULL`,
	"poster":     `mi.poster_image_id IS NULL`,
}

// adminVideoTracksFilter narrows the list by the audio and subtitle tracks of
// a title's files. $codecs is the list of text subtitle codecs.
var adminVideoTracksFilter = map[string]string{
	"subtitles": `EXISTS (SELECT 1 FROM media_file mf5 WHERE mf5.media_item_id = mi.id AND (COALESCE(mf5.sidecar_subtitles, 0) > 0
		OR EXISTS (SELECT 1 FROM media_track t WHERE t.media_file_id = mf5.id AND t.kind = 'subtitle' AND t.codec = ANY($codecs))))`,
	"nosubtitles": `EXISTS (SELECT 1 FROM media_file mf5 WHERE mf5.media_item_id = mi.id AND mf5.tracks_probed_at IS NOT NULL)
		AND NOT EXISTS (SELECT 1 FROM media_file mf5 WHERE mf5.media_item_id = mi.id AND (COALESCE(mf5.sidecar_subtitles, 0) > 0
		OR EXISTS (SELECT 1 FROM media_track t WHERE t.media_file_id = mf5.id AND t.kind = 'subtitle' AND t.codec = ANY($codecs))))`,
	"multiaudio": `EXISTS (SELECT 1 FROM media_track t JOIN media_file mf5 ON mf5.id = t.media_file_id
		WHERE mf5.media_item_id = mi.id AND t.kind = 'audio' AND t.kind_index = 1)`,
	"unread": `EXISTS (SELECT 1 FROM media_file mf5 WHERE mf5.media_item_id = mi.id AND mf5.available AND mf5.tracks_probed_at IS NULL)`,
}

var adminVideoSort = map[string]string{
	"":         `mi.date_added DESC, mi.id DESC`,
	"oldest":   `mi.date_added ASC, mi.id ASC`,
	"title":    `LOWER(mi.title) ASC, mi.id ASC`,
	"size":     `f.file_size DESC NULLS LAST, mi.id DESC`,
	"duration": `mi.duration_seconds DESC NULLS LAST, mi.id DESC`,
}

func (a *API) listAdminVideos(w http.ResponseWriter, r *http.Request) {
	limit, offset := parseLimitOffset(r)
	q := r.URL.Query()
	args := []any{}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }

	// The best file: available first, then highest resolution, then largest.
	from := `
		FROM media_item mi
		LEFT JOIN studio s ON s.id = mi.studio_id
		LEFT JOIN series se ON se.id = mi.series_id
		LEFT JOIN LATERAL (
			SELECT mf.id, mf.file_name, mf.relative_path, mf.storage_location_id, mf.file_size,
			       mf.width, mf.height, mf.video_codec, mf.available, mf.tracks_probed_at, mf.sidecar_subtitles
			FROM media_file mf WHERE mf.media_item_id = mi.id
			ORDER BY mf.available DESC, mf.height DESC NULLS LAST, mf.file_size DESC LIMIT 1
		) f ON true
		LEFT JOIN storage_location sl ON sl.id = f.storage_location_id`

	where := []string{"TRUE"}
	if search := strings.TrimSpace(q.Get("q")); search != "" {
		like := arg(likeContains(search))
		where = append(where, `(mi.title ILIKE `+like+` ESCAPE '\' OR COALESCE(s.name, '') ILIKE `+like+` ESCAPE '\'
			OR EXISTS (SELECT 1 FROM media_file mf2 WHERE mf2.media_item_id = mi.id AND mf2.relative_path ILIKE `+like+` ESCAPE '\')
			OR EXISTS (SELECT 1 FROM media_item_performer mip JOIN performer p ON p.id = mip.performer_id
			           WHERE mip.media_item_id = mi.id AND p.name ILIKE `+like+` ESCAPE '\'))`)
	}
	if id, err := strconv.ParseInt(q.Get("storageLocationId"), 10, 64); err == nil && id > 0 {
		where = append(where, `EXISTS (SELECT 1 FROM media_file mf3 WHERE mf3.media_item_id = mi.id AND mf3.storage_location_id = `+arg(id)+`)`)
	}
	if id, err := strconv.ParseInt(q.Get("libraryId"), 10, 64); err == nil && id > 0 {
		where = append(where, `mi.library_id = `+arg(id))
	}
	if cond, ok := adminVideoMissing[q.Get("missing")]; ok {
		where = append(where, cond)
	}
	codecs := scanner.TextSubtitleCodecList()
	if cond, ok := adminVideoTracksFilter[q.Get("tracks")]; ok {
		if strings.Contains(cond, "$codecs") {
			cond = strings.ReplaceAll(cond, "$codecs", arg(codecs))
		}
		where = append(where, cond)
	}
	base := strings.Join(where, " AND ")

	var counts adminVideoCounts
	if err := a.DB.QueryRow(r.Context(), `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE `+adminVideoStatus["review"]+`),
		       COUNT(*) FILTER (WHERE `+adminVideoStatus["filename"]+`),
		       COUNT(*) FILTER (WHERE `+adminVideoStatus["matched"]+`),
		       COUNT(*) FILTER (WHERE `+adminVideoStatus["rejected"]+`)
		`+from+` WHERE `+base, args...).Scan(&counts.All, &counts.Review, &counts.Filename, &counts.Matched, &counts.Rejected); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count videos")
		return
	}

	filtered := base
	total := counts.All
	switch status := q.Get("status"); status {
	case "review":
		total = counts.Review
	case "filename":
		total = counts.Filename
	case "matched":
		total = counts.Matched
	case "rejected":
		total = counts.Rejected
	}
	if cond, ok := adminVideoStatus[q.Get("status")]; ok {
		filtered += " AND " + cond
	}
	order, ok := adminVideoSort[q.Get("sort")]
	if !ok {
		order = adminVideoSort[""]
	}

	rows, err := a.DB.Query(r.Context(), `
		SELECT mi.id, mi.title, mi.poster_image_id, s.name, se.name, mi.duration_seconds, mi.date_added,
		       mi.match_status, mi.metadata_source,
		       COALESCE((SELECT jsonb_array_length(ms.candidates) FROM media_match_suggestion ms WHERE ms.media_item_id = mi.id), 0),
		       (SELECT COUNT(*) FROM media_file mf4 WHERE mf4.media_item_id = mi.id),
		       f.id, f.file_name, f.relative_path, f.storage_location_id, sl.name, f.file_size,
		       f.width, f.height, f.video_codec, f.available,
		       f.tracks_probed_at IS NOT NULL OR f.sidecar_subtitles IS NOT NULL,
		       (SELECT COUNT(*) FROM media_track t WHERE t.media_file_id = f.id AND t.kind = 'audio'),
		       COALESCE(f.sidecar_subtitles, 0) + (SELECT COUNT(*) FROM media_track t WHERE t.media_file_id = f.id AND t.kind = 'subtitle' AND t.codec = ANY(`+arg(codecs)+`)),
		       (SELECT COUNT(*) FROM media_track t WHERE t.media_file_id = f.id AND t.kind = 'subtitle' AND NOT (t.codec = ANY(`+arg(codecs)+`)))
		`+from+` WHERE `+filtered+`
		ORDER BY `+order+` LIMIT `+arg(limit)+` OFFSET `+arg(offset), args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load videos")
		return
	}
	defer rows.Close()

	items := []adminVideo{}
	byID := map[int64]int{}
	for rows.Next() {
		var v adminVideo
		var fileID, storageID, size *int64
		var fileName, relPath, storageName *string
		var available *bool
		var file adminVideoFile
		var probed *bool
		var audio, subs, image *int
		if err := rows.Scan(&v.ID, &v.Title, &v.PosterImageID, &v.StudioName, &v.SeriesName, &v.DurationSeconds, &v.DateAdded,
			&v.MatchStatus, &v.MetadataSource, &v.Suggestions, &v.FileCount,
			&fileID, &fileName, &relPath, &storageID, &storageName, &size,
			&file.Width, &file.Height, &file.VideoCodec, &available, &probed, &audio, &subs, &image); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read video")
			return
		}
		if fileID != nil {
			file.ID, file.FileName, file.RelativePath, file.StorageID, file.Size, file.Available = *fileID, *fileName, *relPath, *storageID, *size, *available
			if storageName != nil {
				file.StorageName = *storageName
			}
			if probed != nil && *probed {
				file.Tracks = &adminVideoTracks{Audio: *audio, Subtitles: *subs, Image: *image}
			}
			v.File = &file
		}
		v.Performers = []string{}
		byID[v.ID] = len(items)
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load videos")
		return
	}
	rows.Close()

	if len(items) > 0 {
		ids := make([]int64, 0, len(items))
		for _, v := range items {
			ids = append(ids, v.ID)
		}
		prow, err := a.DB.Query(r.Context(), `
			SELECT mip.media_item_id, p.name FROM media_item_performer mip
			JOIN performer p ON p.id = mip.performer_id
			WHERE mip.media_item_id = ANY($1) ORDER BY mip.media_item_id, mip.position`, ids)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load performers")
			return
		}
		defer prow.Close()
		for prow.Next() {
			var id int64
			var name string
			if err := prow.Scan(&id, &name); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read performer")
				return
			}
			items[byID[id]].Performers = append(items[byID[id]].Performers, name)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "counts": counts, "limit": limit, "offset": offset})
}

type adminVideoIDs struct {
	IDs []int64 `json:"ids"`
}

const maxBulkVideos = 1000

func decodeVideoIDs(w http.ResponseWriter, r *http.Request) ([]int64, bool) {
	var req adminVideoIDs
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids are required")
		return nil, false
	}
	if len(req.IDs) > maxBulkVideos {
		writeError(w, http.StatusBadRequest, "select at most "+strconv.Itoa(maxBulkVideos)+" videos at a time")
		return nil, false
	}
	return req.IDs, true
}

// matchAdminVideos queues a metadata lookup for the chosen titles. Results go
// to the review queue unless automatic title matching is on (see
// provider.AutoMatch), in which case confident matches are applied.
func (a *API) matchAdminVideos(w http.ResponseWriter, r *http.Request) {
	ids, ok := decodeVideoIDs(w, r)
	if !ok || !a.namerReady(w) {
		return
	}
	if a.Jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "job runner not available")
		return
	}
	jobID, err := a.Jobs.Enqueue(r.Context(), "METADATA_MATCH", map[string]any{"mediaItemIds": ids})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue metadata match")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"backgroundJobId": jobID})
}

// confirmAdminVideos accepts filename-guessed metadata as final for the
// chosen titles (the bulk form of confirmReview).
func (a *API) confirmAdminVideos(w http.ResponseWriter, r *http.Request) {
	ids, ok := decodeVideoIDs(w, r)
	if !ok {
		return
	}
	tag, err := a.DB.Exec(r.Context(), `
		UPDATE media_item SET metadata_source = 'MANUAL', updated_at = now()
		WHERE id = ANY($1) AND metadata_source = 'FILENAME'
	`, ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to confirm videos")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"confirmed": tag.RowsAffected()})
}
