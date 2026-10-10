package namer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"reelstash/backend/internal/names"
	"reelstash/backend/internal/provider"
)

const maxPerformerCandidates = 8

type performerCandidate struct {
	Provider       string `json:"provider,omitempty"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	Disambiguation string `json:"disambiguation,omitempty"`
	Image          string `json:"image,omitempty"`
}

type storedPerformerMetadata struct {
	Provider       string               `json:"provider,omitempty"`
	Name           string               `json:"name,omitempty"`
	Disambiguation string               `json:"disambiguation,omitempty"`
	Bio            string               `json:"bio,omitempty"`
	Aliases        []string             `json:"aliases,omitempty"`
	Gender         string               `json:"gender,omitempty"`
	Birthdate      string               `json:"birthdate,omitempty"`
	Birthplace     string               `json:"birthplace,omitempty"`
	Ethnicity      string               `json:"ethnicity,omitempty"`
	Nationality    string               `json:"nationality,omitempty"`
	HairColor      string               `json:"hairColor,omitempty"`
	EyeColor       string               `json:"eyeColor,omitempty"`
	Height         string               `json:"height,omitempty"`
	Measurements   string               `json:"measurements,omitempty"`
	Candidates     []performerCandidate `json:"candidates,omitempty"`
}

// SyncResult is the progress payload written onto a PERFORMER_SYNC job.
type SyncResult struct {
	Total     int    `json:"total"`
	Processed int    `json:"processed"`
	Matched   int    `json:"matched"`
	Ambiguous int    `json:"ambiguous"`
	NoMatch   int    `json:"noMatch"`
	Current   string `json:"current,omitempty"`
	Stopped   string `json:"stopped,omitempty"`
}

// PerformerMatchResult is returned by a single PERFORMER_MATCH job.
type PerformerMatchResult struct {
	Status      string `json:"status"`
	PerformerID int64  `json:"performerId"`
	PhotoError  string `json:"photoError,omitempty"`
}

// isRateLimit reports whether a provider error was an HTTP 429.
func isRateLimit(err error) bool { return err != nil && strings.Contains(err.Error(), "429") }

// SyncPerformers looks up every performer that has not been checked yet.
// Providers are tried in priority order: an exact name match is linked, and in
// the default priority mode the search stops at the first provider that finds
// one (in "best" mode every provider's exact match is linked). A performer with
// no exact match anywhere is stored with the candidates from all providers for
// a manual choice.
//
// With retryUnresolved, performers that previously had no match or an
// ambiguous result are looked up again too.
func (e *Engine) SyncPerformers(ctx context.Context, jobID int64, retryUnresolved bool) (SyncResult, error) {
	var result SyncResult
	providers := e.Providers.Enabled(provider.KindPerformer)
	if e == nil || len(providers) == 0 {
		return result, errors.New("no metadata provider is configured for performers")
	}
	both := e.matchMode(ctx) == "best"
	statuses := []string{"unmatched"}
	if retryUnresolved {
		statuses = append(statuses, "no_match", "ambiguous")
	}
	rows, err := e.DB.Query(ctx, `
		SELECT id, name FROM performer
		WHERE match_status = ANY($1)
		ORDER BY name
	`, statuses)
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
	auto := e.autoApply(ctx, "performers")

	for _, item := range queue {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		result.Current = item.name
		e.writeJobProgress(ctx, jobID, result)

		// Only the providers of the libraries this performer appears in.
		providers := e.entityProviders(ctx, provider.KindPerformer, item.id)
		if len(providers) == 0 {
			continue
		}
		rejected := e.RejectedPerformers(ctx, item.id)
		linked := 0
		var candidates []provider.PerformerResult
		var firstErr error
		failed := 0
		for _, p := range providers {
			hits, err := p.SearchPerformers(ctx, item.name)
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
			kept := hits[:0]
			for _, hit := range hits {
				if !rejected[rejectKey(p.ID(), hit.Ref.ExternalID)] {
					kept = append(kept, hit)
				}
			}
			match, cands := classifyPerformerHits(item.name, kept, auto)
			if match == nil {
				candidates = append(candidates, cands...)
				continue
			}
			if _, err := e.applyPerformer(ctx, item.id, *match); err != nil {
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
			if err := e.savePerformerCandidates(ctx, item.id, candidates); err != nil {
				return result, err
			}
			result.Ambiguous++
		case failed == len(providers) && firstErr != nil:
			return result, firstErr
		default:
			if err := e.markPerformerNoMatch(ctx, item.id); err != nil {
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

// MatchPerformer links one manually chosen provider record to a performer and
// downloads their photo. A performer can be linked to one record per provider.
func (e *Engine) MatchPerformer(ctx context.Context, performerID int64, providerID, externalID string) (PerformerMatchResult, error) {
	result := PerformerMatchResult{Status: "matched", PerformerID: performerID}
	p, ok := e.Providers.Get(providerID)
	if e == nil || !ok || !p.Enabled() {
		return result, fmt.Errorf("provider %q is not configured", providerID)
	}
	remote, err := p.GetPerformer(ctx, externalID)
	if err != nil {
		return result, err
	}
	photoError, err := e.applyPerformer(ctx, performerID, remote)
	if err != nil {
		return result, err
	}
	result.PhotoError = photoError
	e.DB.Exec(ctx, `DELETE FROM performer_rejected_match WHERE performer_id = $1 AND provider = $2 AND external_id = $3`,
		performerID, providerID, externalID)
	return result, nil
}

func classifyPerformerHits(localName string, hits []provider.PerformerResult, auto bool) (*provider.PerformerResult, []provider.PerformerResult) {
	var exact []provider.PerformerResult
	for _, hit := range hits {
		if hit.Ref.ExternalID == "" {
			continue
		}
		if samePerformerName(hit.Name, localName) {
			exact = append(exact, hit)
			continue
		}
		for _, alias := range hit.Aliases {
			if samePerformerName(alias, localName) {
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
		return nil, limitCandidates(exact)
	}
	return nil, limitCandidates(hits)
}

func limitCandidates(hits []provider.PerformerResult) []provider.PerformerResult {
	out := make([]provider.PerformerResult, 0, len(hits))
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

// samePerformerName compares names ignoring case, accents, punctuation and
// word order, so "Buck  Richards", "Richards, Buck" and "buck-richards" agree.
func samePerformerName(a, b string) bool {
	ka, kb := NormalizePerformerName(a), NormalizePerformerName(b)
	if ka == "" || kb == "" {
		return false
	}
	if ka == kb {
		return true
	}
	return sortedTokens(ka) == sortedTokens(kb)
}

// NormalizePerformerName lower-cases a name, folds common accents and reduces
// everything that is not a letter or digit to single spaces.
func NormalizePerformerName(name string) string { return names.Normalize(name) }

func sortedTokens(normalized string) string { return names.SortedTokens(normalized) }

// SortedNameKey is the order-independent key used to spot duplicate performers.
func SortedNameKey(name string) string {
	return sortedTokens(NormalizePerformerName(name))
}

func (e *Engine) applyPerformer(ctx context.Context, performerID int64, remote provider.PerformerResult) (string, error) {
	own := metadataFromRemote(remote)
	merged := own

	var oldRaw []byte
	var otherLinks int
	if err := e.DB.QueryRow(ctx, `
		SELECT remote_metadata, (SELECT COUNT(*) FROM match_link WHERE kind = 'performer' AND entity_id = $1 AND provider <> $2)
		FROM performer WHERE id = $1
	`, performerID, remote.Ref.Provider).Scan(&oldRaw, &otherLinks); err != nil {
		return "", fmt.Errorf("performer not found")
	}
	if otherLinks > 0 && len(oldRaw) > 0 {
		var old storedPerformerMetadata
		if json.Unmarshal(oldRaw, &old) == nil {
			merged = mergePerformerMetadata(own, old)
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
	if err := upsertLink(ctx, tx, provider.KindPerformer, performerID, remote.Ref, ownRaw); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE performer SET
			match_status = 'matched',
			remote_metadata = $2,
			matched_at = now(),
			updated_at = now()
		WHERE id = $1
	`, performerID, mergedRaw); err != nil {
		return "", wrapLinkConflict(err, "performer")
	}
	for _, alias := range remote.Aliases {
		if _, err := tx.Exec(ctx, `
			INSERT INTO performer_alias (performer_id, alias) VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, performerID, alias); err != nil {
			return "", err
		}
	}
	// Honour the user's per-field provider choices over the plain merge.
	if err := RebuildPerformerMetadata(ctx, tx, performerID); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}

	if remote.ImageURL != "" {
		// The photo joins the gallery; it is shown unless another provider's
		// photo is already showing or the user picked one.
		if err := e.AddPerformerPhoto(ctx, performerID, remote.Ref.Provider, remote.ImageURL, otherLinks == 0); err != nil {
			return "saved the match, but the photo could not be downloaded", nil
		}
	}
	return "", nil
}

// mergePerformerMetadata keeps fresh's values and fills its blanks from old.
func mergePerformerMetadata(fresh, old storedPerformerMetadata) storedPerformerMetadata {
	fill := func(dst *string, src string) {
		if *dst == "" {
			*dst = src
		}
	}
	out := fresh
	fill(&out.Name, old.Name)
	fill(&out.Disambiguation, old.Disambiguation)
	fill(&out.Bio, old.Bio)
	fill(&out.Gender, old.Gender)
	fill(&out.Birthdate, old.Birthdate)
	fill(&out.Birthplace, old.Birthplace)
	fill(&out.Ethnicity, old.Ethnicity)
	fill(&out.Nationality, old.Nationality)
	fill(&out.HairColor, old.HairColor)
	fill(&out.EyeColor, old.EyeColor)
	fill(&out.Height, old.Height)
	fill(&out.Measurements, old.Measurements)
	seen := map[string]bool{}
	out.Aliases = nil
	for _, a := range append(append([]string{}, fresh.Aliases...), old.Aliases...) {
		if k := strings.ToLower(a); a != "" && !seen[k] {
			seen[k] = true
			out.Aliases = append(out.Aliases, a)
		}
	}
	out.Candidates = nil
	return out
}

func metadataFromRemote(remote provider.PerformerResult) storedPerformerMetadata {
	return storedPerformerMetadata{
		Provider:       remote.Ref.Provider,
		Name:           remote.Name,
		Disambiguation: remote.Disambiguation,
		Bio:            remote.Bio,
		Aliases:        remote.Aliases,
		Gender:         remote.Gender,
		Birthdate:      remote.Birthdate,
		Birthplace:     remote.Birthplace,
		Ethnicity:      remote.Ethnicity,
		Nationality:    remote.Nationality,
		HairColor:      remote.HairColor,
		EyeColor:       remote.EyeColor,
		Height:         remote.Height,
		Measurements:   remote.Measurements,
	}
}

func (e *Engine) savePerformerCandidates(ctx context.Context, performerID int64, hits []provider.PerformerResult) error {
	candidates := make([]performerCandidate, 0, len(hits))
	for _, hit := range hits {
		candidates = append(candidates, performerCandidate{
			Provider:       hit.Ref.Provider,
			ID:             hit.Ref.ExternalID,
			Name:           hit.Name,
			Disambiguation: hit.Disambiguation,
			Image:          hit.ImageURL,
		})
	}
	raw, err := json.Marshal(storedPerformerMetadata{Candidates: candidates})
	if err != nil {
		return err
	}
	// A record already linked to a provider keeps its match; candidates are only
	// for records with nothing linked.
	_, err = e.DB.Exec(ctx, `
		UPDATE performer SET
			match_status = 'ambiguous',
			remote_metadata = $2,
			updated_at = now()
		WHERE id = $1
		  AND NOT EXISTS (SELECT 1 FROM match_link WHERE kind = 'performer' AND entity_id = $1)
	`, performerID, raw)
	if err != nil {
		return err
	}
	return nil
}

func (e *Engine) markPerformerNoMatch(ctx context.Context, performerID int64) error {
	tag, err := e.DB.Exec(ctx, `
		UPDATE performer SET
			match_status = 'no_match',
			remote_metadata = NULL,
			updated_at = now()
		WHERE id = $1
	`, performerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("performer not found")
	}
	return nil
}

func (e *Engine) writeJobProgress(ctx context.Context, jobID int64, result any) {
	if jobID == 0 {
		return
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return
	}
	_, _ = e.DB.Exec(ctx, `UPDATE background_job SET result = $2 WHERE id = $1`, jobID, raw)
}
