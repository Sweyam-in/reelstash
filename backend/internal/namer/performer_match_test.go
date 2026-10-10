package namer

import (
	"testing"

	"reelstash/backend/internal/provider"
)

func TestClassifyPerformerHits(t *testing.T) {
	hits := []provider.PerformerResult{
		{Ref: provider.Ref{ExternalID: "1"}, Name: "Alex Chance", Aliases: []string{"Abbey Brooks"}},
		{Ref: provider.Ref{ExternalID: "2"}, Name: "Someone Else"},
	}
	match, candidates := classifyPerformerHits("Abbey Brooks", hits, false)
	// Automatic matching off: an exact alias hit is only a suggestion.
	if match != nil || len(candidates) != 1 || candidates[0].Ref.ExternalID != "1" {
		t.Fatalf("alias match = %+v candidates %d", match, len(candidates))
	}
	// On: the one exact (alias) hit is applied.
	if match, _ := classifyPerformerHits("Abbey Brooks", hits, true); match == nil || match.Ref.ExternalID != "1" {
		t.Fatalf("auto alias match = %+v", match)
	}

	ambiguous := []provider.PerformerResult{
		{Ref: provider.Ref{ExternalID: "1"}, Name: "Alex A"},
		{Ref: provider.Ref{ExternalID: "2"}, Name: "Alex B"},
	}
	match, candidates = classifyPerformerHits("Alex", ambiguous, false)
	if match != nil || len(candidates) != 2 {
		t.Fatalf("expected two candidates, match=%v candidates=%d", match, len(candidates))
	}
	if match, _ := classifyPerformerHits("Alex", ambiguous, true); match != nil {
		t.Fatalf("ambiguous auto match = %+v", match)
	}

	match, candidates = classifyPerformerHits("Nobody", nil, false)
	if match != nil || len(candidates) != 0 {
		t.Fatalf("expected no match")
	}
}

func TestSamePerformerNameIgnoresFormatting(t *testing.T) {
	same := [][2]string{
		{"Buck Richards", "buck  richards"},
		{"Richards, Buck", "Buck Richards"},
		{"Angela-White", "Angela White"},
		{"Chloé Lamour", "Chloe Lamour"},
	}
	for _, pair := range same {
		if !samePerformerName(pair[0], pair[1]) {
			t.Errorf("%q should equal %q", pair[0], pair[1])
		}
	}
	if samePerformerName("Buck Richards", "Buck Rogers") || samePerformerName("", "") {
		t.Error("different or empty names matched")
	}
}
