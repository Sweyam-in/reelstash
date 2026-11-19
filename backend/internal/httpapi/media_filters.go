package httpapi

import (
	"net/http"
	"strconv"
	"strings"
)

// libraryFilterConds turns the Library page's filters into WHERE conditions on
// media_item mi (joined to studio s):
//
//	studios=Name A,Name B   any of several studios (by name)
//	minDuration, maxDuration  runtime bounds in seconds
//	quality=4k|1080p|720p|sd  the best available file's resolution
//	watched=unwatched|inProgress|watched  this user's progress
//	addedWithin=N           added in the last N days (1..3650)
//	notifyOnly=true         only titles with a person this user chose to be told about
//
// Every condition is plain SQL that the SQLite adapter also understands.
func libraryFilterConds(r *http.Request, arg func(any) string) []string {
	q := r.URL.Query()
	conds := []string{}

	if raw := strings.TrimSpace(q.Get("studios")); raw != "" {
		names := []string{}
		for _, name := range strings.Split(raw, ",") {
			if name = strings.TrimSpace(name); name != "" {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			conds = append(conds, `s.name = ANY(`+arg(names)+`)`)
		}
	}

	if v, err := strconv.Atoi(q.Get("minDuration")); err == nil && v > 0 {
		conds = append(conds, `mi.duration_seconds >= `+arg(v))
	}
	if v, err := strconv.Atoi(q.Get("maxDuration")); err == nil && v > 0 {
		conds = append(conds, `mi.duration_seconds IS NOT NULL AND mi.duration_seconds > 0 AND mi.duration_seconds <= `+arg(v))
	}

	if cond := qualityCond(q.Get("quality")); cond != "" {
		conds = append(conds, `EXISTS (SELECT 1 FROM media_file qf WHERE qf.media_item_id = mi.id AND qf.available = TRUE AND `+cond+`)`)
	}

	if state := q.Get("watched"); state != "" {
		user, _ := UserFromContext(r.Context())
		uid := arg(user.ID)
		progressed := `EXISTS (SELECT 1 FROM playback_state wps WHERE wps.media_item_id = mi.id AND wps.user_id = ` + uid
		switch state {
		case "watched":
			conds = append(conds, `(mi.watched = TRUE OR `+progressed+` AND wps.completed = TRUE))`)
		case "unwatched":
			conds = append(conds, `(mi.watched = FALSE AND NOT `+progressed+` AND (wps.completed = TRUE OR wps.position_seconds >= 5)))`)
		case "inProgress":
			conds = append(conds, progressed+` AND wps.completed = FALSE AND wps.position_seconds >= 5
				AND (mi.duration_seconds IS NULL OR wps.position_seconds < mi.duration_seconds * 0.95))`)
		}
	}

	// notifyOnly narrows to titles with someone this user asked to hear about.
	if q.Get("notifyOnly") == "true" {
		user, _ := UserFromContext(r.Context())
		conds = append(conds, `EXISTS (SELECT 1 FROM media_item_performer np JOIN performer_notify pn ON pn.performer_id = np.performer_id
			WHERE np.media_item_id = mi.id AND pn.user_id = `+strconv.FormatInt(user.ID, 10)+`)`)
	}

	if days, err := strconv.Atoi(q.Get("addedWithin")); err == nil && days > 0 && days <= 3650 {
		// Inlined (validated) so the SQLite adapter can rewrite the interval.
		conds = append(conds, `mi.date_added >= now() - INTERVAL '`+strconv.Itoa(days)+` days'`)
	}
	return conds
}

// qualityCond matches a media_file qf by resolution bucket. Portrait files count by their long side.
func qualityCond(quality string) string {
	long := `(CASE WHEN COALESCE(qf.width, 0) > COALESCE(qf.height, 0) THEN COALESCE(qf.width, 0) ELSE COALESCE(qf.height, 0) END)`
	short := `(CASE WHEN COALESCE(qf.width, 0) > COALESCE(qf.height, 0) THEN COALESCE(qf.height, 0) ELSE COALESCE(qf.width, 0) END)`
	switch quality {
	case "4k":
		return `(` + long + ` >= 3800 OR ` + short + ` >= 2100)`
	case "1080p":
		return `(` + short + ` >= 1000 AND ` + long + ` < 3800 AND ` + short + ` < 2100)`
	case "720p":
		return `(` + short + ` >= 700 AND ` + short + ` < 1000)`
	case "sd":
		return `(` + short + ` > 0 AND ` + short + ` < 700)`
	}
	return ""
}
