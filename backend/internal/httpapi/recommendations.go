package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"reelstash/backend/internal/recommend"
)

type recSection struct {
	Key      string      `json:"key"`
	Title    string      `json:"title"`
	Subtitle string      `json:"subtitle,omitempty"`
	Items    []mediaItem `json:"items"`
}

type recPayload struct {
	Sections    []recSection `json:"sections"`
	GeneratedAt time.Time    `json:"generatedAt"`
	// libraries is the library scope the shelves were built for.
	libraries string
}

const recTTL = 2 * time.Minute
const recPerSection = 12

var (
	recMu    sync.Mutex
	recCache = map[int64]recPayload{}
)

func invalidateRecommendations(userID int64) {
	recMu.Lock()
	delete(recCache, userID)
	recMu.Unlock()
}

// homeRecommendations builds the personalised homepage shelves:
// picks for you, unwatched titles, "because you watched", playlist-based
// and rewatch suggestions.
func (a *API) homeRecommendations(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFromContext(r.Context())

	recMu.Lock()
	cached, ok := recCache[user.ID]
	recMu.Unlock()
	libs := fmt.Sprint(visibleLibraryIDs(r))
	if ok && cached.libraries == libs && time.Since(cached.GeneratedAt) < recTTL {
		writeJSON(w, http.StatusOK, cached)
		return
	}

	payload, err := a.buildRecommendations(r, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build recommendations")
		return
	}
	payload.libraries = libs
	recMu.Lock()
	recCache[user.ID] = payload
	recMu.Unlock()
	writeJSON(w, http.StatusOK, payload)
}

