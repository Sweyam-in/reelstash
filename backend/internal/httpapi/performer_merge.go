package httpapi

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"reelstash/backend/internal/namer"
	"reelstash/backend/internal/scanner"
)

type mergePerformerRequest struct {
	SourceID int64 `json:"sourceId"`
}

// mergePerformer folds performer {sourceId} into performer {id}: their titles,
// aliases, favorite flag, photo and provider match move across, the source
// record is deleted, and the NFO sidecars of every affected title are
// rewritten so a rescan cannot bring the old name back.
func (a *API) mergePerformer(w http.ResponseWriter, r *http.Request) {
	targetID, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid performer id")
		return
	}
	var req mergePerformerRequest
	if err := decodeJSON(r, &req); err != nil || req.SourceID < 1 {
		writeError(w, http.StatusBadRequest, "sourceId is required")
		return
	}
	if req.SourceID == targetID {
		writeError(w, http.StatusBadRequest, "choose two different performers")
		return
	}
	ctx := r.Context()
	merged, ok := a.applyPerformerMerge(ctx, w, targetID, req.SourceID)
	if !ok {
		return
	}
	for _, key := range merged.dupKeys {
		if key != "" && filepath.Base(key) == key {
			os.Remove(filepath.Join(a.Cfg.PosterDir, key))
		}
	}
	names := append([]string{merged.sourceName}, merged.aliases...)
	updated, failed := a.rewritePerformerNFOs(ctx, merged.itemIDs, names, a.performerName(ctx, targetID))
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         targetID,
		"titles":     len(merged.itemIDs),
		"nfoUpdated": updated,
		"nfoFailed":  failed,
	})
}

type performerMergeOutcome struct {
	itemIDs    []int64
	dupKeys    []string
	sourceName string
	aliases    []string
}

