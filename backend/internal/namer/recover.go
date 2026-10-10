package namer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"reelstash/backend/internal/provider"
)

// LinkOnly links a title to a provider record without touching the file, NFO
// or the title's details. It is for titles whose metadata is already right but
// that carry no provider link (for example matched from an older NFO).
func (e *Engine) LinkOnly(ctx context.Context, mediaItemID int64, providerID, externalID string) error {
	p, ok := e.Providers.Get(providerID)
	if !ok || !p.Enabled() || !p.Capabilities().Media {
		return fmt.Errorf("provider %q is not configured", providerID)
	}
	res, err := p.GetMedia(ctx, externalID) // confirms the record exists
	if err != nil {
		return fmt.Errorf("fetch from %s: %w", p.Name(), err)
	}
	e.recordMediaLink(ctx, mediaItemID, res.Ref)
	return nil
}

// RecoverResult is the progress payload of a LINK_RECOVERY job.
type RecoverResult struct {
	Total     int    `json:"total"`
	Processed int    `json:"processed"`
	Linked    int    `json:"linked"`
	NotFound  int    `json:"notFound"`
	Current   string `json:"current,omitempty"`
	Stopped   string `json:"stopped,omitempty"`
}

type recoverRow struct {
	id                  int64
	title, date, studio string
}

// RecoverLinks finds the provider record for titles that were matched but have
// no provider link. Nothing on disk changes: a link is only recorded when a
// provider's record has the same title and the same date (or, with no date on
// either side, the same studio), so a near miss is left alone.
func (e *Engine) RecoverLinks(ctx context.Context, jobID int64) (RecoverResult, error) {
	var result RecoverResult
	providers := e.Providers.Enabled(provider.KindMedia)
	if len(providers) == 0 {
		return result, errors.New("no metadata provider is configured")
	}
	both := e.matchMode(ctx) == "best"

	rows, err := e.DB.Query(ctx, `
		SELECT mi.id, mi.title, COALESCE(mi.release_date::text, ''), COALESCE(s.name, '')
		FROM media_item mi LEFT JOIN studio s ON s.id = mi.studio_id
		WHERE (mi.metadata_source = 'NFO' OR mi.match_status = 'matched') AND mi.match_status <> 'rejected'
		  AND NOT EXISTS (SELECT 1 FROM match_link l WHERE l.kind = 'media' AND l.entity_id = mi.id)
		ORDER BY mi.id
	`)
	if err != nil {
		return result, err
	}
	var queue []recoverRow
	for rows.Next() {
		var r recoverRow
		if err := rows.Scan(&r.id, &r.title, &r.date, &r.studio); err != nil {
			rows.Close()
			return result, err
		}
		queue = append(queue, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return result, err
	}

	result.Total = len(queue)
	e.writeJobProgress(ctx, jobID, result)
	for _, item := range queue {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		result.Current = item.title
		e.writeJobProgress(ctx, jobID, result)

		rejected := e.RejectedMatches(ctx, item.id)
		linked := 0
		for _, p := range e.mediaProviders(ctx, item.id, provider.KindMedia) {
			cands, err := p.SearchMedia(ctx, provider.MediaQuery{Text: item.title})
			if err != nil {
				if isRateLimit(err) {
					result.Current = ""
					result.Stopped = p.Name() + " rate limit reached. Run the job again to continue."
					return result, nil
				}
				continue
			}
			var same []provider.MediaResult
			for _, c := range cands {
				if !rejected[rejectKey(p.ID(), c.Result.Ref.ExternalID)] && sameRecord(item, c.Result) {
					same = append(same, c.Result)
				}
			}
			if len(same) != 1 { // none, or ambiguous: leave it for a human
				continue
			}
			e.recordMediaLink(ctx, item.id, same[0].Ref)
			linked++
			if !both {
				break
			}
		}
		if linked > 0 {
			result.Linked++
		} else {
			result.NotFound++
		}
		result.Processed++
		e.writeJobProgress(ctx, jobID, result)

		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(350 * time.Millisecond):
		}
	}
	result.Current = ""
	return result, nil
}

func normalizeTitle(s string) string { return NormalizePerformerName(s) }

// sameRecord reports whether a provider record is the title we already have:
// same title, and the same date, or the same studio when a date is missing.
func sameRecord(item recoverRow, rec provider.MediaResult) bool {
	if normalizeTitle(item.title) == "" || normalizeTitle(item.title) != normalizeTitle(rec.Title) {
		return false
	}
	if item.date != "" && rec.Date != "" {
		return item.date == rec.Date
	}
	return item.studio != "" && rec.Studio != "" && compactStudioName(item.studio) == compactStudioName(rec.Studio)
}
