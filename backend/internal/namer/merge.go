package namer

import (
	"context"
	"strings"

	"reelstash/backend/internal/names"
	"reelstash/backend/internal/provider"
	"reelstash/backend/internal/scanner"
)

// currentMediaState is what a title already has, for merging a new record in.
type currentMediaState struct {
	Title       string
	FromFile    bool // the title is only its file name, not real metadata
	Description string
	Studio      string
	Date        string
	HasPoster   bool
	Performers  []string
	Categories  []string
}

func (e *Engine) currentMedia(ctx context.Context, mediaItemID int64) (currentMediaState, error) {
	var cur currentMediaState
	var source string
	if err := e.DB.QueryRow(ctx, `
		SELECT mi.title, mi.metadata_source, COALESCE(mi.description, ''), COALESCE(s.name, ''),
		       COALESCE(mi.release_date::text, ''), mi.poster_image_id IS NOT NULL
		FROM media_item mi LEFT JOIN studio s ON s.id = mi.studio_id WHERE mi.id = $1
	`, mediaItemID).Scan(&cur.Title, &source, &cur.Description, &cur.Studio, &cur.Date, &cur.HasPoster); err != nil {
		return cur, err
	}
	cur.FromFile = source == "FILENAME"
	names := func(query string) []string {
		rows, err := e.DB.Query(ctx, query, mediaItemID)
		if err != nil {
			return nil
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var n string
			if rows.Scan(&n) == nil {
				out = append(out, n)
			}
		}
		return out
	}
	cur.Performers = names(`SELECT p.name FROM media_item_performer m JOIN performer p ON p.id = m.performer_id WHERE m.media_item_id = $1 ORDER BY m.position`)
	cur.Categories = names(`SELECT c.name FROM media_item_category m JOIN category c ON c.id = m.category_id WHERE m.media_item_id = $1 ORDER BY c.name`)
	return cur, nil
}

// mergeMedia combines a title's existing details with a new provider record:
// existing values win, the record fills what is missing, and performers and
// categories are combined (existing first).
func mergeMedia(cur currentMediaState, in provider.MediaResult) provider.MediaResult {
	out := in
	if !cur.FromFile && cur.Title != "" {
		out.Title = cur.Title
	}
	if cur.Description != "" {
		out.Description = cur.Description
	}
	if cur.Studio != "" {
		out.Studio = cur.Studio
	}
	if cur.Date != "" {
		out.Date = cur.Date
	}
	if cur.HasPoster {
		out.PosterURL = ""
	}
	out.Performers = mergePeople(cur.Performers, in.Performers)
	out.Tags = mergeNames(cur.Categories, in.Tags)
	return out
}

// nfoIDs are the provider ids to write into a title's NFO: the record being
// applied first, then its links at other providers.
func (e *Engine) nfoIDs(ctx context.Context, mediaItemID int64, ref provider.Ref) []scanner.NFOID {
	ids := []scanner.NFOID{}
	if ref.Provider != "" && ref.ExternalID != "" {
		ids = append(ids, scanner.NFOID{Provider: ref.Provider, ExternalID: ref.ExternalID})
	}
	for _, id := range scanner.MediaLinkIDs(ctx, e.DB, mediaItemID) {
		if !strings.EqualFold(id.Provider, ref.Provider) {
			ids = append(ids, id)
		}
	}
	return ids
}

// mergePeople combines performer lists, dropping names that differ only in
// case, accents, punctuation or word order (the first spelling wins).
func mergePeople(lists ...[]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, list := range lists {
		for _, n := range list {
			k := names.Key(n)
			if k == "" {
				k = strings.ToLower(n)
			}
			if n != "" && !seen[k] {
				seen[k] = true
				out = append(out, n)
			}
		}
	}
	return out
}
