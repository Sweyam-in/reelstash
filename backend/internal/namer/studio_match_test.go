package namer

import (
	"testing"

	"reelstash/backend/internal/provider"
)

func TestClassifyStudioHits(t *testing.T) {
	hits := []provider.StudioResult{
		{Ref: provider.Ref{ExternalID: "1"}, Name: "Evil Angel"},
		{Ref: provider.Ref{ExternalID: "2"}, Name: "Evil Angel Films"},
	}
	match, candidates := classifyStudioHits("EvilAngel", hits, false)
	// Automatic matching off: an exact hit is only a suggestion.
	if match != nil || len(candidates) != 1 || candidates[0].Ref.ExternalID != "1" {
		t.Fatalf("compact match = %+v candidates %d", match, len(candidates))
	}
	// On: the one exact hit is applied.
	if match, _ := classifyStudioHits("EvilAngel", hits, true); match == nil || match.Ref.ExternalID != "1" {
		t.Fatalf("auto match = %+v", match)
	}

	ambiguous := []provider.StudioResult{
		{Ref: provider.Ref{ExternalID: "1"}, Name: "Brazzers"},
		{Ref: provider.Ref{ExternalID: "2"}, Name: "Brazzers Exxtra"},
	}
	match, candidates = classifyStudioHits("Studio", ambiguous, false)
	if match != nil || len(candidates) != 2 {
		t.Fatalf("expected two candidates, match=%v candidates=%d", match, len(candidates))
	}
	// No exact hit: nothing is applied even with automatic matching on.
	if match, _ := classifyStudioHits("Studio", ambiguous, true); match != nil {
		t.Fatalf("ambiguous auto match = %+v", match)
	}
}
