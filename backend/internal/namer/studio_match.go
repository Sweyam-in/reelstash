package namer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"reelstash/backend/internal/provider"
)

type studioCandidate struct {
	Provider string `json:"provider,omitempty"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Network  string `json:"network,omitempty"`
	Image    string `json:"image,omitempty"`
}

type storedStudioMetadata struct {
	Provider    string            `json:"provider,omitempty"`
	Name        string            `json:"name,omitempty"`
	ShortName   string            `json:"shortName,omitempty"`
	Description string            `json:"description,omitempty"`
	URL         string            `json:"url,omitempty"`
	Network     string            `json:"network,omitempty"`
	Aliases     []string          `json:"aliases,omitempty"`
	Candidates  []studioCandidate `json:"candidates,omitempty"`
}

type StudioMatchResult struct {
	Status     string `json:"status"`
	StudioID   int64  `json:"studioId"`
	PhotoError string `json:"photoError,omitempty"`
}

// SyncStudios looks up every studio that has not been checked yet, like
// SyncPerformers: providers in priority order, first exact match wins (every
// provider's in "best" mode), otherwise candidates from all of them.
func (e *Engine) SyncStudios(ctx context.Context, jobID int64) (SyncResult, error) {
	var result SyncResult
	providers := e.Providers.Enabled(provider.KindStudio)
	if e == nil || len(providers) == 0 {
		return result, errors.New("no metadata provider is configured for studios")
	}
	both := e.matchMode(ctx) == "best"
	rows, err := e.DB.Query(ctx, `
		SELECT id, name FROM studio
		WHERE match_status = 'unmatched'
		ORDER BY name
	`)
	if err != nil {
		return result, err
	}
	defer rows.Close()

	type row struct {
		id   int64
		name string
	}
	var queue []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.id, &item.name); err != nil {
			return result, err
		}
		queue = append(queue, item)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	rows.Close()

	result.Total = len(queue)
	e.writeJobProgress(ctx, jobID, result)
	auto := e.autoApply(ctx, "studios")

	for _, item := range queue {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		result.Current = item.name
		e.writeJobProgress(ctx, jobID, result)

		// Only the providers of the libraries this studio appears in.
		providers := e.entityProviders(ctx, provider.KindStudio, item.id)
		if len(providers) == 0 {
			continue
		}
		linked := 0
		var candidates []provider.StudioResult
		var firstErr error
		failed := 0
		for _, p := range providers {
			hits, err := p.SearchStudios(ctx, item.name)
			if err != nil {
				if isRateLimit(err) {
					result.Current = ""
					result.Stopped = p.Name() + " rate limit reached. Run the job again to continue."
					return result, nil
				}
				failed++
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			match, cands := classifyStudioHits(item.name, hits, auto)
			if match == nil {
				candidates = append(candidates, cands...)
				continue
			}
			if _, err := e.applyStudio(ctx, item.id, *match); err != nil {
				return result, err
			}
			linked++
			if !both {
				break
			}
		}
		switch {
		case linked > 0:
			result.Matched++
		case len(candidates) > 0:
			if err := e.saveStudioCandidates(ctx, item.id, candidates); err != nil {
				return result, err
			}
			result.Ambiguous++
		case failed == len(providers) && firstErr != nil:
			return result, firstErr
		default:
			if err := e.markStudioNoMatch(ctx, item.id); err != nil {
				return result, err
			}
			result.NoMatch++
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

// MatchStudio links one manually chosen provider record to a studio and
// downloads its logo. A studio can be linked to one record per provider.
func (e *Engine) MatchStudio(ctx context.Context, studioID int64, providerID, externalID string) (StudioMatchResult, error) {
	result := StudioMatchResult{Status: "matched", StudioID: studioID}
	p, ok := e.Providers.Get(providerID)
	if e == nil || !ok || !p.Enabled() {
		return result, fmt.Errorf("provider %q is not configured", providerID)
	}
	remote, err := p.GetStudio(ctx, externalID)
	if err != nil {
		return result, err
	}
	photoError, err := e.applyStudio(ctx, studioID, remote)
	if err != nil {
		return result, err
	}
	result.PhotoError = photoError
	return result, nil
}

func classifyStudioHits(localName string, hits []provider.StudioResult, auto bool) (*provider.StudioResult, []provider.StudioResult) {
	var exact []provider.StudioResult
	for _, hit := range hits {
		if hit.Ref.ExternalID == "" {
			continue
		}
		names := append([]string{hit.Name, hit.ShortName}, hit.Aliases...)
		for _, name := range names {
			if sameStudioName(name, localName) {
				exact = append(exact, hit)
				break
			}
		}
	}
	if len(exact) == 1 && auto {
		match := exact[0]
		return &match, nil
	}
	if len(exact) > 0 {
		return nil, limitStudioCandidates(exact)
	}
	return nil, limitStudioCandidates(hits)
}

func limitStudioCandidates(hits []provider.StudioResult) []provider.StudioResult {
	out := make([]provider.StudioResult, 0, len(hits))
	for _, hit := range hits {
		if hit.Ref.ExternalID == "" || hit.Name == "" {
			continue
		}
		out = append(out, hit)
		if len(out) == maxPerformerCandidates {
			break
		}
	}
	return out
}

func sameStudioName(a, b string) bool {
	if strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) {
		return true
	}
	left, right := compactStudioName(a), compactStudioName(b)
	return len(left) >= 4 && left == right
}

func compactStudioName(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (e *Engine) applyStudio(ctx context.Context, studioID int64, remote provider.StudioResult) (string, error) {
	own := storedStudioMetadata{
		Provider:    remote.Ref.Provider,
		Name:        remote.Name,
		ShortName:   remote.ShortName,
		Description: remote.Description,
		URL:         remote.URL,
		Network:     remote.Network,
		Aliases:     remote.Aliases,
	}
	merged := own

	var oldRaw []byte
	var otherLinks int
	if err := e.DB.QueryRow(ctx, `
		SELECT remote_metadata, (SELECT COUNT(*) FROM match_link WHERE kind = 'studio' AND entity_id = $1 AND provider <> $2)
		FROM studio WHERE id = $1
	`, studioID, remote.Ref.Provider).Scan(&oldRaw, &otherLinks); err != nil {
		return "", fmt.Errorf("studio not found")
	}
	if otherLinks > 0 && len(oldRaw) > 0 {
		var old storedStudioMetadata
		if json.Unmarshal(oldRaw, &old) == nil {
			if merged.ShortName == "" {
				merged.ShortName = old.ShortName
			}
			if merged.Description == "" {
				merged.Description = old.Description
			}
			if merged.URL == "" {
				merged.URL = old.URL
			}
			if merged.Network == "" {
				merged.Network = old.Network
			}
			merged.Aliases = mergeNames(own.Aliases, old.Aliases)
		}
	}
	mergedRaw, err := json.Marshal(merged)
	if err != nil {
		return "", err
	}
	ownRaw, err := json.Marshal(own)
	if err != nil {
		return "", err
	}

	tx, err := e.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if err := upsertLink(ctx, tx, provider.KindStudio, studioID, remote.Ref, ownRaw); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE studio SET
			match_status = 'matched',
			remote_metadata = $2,
			description = CASE WHEN $5 THEN COALESCE(NULLIF($3, ''), description) ELSE COALESCE(description, NULLIF($3, '')) END,
			website = CASE WHEN $5 THEN COALESCE(NULLIF($4, ''), website) ELSE COALESCE(website, NULLIF($4, '')) END,
			matched_at = now(),
			updated_at = now()
		WHERE id = $1
	`, studioID, mergedRaw, remote.Description, remote.URL, otherLinks == 0); err != nil {
		return "", wrapLinkConflict(err, "studio")
	}
	for _, alias := range remote.Aliases {
		if _, err := tx.Exec(ctx, `
			INSERT INTO studio_alias (studio_id, alias) VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, studioID, alias); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}

	if remote.ImageURL != "" {
		save := e.ReplaceStudioLogo
		if otherLinks > 0 {
			save = e.saveStudioLogo // another provider's logo stays
		}
		if err := save(ctx, studioID, remote.ImageURL); err != nil {
			return "saved the match, but the studio image could not be downloaded", nil
		}
	}
	return "", nil
}

func mergeNames(lists ...[]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, list := range lists {
		for _, n := range list {
			if k := strings.ToLower(n); n != "" && !seen[k] {
				seen[k] = true
				out = append(out, n)
			}
		}
	}
	return out
}

// saveStudioLogo downloads a logo only when the studio has none.
func (e *Engine) saveStudioLogo(ctx context.Context, studioID int64, imageURL string) error {
	return e.storeLinkedImage(ctx, "studio", "logo_image_id", "LOGO", studioID, imageURL, false)
}

func (e *Engine) saveStudioCandidates(ctx context.Context, studioID int64, hits []provider.StudioResult) error {
	candidates := make([]studioCandidate, 0, len(hits))
	for _, hit := range hits {
		candidates = append(candidates, studioCandidate{
			Provider: hit.Ref.Provider,
			ID:       hit.Ref.ExternalID,
			Name:     hit.Name,
			Network:  hit.Network,
			Image:    hit.ImageURL,
		})
	}
	raw, err := json.Marshal(storedStudioMetadata{Candidates: candidates})
	if err != nil {
		return err
	}
	// A record already linked to a provider keeps its match; candidates are only
	// for records with nothing linked.
	_, err = e.DB.Exec(ctx, `
		UPDATE studio SET
			match_status = 'ambiguous',
			remote_metadata = $2,
			updated_at = now()
		WHERE id = $1
		  AND NOT EXISTS (SELECT 1 FROM match_link WHERE kind = 'studio' AND entity_id = $1)
	`, studioID, raw)
	if err != nil {
		return err
	}
	return nil
}

func (e *Engine) markStudioNoMatch(ctx context.Context, studioID int64) error {
	tag, err := e.DB.Exec(ctx, `
		UPDATE studio SET
			match_status = 'no_match',
			remote_metadata = NULL,
			updated_at = now()
		WHERE id = $1
	`, studioID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("studio not found")
	}
	return nil
}
