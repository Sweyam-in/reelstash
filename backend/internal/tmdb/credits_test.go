package tmdb

import "testing"

func TestCrewCreditsDirectorsFirstAndDeduped(t *testing.T) {
	episode := []crewMember{{ID: 3, Name: "Ben Stiller", Job: "Director"}, {ID: 4, Name: "Dan Erickson", Job: "Writer"}}
	show := []crewMember{{ID: 4, Name: "Dan Erickson", Job: "Screenplay"}, {ID: 5, Name: "Someone", Job: "Producer"}, {ID: 3, Name: "ben stiller", Job: "Director"}}
	got := crewCredits(episode, show)
	if len(got) != 2 {
		t.Fatalf("got %d credits, want 2: %+v", len(got), got)
	}
	if got[0].Name != "Ben Stiller" || got[0].Job != "director" || got[0].Ref.ExternalID != "3" {
		t.Fatalf("first credit = %+v, want director Ben Stiller", got[0])
	}
	if got[1].Name != "Dan Erickson" || got[1].Job != "writer" {
		t.Fatalf("second credit = %+v, want writer Dan Erickson", got[1])
	}
}

func TestCharactersKeepsFirstRole(t *testing.T) {
	got := characters([]castMember{{Name: "Adam Scott", Character: "Mark S."}}, []castMember{{Name: "Adam Scott", Character: "Mark Scout"}, {Name: "No Role"}})
	if got["Adam Scott"] != "Mark S." || len(got) != 1 {
		t.Fatalf("characters = %v", got)
	}
}
