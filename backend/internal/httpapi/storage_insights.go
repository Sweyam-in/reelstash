package httpapi

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
	"strings"
	"time"

	"reelstash/backend/internal/diskinfo"
	"reelstash/backend/internal/scanner"
)

type storageBreakdown struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
	Bytes int64  `json:"bytes"`
}

type capacityPoint struct {
	At        time.Time `json:"at"`
	Total     int64     `json:"total"`
	Used      int64     `json:"used"`
	Available int64     `json:"available"`
}

type dayBytes struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
	Bytes int64  `json:"bytes"`
}

type storageInsights struct {
	Location     storageLocation    `json:"location"`
	LastSeenAt   *string            `json:"lastSeenAt,omitempty"`
	Live         *diskinfo.Usage    `json:"live,omitempty"`
	LiveError    string             `json:"liveError,omitempty"`
	Mount        *diskinfo.Mount    `json:"mount,omitempty"`
	Files        int64              `json:"files"`
	Available    int64              `json:"availableFiles"`
	Missing      int64              `json:"missingFiles"`
	Titles       int64              `json:"titles"`
	MediaBytes   int64              `json:"mediaBytes"`
	DurationSecs float64            `json:"durationSeconds"`
	ByFormat     []storageBreakdown `json:"byFormat"`
	ByQuality    []storageBreakdown `json:"byQuality"`
	TopFolders   []storageBreakdown `json:"topFolders"`
	History      []capacityPoint    `json:"history"`
	Added        []dayBytes         `json:"added"`
	Rules        int64              `json:"rules"`
}