// libraryIDs limits the model to those libraries' titles (nil: every title).
func (a *API) loadRecommendModel(ctx context.Context, userID int64, now time.Time, libraryIDs []int64) (*recommend.Model, map[int64]int64, error) {
	rows, err := a.DB.Query(ctx, `
		SELECT mi.id, COALESCE(mi.studio_id, 0), COALESCE(mi.series_id, 0),
			EXISTS (SELECT 1 FROM media_item_favorite fav WHERE fav.media_item_id = mi.id AND fav.user_id = $2), mi.rating, mi.date_added,
			EXISTS (SELECT 1 FROM media_file f WHERE f.media_item_id = mi.id AND f.available),
			mi.watch_count, mi.last_watched_at
		FROM media_item mi
		WHERE $1::bigint[] IS NULL OR mi.library_id = ANY($1)
	`, libraryIDs, userID)
	if err != nil {
		return nil, nil, err
	}
	byID := map[int64]*recommend.Item{}
	var items []*recommend.Item
	studioOf := map[int64]int64{}
	eng := map[int64]recommend.Engagement{}
	for rows.Next() {
		it := &recommend.Item{}
		var studioID, seriesID int64
		var watchCount int32
		var lastWatched *time.Time
		if err := rows.Scan(&it.ID, &studioID, &seriesID, &it.Favorite, &it.Rating, &it.AddedAt, &it.Available, &watchCount, &lastWatched); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if studioID != 0 {
			it.Features = append(it.Features, recommend.Feature{Kind: recommend.Studio, ID: studioID})
			studioOf[it.ID] = studioID
		}
		if seriesID != 0 {
			it.Features = append(it.Features, recommend.Feature{Kind: recommend.Series, ID: seriesID})
		}
		// Legacy (pre-session) history: only a weak fallback signal.
		if watchCount > 0 && lastWatched != nil {
			eng[it.ID] = recommend.Engagement{Plays: int(watchCount), Completion: 1, Completed: true, LastPlayed: *lastWatched}
		}
		byID[it.ID] = it
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	for _, q := range []struct {
		sql  string
		kind recommend.Kind
	}{
		{`SELECT media_item_id, performer_id FROM media_item_performer`, recommend.Performer},
		{`SELECT media_item_id, category_id FROM media_item_category`, recommend.Category},
		{`SELECT media_item_id, tag_id FROM media_item_tag`, recommend.Tag},
	} {
		fr, err := a.DB.Query(ctx, q.sql)
		if err != nil {
			return nil, nil, err
		}
		for fr.Next() {
			var mid, fid int64
			if err := fr.Scan(&mid, &fid); err != nil {
				fr.Close()
				return nil, nil, err
			}
			if it := byID[mid]; it != nil {
				it.Features = append(it.Features, recommend.Feature{Kind: q.kind, ID: fid})
			}
		}
		fr.Close()
		if err := fr.Err(); err != nil {
			return nil, nil, err
		}
	}

	// Playback state (position-only history) then real sessions override.
	pr, err := a.DB.Query(ctx, `
		SELECT ps.media_item_id, ps.position_seconds::float8, ps.completed, ps.updated_at, COALESCE(mi.duration_seconds, 0)::float8
		FROM playback_state ps JOIN media_item mi ON mi.id = ps.media_item_id
		WHERE ps.user_id = $1 AND (ps.completed OR ps.position_seconds > 0)`, userID)
	if err != nil {
		return nil, nil, err
	}
	for pr.Next() {
		var id int64
		var pos, dur float64
		var completed bool
		var at time.Time
		if err := pr.Scan(&id, &pos, &completed, &at, &dur); err != nil {
			pr.Close()
			return nil, nil, err
		}
		e := eng[id]
		if e.Plays == 0 {
			e.Plays = 1
		}
		e.Completed = e.Completed || completed
		if completed {
			e.Completion = 1
		} else if dur > 0 && pos/dur > e.Completion {
			e.Completion = min(pos/dur, 1)
		}
		e.WatchedSeconds = max(e.WatchedSeconds, pos)
		if at.After(e.LastPlayed) {
			e.LastPlayed = at
		}
		eng[id] = e
	}
	pr.Close()

	sr, err := a.DB.Query(ctx, `
		SELECT ws.media_item_id,
			COUNT(*) FILTER (WHERE `+countedPlay+`),
			COALESCE(SUM(ws.watched_seconds), 0)::float8,
			COALESCE(MAX(CASE WHEN ws.completed THEN 1
				WHEN ws.duration_seconds > 0 THEN LEAST(GREATEST(ws.max_position_seconds, ws.watched_seconds) / ws.duration_seconds, 1)
				ELSE 0 END), 0)::float8,
			COALESCE(bool_or(ws.completed), FALSE),
			MAX(ws.started_at)
		FROM watch_session ws
		WHERE ws.user_id = $1
		GROUP BY ws.media_item_id`, userID)
	if err != nil {
		return nil, nil, err
	}
	for sr.Next() {
		var id int64
		var plays int
		var e recommend.Engagement
		if err := sr.Scan(&id, &plays, &e.WatchedSeconds, &e.Completion, &e.Completed, &e.LastPlayed); err != nil {
			sr.Close()
			return nil, nil, err
		}
		// An opened-but-abandoned session still counts as a (negative) touch.
		e.Plays = max(plays, 1)
		if plays == 0 {
			e.Completion = min(e.Completion, 0.05)
		}
		eng[id] = e
	}
	sr.Close()

	plays := map[int64]int{}
	// Only the user's own playlists say anything about their taste.
	cr, err := a.DB.Query(ctx, `
		SELECT cmi.media_item_id, COUNT(*) FROM collection_media_item cmi JOIN collection c ON c.id = cmi.collection_id
		WHERE c.user_id = $1 GROUP BY cmi.media_item_id`, userID)
	if err != nil {
		return nil, nil, err
	}
	for cr.Next() {
		var id int64
		var c int
		if err := cr.Scan(&id, &c); err != nil {
			cr.Close()
			return nil, nil, err
		}
		plays[id] = c
	}
	cr.Close()

	return recommend.Build(items, eng, plays, now), studioOf, nil
}

func (a *API) buildRecommendations(r *http.Request, userID int64) (recPayload, error) {
	ctx := r.Context()
	now := time.Now()
	m, studioOf, err := a.loadRecommendModel(ctx, userID, now, visibleLibraryIDs(r))
	if err != nil {
		return recPayload{}, err
	}
	seed := fmt.Sprintf("%d-%s", userID, now.Format("2006-01-02"))
	group := func(id int64) int64 { return studioOf[id] }

	var unseen, seen []*recommend.Item
	for _, it := range m.Items {
		if !it.Available {
			continue
		}
		if m.Seen(it.ID) {
			seen = append(seen, it)
		} else {
			unseen = append(unseen, it)
		}
	}
	used := map[int64]bool{}
	filter := func(in []*recommend.Item) []*recommend.Item {
		var out []*recommend.Item
		for _, it := range in {
			if !used[it.ID] {
				out = append(out, it)
			}
		}
		return out
	}
	take := func(ranked []recommend.Scored, maxPer int) []recommend.Scored {
		out := recommend.Diversify(ranked, recPerSection, maxPer, group)
		for _, s := range out {
			used[s.ID] = true
		}
		return out
	}

	type pick struct {
		key, title, subtitle string
		picks                []recommend.Scored
		reasons              bool
		fixedReason          string
	}
	var picks []pick

	// 1. Picked for you — unwatched, strongest taste match.
	if len(m.Prof.History)+len(m.Prof.Favorites)+len(m.Prof.Playlist) > 0 {
		p := take(m.Rank(filter(unseen), m.Combined, seed, 0.03), 3)
		picks = append(picks, pick{key: "for-you", title: "Picked for you", subtitle: "Based on your history, favorites and playlists", picks: p, reasons: true})
	}

	// 2. Because you watched X — similarity to the most engaged recent title.
	var seedItem *recommend.Item
	bestScore := 0.0
	for id, e := range m.Eng {
		it := m.Items[id]
		if it == nil || (!e.Completed && e.Completion < 0.5) {
			continue
		}
		days := now.Sub(e.LastPlayed).Hours() / 24
		s := (e.Completion + float64(e.Plays)*0.3) * (1 / (1 + days/14))
		if it.Favorite {
			s += 0.5
		}
		if s > bestScore {
			bestScore, seedItem = s, it
		}
	}
	var seedTitle string
	if seedItem != nil {
		_ = a.DB.QueryRow(ctx, `SELECT title FROM media_item WHERE id = $1`, seedItem.ID).Scan(&seedTitle)
		single := recommend.Profile{}
		for _, f := range seedItem.Features {
			single[f] = kindWeightFor(f.Kind)
		}
		match := func(it *recommend.Item) recommend.Match { return m.Match(it, single) }
		p := take(m.Rank(filter(unseen), match, seed+"b", 0.02), 2)
		if len(p) > 0 && seedTitle != "" {
			picks = append(picks, pick{key: "because-watched", title: "Because you watched " + seedTitle, picks: p, reasons: true})
		}
	}

	// 3. More like your playlists — playlist-only taste, not already in one.
	if len(m.Prof.Playlist) > 0 {
		match := func(it *recommend.Item) recommend.Match { return m.Match(it, m.Prof.Playlist) }
		p := take(m.Rank(filter(unseen), match, seed+"p", 0.05), 3)
		if len(p) > 0 {
			picks = append(picks, pick{key: "playlists", title: "More like your playlists", picks: p, reasons: true})
		}
	}

	// 4. Not watched yet — unseen titles with extra rotation for discovery.
	{
		p := take(m.Rank(filter(unseen), m.Combined, seed+"u", 0.3), 2)
		picks = append(picks, pick{key: "unwatched", title: "Not watched yet", subtitle: "Titles you haven't opened", picks: p, fixedReason: "Not watched yet"})
	}

	// 5. Watch again — favourites and repeats you haven't played lately.
	{
		var cands []*recommend.Item
		for _, it := range seen {
			e := m.Eng[it.ID]
			if used[it.ID] || now.Sub(e.LastPlayed) < 21*24*time.Hour {
				continue
			}
			if it.Favorite || e.Plays >= 2 || e.Completion >= 0.9 {
				cands = append(cands, it)
			}
		}
		match := func(it *recommend.Item) recommend.Match {
			mm := m.Combined(it)
			if it.Favorite {
				mm.Score += 1
			}
			mm.Score += float64(m.Eng[it.ID].Plays) * 0.2
			return mm
		}
		p := take(m.Rank(cands, match, seed+"r", 0.1), 3)
		picks = append(picks, pick{key: "watch-again", title: "Watch again", subtitle: "Favorites and repeats from a while ago", picks: p, fixedReason: "Watched before"})
	}

	// Hydrate.
	var ids []int64
	var feats []recommend.Feature
	for _, pk := range picks {
		for _, s := range pk.picks {
			ids = append(ids, s.ID)
			if pk.reasons && s.Has {
				feats = append(feats, s.Reason)
			}
		}
	}
	hydrated := map[int64]mediaItem{}
	if len(ids) > 0 {
		rows, err := a.DB.Query(ctx, mediaItemSelect+` WHERE mi.id = ANY($1)`, ids)
		if err != nil {
			return recPayload{}, err
		}
		var list []mediaItem
		for rows.Next() {
			mi, err := scanMediaItem(rows)
			if err != nil {
				rows.Close()
				return recPayload{}, err
			}
			list = append(list, mi)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return recPayload{}, err
		}
		if err := a.attachPerformers(r, list); err != nil {
			return recPayload{}, err
		}
		for _, mi := range list {
			hydrated[mi.ID] = mi
		}
	}
	names := a.featureNames(ctx, feats)

	out := recPayload{Sections: []recSection{}, GeneratedAt: now}
	for _, pk := range picks {
		sec := recSection{Key: pk.key, Title: pk.title, Subtitle: pk.subtitle, Items: []mediaItem{}}
		for _, s := range pk.picks {
			mi, ok := hydrated[s.ID]
			if !ok {
				continue
			}
			reason := pk.fixedReason
			if pk.reasons && s.Has {
				reason = reasonText(s.Reason, names[s.Reason])
			}
			if reason != "" {
				rs := reason
				mi.Reason = &rs
			}
			sec.Items = append(sec.Items, mi)
		}
		if len(sec.Items) > 0 {
			out.Sections = append(out.Sections, sec)
		}
	}
	return out, nil
}

func kindWeightFor(k recommend.Kind) float64 {
	switch k {
	case recommend.Performer:
		return 3
	case recommend.Series:
		return 2
	case recommend.Studio:
		return 1.5
	case recommend.Tag:
		return 0.7
	}
	return 1
}

func reasonText(f recommend.Feature, name string) string {
	if name == "" {
		return ""
	}
	switch f.Kind {
	case recommend.Performer:
		return "You like " + name
	case recommend.Studio:
		return "More from " + name
	case recommend.Series:
		return "Part of " + name
	}
	return "Into " + name
}

func (a *API) featureNames(ctx context.Context, feats []recommend.Feature) map[recommend.Feature]string {
	byKind := map[recommend.Kind][]int64{}
	for _, f := range feats {
		byKind[f.Kind] = append(byKind[f.Kind], f.ID)
	}
	tables := map[recommend.Kind]string{
		recommend.Performer: "performer", recommend.Studio: "studio", recommend.Series: "series",
		recommend.Category: "category", recommend.Tag: "tag",
	}
	out := map[recommend.Feature]string{}
	for kind, ids := range byKind {
		rows, err := a.DB.Query(ctx, `SELECT id, name FROM `+tables[kind]+` WHERE id = ANY($1)`, ids)
		if err != nil {
			continue
		}
		for rows.Next() {
			var id int64
			var name string
			if rows.Scan(&id, &name) == nil {
				out[recommend.Feature{Kind: kind, ID: id}] = name
			}
		}
		rows.Close()
	}
	return out
}
