package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"reelstash/backend/internal/database"
	"reelstash/backend/internal/diskinfo"
	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/pathguard"
)

// DVDConvertJobKind turns a DVD VIDEO_TS folder (VOB title sets) into MP4s.
const DVDConvertJobKind = "DVD_CONVERT"

// DVDConvertItem is one folder an admin asked to convert: either the VIDEO_TS
// directory itself or the movie folder that contains it.
type DVDConvertItem struct {
	StorageLocationID int64  `json:"storageLocationId"`
	Path              string `json:"path"`
}

// DVDConvertPayload is the DVD_CONVERT job payload.
type DVDConvertPayload struct {
	Items []DVDConvertItem `json:"items"`
}

// DVDConvertResult is what the DVD_CONVERT job reports.
type DVDConvertResult struct {
	Total     int      `json:"total"`
	Converted int      `json:"converted"`
	Failed    int      `json:"failed"`
	Skipped   int      `json:"skipped"`
	Errors    []string `json:"errors,omitempty"`
}

func (r *DVDConvertResult) add(msg string) {
	if msg != "" && len(r.Errors) < 20 {
		r.Errors = append(r.Errors, msg)
	}
}

// dvdConvertTimeout bounds one title. A feature is re-encoded, so this is
// long; cancelling the job still stops ffmpeg.
const dvdConvertTimeout = 8 * time.Hour

const dvdConvertSuffix = ".reelstash-mp4"

// vtsVOB matches a DVD title-set file, VTS_01_1.VOB. The middle number is the
// title, the last is the part. Part 0 is the menu.
var vtsVOB = regexp.MustCompile(`(?i)^VTS_(\d+)_(\d+)\.VOB$`)

// DVDTitle is one playable title: the VOB parts in order, menus left out.
type DVDTitle struct {
	Number int
	Files  []string
}

// FindDVDVideo locates the VIDEO_TS directory for abs, which may be that
// directory or the movie folder that contains it.
func FindDVDVideo(abs string) (string, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("that is not a folder")
	}
	if titles, err := DVDTitles(abs); err != nil {
		return "", err
	} else if len(titles) > 0 {
		return abs, nil
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() && strings.EqualFold(e.Name(), "VIDEO_TS") {
			child := filepath.Join(abs, e.Name())
			titles, err := DVDTitles(child)
			if err != nil {
				return "", err
			}
			if len(titles) > 0 {
				return child, nil
			}
		}
	}
	return "", errors.New("no DVD video in this folder")
}

