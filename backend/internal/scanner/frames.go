package scanner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"os/exec"
	"strings"
)

// Frames darker or flatter than this are treated as unusable posters (a black
// intro, a fade, a solid title card).
const (
	minFrameMean   = 18.0
	minFrameStddev = 8.0
)

// ExtractFrame returns one JPEG frame from the video at the given second,
// scaled to the given width. Seeking happens before decoding, so it stays
// fast on long files.
func ExtractFrame(ctx context.Context, srcPath string, seconds float64, width int) ([]byte, error) {
	if width < 16 {
		width = 640
	}
	return extractFrame(ctx, srcPath, seconds, fmt.Sprintf("scale=%d:-2", width), false)
}

// extractFit returns a frame letterboxed into exactly w x h, seeking to the
// nearest keyframe before the position (fast, and plenty for a small tile).
func extractFit(ctx context.Context, srcPath string, seconds float64, w, h int) ([]byte, error) {
	filter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2", w, h, w, h)
	return extractFrame(ctx, srcPath, seconds, filter, true)
}

func extractFrame(ctx context.Context, srcPath string, seconds float64, videoFilter string, fast bool) ([]byte, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, fmt.Errorf("ffmpeg not available: %w", err)
	}
	if seconds < 0 {
		seconds = 0
	}
	ctx, cancel := context.WithTimeout(ctx, ffmpegTimeout)
	defer cancel()

	args := []string{"-nostdin", "-v", "error"}
	if fast {
		args = append(args, "-noaccurate_seek")
	}
	args = append(args, "-ss", fmt.Sprintf("%.3f", seconds), "-i", srcPath,
		"-an", "-sn", "-dn", "-frames:v", "1",
		"-vf", videoFilter,
		"-q:v", "3", "-strict", "unofficial",
		"-f", "mjpeg", "pipe:1")
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("ffmpeg timed out after %s reading %s (stalled mount?)", ffmpegTimeout, srcPath)
		}
		return nil, fmt.Errorf("ffmpeg frame failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if out.Len() == 0 {
		return nil, errors.New("no frame at that position")
	}
	return out.Bytes(), nil
}

// FrameStats returns the mean and standard deviation of a JPEG's luma
// (0-255), sampled sparsely so it stays cheap.
func FrameStats(data []byte) (mean, stddev float64, err error) {
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	return lumaStats(img), stddevOf(img), nil
}

func lumaSamples(img image.Image, visit func(luma float64)) {
	b := img.Bounds()
	stepX, stepY := max(1, b.Dx()/96), max(1, b.Dy()/54)
	for y := b.Min.Y; y < b.Max.Y; y += stepY {
		for x := b.Min.X; x < b.Max.X; x += stepX {
			r, g, bl, _ := img.At(x, y).RGBA()
			visit((0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)) / 257)
		}
	}
}

func lumaStats(img image.Image) float64 {
	var sum float64
	var n int
	lumaSamples(img, func(l float64) { sum += l; n++ })
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func stddevOf(img image.Image) float64 {
	mean := lumaStats(img)
	var sq float64
	var n int
	lumaSamples(img, func(l float64) { d := l - mean; sq += d * d; n++ })
	if n == 0 {
		return 0
	}
	return math.Sqrt(sq / float64(n))
}

// UsableFrame reports whether a frame is bright and detailed enough to serve as a poster.
func UsableFrame(data []byte) bool {
	mean, sd, err := FrameStats(data)
	return err == nil && mean >= minFrameMean && sd >= minFrameStddev
}

// candidateTimes lists where to look for a good frame, best guess first.
func candidateTimes(duration float64) []float64 {
	if duration <= 0 {
		return []float64{5, 15, 30, 60, 120}
	}
	last := math.Max(0, duration-0.5)
	var times []float64
	for i, f := range []float64{0.10, 0.25, 0.40, 0.55, 0.70, 0.85} {
		t := duration * f
		if i == 0 && t < 5 && duration > 10 {
			t = 5 // same 5s floor the thumbnail generator always used
		}
		times = append(times, math.Min(t, last))
	}
	return times
}

// PickBestFrame samples several points in the video and returns the first
// frame that is not black or flat, or the best one it saw if none qualifies.
func PickBestFrame(ctx context.Context, srcPath string, duration float64, width int) (data []byte, seconds float64, err error) {
	var bestScore = -1.0
	var lastErr error
	for _, t := range candidateTimes(duration) {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		frame, ferr := ExtractFrame(ctx, srcPath, t, width)
		if ferr != nil {
			lastErr = ferr
			continue
		}
		mean, sd, serr := FrameStats(frame)
		if serr != nil {
			lastErr = serr
			continue
		}
		if mean >= minFrameMean && sd >= minFrameStddev {
			return frame, t, nil
		}
		if score := mean + 2*sd; score > bestScore {
			bestScore, data, seconds = score, frame, t
		}
	}
	if data == nil {
		if lastErr == nil {
			lastErr = errors.New("could not read any frame")
		}
		return nil, 0, lastErr
	}
	return data, seconds, nil
}
