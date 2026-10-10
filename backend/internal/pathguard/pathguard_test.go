package pathguard_test

import (
	"reflect"
	"testing"

	"reelstash/backend/internal/pathguard"
)

func TestGuard(t *testing.T) {
	g := pathguard.New([]string{"/mnt/disk1/downloads/incomplete"}, []pathguard.InUse{{Path: "/mnt/disk1/Movies/Film (2020)", Name: "Film"}})

	for _, c := range []struct {
		path            string
		locked, blocked bool
		inside          bool
	}{
		{"/mnt/disk1/downloads/incomplete", true, true, true},
		{"/mnt/disk1/downloads/incomplete/a/b.mkv", true, true, true},
		{"/mnt/disk1/downloads", false, true, false}, // holds the locked folder
		{"/mnt/disk1/downloads/incomplete-other", false, false, false},
		{"/mnt/disk1/Movies/Film (2020)/film.mkv", false, true, true},
		{"/mnt/disk1/Movies", false, true, false}, // holds a seeding torrent
		{"/mnt/disk1/Movies/Other", false, false, false},
		{"/mnt/disk2", false, false, false},
	} {
		if got := g.Locked(c.path); got != c.locked {
			t.Errorf("Locked(%s) = %v", c.path, got)
		}
		if got, why := g.Blocked(c.path); got != c.blocked || (got && why == "") {
			t.Errorf("Blocked(%s) = %v %q", c.path, got, why)
		}
		if got := g.Inside(c.path); got != c.inside {
			t.Errorf("Inside(%s) = %v", c.path, got)
		}
	}
	if got := g.LockedRel("/mnt/disk1"); !reflect.DeepEqual(got, []string{"downloads/incomplete"}) {
		t.Errorf("LockedRel = %v", got)
	}
	if got := g.LockedRel("/mnt/disk1/downloads/incomplete/x"); !reflect.DeepEqual(got, []string{""}) {
		t.Errorf("LockedRel inside = %v", got)
	}
	if got := g.LockedRel("/mnt/disk2"); len(got) != 0 {
		t.Errorf("LockedRel other disk = %v", got)
	}
	var nilGuard *pathguard.Guard
	if b, _ := nilGuard.Blocked("/x"); b || nilGuard.Locked("/x") {
		t.Error("nil guard blocks")
	}
}

func TestInUseSource(t *testing.T) {
	pathguard.SetInUseSource(func() []pathguard.InUse { return []pathguard.InUse{{Path: "/data/t", Name: "T"}} })
	defer pathguard.SetInUseSource(nil)
	if !pathguard.Load(t.Context(), nil).Inside("/data/t/file") {
		t.Fatal("in-use path from the source is not guarded")
	}
}
