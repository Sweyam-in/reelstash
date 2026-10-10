package namer

import (
	"context"
	"sync"
)

// LinkedRecord is the provider's own record for a title's link, for showing
// what the title is linked to.
type LinkedRecord struct {
	CandidateView
	URL string `json:"url,omitempty"`
	// Error is set when the provider could not return the record (offline,
	// disabled, record removed); the link itself is still valid.
	Error string `json:"error,omitempty"`
}

// LinkedRecords fetches the provider record behind each of a title's links.
func (e *Engine) LinkedRecords(ctx context.Context, mediaItemID int64) ([]LinkedRecord, error) {
	rows, err := e.DB.Query(ctx, `SELECT provider, external_id FROM match_link WHERE kind = 'media' AND entity_id = $1 ORDER BY matched_at`, mediaItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LinkedRecord
	for rows.Next() {
		var r LinkedRecord
		if err := rows.Scan(&r.Provider, &r.ExternalID); err != nil {
			return nil, err
		}
		r.UUID = r.ExternalID
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var wg sync.WaitGroup
	for i := range out {
		wg.Add(1)
		go func(r *LinkedRecord) {
			defer wg.Done()
			res, err := e.fetchMedia(ctx, r.Provider, r.ExternalID)
			if err != nil {
				r.Error = err.Error()
				return
			}
			r.Kind, r.Title, r.Site, r.Date = res.Kind, res.Title, res.Studio, res.Date
			r.Performers, r.Poster, r.URL = res.Performers, res.PosterURL, res.SourceURL
		}(&out[i])
	}
	wg.Wait()
	return out, nil
}
