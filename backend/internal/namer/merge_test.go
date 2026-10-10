package namer

import "testing"

func TestMergePeopleIgnoresSpellingVariants(t *testing.T) {
	got := mergePeople([]string{"Drake North", "Daddy Dan"}, []string{"DRAKE NORTH", "Dan, Daddy", "Greg Dixxon"})
	want := []string{"Drake North", "Daddy Dan", "Greg Dixxon"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
