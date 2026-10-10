package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// smartRules are the saved filters behind a smart playlist: a list of
// conditions that must all hold (or any of which may hold), evaluated afresh
// every time the playlist is opened, so new titles join it on their own.
type smartRules struct {
	Match      string           `json:"match"` // "all" (default) or "any"
	Conditions []smartCondition `json:"conditions"`
	Sort       string           `json:"sort,omitempty"`
	// Limit caps the playlist at this many titles (0: no cap), e.g. "top 25 rated".
	Limit int `json:"limit,omitempty"`
}

type smartCondition struct {
	Field string          `json:"field"`
	Op    string          `json:"op,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

const (
	maxSmartConditions = 20
	maxSmartLimit      = 5000
)

// smartField describes one kind of condition: the operators it accepts and
// how its value is read.
type smartField struct {
	ops  []string // empty: no operator
	kind string   // "bool", "text", "int", "date"
}

var smartFields = map[string]smartField{
	"watched":     {kind: "bool"},                                   // I have finished it
	"favorite":    {kind: "bool"},                                   // in my favorites
	"watchLater":  {kind: "bool"},                                   // on my Watch later list
	"rating":      {ops: []string{"gte", "lte", "eq"}, kind: "int"}, // my rating in stars (1-5)
	"avgRating":   {ops: []string{"gte", "lte", "eq"}, kind: "int"}, // everyone's average, in stars
	"tag":         {ops: []string{"is", "isNot"}, kind: "text"},
	"category":    {ops: []string{"is", "isNot"}, kind: "text"},
	"studio":      {ops: []string{"is", "contains"}, kind: "text"},
	"performer":   {ops: []string{"is", "contains"}, kind: "text"},
	"series":      {ops: []string{"is", "contains"}, kind: "text"},
	"kind":        {kind: "text"}, // movie or episode
	"year":        {ops: []string{"gte", "lte", "eq"}, kind: "int"},
	"addedWithin": {kind: "int"}, // added in the last N days
	"addedYear":   {kind: "int"},
	"addedAfter":  {kind: "date"},
	"addedBefore": {kind: "date"},
	"duration":    {ops: []string{"gte", "lte"}, kind: "int"}, // minutes
	"text":        {kind: "text"},                             // words in the title, studio, series, cast or description
}

var smartSorts = map[string]string{
	"added":    `mi.date_added DESC, mi.id DESC`,
	"oldest":   `mi.date_added ASC, mi.id ASC`,
	"title":    `LOWER(mi.title) ASC, mi.id ASC`,
	"rated":    `mi.rating DESC NULLS LAST, mi.rating_count DESC, mi.id DESC`,
	"watched":  `mi.watch_count DESC, mi.last_watched_at DESC NULLS LAST, mi.id DESC`,
	"released": `mi.release_date DESC NULLS LAST, mi.id DESC`,
	"series":   seriesOrder,
}

// parseSmartRules validates raw rules and returns them normalised.
func parseSmartRules(raw json.RawMessage) (smartRules, error) {
	var rules smartRules
	if len(raw) == 0 || string(raw) == "null" {
		return rules, fmt.Errorf("a smart playlist needs rules")
	}
	if err := json.Unmarshal(raw, &rules); err != nil {
		return rules, fmt.Errorf("rules are not valid")
	}
	switch rules.Match {
	case "":
		rules.Match = "all"
	case "all", "any":
	default:
		return rules, fmt.Errorf(`match must be "all" or "any"`)
	}
	if len(rules.Conditions) == 0 {
		return rules, fmt.Errorf("add at least one condition")
	}
	if len(rules.Conditions) > maxSmartConditions {
		return rules, fmt.Errorf("at most %d conditions", maxSmartConditions)
	}
	if rules.Sort == "" {
		rules.Sort = "added"
	}
	if _, ok := smartSorts[rules.Sort]; !ok {
		return rules, fmt.Errorf("unknown sort %q", rules.Sort)
	}
	if rules.Limit < 0 || rules.Limit > maxSmartLimit {
		return rules, fmt.Errorf("limit must be between 0 and %d", maxSmartLimit)
	}
	for i := range rules.Conditions {
		c := &rules.Conditions[i]
		spec, ok := smartFields[c.Field]
		if !ok {
			return rules, fmt.Errorf("unknown condition %q", c.Field)
		}
		if len(spec.ops) == 0 {
			c.Op = ""
		} else if c.Op == "" {
			c.Op = spec.ops[0]
		} else if !inList(spec.ops, c.Op) {
			return rules, fmt.Errorf("%s does not support %q", c.Field, c.Op)
		}
		if _, err := conditionValue(*c, spec); err != nil {
			return rules, err
		}
	}
	return rules, nil
}

func inList(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// conditionValue reads a condition's value as a bool, string, int or date
// string, checking its range.
func conditionValue(c smartCondition, spec smartField) (any, error) {
	bad := func(what string) (any, error) { return nil, fmt.Errorf("%s: %s", c.Field, what) }
	switch spec.kind {
	case "bool":
		if len(c.Value) == 0 {
			return true, nil
		}
		var b bool
		if err := json.Unmarshal(c.Value, &b); err != nil {
			return bad("true or false expected")
		}
		return b, nil
	case "text":
		var s string
		if err := json.Unmarshal(c.Value, &s); err != nil || strings.TrimSpace(s) == "" {
			return bad("a value is required")
		}
		s = strings.TrimSpace(s)
		if len(s) > 200 {
			return bad("value is too long")
		}
		if c.Field == "kind" && s != "movie" && s != "episode" {
			return bad(`must be "movie" or "episode"`)
		}
		return s, nil
	case "int":
		var n float64
		if err := json.Unmarshal(c.Value, &n); err != nil || n != float64(int(n)) {
			return bad("a whole number is required")
		}
		v := int(n)
		switch c.Field {
		case "rating", "avgRating":
			if v < 1 || v > 5 {
				return bad("stars must be between 1 and 5")
			}
		case "addedWithin", "duration":
			if v < 0 || v > 100000 {
				return bad("number is out of range")
			}
		case "year", "addedYear":
			if v < 1800 || v > 2200 {
				return bad("year is out of range")
			}
		}
		return v, nil
	case "date":
		var s string
		if err := json.Unmarshal(c.Value, &s); err != nil || len(s) != 10 {
			return bad("a date like 2024-01-31 is required")
		}
		if _, err := timeParseDate(s); err != nil {
			return bad("a date like 2024-01-31 is required")
		}
		return s, nil
	}
	return bad("unsupported")
}

// smartWhere builds the SQL condition for rules over media_item alias mi, for
// the user whose personal data (ratings, favourites, history) they refer to.
func smartWhere(rules smartRules, userID int64, arg func(any) string) (string, error) {
	parts := make([]string, 0, len(rules.Conditions))
	// The user id is only bound when a condition needs it: Postgres cannot
	// infer the type of a parameter no statement refers to.
	userPlaceholder := ""
	user := func() string {
		if userPlaceholder == "" {
			userPlaceholder = arg(userID)
		}
		return userPlaceholder
	}
	for _, c := range rules.Conditions {
		spec := smartFields[c.Field]
		v, err := conditionValue(c, spec)
		if err != nil {
			return "", err
		}
		var sql string
		switch c.Field {
		case "watched":
			sql = `(EXISTS (SELECT 1 FROM playback_state ps WHERE ps.media_item_id = mi.id AND ps.user_id = ` + user() + ` AND ps.completed)
				OR EXISTS (SELECT 1 FROM watch_session ws WHERE ws.media_item_id = mi.id AND ws.user_id = ` + user() + ` AND ws.completed))`
			if !v.(bool) {
				sql = `NOT ` + sql
			}
		case "favorite":
			sql = `EXISTS (SELECT 1 FROM media_item_favorite f WHERE f.media_item_id = mi.id AND f.user_id = ` + user() + `)`
			if !v.(bool) {
				sql = `NOT ` + sql
			}
		case "watchLater":
			sql = `EXISTS (SELECT 1 FROM watch_later wl WHERE wl.media_item_id = mi.id AND wl.user_id = ` + user() + `)`
			if !v.(bool) {
				sql = `NOT ` + sql
			}
		case "rating":
			sql = `EXISTS (SELECT 1 FROM media_item_rating r WHERE r.media_item_id = mi.id AND r.user_id = ` + user() + ` AND r.rating ` + cmpOp(c.Op) + ` ` + arg(v.(int)*2) + `)`
		case "avgRating":
			sql = `mi.rating ` + cmpOp(c.Op) + ` ` + arg(v.(int)*2)
		case "tag":
			sql = `EXISTS (SELECT 1 FROM media_item_tag mt JOIN tag t ON t.id = mt.tag_id WHERE mt.media_item_id = mi.id AND LOWER(t.name) = LOWER(` + arg(v) + `))`
			if c.Op == "isNot" {
				sql = `NOT ` + sql
			}
		case "category":
			sql = `EXISTS (SELECT 1 FROM media_item_category mc JOIN category cat ON cat.id = mc.category_id WHERE mc.media_item_id = mi.id AND LOWER(cat.name) = LOWER(` + arg(v) + `))`
			if c.Op == "isNot" {
				sql = `NOT ` + sql
			}
		case "studio":
			sql = `EXISTS (SELECT 1 FROM studio st WHERE st.id = mi.studio_id AND ` + nameMatch("st.name", c.Op, v.(string), arg) + `)`
		case "series":
			sql = `EXISTS (SELECT 1 FROM series sr WHERE sr.id = mi.series_id AND ` + nameMatch("sr.name", c.Op, v.(string), arg) + `)`
		case "performer":
			sql = `EXISTS (SELECT 1 FROM media_item_performer mp JOIN performer pf ON pf.id = mp.performer_id WHERE mp.media_item_id = mi.id AND ` +
				nameMatch("pf.name", c.Op, v.(string), arg) + `)`
		case "kind":
			if v.(string) == "episode" {
				sql = `mi.kind = 'episode'`
			} else {
				sql = `mi.kind IS DISTINCT FROM 'episode'`
			}
		case "year":
			sql = `COALESCE(mi.production_year, EXTRACT(YEAR FROM mi.release_date)::int) ` + cmpOp(c.Op) + ` ` + arg(v.(int))
		case "addedWithin":
			sql = `mi.date_added >= now() - make_interval(days => ` + arg(v.(int)) + `)`
		case "addedYear":
			sql = `EXTRACT(YEAR FROM mi.date_added)::int = ` + arg(v.(int))
		case "addedAfter":
			sql = `mi.date_added >= ` + arg(v.(string)) + `::date`
		case "addedBefore":
			sql = `mi.date_added < (` + arg(v.(string)) + `::date + 1)`
		case "duration":
			sql = `mi.duration_seconds ` + cmpOp(c.Op) + ` ` + arg(float64(v.(int))*60)
		case "text":
			s := v.(string)
			hay := `(mi.title || ' ' || COALESCE((SELECT st.name FROM studio st WHERE st.id = mi.studio_id), '') || ' ' || COALESCE((SELECT sr.name FROM series sr WHERE sr.id = mi.series_id), '') || ' ' || COALESCE((
				SELECT string_agg(pf.name, ' ') FROM media_item_performer mp JOIN performer pf ON pf.id = mp.performer_id WHERE mp.media_item_id = mi.id), ''))`
			sql = `((` + fuzzyMatch(hay, fuzzyTokens(s), arg) + `) OR COALESCE(mi.description, '') ILIKE ` + arg(likeContains(s)) + ` ESCAPE '\')`
		default:
			return "", fmt.Errorf("unknown condition %q", c.Field)
		}
		parts = append(parts, `(`+sql+`)`)
	}
	join := ` AND `
	if rules.Match == "any" {
		join = ` OR `
	}
	return `(` + strings.Join(parts, join) + `)`, nil
}