// storageLocationInsights is everything the disk detail page shows: live
// capacity and mount details, what the library holds there, and history.
func (a *API) storageLocationInsights(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid storage location id")
		return
	}
	days := 30
	if v, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && v > 0 && v <= 400 {
		days = v
	}
	ctx := r.Context()
	fail := func() { writeError(w, http.StatusInternalServerError, "failed to load storage insights") }

	var p storageInsights
	s := &p.Location
	err = a.DB.QueryRow(ctx, `
		SELECT id, name, type, mount_path, total_capacity, used_capacity, available_capacity, online, last_scan_at::text, last_seen_at::text
		FROM storage_location WHERE id = $1`, id).
		Scan(&s.ID, &s.Name, &s.Type, &s.MountPath, &s.TotalCapacity, &s.UsedCapacity, &s.AvailableCapacity, &s.Online, &s.LastScanAt, &p.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "storage location not found")
		return
	} else if err != nil {
		fail()
		return
	}

	// Read live, but with a short timeout: a stalled network mount must not hang the page.
	if usage, err := diskinfo.Stat(s.MountPath, 3*time.Second); err == nil {
		p.Live = &usage
		if mount, err := diskinfo.MountOf(s.MountPath, 3*time.Second); err == nil && (mount.FSType != "" || mount.Device != "") {
			p.Mount = &mount
		}
	} else {
		p.LiveError = err.Error()
	}

	if err := a.DB.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE mf.available), COUNT(*) FILTER (WHERE NOT mf.available),
		       COUNT(DISTINCT mf.media_item_id) FILTER (WHERE mf.available),
		       COALESCE(SUM(mf.file_size) FILTER (WHERE mf.available), 0),
		       COALESCE(SUM(mf.duration_seconds) FILTER (WHERE mf.available), 0)
		FROM media_file mf WHERE mf.storage_location_id = $1`, id).
		Scan(&p.Files, &p.Available, &p.Missing, &p.Titles, &p.MediaBytes, &p.DurationSecs); err != nil {
		fail()
		return
	}

	breakdown := func(expr string, limit int) ([]storageBreakdown, error) {
		rows, err := a.DB.Query(ctx, `
			SELECT `+expr+` AS name, COUNT(*), COALESCE(SUM(file_size), 0) AS bytes
			FROM media_file WHERE storage_location_id = $1 AND available
			GROUP BY 1 ORDER BY bytes DESC LIMIT `+strconv.Itoa(limit), id)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []storageBreakdown{}
		for rows.Next() {
			var b storageBreakdown
			if err := rows.Scan(&b.Name, &b.Count, &b.Bytes); err != nil {
				return nil, err
			}
			out = append(out, b)
		}
		return out, rows.Err()
	}
	if p.ByFormat, err = breakdown(`UPPER(LTRIM(extension, '.'))`, 8); err != nil {
		fail()
		return
	}
	if p.ByQuality, err = breakdown(`CASE WHEN height >= 2160 THEN '4K' WHEN height >= 1440 THEN '1440p'
		WHEN height >= 1080 THEN '1080p' WHEN height >= 720 THEN '720p' WHEN height > 0 THEN 'SD' ELSE 'Unknown' END`, 8); err != nil {
		fail()
		return
	}
	if p.TopFolders, err = breakdown(`CASE WHEN position('/' in relative_path) > 0 THEN split_part(relative_path, '/', 1) ELSE '(disk root)' END`, 10); err != nil {
		fail()
		return
	}

	// Hourly readings for short ranges; one reading per day (the last) beyond a week.
	bucket := "hour"
	if days > 7 {
		bucket = "day"
	}
	hrows, err := a.DB.Query(ctx, `
		SELECT DISTINCT ON (date_trunc('`+bucket+`', sampled_at)) sampled_at, total_bytes, used_bytes, available_bytes
		FROM storage_capacity_sample
		WHERE storage_location_id = $1 AND sampled_at >= now() - make_interval(days => $2)
		ORDER BY date_trunc('`+bucket+`', sampled_at), sampled_at DESC`, id, days)
	if err != nil {
		fail()
		return
	}
	p.History = []capacityPoint{}
	for hrows.Next() {
		var c capacityPoint
		if err := hrows.Scan(&c.At, &c.Total, &c.Used, &c.Available); err != nil {
			hrows.Close()
			fail()
			return
		}
		p.History = append(p.History, c)
	}
	hrows.Close()

	arows, err := a.DB.Query(ctx, `
		SELECT to_char(d.day, 'YYYY-MM-DD'), COUNT(mf.id), COALESCE(SUM(mf.file_size), 0)
		FROM generate_series(CURRENT_DATE - make_interval(days => $2 - 1), CURRENT_DATE, INTERVAL '1 day') AS d(day)
		LEFT JOIN media_file mf ON mf.storage_location_id = $1 AND mf.discovered_at::date = d.day
		GROUP BY d.day ORDER BY d.day`, id, days)
	if err != nil {
		fail()
		return
	}
	p.Added = []dayBytes{}
	for arows.Next() {
		var d dayBytes
		if err := arows.Scan(&d.Date, &d.Count, &d.Bytes); err != nil {
			arows.Close()
			fail()
			return
		}
		p.Added = append(p.Added, d)
	}
	arows.Close()

	if err := a.DB.QueryRow(ctx, `
		SELECT COUNT(*) FROM scan_rule
		WHERE library_id IS NULL OR library_id IN (SELECT library_id FROM library_root WHERE storage_location_id = $1)`, id).Scan(&p.Rules); err != nil {
		fail()
		return
	}
	writeJSON(w, http.StatusOK, p)
}

type scanRule struct {
	ID          int64     `json:"id"`
	LibraryID   *int64    `json:"libraryId,omitempty"`
	LibraryName *string   `json:"libraryName,omitempty"`
	Pattern     string    `json:"pattern"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (a *API) listScanRules(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(r.Context(), `
		SELECT sr.id, sr.library_id, l.name, sr.pattern, sr.created_at
		FROM scan_rule sr LEFT JOIN library l ON l.id = sr.library_id
		ORDER BY l.position NULLS FIRST, lower(l.name), sr.pattern`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load scan rules")
		return
	}
	defer rows.Close()
	items := []scanRule{}
	for rows.Next() {
		var s scanRule
		if err := rows.Scan(&s.ID, &s.LibraryID, &s.LibraryName, &s.Pattern, &s.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read scan rule")
			return
		}
		items = append(items, s)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// scanRuleRequest is a rule for one library, or for every library when
// LibraryID is empty.
type scanRuleRequest struct {
	Pattern   string `json:"pattern"`
	LibraryID *int64 `json:"libraryId"`
}

func (a *API) decodeScanRule(w http.ResponseWriter, r *http.Request) (scanRuleRequest, bool) {
	var req scanRuleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return req, false
	}
	req.Pattern = strings.TrimSpace(req.Pattern)
	if err := scanner.ValidatePattern(req.Pattern); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return req, false
	}
	if req.LibraryID != nil && *req.LibraryID == 0 {
		req.LibraryID = nil
	}
	if req.LibraryID != nil {
		var ok bool
		if err := a.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM library WHERE id = $1)`, *req.LibraryID).Scan(&ok); err != nil || !ok {
			writeError(w, http.StatusBadRequest, "unknown library")
			return req, false
		}
	}
	return req, true
}

func (a *API) createScanRule(w http.ResponseWriter, r *http.Request) {
	req, ok := a.decodeScanRule(w, r)
	if !ok {
		return
	}
	var id int64
	err := a.DB.QueryRow(r.Context(), `
		INSERT INTO scan_rule (library_id, pattern) VALUES ($1, $2)
		ON CONFLICT DO NOTHING RETURNING id`, req.LibraryID, req.Pattern).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "that rule already exists")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save scan rule")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *API) deleteScanRule(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid scan rule id")
		return
	}
	tag, err := a.DB.Exec(r.Context(), `DELETE FROM scan_rule WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete scan rule")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "scan rule not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// testScanRule shows which indexed files a pattern would cover. It checks
// the library index rather than walking the disk, so it is instant but
// can't show files that were never indexed.
func (a *API) testScanRule(w http.ResponseWriter, r *http.Request) {
	req, ok := a.decodeScanRule(w, r)
	if !ok {
		return
	}
	// The pattern resolves against each file's library folder, as in a scan.
	rootRows, err := a.DB.Query(r.Context(), `SELECT storage_location_id, path, library_id FROM library_root`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to test scan rule")
		return
	}
	roots := map[int64]map[string]int64{}
	for rootRows.Next() {
		var loc, libraryID int64
		var path string
		if err := rootRows.Scan(&loc, &path, &libraryID); err != nil {
			rootRows.Close()
			writeError(w, http.StatusInternalServerError, "failed to test scan rule")
			return
		}
		if roots[loc] == nil {
			roots[loc] = map[string]int64{}
		}
		roots[loc][path] = libraryID
	}
	rootRows.Close()
	var global []string
	byLibrary := map[int64][]string{}
	if req.LibraryID == nil {
		global = []string{req.Pattern}
	} else {
		byLibrary[*req.LibraryID] = []string{req.Pattern}
	}
	sets := map[int64]*scanner.RuleSet{}

	rows, err := a.DB.Query(r.Context(), `
		SELECT mf.storage_location_id, sl.name, mf.relative_path FROM media_file mf
		JOIN storage_location sl ON sl.id = mf.storage_location_id
		WHERE $1::bigint IS NULL OR mf.library_id = $1
		ORDER BY sl.name, mf.relative_path`, req.LibraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to test scan rule")
		return
	}
	defer rows.Close()
	type example struct {
		StorageLocationName string `json:"storageLocationName"`
		RelativePath        string `json:"relativePath"`
	}
	examples := []example{}
	count, checked := 0, 0
	for rows.Next() {
		var loc int64
		var e example
		if err := rows.Scan(&loc, &e.StorageLocationName, &e.RelativePath); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to test scan rule")
			return
		}
		rs, ok := sets[loc]
		if !ok {
			if rs, err = scanner.NewRuleSet(global, byLibrary, roots[loc]); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			sets[loc] = rs
		}
		checked++
		if rs.Excluded(e.RelativePath) {
			count++
			if len(examples) < 20 {
				examples = append(examples, e)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": count, "checked": checked, "examples": examples})
}
