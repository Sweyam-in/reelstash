package scanner

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func encodeJPEG(t *testing.T, fill func(x, y int) color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 160, 90))
	for y := 0; y < 90; y++ {
		for x := 0; x < 160; x++ {
			img.Set(x, y, fill(x, y))
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestUsableFrame(t *testing.T) {
	black := encodeJPEG(t, func(x, y int) color.Color { return color.RGBA{0, 0, 0, 255} })
	nearBlack := encodeJPEG(t, func(x, y int) color.Color { return color.RGBA{6, 6, 6, 255} })
	flatWhite := encodeJPEG(t, func(x, y int) color.Color { return color.RGBA{240, 240, 240, 255} })
	rng := rand.New(rand.NewSource(1))
	busy := encodeJPEG(t, func(x, y int) color.Color {
		v := uint8(60 + rng.Intn(140))
		return color.RGBA{v, v, v, 255}
	})

	if UsableFrame(black) || UsableFrame(nearBlack) {
		t.Error("black frames must not be usable")
	}
	if UsableFrame(flatWhite) {
		t.Error("a flat solid frame must not be usable")
	}
	if !UsableFrame(busy) {
		t.Error("a bright detailed frame should be usable")
	}
	if UsableFrame([]byte("not a jpeg")) {
		t.Error("garbage must not be usable")
	}
}

func TestCandidateTimesStayInsideVideo(t *testing.T) {
	for _, d := range []float64{3, 9, 45, 600, 7200} {
		for _, at := range candidateTimes(d) {
			if at < 0 || at > d {
				t.Errorf("duration %v: candidate %v is outside the video", d, at)
			}
		}
	}
	if got := candidateTimes(0); len(got) == 0 {
		t.Error("unknown duration should still yield candidates")
	}
	if got := candidateTimes(600)[0]; got != 60 {
		t.Errorf("first candidate for a 10 minute video = %v, want 60", got)
	}
	if got := candidateTimes(30)[0]; got != 5 {
		t.Errorf("first candidate for a 30s video = %v, want the 5s floor", got)
	}
}

// makeTestVideo builds a clip that is black for its first 3 seconds, then a
// detailed test pattern, so a naive early-frame grab is black.
func makeTestVideo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	path := filepath.Join(t.TempDir(), "clip.mp4")
	cmd := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-y",
		"-f", "lavfi", "-i", "color=c=black:s=320x180:d=3:r=24",
		"-f", "lavfi", "-i", "testsrc2=s=320x180:d=9:r=24",
		"-filter_complex", "[0:v][1:v]concat=n=2:v=1:a=0[v]", "-map", "[v]",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot build test video: %v: %s", err, out)
	}
	return path
}

func TestExtractFrameAndPickBestFrame(t *testing.T) {
	video := makeTestVideo(t)
	ctx := context.Background()

	early, err := ExtractFrame(ctx, video, 1, 160)
	if err != nil {
		t.Fatal(err)
	}
	if UsableFrame(early) {
		t.Error("frame at 1s is inside the black intro and should be unusable")
	}

	late, err := ExtractFrame(ctx, video, 8, 160)
	if err != nil {
		t.Fatal(err)
	}
	if !UsableFrame(late) {
		t.Error("frame at 8s shows the test pattern and should be usable")
	}

	best, at, err := PickBestFrame(ctx, video, 12, 160)
	if err != nil {
		t.Fatal(err)
	}
	if !UsableFrame(best) {
		t.Errorf("PickBestFrame returned an unusable frame at %vs", at)
	}
}

func TestGenerateSpriteSheetAndPreviewFiles(t *testing.T) {
	video := makeTestVideo(t)
	dir := t.TempDir()

	sheet := filepath.Join(dir, "sheet.jpg")
	interval, count, cols, rows := spriteLayout(12)
	if err := renderSprite(context.Background(), video, sheet, interval, 12, count, cols, rows); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(sheet)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("sprite sheet is not a JPEG: %v", err)
	}
	if got, want := img.Bounds().Dx(), cols*spriteTileW; got != want {
		t.Errorf("sheet width = %d, want %d", got, want)
	}
	if got, want := img.Bounds().Dy(), rows*spriteTileH; got != want {
		t.Errorf("sheet height = %d (count %d, %dx%d), want %d", got, count, cols, rows, want)
	}

	clip := filepath.Join(dir, "clip.mp4")
	starts, length := previewSegments(12)
	if err := renderPreview(context.Background(), video, clip, starts, length); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(clip); err != nil || info.Size() == 0 {
		t.Fatalf("preview clip was not written: %v", err)
	}
}