// DVDTitles lists the playable titles in a VIDEO_TS directory. Menu files
// (VTS_01_0.VOB and VIDEO_TS.VOB) are not titles.
func DVDTitles(dir string) ([]DVDTitle, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	type part struct {
		n    int
		path string
	}
	groups := map[int][]part{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := vtsVOB.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		title, _ := strconv.Atoi(m[1])
		piece, _ := strconv.Atoi(m[2])
		if piece == 0 {
			continue
		}
		groups[title] = append(groups[title], part{n: piece, path: filepath.Join(dir, e.Name())})
	}
	nums := make([]int, 0, len(groups))
	for n := range groups {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	out := make([]DVDTitle, 0, len(nums))
	for _, n := range nums {
		parts := groups[n]
		sort.Slice(parts, func(i, j int) bool { return parts[i].n < parts[j].n })
		files := make([]string, len(parts))
		for i, p := range parts {
			files[i] = p.path
		}
		out = append(out, DVDTitle{Number: n, Files: files})
	}
	return out, nil
}

// DVDOutputNames names the MP4 for each title. A disc with one title uses the
// movie folder's name. When there are several, the largest (the feature)
// keeps that name and the others are numbered with their title-set number,
// so a short intro does not take the movie's filename.
func DVDOutputNames(movie string, titles []DVDTitle) ([]string, error) {
	names := make([]string, len(titles))
	if len(titles) == 0 {
		return names, nil
	}
	if len(titles) == 1 {
		names[0] = movie + ".mp4"
		return names, nil
	}
	main := 0
	var biggest int64
	for i, title := range titles {
		var n int64
		for _, file := range title.Files {
			info, err := os.Stat(file)
			if err != nil {
				return nil, err
			}
			n += info.Size()
		}
		if n > biggest {
			biggest, main = n, i
		}
	}
	for i, title := range titles {
		if i == main {
			names[i] = movie + ".mp4"
			continue
		}
		names[i] = fmt.Sprintf("%s %d.mp4", movie, title.Number)
	}
	return names, nil
}

// handleDVDConvertJob encodes each DVD title to an MP4 in the movie folder,
// next to VIDEO_TS. The VOB files are left in place.
func (s *Scanner) handleDVDConvertJob(ctx context.Context, db database.DB, jobID int64, payload json.RawMessage) (any, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, errors.New("ffmpeg is not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return nil, errors.New("ffprobe is not installed")
	}
	var p DVDConvertPayload
	if err := json.Unmarshal(payload, &p); err != nil || len(p.Items) == 0 {
		return nil, errors.New("no DVD folder to convert")
	}
	res := DVDConvertResult{Errors: []string{}}
	guard := pathguard.Load(ctx, db)
	busy := map[int64]bool{}
	done := 0
	for _, item := range p.Items {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		var mount string
		if err := db.QueryRow(ctx, `SELECT mount_path FROM storage_location WHERE id = $1`, item.StorageLocationID).Scan(&mount); err != nil {
			res.Failed++
			res.Total++
			res.add(item.Path + ": storage location not found")
			continue
		}
		abs, rel, err := mediaAbs(mount, item.Path)
		if err != nil {
			res.Failed++
			res.Total++
			res.add(item.Path + ": " + err.Error())
			continue
		}
		videoDir, err := FindDVDVideo(abs)
		if err != nil {
			res.Failed++
			res.Total++
			res.add(rel + ": " + err.Error())
			continue
		}
		if blocked, why := guard.Blocked(videoDir); blocked {
			res.Skipped++
			res.Total++
			res.add(rel + ": " + why)
			continue
		}
		if locationBusy, ok := busy[item.StorageLocationID]; ok && locationBusy {
			res.Skipped++
			res.Total++
			res.add(rel + ": a scan or the organizer is using this disk")
			continue
		} else if !ok {
			locationBusy, err = storageBusy(ctx, db, item.StorageLocationID)
			if err != nil {
				return res, err
			}
			busy[item.StorageLocationID] = locationBusy
			if locationBusy {
				res.Skipped++
				res.Total++
				res.add(rel + ": a scan or the organizer is using this disk")
				continue
			}
		}
		titles, err := DVDTitles(videoDir)
		if err != nil {
			res.Failed++
			res.Total++
			res.add(rel + ": " + err.Error())
			continue
		}
		outDir := dvdOutputDir(videoDir, filepath.Clean(mount))
		movie := filepath.Base(outDir)
		if movie == "." || movie == string(filepath.Separator) {
			movie = "DVD"
		}
		res.Total += len(titles)
		names, err := DVDOutputNames(movie, titles)
		if err != nil {
			res.Failed++
			res.add(rel + ": " + err.Error())
			continue
		}
		for i, title := range titles {
			if err := ctx.Err(); err != nil {
				return res, err
			}
			name := names[i]
			dest := filepath.Join(outDir, name)
			jobs.ReportProgress(db, jobID, done, res.Total, name)
			if _, err := os.Stat(dest); err == nil {
				res.Skipped++
				res.add(name + ": an MP4 with this name already exists")
				done++
				continue
			}
			if err := encodeDVDTitle(ctx, title, dest); err != nil {
				if ctx.Err() != nil {
					return res, ctx.Err()
				}
				res.Failed++
				res.add(name + ": " + err.Error())
				log.Printf("dvd convert: %s: %v", name, err)
				done++
				continue
			}
			res.Converted++
			done++
		}
	}
	jobs.ReportProgress(db, jobID, done, res.Total, "")
	if res.Total == 0 && res.Failed == 0 && res.Skipped == 0 {
		return res, errors.New("no DVD video to convert")
	}
	return res, nil
}

// dvdOutputDir is the movie folder beside VIDEO_TS. A VIDEO_TS that is the
// disk root keeps its MP4s inside the disk, never in the parent of the mount.
func dvdOutputDir(videoDir, mount string) string {
	if !strings.EqualFold(filepath.Base(videoDir), "VIDEO_TS") {
		return videoDir
	}
	parent := filepath.Dir(videoDir)
	if insideMount(parent, mount) || filepath.Clean(parent) == mount {
		return parent
	}
	return videoDir
}

func insideMount(path, mount string) bool {
	rel, err := filepath.Rel(mount, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func encodeDVDTitle(ctx context.Context, title DVDTitle, dest string) error {
	var size int64
	var mode os.FileMode = 0o644
	var modTime time.Time
	var sum float64
	probed := true
	for i, file := range title.Files {
		info, err := os.Stat(file)
		if err != nil {
			return err
		}
		size += info.Size()
		if i == 0 {
			mode = info.Mode().Perm()
			modTime = info.ModTime()
		}
		p, err := Probe(ctx, file)
		if err != nil || p.DurationSeconds == nil {
			probed = false
		} else {
			sum += *p.DurationSeconds
		}
	}
	if usage, err := diskinfo.Stat(filepath.Dir(dest), diskinfo.DefaultTimeout); err == nil && usage.Available > 0 {
		need := size / 5
		if need < 256<<20 {
			need = 256 << 20
		}
		if usage.Available < need {
			return errors.New("not enough free space to convert this title")
		}
	}
	list := dest + ".reelstash-dvdlist"
	if err := writeConcat(list, title.Files); err != nil {
		return err
	}
	defer os.Remove(list)

	before := ProbeResult{}
	if probed && sum > 0 {
		before.DurationSeconds = &sum
	}
	info := stubInfo{mode: mode, mod: modTime}
	return runDVDEncode(ctx, list, dest, info, before)
}

type stubInfo struct {
	mode os.FileMode
	mod  time.Time
}

func (s stubInfo) Mode() os.FileMode  { return s.mode }
func (s stubInfo) ModTime() time.Time { return s.mod }

func runDVDEncode(ctx context.Context, list, dest string, orig interface {
	Mode() os.FileMode
	ModTime() time.Time
}, before ProbeResult) error {
	tmp := filepath.Join(filepath.Dir(dest), "."+filepath.Base(dest)+dvdConvertSuffix)
	defer os.Remove(tmp)
	cctx, cancel := context.WithTimeout(ctx, dvdConvertTimeout)
	defer cancel()
	// DVD video is MPEG-2, usually interlaced, with AC-3 audio. Browsers play
	// H.264 and AAC, so this is an encode. The first audio track is the main
	// language. Picture subtitles cannot live in an MP4 a browser can show.
	args := []string{"-nostdin", "-v", "error", "-y",
		"-fflags", "+genpts+discardcorrupt",
		"-f", "concat", "-safe", "0", "-i", list,
		"-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn",
		"-vf", "yadif=mode=0:parity=-1:deint=0,scale=trunc(iw/2)*2:trunc(ih/2)*2",
		"-c:v", "libx264", "-preset", "fast", "-crf", "20", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-ac", "2", "-b:a", "192k",
		"-max_muxing_queue_size", "1024",
		"-movflags", "+faststart", "-f", "mp4", tmp}
	if out, err := exec.CommandContext(cctx, "ffmpeg", args...).CombinedOutput(); err != nil {
		_ = os.Remove(tmp)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ffmpeg: %w: %s", err, trimDVDOutput(out))
	}
	after, err := Probe(ctx, tmp)
	if err != nil {
		return fmt.Errorf("probe converted file: %w", err)
	}
	if err := acceptableDVDMP4(before, after); err != nil {
		return err
	}
	if err := os.Chmod(tmp, orig.Mode().Perm()); err != nil {
		return err
	}
	if err := os.Chtimes(tmp, orig.ModTime(), orig.ModTime()); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func acceptableDVDMP4(before, after ProbeResult) error {
	if probeCodec(after.VideoCodec) == "" {
		return errors.New("converted file has no video")
	}
	if after.DurationSeconds == nil || *after.DurationSeconds <= 0 {
		return errors.New("converted file has no duration")
	}
	if before.DurationSeconds == nil {
		return nil
	}
	// VOB timestamps are not always exact, so allow a wide gap, but a file
	// that is only a fraction of the disc was cut off.
	if *after.DurationSeconds < *before.DurationSeconds*0.5 {
		return fmt.Errorf("converted file is %.0f seconds long, the DVD title is %.0f", *after.DurationSeconds, *before.DurationSeconds)
	}
	return nil
}

func writeConcat(path string, files []string) error {
	var b strings.Builder
	for _, f := range files {
		b.WriteString("file '")
		b.WriteString(strings.ReplaceAll(f, `'`, `'\''`))
		b.WriteString("'\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func trimDVDOutput(out []byte) string {
	msg := strings.TrimSpace(string(out))
	if len(msg) > 400 {
		msg = msg[len(msg)-400:]
	}
	return msg
}

func probeCodec(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func mediaAbs(mount, rel string) (abs, cleanRel string, err error) {
	mount = filepath.Clean(mount)
	if strings.TrimSpace(rel) == "" || strings.Contains(rel, "\x00") {
		return "", "", errors.New("invalid path")
	}
	abs = filepath.Join(mount, filepath.Clean("/"+rel))
	out, err := filepath.Rel(mount, abs)
	if err != nil || out == ".." || strings.HasPrefix(out, ".."+string(filepath.Separator)) {
		return "", "", errors.New("path escapes storage location")
	}
	return abs, filepath.ToSlash(out), nil
}

func storageBusy(ctx context.Context, db database.DB, storageID int64) (bool, error) {
	var busy bool
	err := db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM scan_job WHERE storage_location_id = $1 AND completed_at IS NULL)
		    OR EXISTS (SELECT 1 FROM background_job WHERE kind = 'ORGANIZE' AND status IN ('PENDING','RUNNING')
		               AND payload->>'storageLocationId' = $1::text)
	`, storageID).Scan(&busy)
	return busy, err
}