// applyPerformerMerge runs the performer merge transaction. false means the
// response was already written.
func (a *API) applyPerformerMerge(ctx context.Context, w http.ResponseWriter, targetID, sourceID int64) (performerMergeOutcome, bool) {
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start merge")
		return performerMergeOutcome{}, false
	}
	defer tx.Rollback(ctx)

	type row struct {
		name         string
		profileImage *int64
		imageCustom  bool
		linked       bool
		favorite     bool
	}
	load := func(id int64) (row, error) {
		var p row
		err := tx.QueryRow(ctx, `
			SELECT p.name, p.profile_image_id, COALESCE(ar.custom, false) OR p.photo_chosen, EXISTS (SELECT 1 FROM match_link ml WHERE ml.kind = 'performer' AND ml.entity_id = p.id), p.favorite
			FROM performer p LEFT JOIN artwork ar ON ar.id = p.profile_image_id WHERE p.id = $1 FOR UPDATE OF p
		`, id).Scan(&p.name, &p.profileImage, &p.imageCustom, &p.linked, &p.favorite)
		return p, err
	}
	// Lock in id order so two concurrent merges cannot deadlock.
	first, second := targetID, sourceID
	if first > second {
		first, second = second, first
	}
	rows := map[int64]row{}
	for _, id := range []int64{first, second} {
		p, err := load(id)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "performer not found")
			return performerMergeOutcome{}, false
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load performer")
			return performerMergeOutcome{}, false
		}
		rows[id] = p
	}
	target, source := rows[targetID], rows[sourceID]

	fail := func(msg string, err error) {
		log.Printf("performer merge %d into %d: %s: %v", sourceID, targetID, msg, err)
		writeError(w, http.StatusInternalServerError, msg)
	}

	var itemIDs []int64
	itemRows, err := tx.Query(ctx, `SELECT media_item_id FROM media_item_performer WHERE performer_id = $1`, sourceID)
	if err != nil {
		fail("failed to load titles", err)
		return performerMergeOutcome{}, false
	}
	for itemRows.Next() {
		var id int64
		if err := itemRows.Scan(&id); err != nil {
			itemRows.Close()
			fail("failed to load titles", err)
			return performerMergeOutcome{}, false
		}
		itemIDs = append(itemIDs, id)
	}
	itemRows.Close()

	var sourceAliases []string
	aliasRows, err := tx.Query(ctx, `SELECT alias FROM performer_alias WHERE performer_id = $1`, sourceID)
	if err != nil {
		fail("failed to load aliases", err)
		return performerMergeOutcome{}, false
	}
	for aliasRows.Next() {
		var alias string
		if err := aliasRows.Scan(&alias); err != nil {
			aliasRows.Close()
			fail("failed to load aliases", err)
			return performerMergeOutcome{}, false
		}
		sourceAliases = append(sourceAliases, alias)
	}
	aliasRows.Close()

	steps := []struct {
		msg  string
		sql  string
		args []any
	}{
		{"failed to move titles", `
			INSERT INTO media_item_performer (media_item_id, performer_id, position)
			SELECT media_item_id, $1, position FROM media_item_performer WHERE performer_id = $2
			ON CONFLICT (media_item_id, performer_id) DO NOTHING`, []any{targetID, sourceID}},
		{"failed to move titles", `DELETE FROM media_item_performer WHERE performer_id = $1`, []any{sourceID}},
		{"failed to move aliases", `
			INSERT INTO performer_alias (performer_id, alias)
			SELECT $1, alias FROM performer_alias WHERE performer_id = $2
			ON CONFLICT DO NOTHING`, []any{targetID, sourceID}},
		{"failed to record alias", `
			INSERT INTO performer_alias (performer_id, alias) VALUES ($1, $2) ON CONFLICT DO NOTHING`, []any{targetID, source.name}},
		{"failed to move favorites", `
			INSERT INTO performer_favorite (user_id, performer_id)
			SELECT user_id, $1 FROM performer_favorite WHERE performer_id = $2
			ON CONFLICT DO NOTHING`, []any{targetID, sourceID}},
		{"failed to move crew credits", `
			INSERT INTO media_item_crew (media_item_id, performer_id, job, position)
			SELECT media_item_id, $1, job, position FROM media_item_crew WHERE performer_id = $2
			ON CONFLICT DO NOTHING`, []any{targetID, sourceID}},
		{"failed to merge details", `
			UPDATE performer t SET
				notes = COALESCE(t.notes, s.notes),
				display_name = COALESCE(t.display_name, s.display_name),
				updated_at = now()
			FROM performer s WHERE t.id = $1 AND s.id = $2`, []any{targetID, sourceID}},
	}
	for _, step := range steps {
		if _, err := tx.Exec(ctx, step.sql, step.args...); err != nil {
			fail(step.msg, err)
			return performerMergeOutcome{}, false
		}
	}

	// A survivor with no provider match takes the source's match state; the
	// links themselves move below.
	if !target.linked && source.linked {
		if _, err := tx.Exec(ctx, `
			UPDATE performer t SET match_status = s.match_status,
				remote_metadata = s.remote_metadata, matched_at = s.matched_at
			FROM performer s WHERE t.id = $1 AND s.id = $2`, targetID, sourceID); err != nil {
			fail("failed to move the provider match", err)
			return performerMergeOutcome{}, false
		}
	}

	// Links to other providers move to the survivor too, unless it already has
	// a link for that provider.
	if _, err := tx.Exec(ctx, `
		UPDATE match_link SET entity_id = $1
		WHERE kind = 'performer' AND entity_id = $2
		  AND provider NOT IN (SELECT provider FROM match_link WHERE kind = 'performer' AND entity_id = $1)
	`, targetID, sourceID); err != nil {
		fail("failed to move the provider matches", err)
		return performerMergeOutcome{}, false
	}
	if _, err := tx.Exec(ctx, `
		UPDATE performer t SET match_status = 'matched', remote_metadata = s.remote_metadata, matched_at = s.matched_at
		FROM performer s
		WHERE t.id = $1 AND s.id = $2 AND t.match_status <> 'matched'
		  AND EXISTS (SELECT 1 FROM match_link WHERE kind = 'performer' AND entity_id = $1)
	`, targetID, sourceID); err != nil {
		fail("failed to move the provider matches", err)
		return performerMergeOutcome{}, false
	}

	// Both galleries are kept. A photo the survivor already has from the same
	// URL is a duplicate and goes; the rest move over.
	var dupKeys []string
	dupRows, err := tx.Query(ctx, `
		DELETE FROM artwork WHERE id IN (
			SELECT s.artwork_id FROM performer_photo s JOIN performer_photo t
			  ON t.performer_id = $1 AND t.source_url = s.source_url
			WHERE s.performer_id = $2 AND s.artwork_id IS DISTINCT FROM (SELECT profile_image_id FROM performer WHERE id = $2))
		RETURNING storage_key`, targetID, sourceID)
	if err != nil {
		fail("failed to move photos", err)
		return performerMergeOutcome{}, false
	}
	for dupRows.Next() {
		var key string
		if dupRows.Scan(&key) == nil {
			dupKeys = append(dupKeys, key)
		}
	}
	dupRows.Close()
	if _, err := tx.Exec(ctx, `
		UPDATE performer_photo s SET performer_id = $1
		WHERE s.performer_id = $2
		  AND NOT EXISTS (SELECT 1 FROM performer_photo t WHERE t.performer_id = $1 AND t.source_url = s.source_url)
	`, targetID, sourceID); err != nil {
		fail("failed to move photos", err)
		return performerMergeOutcome{}, false
	}
	// The survivor shows the source's photo when it has none, or when only the
	// source's was picked by hand.
	if source.profileImage != nil && (target.profileImage == nil || (source.imageCustom && !target.imageCustom)) {
		if _, err := tx.Exec(ctx, `UPDATE performer SET profile_image_id = NULL WHERE id = $1`, sourceID); err != nil {
			fail("failed to move photo", err)
			return performerMergeOutcome{}, false
		}
		if _, err := tx.Exec(ctx, `
			UPDATE performer SET profile_image_id = $2, photo_chosen = photo_chosen OR $3
			WHERE id = $1 AND EXISTS (SELECT 1 FROM performer_photo WHERE performer_id = $1 AND artwork_id = $2)
		`, targetID, *source.profileImage, source.imageCustom); err != nil {
			fail("failed to move photo", err)
			return performerMergeOutcome{}, false
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM performer WHERE id = $1`, sourceID); err != nil {
		fail("failed to remove the merged performer", err)
		return performerMergeOutcome{}, false
	}
	if err := tx.Commit(ctx); err != nil {
		fail("failed to save merge", err)
		return performerMergeOutcome{}, false
	}
	return performerMergeOutcome{itemIDs: itemIDs, dupKeys: dupKeys, sourceName: source.name, aliases: sourceAliases}, true
}

func (a *API) performerName(ctx context.Context, id int64) string {
	var name string
	_ = a.DB.QueryRow(ctx, `SELECT name FROM performer WHERE id = $1`, id).Scan(&name)
	return name
}

// rewritePerformerNFOs renames a performer inside the NFO of every file that
// belongs to the given titles. Titles without an NFO (or on an offline disk)
// are skipped; files that could not be written are returned by path.
func (a *API) rewritePerformerNFOs(ctx context.Context, mediaItemIDs []int64, from []string, to string) (int, []string) {
	if len(mediaItemIDs) == 0 || to == "" {
		return 0, nil
	}
	rows, err := a.DB.Query(ctx, `
		SELECT mf.id, sl.mount_path, mf.relative_path
		FROM media_file mf JOIN storage_location sl ON sl.id = mf.storage_location_id
		WHERE mf.media_item_id = ANY($1)
	`, mediaItemIDs)
	if err != nil {
		return 0, []string{"could not list files: " + err.Error()}
	}
	type file struct {
		id    int64
		mount string
		rel   string
	}
	var files []file
	for rows.Next() {
		var f file
		if err := rows.Scan(&f.id, &f.mount, &f.rel); err == nil {
			files = append(files, f)
		}
	}
	rows.Close()

	updated := 0
	var failed []string
	for _, f := range files {
		abs := filepath.Join(f.mount, filepath.FromSlash(f.rel))
		nfoPath, ok := scanner.FindAdjacentNFO(abs)
		if !ok {
			continue
		}
		changed, err := scanner.RewriteNFOPerformer(nfoPath, from, to)
		if err == nil && changed {
			err = scanner.RefreshNFORecord(ctx, a.DB, f.id, nfoPath)
		}
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", nfoPath, err))
			continue
		}
		if changed {
			updated++
		}
	}
	return updated, failed
}

type duplicateMember struct {
	ID             int64    `json:"id"`
	Name           string   `json:"name"`
	MediaCount     int64    `json:"mediaCount"`
	ProfileImageID *int64   `json:"profileImageId,omitempty"`
	MatchStatus    string   `json:"matchStatus"`
	Favorite       bool     `json:"favorite"`
	Aliases        []string `json:"aliases,omitempty"`
}

type duplicatePair struct {
	A      duplicateMember `json:"a"`
	B      duplicateMember `json:"b"`
	Reason string          `json:"reason"`
	Score  float64         `json:"score"`
}

func pairKey(a, b int64) [2]int64 {
	if a > b {
		a, b = b, a
	}
	return [2]int64{a, b}
}

// listPerformerDuplicates suggests performers that look like the same person:
// names that differ only by punctuation, accents or word order, an alias that
// equals another name, one name containing the other, or near-identical
// spelling. ?performerId= narrows it to one performer.
func (a *API) listPerformerDuplicates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	only := int64(0)
	if v := r.URL.Query().Get("performerId"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid performer id")
			return
		}
		only = id
	}

	rows, err := a.DB.Query(ctx, `
		SELECT p.id, p.name, p.profile_image_id, p.match_status, `+performerFavorite("p", currentUserID(r))+`,
		       (SELECT COUNT(*) FROM media_item_performer m WHERE m.performer_id = p.id),
		       COALESCE((SELECT array_agg(alias ORDER BY alias) FROM performer_alias WHERE performer_id = p.id), '{}')
		FROM performer p
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load performers")
		return
	}
	members := map[int64]*duplicateMember{}
	var order []int64
	for rows.Next() {
		var m duplicateMember
		if err := rows.Scan(&m.ID, &m.Name, &m.ProfileImageID, &m.MatchStatus, &m.Favorite, &m.MediaCount, &m.Aliases); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "failed to read performers")
			return
		}
		members[m.ID] = &m
		order = append(order, m.ID)
	}
	rows.Close()

	dismissed := map[[2]int64]bool{}
	dRows, err := a.DB.Query(ctx, `SELECT a_id, b_id FROM performer_distinct`)
	if err == nil {
		for dRows.Next() {
			var x, y int64
			if dRows.Scan(&x, &y) == nil {
				dismissed[pairKey(x, y)] = true
			}
		}
		dRows.Close()
	}

	found := map[[2]int64]*duplicatePair{}
	add := func(x, y int64, score float64, reason string) {
		if x == y || dismissed[pairKey(x, y)] {
			return
		}
		if only != 0 && x != only && y != only {
			return
		}
		key := pairKey(x, y)
		if existing, ok := found[key]; ok && existing.Score >= score {
			return
		}
		found[key] = &duplicatePair{A: *members[key[0]], B: *members[key[1]], Reason: reason, Score: score}
	}

	bySorted := map[string][]int64{}
	byNorm := map[string][]int64{}
	byAlias := map[string][]int64{}
	tokenSets := map[int64]map[string]bool{}
	for _, id := range order {
		m := members[id]
		norm := namer.NormalizePerformerName(m.Name)
		if norm == "" {
			continue
		}
		bySorted[namer.SortedNameKey(m.Name)] = append(bySorted[namer.SortedNameKey(m.Name)], id)
		byNorm[norm] = append(byNorm[norm], id)
		tokens := map[string]bool{}
		for _, t := range strings.Fields(norm) {
			tokens[t] = true
		}
		tokenSets[id] = tokens
		for _, alias := range m.Aliases {
			if key := namer.SortedNameKey(alias); key != "" {
				byAlias[key] = append(byAlias[key], id)
			}
		}
	}
	for _, ids := range bySorted {
		for i := range ids {
			for j := i + 1; j < len(ids); j++ {
				add(ids[i], ids[j], 1, "Same name apart from spacing, punctuation or word order")
			}
		}
	}
	for key, ids := range bySorted {
		for _, holder := range byAlias[key] {
			for _, id := range ids {
				add(holder, id, 0.95, fmt.Sprintf("%s is listed as an alias", members[id].Name))
			}
		}
	}
	for _, ids := range byAlias {
		for i := range ids {
			for j := i + 1; j < len(ids); j++ {
				add(ids[i], ids[j], 0.85, "They share an alias")
			}
		}
	}
	// One full name fully inside another ("Buck Richards" / "Buck Richards Jr").
	for _, x := range order {
		tx := tokenSets[x]
		if len(tx) < 2 || (only != 0 && x != only) {
			continue
		}
		for _, y := range order {
			ty := tokenSets[y]
			if x == y || len(ty) <= len(tx) {
				continue
			}
			inside := true
			for t := range tx {
				if !ty[t] {
					inside = false
					break
				}
			}
			if inside {
				add(x, y, 0.8, "One name contains the other")
			}
		}
	}
	{
		sim, err := a.DB.Query(ctx, `
			SELECT x.id, y.id, similarity(x.name, y.name)
			FROM performer x JOIN performer y ON x.id < y.id AND x.name % y.name
			WHERE similarity(x.name, y.name) >= 0.5
			  AND ($1::bigint = 0 OR x.id = $1 OR y.id = $1)
		`, only)
		if err == nil {
			for sim.Next() {
				var x, y int64
				var score float64
				if sim.Scan(&x, &y, &score) == nil {
					add(x, y, score*0.75, "Very similar spelling")
				}
			}
			sim.Close()
		}
	}

	pairs := make([]duplicatePair, 0, len(found))
	for _, p := range found {
		pairs = append(pairs, *p)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Score != pairs[j].Score {
			return pairs[i].Score > pairs[j].Score
		}
		return pairs[i].A.Name < pairs[j].A.Name
	})
	if len(pairs) > 300 {
		pairs = pairs[:300]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": pairs})
}

// dismissPerformerDuplicate records that two performers are different people.
func (a *API) dismissPerformerDuplicate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		A int64 `json:"a"`
		B int64 `json:"b"`
	}
	if err := decodeJSON(r, &req); err != nil || req.A < 1 || req.B < 1 || req.A == req.B {
		writeError(w, http.StatusBadRequest, "two different performer ids are required")
		return
	}
	key := pairKey(req.A, req.B)
	if _, err := a.DB.Exec(r.Context(), `
		INSERT INTO performer_distinct (a_id, b_id) VALUES ($1, $2) ON CONFLICT DO NOTHING
	`, key[0], key[1]); err != nil {
		writeError(w, http.StatusBadRequest, "performer not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
