package namer

import (
	"context"

	"reelstash/backend/internal/provider"
)

// Each library chooses the metadata providers its titles are matched with,
// in its own priority order (library_provider). A title outside every
// library, and performers/studios with no titles in one, fall back to every
// enabled provider in the global order.

// mediaProviders returns the enabled providers for a title, in its library's order.
func (e *Engine) mediaProviders(ctx context.Context, mediaItemID int64, kind provider.Kind) []provider.Provider {
	var libraryID *int64
	if err := e.DB.QueryRow(ctx, `SELECT library_id FROM media_item WHERE id = $1`, mediaItemID).Scan(&libraryID); err != nil || libraryID == nil {
		return e.Providers.Enabled(kind)
	}
	rows, err := e.DB.Query(ctx, `SELECT provider FROM library_provider WHERE library_id = $1 ORDER BY position, provider`, *libraryID)
	if err != nil {
		return e.Providers.Enabled(kind)
	}
	defer rows.Close()
	var out []provider.Provider
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			continue
		}
		if p, ok := e.Providers.Get(id); ok && p.Enabled() && p.Capabilities().Supports(kind) {
			out = append(out, p)
		}
	}
	return out
}

// entityProviders returns the enabled providers allowed for a performer or
// studio: those of any library holding one of its titles.
func (e *Engine) entityProviders(ctx context.Context, kind provider.Kind, entityID int64) []provider.Provider {
	var query string
	switch kind {
	case provider.KindPerformer:
		query = `SELECT DISTINCT lp.provider FROM media_item_performer mip JOIN media_item mi ON mi.id = mip.media_item_id
			JOIN library_provider lp ON lp.library_id = mi.library_id WHERE mip.performer_id = $1`
	case provider.KindStudio:
		query = `SELECT DISTINCT lp.provider FROM media_item mi JOIN library_provider lp ON lp.library_id = mi.library_id WHERE mi.studio_id = $1`
	default:
		return e.Providers.Enabled(kind)
	}
	var inLibrary bool
	existsQuery := map[provider.Kind]string{
		provider.KindPerformer: `SELECT EXISTS (SELECT 1 FROM media_item_performer mip JOIN media_item mi ON mi.id = mip.media_item_id WHERE mip.performer_id = $1 AND mi.library_id IS NOT NULL)`,
		provider.KindStudio:    `SELECT EXISTS (SELECT 1 FROM media_item mi WHERE mi.studio_id = $1 AND mi.library_id IS NOT NULL)`,
	}[kind]
	if err := e.DB.QueryRow(ctx, existsQuery, entityID).Scan(&inLibrary); err != nil || !inLibrary {
		return e.Providers.Enabled(kind)
	}
	rows, err := e.DB.Query(ctx, query, entityID)
	if err != nil {
		return e.Providers.Enabled(kind)
	}
	allowed := map[string]bool{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			allowed[id] = true
		}
	}
	rows.Close()
	var out []provider.Provider
	for _, p := range e.Providers.Enabled(kind) {
		if allowed[p.ID()] {
			out = append(out, p)
		}
	}
	return out
}
