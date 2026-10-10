package namer

import (
	"context"
	"encoding/json"
)

// saveSuggestions keeps the candidates for a title that needs manual review.
func (e *Engine) saveSuggestions(ctx context.Context, mediaItemID int64, cands []CandidateView) {
	raw, err := json.Marshal(cands)
	if err != nil {
		return
	}
	e.DB.Exec(ctx, `
		INSERT INTO media_match_suggestion (media_item_id, candidates) VALUES ($1, $2)
		ON CONFLICT (media_item_id) DO UPDATE SET candidates = EXCLUDED.candidates, updated_at = now()
	`, mediaItemID, raw)
}

// clearSuggestions drops a title's waiting candidates (it was matched, or the
// search found nothing).
func (e *Engine) clearSuggestions(ctx context.Context, mediaItemID int64) {
	e.DB.Exec(ctx, `DELETE FROM media_match_suggestion WHERE media_item_id = $1`, mediaItemID)
}
