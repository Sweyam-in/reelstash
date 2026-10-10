package namer

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"reelstash/backend/internal/provider"
)

// rejectKey namespaces an external id by provider (ids are only unique per provider).
func rejectKey(providerID, externalID string) string { return providerID + "\x00" + externalID }

// RejectedPerformers are the provider records marked as wrong for a performer,
// keyed by rejectKey(provider, externalID).
func (e *Engine) RejectedPerformers(ctx context.Context, performerID int64) map[string]bool {
	set := map[string]bool{}
	rows, err := e.DB.Query(ctx, `SELECT provider, external_id FROM performer_rejected_match WHERE performer_id = $1`, performerID)
	if err != nil {
		return set
	}
	defer rows.Close()
	for rows.Next() {
		var prov, ext string
		if rows.Scan(&prov, &ext) == nil {
			set[rejectKey(prov, ext)] = true
		}
	}
	return set
}

// RejectedMatches are the provider records marked as wrong for a title, keyed
// by rejectKey(provider, externalID).
func (e *Engine) RejectedMatches(ctx context.Context, mediaItemID int64) map[string]bool {
	set := map[string]bool{}
	rows, err := e.DB.Query(ctx, `SELECT provider, scene_uuid FROM media_rejected_match WHERE media_item_id = $1`, mediaItemID)
	if err != nil {
		return set
	}
	defer rows.Close()
	for rows.Next() {
		var prov, ext string
		if rows.Scan(&prov, &ext) == nil {
			set[rejectKey(prov, ext)] = true
		}
	}
	return set
}

// recordMediaLink links a title to a provider record. The title stays linked to
// its records at other providers; only this provider's link is replaced.
func (e *Engine) recordMediaLink(ctx context.Context, mediaItemID int64, ref provider.Ref) {
	e.DB.Exec(ctx, `
		INSERT INTO match_link (kind, entity_id, provider, external_id) VALUES ('media', $1, $2, $3)
		ON CONFLICT (kind, entity_id, provider) DO UPDATE SET external_id = EXCLUDED.external_id, matched_at = now()
	`, mediaItemID, ref.Provider, ref.ExternalID)
	e.DB.Exec(ctx, `UPDATE media_item SET match_status = 'matched' WHERE id = $1`, mediaItemID)
	e.clearSuggestions(ctx, mediaItemID)
	e.DB.Exec(ctx, `DELETE FROM media_rejected_match WHERE media_item_id = $1 AND provider = $2 AND scene_uuid = $3`,
		mediaItemID, ref.Provider, ref.ExternalID)
}

// execer is satisfied by both the pool and a transaction.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// upsertLink links a performer or studio to a provider record, keeping the
// provider's own metadata on the link. A provider record can be linked to only
// one entity, so a clash with another one reports a readable error.
func upsertLink(ctx context.Context, db execer, kind provider.Kind, entityID int64, ref provider.Ref, metadata []byte) error {
	_, err := db.Exec(ctx, `
		INSERT INTO match_link (kind, entity_id, provider, external_id, remote_metadata) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (kind, entity_id, provider) DO UPDATE
		SET external_id = EXCLUDED.external_id, remote_metadata = EXCLUDED.remote_metadata, matched_at = now()
	`, string(kind), entityID, ref.Provider, ref.ExternalID, metadata)
	return wrapLinkConflict(err, string(kind))
}

// wrapLinkConflict turns a unique violation into a message a user can act on.
func wrapLinkConflict(err error, noun string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("that record is already matched to another %s", noun)
	}
	return err
}
