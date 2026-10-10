package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

type searchResult struct {
	Type  string `json:"type"`
	ID    int64  `json:"id"`
	Label string `json:"label"`
}

type scoredResult struct {
	searchResult
	score float64
}

const (
	searchTotalLimit = 30
	searchPerType    = 10
)

// matchScore ranks how well hay matches the query: exact (4) > prefix (3) >
// substring (2) > fuzzy/typo (1), plus word similarity as a tie-breaker.
// q is the raw query, registered once by the caller as a bind parameter.
func matchScore(hay, q string, arg func(any) string) string {
	prefix := arg(likePrefix(q))
	contains := arg(likeContains(q))
	return `(CASE WHEN lower(` + hay + `) = lower(` + q + `) THEN 4
		WHEN ` + hay + ` ILIKE ` + prefix + ` ESCAPE '\' THEN 3
		WHEN ` + hay + ` ILIKE ` + contains + ` ESCAPE '\' THEN 2
		ELSE 1 END + word_similarity(lower(` + q + `), lower(` + hay + `)))`
}

func likePrefix(value string) string {
	return strings.TrimPrefix(strings.TrimSuffix(likeContains(value), "%"), "%") + "%"
}

// handleSearch searches titles (by title, description and file name),
// performers and studios (including aliases), and series. Every query token
// must match as a prefix/substring (type-ahead) or via pg_trgm word
// similarity (typos). Hits are ranked exact > prefix > substring > fuzzy and
// merged into one list, so the best match overall comes first.
func (a *API) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"items": []searchResult{}})
		return
	}
	tokens := fuzzyTokens(q)
	ctx := r.Context()

	// run builds a fresh argument list per query ($1 is always the query) so
	// no query is handed a bind parameter it does not use.
	run := func(typ string, build func(arg func(any) string) string) []scoredResult {
		args := []any{q}
		arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
		out, err := a.searchQuery(ctx, typ, build(arg), args)
		if err != nil {
			slog.Error("search query failed", "type", typ, "err", err)
		}
		return out
	}

	filtered := libraryFiltered(r)
	var all []scoredResult

	// Titles: title/description, plus hits on the file name surfaced as the title.
	all = append(all, run("media_item", func(arg func(any) string) string {
		scoped := visibleMediaCond(r, "mi", arg)
		titleMatch := fuzzyMatch("mi.title", tokens, arg)
		fileMatch := fuzzyMatch("mf.file_name", tokens, arg)
		return `SELECT id, title, MAX(score) FROM (
			SELECT mi.id, mi.title, ` + matchScore("mi.title", "$1", arg) + ` AS score
			FROM media_item mi WHERE (` + titleMatch + `) AND ` + scoped + `
			UNION ALL
			SELECT mi.id, mi.title, 0.5 AS score
			FROM media_item mi WHERE COALESCE(mi.description, '') ILIKE ` + arg(likeContains(q)) + ` ESCAPE '\' AND ` + scoped + `
			UNION ALL
			SELECT mi.id, mi.title, ` + matchScore("mf.file_name", "$1", arg) + ` - 0.5 AS score
			FROM media_file mf JOIN media_item mi ON mi.id = mf.media_item_id
			WHERE (` + fileMatch + `) AND ` + scoped + `
		) t GROUP BY id, title ORDER BY 3 DESC LIMIT ` + strconv.Itoa(searchPerType)
	})...)

	// People and studios: the best of the name and any alias. Admins also find
	// those with no titles; users only those with a title they can see.
	entity := func(typ, table, aliasTable, fk, link, linkFK string) {
		all = append(all, run(typ, func(arg func(any) string) string {
			scope := ""
			if filtered {
				scope = ` AND EXISTS (SELECT 1 FROM ` + link + ` mi WHERE mi.` + linkFK + ` = e.id AND ` + visibleMediaCond(r, "mi", arg) + `)`
			}
			nameMatch := fuzzyMatch("e.name", tokens, arg)
			aliasMatch := fuzzyMatch("ea.alias", tokens, arg)
			return `SELECT id, name, MAX(score) FROM (
				SELECT e.id, e.name, ` + matchScore("e.name", "$1", arg) + ` AS score
				FROM ` + table + ` e WHERE (` + nameMatch + `)` + scope + `
				UNION ALL
				SELECT e.id, e.name, ` + matchScore("ea.alias", "$1", arg) + ` - 0.25 AS score
				FROM ` + table + ` e JOIN ` + aliasTable + ` ea ON ea.` + fk + ` = e.id
				WHERE (` + aliasMatch + `)` + scope + `
			) t GROUP BY id, name ORDER BY 3 DESC, name LIMIT ` + strconv.Itoa(searchPerType)
		})...)
	}
	// Performers are linked to titles through media_item_performer, so they
	// need their own visibility subquery.
	all = append(all, run("performer", func(arg func(any) string) string {
		scope := ""
		if filtered {
			scope = ` AND EXISTS (SELECT 1 FROM media_item_performer mip JOIN media_item mi ON mi.id = mip.media_item_id WHERE mip.performer_id = e.id AND ` + visibleMediaCond(r, "mi", arg) + `)`
		}
		nameMatch := fuzzyMatch("e.name", tokens, arg)
		aliasMatch := fuzzyMatch("ea.alias", tokens, arg)
		return `SELECT id, name, MAX(score) FROM (
			SELECT e.id, e.name, ` + matchScore("e.name", "$1", arg) + ` AS score
			FROM performer e WHERE (` + nameMatch + `)` + scope + `
			UNION ALL
			SELECT e.id, e.name, ` + matchScore("ea.alias", "$1", arg) + ` - 0.25 AS score
			FROM performer e JOIN performer_alias ea ON ea.performer_id = e.id
			WHERE (` + aliasMatch + `)` + scope + `
		) t GROUP BY id, name ORDER BY 3 DESC, name LIMIT ` + strconv.Itoa(searchPerType)
	})...)
	entity("studio", "studio", "studio_alias", "studio_id", "media_item", "studio_id")

	all = append(all, run("series", func(arg func(any) string) string {
		scope := ""
		if filtered {
			scope = ` AND EXISTS (SELECT 1 FROM media_item mi WHERE mi.series_id = e.id AND ` + visibleMediaCond(r, "mi", arg) + `)`
		}
		return `SELECT e.id, e.name, ` + matchScore("e.name", "$1", arg) + ` FROM series e
			WHERE (` + fuzzyMatch("e.name", tokens, arg) + `)` + scope + `
			ORDER BY 3 DESC, e.name LIMIT ` + strconv.Itoa(searchPerType)
	})...)

	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	if len(all) > searchTotalLimit {
		all = all[:searchTotalLimit]
	}
	results := make([]searchResult, 0, len(all))
	for _, s := range all {
		results = append(results, s.searchResult)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": results})
}

// searchQuery runs one (id, label, score) query and tags the rows with typ.
func (a *API) searchQuery(ctx context.Context, typ, sql string, args []any) ([]scoredResult, error) {
	rows, err := a.DB.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []scoredResult
	for rows.Next() {
		var s scoredResult
		if err := rows.Scan(&s.ID, &s.Label, &s.score); err != nil {
			return out, err
		}
		s.Type = typ
		out = append(out, s)
	}
	return out, rows.Err()
}
