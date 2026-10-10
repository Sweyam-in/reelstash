package scanner

import (
	"context"
	"encoding/json"
	"math"
	"os/exec"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type probeFormat struct {
	Duration string `json:"duration"`
	BitRate  string `json:"bit_rate"`
}

type probeStream struct {
	Index       int               `json:"index"`
	Channels    int               `json:"channels"`
	Tags        map[string]string `json:"tags"`
	Disposition map[string]int    `json:"disposition"`
	CodecType   string            `json:"codec_type"`
	CodecName   string            `json:"codec_name"`
	Width       int               `json:"width"`
	Height      int               `json:"height"`
	RFrameRate  string            `json:"r_frame_rate"`
	BitRate     string            `json:"bit_rate"`
	SideData    []probeSideData   `json:"side_data_list"`
}

type probeSideData struct {
	Rotation float64 `json:"rotation"`
}

type probeOutput struct {
	Format  probeFormat   `json:"format"`
	Streams []probeStream `json:"streams"`
}

// ProbeResult is the subset of ffprobe output ReelStash persists on media_file.
type ProbeResult struct {
	DurationSeconds *float64
	Width           *int
	Height          *int
	FrameRate       *float64
	VideoCodec      *string
	AudioCodec      *string
	VideoBitrate    *int64
	AudioBitrate    *int64
	OverallBitrate  *int64
	// Rotation is the clockwise turn a player applies to the stored frame
	// (0, 90, 180 or 270); phones record portrait video as a rotated
	// landscape frame.
	Rotation int
	Tracks   []Track
}

// RotationOrNil is the rotation to store for a probe that returned probeErr:
// unknown (nil) when ffprobe failed, so a later track scan retries it.
func (p ProbeResult) RotationOrNil(probeErr error) *int {
	if probeErr != nil {
		return nil
	}
	r := p.Rotation
	return &r
}

// SaveRotation records a file's probed rotation, which decides (with its
// frame size) whether it shows among the reels.
func SaveRotation(ctx context.Context, db *pgxpool.Pool, mediaFileID int64, p ProbeResult) error {
	_, err := db.Exec(ctx, `UPDATE media_file SET rotation = $2 WHERE id = $1`, mediaFileID, p.RotationOrNil(nil))
	return err
}

// Probe runs ffprobe against a media file. It returns an error (which the
// caller should treat as non-fatal for the scan as a whole) if ffprobe is
// missing or the file cannot be parsed.
func Probe(ctx context.Context, path string) (ProbeResult, error) {
	var result ProbeResult

	if _, err := exec.LookPath("ffprobe"); err != nil {
		return result, err
	}

	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-print_format", "json", "-show_format", "-show_streams", path)
	out, err := cmd.Output()
	if err != nil {
		return result, err
	}

	var parsed probeOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return result, err
	}

	if d, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil {
		result.DurationSeconds = &d
	}
	if b, err := strconv.ParseInt(parsed.Format.BitRate, 10, 64); err == nil {
		result.OverallBitrate = &b
	}

	result.Tracks = tracksFromStreams(parsed.Streams)
	for _, s := range parsed.Streams {
		switch s.CodecType {
		case "video":
			if result.VideoCodec == nil {
				codec := s.CodecName
				result.VideoCodec = &codec
				if s.Width > 0 {
					w := s.Width
					result.Width = &w
				}
				if s.Height > 0 {
					h := s.Height
					result.Height = &h
				}
				if fr := parseFrameRate(s.RFrameRate); fr > 0 {
					result.FrameRate = &fr
				}
				if b, err := strconv.ParseInt(s.BitRate, 10, 64); err == nil {
					result.VideoBitrate = &b
				}
				result.Rotation = streamRotation(s)
			}
		case "audio":
			if result.AudioCodec == nil {
				codec := s.CodecName
				result.AudioCodec = &codec
				if b, err := strconv.ParseInt(s.BitRate, 10, 64); err == nil {
					result.AudioBitrate = &b
				}
			}
		}
	}

	return result, nil
}

// streamRotation reads a video stream's rotation from its display matrix
// (current ffprobe) or its rotate tag (older files and ffprobe builds),
// normalised to 0, 90, 180 or 270.
func streamRotation(s probeStream) int {
	var deg float64
	found := false
	for _, sd := range s.SideData {
		if sd.Rotation != 0 {
			deg, found = sd.Rotation, true
			break
		}
	}
	if !found {
		if v, err := strconv.ParseFloat(s.Tags["rotate"], 64); err == nil {
			deg = v
		}
	}
	r := int(math.Round(deg/90)) * 90 % 360
	if r < 0 {
		r += 360
	}
	return r
}

func parseFrameRate(raw string) float64 {
	parts := strings.SplitN(raw, "/", 2)
	if len(parts) != 2 {
		return 0
	}
	num, err1 := strconv.ParseFloat(parts[0], 64)
	den, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || den == 0 {
		return 0
	}
	return num / den
}
