package scanner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDVDTitlesSkipsMenusAndOrdersParts(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"VIDEO_TS.VOB", "VIDEO_TS.IFO", "VIDEO_TS.BUP",
		"VTS_01_0.VOB", "VTS_01_0.IFO", "VTS_01_1.VOB",
		"VTS_02_0.VOB", "VTS_02_2.VOB", "VTS_02_1.VOB", "VTS_02_4.VOB",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	titles, err := DVDTitles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(titles) != 2 || titles[0].Number != 1 || len(titles[0].Files) != 1 {
		t.Fatalf("titles = %+v", titles)
	}
	if titles[1].Number != 2 || len(titles[1].Files) != 3 {
		t.Fatalf("second title = %+v", titles[1])
	}
	for i, part := range []string{"VTS_02_1.VOB", "VTS_02_2.VOB", "VTS_02_4.VOB"} {
		if filepath.Base(titles[1].Files[i]) != part {
			t.Fatalf("part %d = %s", i, titles[1].Files[i])
		}
	}
	empty := t.TempDir()
	if _, err := FindDVDVideo(empty); err == nil {
		t.Fatal("a folder with no VIDEO_TS should not be a DVD")
	}
	if _, err := FindDVDVideo(filepath.Join(empty, "missing")); err == nil {
		t.Fatal("missing path should fail")
	}
	// dir itself holds the VOBs, so it is the video directory even though its
	// name is not VIDEO_TS.
	if got, err := FindDVDVideo(dir); err != nil || got != dir {
		t.Fatalf("FindDVDVideo(video dir) = %s, %v", got, err)
	}
	movie := t.TempDir()
	nested := filepath.Join(movie, "VIDEO_TS")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "VTS_01_1.VOB"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := FindDVDVideo(movie); err != nil || got != nested {
		t.Fatalf("FindDVDVideo(movie) = %s, %v", got, err)
	}
	// The larger title keeps the movie name. Title 2's parts are bigger here
	// only because the files are the same size each, so give title 2 an extra
	// byte via a second write in the name test below.
	one, err := DVDOutputNames("DADDY_RIDE_ALONG", titles[:1])
	if err != nil || len(one) != 1 || one[0] != "DADDY_RIDE_ALONG.mp4" {
		t.Fatalf("one title: %v %v", one, err)
	}
	if err := os.WriteFile(titles[1].Files[0], []byte("xxxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	many, err := DVDOutputNames("DADDY_RIDE_ALONG", titles)
	if err != nil || many[0] != "DADDY_RIDE_ALONG 1.mp4" || many[1] != "DADDY_RIDE_ALONG.mp4" {
		t.Fatalf("feature should take the folder name: %v %v", many, err)
	}
}

func TestEncodeDVDTitle(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	vts := filepath.Join(dir, "VIDEO_TS")
	if err := os.Mkdir(vts, 0o755); err != nil {
		t.Fatal(err)
	}
	writeVOB(t, filepath.Join(vts, "VTS_01_0.VOB"))
	writeVOB(t, filepath.Join(vts, "VTS_01_1.VOB"))
	writeVOB(t, filepath.Join(vts, "VTS_01_2.VOB"))
	titles, err := DVDTitles(vts)
	if err != nil || len(titles) != 1 || len(titles[0].Files) != 2 {
		t.Fatalf("titles = %+v, %v", titles, err)
	}
	dest := filepath.Join(dir, "DADDY_RIDE_ALONG.mp4")
	if err := encodeDVDTitle(context.Background(), titles[0], dest); err != nil {
		t.Fatal(err)
	}
	probe, err := Probe(context.Background(), dest)
	if err != nil || probeCodec(probe.VideoCodec) != "h264" || probe.DurationSeconds == nil || *probe.DurationSeconds < 1 {
		t.Fatalf("mp4 probes as %+v, %v", probe, err)
	}
	for _, name := range []string{"VTS_01_0.VOB", "VTS_01_1.VOB", "VTS_01_2.VOB"} {
		if _, err := os.Stat(filepath.Join(vts, name)); err != nil {
			t.Errorf("DVD file removed: %s", name)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".*"+dvdConvertSuffix)); len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.reelstash-dvdlist")); len(left) != 0 {
		t.Errorf("concat list left behind: %v", left)
	}
}

func TestDVDDiscRel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"reelstash/DADDY_RIDE_ALONG/VIDEO_TS/VTS_02_1.VOB", "reelstash/DADDY_RIDE_ALONG/VIDEO_TS"},
		{"reelstash/DADDY_RIDE_ALONG/video_ts/vts_01_1.vob", "reelstash/DADDY_RIDE_ALONG/video_ts"},
		{"VIDEO_TS/VTS_01_1.VOB", "VIDEO_TS"},
		{"reelstash/DADDY_RIDE_ALONG/VIDEO_TS/VTS_02_0.VOB", ""},
		{"reelstash/DADDY_RIDE_ALONG/VIDEO_TS/VIDEO_TS.VOB", ""},
		{"reelstash/DADDY_RIDE_ALONG/VIDEO_TS/VTS_01_1.IFO", ""},
		{"loose/VTS_01_1.VOB", ""},
		{"VTS_01_1.VOB", ""},
	}
	for _, c := range cases {
		if got := dvdDiscRel(c.in); got != c.want {
			t.Errorf("dvdDiscRel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := DVDMovieName("reelstash/DADDY_RIDE_ALONG/VIDEO_TS"); got != "DADDY_RIDE_ALONG" {
		t.Fatalf("movie name = %q", got)
	}
}

func writeVOB(t *testing.T, path string) {
	t.Helper()
	out, err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=25:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:v", "mpeg2video", "-b:v", "400k", "-c:a", "mp2", "-f", "vob", path).CombinedOutput()
	if err != nil {
		t.Fatalf("vob %s: %v: %s", filepath.Base(path), err, strings.TrimSpace(string(out)))
	}
}