func cmpOp(op string) string {
	switch op {
	case "gte":
		return ">="
	case "lte":
		return "<="
	}
	return "="
}

func nameMatch(col, op, value string, arg func(any) string) string {
	if op == "contains" {
		return col + ` ILIKE ` + arg(likeContains(value)) + ` ESCAPE '\'`
	}
	return `LOWER(` + col + `) = LOWER(` + arg(value) + `)`
}

// smartMatches returns a CTE named matched(id, pos) holding the titles the
// rules select for the request's user, in the rules' order and capped at the
// rules' limit. args/arg accumulate the bind parameters.
func (a *API) smartMatches(r *http.Request, rules smartRules, arg func(any) string) (string, error) {
	where, err := smartWhere(rules, currentUserID(r), arg)
	if err != nil {
		return "", err
	}
	limit := ""
	if rules.Limit > 0 {
		limit = ` LIMIT ` + strconv.Itoa(rules.Limit)
	}
	order := smartSorts[rules.Sort]
	return `matched AS (
		SELECT id, (row_number() OVER (ORDER BY ` + order + `))::int AS pos FROM (
			SELECT mi.* FROM media_item mi
			WHERE ` + visibleMediaCond(r, "mi", arg) + ` AND ` + where + `
			ORDER BY ` + order + limit + `) mi
	)`, nil
}

func timeParseDate(s string) (any, error) {
	t, err := time.Parse("2006-01-02", s)
	return t, err
}
