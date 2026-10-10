package httpapi

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"reelstash/backend/internal/scanner"
)

// defaultHLSSegmentSeconds is used when REELSTASH_HLS_SEGMENT_SECONDS is
// unset. Because every segment is encoded independently from a
// keyframe-accurate seek, the playlist can be generated instantly from the
// duration alone (no pre-transcode), and seeking anywhere just transcodes
// the segment that is needed. Shorter segments start faster but re-encode
// more often on seeks; longer segments are more CPU-efficient but slower to
// start.
const defaultHLSSegmentSeconds = 6

// segmentSeconds returns the configured HLS segment length, or the default.
func (a *API) segmentSeconds() int {
	if a.Cfg.HLSSegmentSeconds > 0 {
		return a.Cfg.HLSSegmentSeconds
	}
	return defaultHLSSegmentSeconds
}

type mediaSource struct {
	FileID     int64
	FullPath   string
	FileName   string
	Container  string
	VideoCodec string
	AudioCodec string
	Duration   float64
	Quality    hlsQuality
	// Audio is the wanted audio track among the file's audio tracks (0: first).
	Audio int
	// Burn overlays picture subtitles (PGS, VobSub) onto the video. BurnIndex
	// is that subtitle track. The zero value leaves subtitles alone.
	Burn      bool
	BurnIndex int
}

// hlsQuality is one transcode quality tier, picked per request with
// ?quality=high|balanced|low. Each tier has its own segment cache.
type hlsQuality struct {
	Name         string
	MaxHeight    int
	CRF          int    // software x264
	HWBitrate    string // videotoolbox / nvenc
	QP           int    // vaapi
	AudioBitrate string
}

var hlsQualities = map[string]hlsQuality{
	"high":     {Name: "high", MaxHeight: 1080, CRF: 18, HWBitrate: "10M", QP: 18, AudioBitrate: "192k"},
	"balanced": {Name: "balanced", MaxHeight: 1080, CRF: 21, HWBitrate: "6M", QP: 21, AudioBitrate: "160k"},
	"low":      {Name: "low", MaxHeight: 720, CRF: 27, HWBitrate: "2500k", QP: 27, AudioBitrate: "96k"},
}

// qualityFromRequest reads ?quality=, defaulting to balanced for anything unknown.
func qualityFromRequest(r *http.Request) hlsQuality {
	if q, ok := hlsQualities[r.URL.Query().Get("quality")]; ok {
		return q
	}
	return hlsQualities["balanced"]
}

// encoderArgsFor returns the profile's encoder arguments with the quality
// tier's rate control swapped in.
func encoderArgsFor(p hwProfile, q hlsQuality) []string {
	args := append([]string(nil), p.encoderArgs...)
	for i := 0; i+1 < len(args); i++ {
		switch args[i] {
		case "-crf":
			args[i+1] = strconv.Itoa(q.CRF)
		case "-b:v":
			args[i+1] = q.HWBitrate
		case "-qp":
			args[i+1] = strconv.Itoa(q.QP)
		}
	}
	return args
}

// resolveMediaFile picks the requested (?fileId=) or best available file of a
// media item and validates that it stays inside its storage location. On
// failure it returns the HTTP status and message to send.
func (a *API) resolveMediaFile(r *http.Request, mediaItemID int64) (mediaSource, int, string) {
	var (
		src                       mediaSource
		mountPath, relativePath   string
		container, vcodec, acodec *string
		duration                  *float64
	)
	const cols = `SELECT mf.id, sl.mount_path, mf.relative_path, mf.file_name, mf.container, mf.video_codec, mf.audio_codec, mf.duration_seconds
		FROM media_file mf JOIN storage_location sl ON sl.id = mf.storage_location_id `
	var err error
	if p := r.URL.Query().Get("fileId"); p != "" {
		fileID, perr := strconv.ParseInt(p, 10, 64)
		if perr != nil {
			return src, http.StatusBadRequest, "invalid fileId"
		}
		err = a.DB.QueryRow(r.Context(), cols+`WHERE mf.id = $1 AND mf.media_item_id = $2 AND mf.available`, fileID, mediaItemID).
			Scan(&src.FileID, &mountPath, &relativePath, &src.FileName, &container, &vcodec, &acodec, &duration)
		if err != nil {
			return src, http.StatusNotFound, "media file not found"
		}
	} else {
		err = a.DB.QueryRow(r.Context(), cols+`WHERE mf.media_item_id = $1 AND mf.available
			ORDER BY mf.height DESC NULLS LAST, mf.file_size DESC LIMIT 1`, mediaItemID).
			Scan(&src.FileID, &mountPath, &relativePath, &src.FileName, &container, &vcodec, &acodec, &duration)
		if err != nil {
			return src, http.StatusNotFound, "no available file for this media item"
		}
	}
	src.FullPath = filepath.Join(mountPath, filepath.FromSlash(relativePath))
	cleanMount := filepath.Clean(mountPath)
	if cf := filepath.Clean(src.FullPath); cf != cleanMount && !isWithinDir(cf, cleanMount) {
		return src, http.StatusForbidden, "resolved path escapes storage location"
	}
	if container != nil {
		src.Container = strings.ToLower(*container)
	}
	if vcodec != nil {
		src.VideoCodec = strings.ToLower(*vcodec)
	}
	if acodec != nil {
		src.AudioCodec = strings.ToLower(*acodec)
	}
	if duration != nil {
		src.Duration = *duration
	}
	if ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(src.FileName), ".")); src.Container == "" {
		src.Container = ext
	}
	return src, 0, ""
}

// browserDirectPlayable is the server-side first guess at whether mainstream
// browsers can play the file untouched. The client double-checks with
// canPlayType and falls back to HLS on any decode failure.
func (s mediaSource) browserDirectPlayable() bool {
	c := s.Container
	okContainer := strings.Contains(c, "mp4") || strings.Contains(c, "mov") || strings.Contains(c, "m4v") || strings.Contains(c, "webm")
	okVideo := s.VideoCodec == "h264" || s.VideoCodec == "vp8" || s.VideoCodec == "vp9" || s.VideoCodec == "av1"
	okAudio := s.AudioCodec == "" || s.AudioCodec == "aac" || s.AudioCodec == "mp3" || s.AudioCodec == "opus" || s.AudioCodec == "vorbis" || s.AudioCodec == "flac"
	return okContainer && okVideo && okAudio
}

// remuxable reports whether the file's codecs are already browser-playable
// but wrapped in a container browsers won't parse — the common case being
// h264/aac (or h264 with no/mp3 audio) inside an .mkv. Such files only need
// a one-time zero-recode container remux (stream copy into .mp4), not a
// full per-segment HLS transcode, which is dramatically cheaper: a remux
// runs at disk-copy speed, not encode speed. Restricted to h264 video since
// mp4 (the remux target) has universal browser support for it, unlike
// vp8/vp9/av1 which are usually paired with webm.
func (s mediaSource) remuxable() bool {
	if s.browserDirectPlayable() {
		return false
	}
	return s.VideoCodec == "h264" && (s.AudioCodec == "" || s.AudioCodec == "aac" || s.AudioCodec == "mp3")
}

func (a *API) mediaPlaybackInfo(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	src, status, msg := a.resolveMediaFile(r, id)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	canFFmpeg := ffmpegAvailable()
	writeJSON(w, http.StatusOK, map[string]any{
		"fileId":      src.FileID,
		"container":   src.Container,
		"videoCodec":  src.VideoCodec,
		"audioCodec":  src.AudioCodec,
		"duration":    src.Duration,
		"directPlay":  src.browserDirectPlayable() || (src.remuxable() && canFFmpeg),
		"transcoding": canFFmpeg,
	})
}

func ffmpegAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

func (a *API) hlsDuration(ctx context.Context, src *mediaSource) error {
	if src.Duration > 0 {
		return nil
	}
	pr, err := scanner.Probe(ctx, src.FullPath)
	if err != nil || pr.DurationSeconds == nil || *pr.DurationSeconds <= 0 {
		return fmt.Errorf("unknown duration")
	}
	src.Duration = *pr.DurationSeconds
	return nil
}

func (a *API) hlsPlaylist(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	src, status, msg := a.resolveMediaFile(r, id)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	src.Quality = qualityFromRequest(r)
	src.Audio = audioFromRequest(r)
	if n := burnFromRequest(r); n >= 0 {
		src.Burn, src.BurnIndex = true, n
	}
	if !ffmpegAvailable() {
		writeError(w, http.StatusNotImplemented, "ffmpeg is not installed on the server")
		return
	}
	if err := a.hlsDuration(r.Context(), &src); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "cannot determine media duration")
		return
	}
	segSec := a.segmentSeconds()
	q := "?fileId=" + strconv.FormatInt(src.FileID, 10)
	if quality := qualityFromRequest(r); quality.Name != "balanced" {
		q += "&quality=" + quality.Name
	}
	if src.Audio > 0 {
		q += "&audio=" + strconv.Itoa(src.Audio)
	}
	if src.Burn {
		q += "&burn=" + strconv.Itoa(src.BurnIndex)
	}
	count := int(math.Ceil(src.Duration / float64(segSec)))
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-PLAYLIST-TYPE:VOD\n")
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:0\n", segSec)
	for i := 0; i < count; i++ {
		d := float64(segSec)
		if rem := src.Duration - float64(i*segSec); rem < d {
			d = rem
		}
		fmt.Fprintf(&b, "#EXTINF:%.3f,\nseg/%d.ts%s\n", d, i, q)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write([]byte(b.String()))
}

var (
	hlsLocks       sync.Map // segment path -> *sync.Mutex
	hlsSemOnce     sync.Once
	hlsSem         chan struct{}
	hlsCleanup     sync.Once
	lastRequestSeg sync.Map // fileID -> last segment index requested (seek hint)
)

// hlsSemaphore lazily sizes the transcode concurrency limiter from config
// (REELSTASH_HLS_WORKERS), falling back to max(2, NumCPU/2) when unset.
func (a *API) hlsSemaphore() chan struct{} {
	hlsSemOnce.Do(func() {
		n := a.Cfg.HLSWorkers
		if n <= 0 {
			n = max(2, runtime.NumCPU()/2)
		}
		hlsSem = make(chan struct{}, n)
	})
	return hlsSem
}

// hwProfile is one candidate ffmpeg encode configuration: which encoder to
// use, any global ffmpeg options it needs (e.g. -vaapi_device), and the
// pixel-format filter stage required to feed frames into it.
type hwProfile struct {
	name        string
	globalArgs  []string
	encoderArgs []string
	pixFmt      string
}

var softwareProfile = hwProfile{
	name:        "software",
	encoderArgs: []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "21"},
	pixFmt:      "yuv420p",
}

var (
	hwOnce   sync.Once
	hwChosen hwProfile
)

// detectHW probes ffmpeg once per process for a usable hardware encoder, in
// order: VideoToolbox (macOS), NVENC (Nvidia+CUDA), then VAAPI (Intel/AMD on
// Linux — including older iGPUs served by the legacy i965 driver as well as
// newer ones on iHD). Each candidate is test-encoded with a single black
// frame before being trusted, so an encoder that's merely *listed* by ffmpeg
// but non-functional here (missing driver, no /dev/dri permission, disabled
// in a VM, etc.) falls back to software instead of breaking every transcode.
func detectHW() hwProfile {
	hwOnce.Do(func() {
		hwChosen = softwareProfile
		encOut, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
		if err != nil {
			return
		}
		encoders := string(encOut)

		if runtime.GOOS == "darwin" && strings.Contains(encoders, "h264_videotoolbox") {
			p := hwProfile{name: "videotoolbox", encoderArgs: []string{"-c:v", "h264_videotoolbox", "-b:v", "6M", "-allow_sw", "1"}, pixFmt: "yuv420p"}
			if testEncode(p) {
				log.Printf("hls: using hardware encoder %q", p.name)
				hwChosen = p
				return
			}
		}
		if strings.Contains(encoders, "h264_nvenc") {
			p := hwProfile{name: "nvenc", encoderArgs: []string{"-c:v", "h264_nvenc", "-preset", "p4", "-b:v", "6M"}, pixFmt: "yuv420p"}
			if testEncode(p) {
				log.Printf("hls: using hardware encoder %q", p.name)
				hwChosen = p
				return
			}
		}
		if strings.Contains(encoders, "h264_vaapi") {
			if dev := vaapiDevice(); dev != "" {
				p := hwProfile{
					name:        "vaapi",
					globalArgs:  []string{"-vaapi_device", dev},
					encoderArgs: []string{"-c:v", "h264_vaapi", "-qp", "21"},
					pixFmt:      "nv12,hwupload",
				}
				if testEncode(p) {
					log.Printf("hls: using hardware encoder %q (device %s)", p.name, dev)
					hwChosen = p
					return
				}
			}
		}
		log.Printf("hls: no working hardware encoder detected, using software libx264")
	})
	return hwChosen
}

// vaapiDevice returns the first DRI render node found on disk, or "" if
// none is mounted into the container (see compose device passthrough
// for /dev/dri).
func vaapiDevice() string {
	for _, p := range []string{"/dev/dri/renderD128", "/dev/dri/renderD129", "/dev/dri/card0"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// testEncode runs a candidate hardware profile against one synthetic black
// frame to confirm it actually works on this machine, not just that ffmpeg
// was built with the encoder compiled in.
func testEncode(p hwProfile) bool {
	args := []string{"-hide_banner", "-v", "error"}
	args = append(args, p.globalArgs...)
	args = append(args, "-f", "lavfi", "-i", "color=c=black:s=64x64:d=0.1")
	args = append(args, p.encoderArgs...)
	args = append(args, "-vf", "format="+p.pixFmt, "-frames:v", "1", "-f", "null", "-")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "ffmpeg", args...).Run() == nil
}

// hlsCacheDir is where a file's segments live. Balanced keeps the original
// directory so existing caches stay valid; other tiers get a sibling.
func (a *API) hlsCacheDir(fileID int64, q hlsQuality) string {
	return a.hlsCacheDirAudio(fileID, q, 0)
}

// hlsCacheDirAudio is hlsCacheDir for one audio track: the first track keeps
// the original directory name, others get an "-a<N>" suffix.
func (a *API) hlsCacheDirAudio(fileID int64, q hlsQuality, audio int) string {
	name := strconv.FormatInt(fileID, 10)
	if q.Name != "" && q.Name != "balanced" {
		name += "-" + q.Name
	}
	if audio > 0 {
		name += "-a" + strconv.Itoa(audio)
	}
	return filepath.Join(filepath.Dir(a.Cfg.ThumbnailDir), "hls", name)
}

func (a *API) hlsCacheDirBurn(fileID int64, q hlsQuality, audio, burn int) string {
	dir := a.hlsCacheDirAudio(fileID, q, audio)
	if burn >= 0 {
		dir += "-s" + strconv.Itoa(burn)
	}
	return dir
}

func (a *API) remuxCacheDir() string {
	return filepath.Join(filepath.Dir(a.Cfg.ThumbnailDir), "remux")
}

// pruneHLSCache drops cached segments untouched for 24h; started once.
func (a *API) pruneHLSCache() {
	hlsCleanup.Do(func() {
		root := filepath.Join(filepath.Dir(a.Cfg.ThumbnailDir), "hls")
		go func() {
			for {
				cutoff := time.Now().Add(-24 * time.Hour)
				_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
					if err == nil && !info.IsDir() && info.ModTime().Before(cutoff) {
						_ = os.Remove(p)
					}
					return nil
				})
				time.Sleep(time.Hour)
			}
		}()
	})
}

var remuxLocks sync.Map // fileID -> *sync.Mutex

// touchCache marks a cached file as used now, at most hourly, so
// housekeeping only clears what nobody has played for a while.
func touchCache(path string) {
	if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > time.Hour {
		now := time.Now()
		_ = os.Chtimes(path, now, now)
	}
}

// ensureRemux produces (and caches) a zero-recode MP4 remux of a file whose
// codecs are already browser-playable but whose container isn't. This is a
// stream copy, not a transcode — it runs at disk-copy speed and needs no
// ffmpeg worker slot from hlsSem, since it costs negligible CPU.
func (a *API) ensureRemux(src mediaSource) (string, error) {
	dir := a.remuxCacheDir()
	name := strconv.FormatInt(src.FileID, 10)
	if src.Audio > 0 {
		name += "-a" + strconv.Itoa(src.Audio)
	}
	out := filepath.Join(dir, name+".mp4")
	if _, err := os.Stat(out); err == nil {
		touchCache(out)
		return out, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	lk, _ := remuxLocks.LoadOrStore(out, &sync.Mutex{})
	mu := lk.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}

	tmp := out + ".part"
	args := []string{"-nostdin", "-v", "error", "-y",
		"-i", src.FullPath, "-map", "0:v:0", "-map", "0:a:" + strconv.Itoa(src.Audio) + "?", "-sn", "-dn",
		"-c", "copy", "-movflags", "+faststart", "-f", "mp4", tmp}
	// A stream-copy remux is I/O-bound, not CPU-bound, so it runs far
	// faster than realtime even for large files — 10 minutes is a generous
	// ceiling, not an expected duration.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if outb, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("ffmpeg remux: %w: %s", err, strings.TrimSpace(string(outb)))
	}
	if err := os.Rename(tmp, out); err != nil {
		return "", err
	}
	return out, nil
}

// ensureSegment returns the path of segment n, transcoding it if not cached.
func (a *API) ensureSegment(ctx context.Context, src mediaSource, n int) (string, error) {
	if src.Quality.Name == "" {
		src.Quality = hlsQualities["balanced"]
	}
	burnCache := -1
	if src.Burn {
		burnCache = src.BurnIndex
	}
	dir := a.hlsCacheDirBurn(src.FileID, src.Quality, src.Audio, burnCache)
	out := filepath.Join(dir, fmt.Sprintf("%d.ts", n))
	if segmentCached(out) {
		return out, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	lk, _ := hlsLocks.LoadOrStore(out, &sync.Mutex{})
	mu := lk.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	if segmentCached(out) {
		return out, nil
	}
	sem := a.hlsSemaphore()
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	segSec := a.segmentSeconds()
	start := n * segSec
	tmp := out + ".part"

	run := func(prof hwProfile) ([]byte, error) {
		args := []string{"-nostdin", "-v", "error", "-y"}
		args = append(args, prof.globalArgs...)
		args = append(args,
			// Damaged sources (partial downloads, bad sectors) are decoded
			// best-effort: corrupt packets are dropped instead of aborting
			// the whole segment.
			"-err_detect", "ignore_err", "-fflags", "+discardcorrupt+genpts",
			"-ss", strconv.Itoa(start), "-i", src.FullPath, "-t", strconv.Itoa(segSec))
		if src.Burn {
			args = append(args,
				"-filter_complex", fmt.Sprintf("[0:v:0]scale=-2:'min(%d,ih)',format=%s[vs];[vs][0:s:%d]overlay=eof_action=pass[v]", src.Quality.MaxHeight, prof.pixFmt, src.BurnIndex),
				"-map", "[v]", "-map", "0:a:"+strconv.Itoa(src.Audio)+"?", "-dn")
		} else {
			args = append(args, "-map", "0:v:0", "-map", "0:a:"+strconv.Itoa(src.Audio)+"?", "-sn", "-dn")
		}
		args = append(args, encoderArgsFor(prof, src.Quality)...)
		if !src.Burn {
			args = append(args, "-vf", fmt.Sprintf("scale=-2:'min(%d,ih)',format=%s", src.Quality.MaxHeight, prof.pixFmt))
		}
		args = append(args,
			"-g", "48", "-sc_threshold", "0",
			"-c:a", "aac", "-b:a", src.Quality.AudioBitrate, "-ac", "2",
			"-output_ts_offset", strconv.Itoa(start),
			"-muxdelay", "0", "-f", "mpegts", tmp)
		cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		return exec.CommandContext(cctx, "ffmpeg", args...).CombinedOutput()
	}

	prof := detectHW()
	outb, err := run(prof)
	if err != nil && prof.name != "software" {
		log.Printf("hls: %s encode failed for segment %d of file %d (%v), retrying with software encoder", prof.name, n, src.FileID, err)
		outb, err = run(softwareProfile)
	}
	// A segment inside an undecodable stretch of the source comes out empty
	// (or ffmpeg rejects it outright). Serving nothing stalls the player, so
	// cover the gap with a black, silent segment of the same length and let
	// playback continue on the original timeline.
	if unusableSegment(tmp, err, outb) {
		log.Printf("hls: segment %d of file %d is undecodable, substituting filler", n, src.FileID)
		_ = os.Remove(tmp)
		if ferr := a.fillerSegment(ctx, tmp, start, segSec); ferr != nil {
			_ = os.Remove(tmp)
			return "", fmt.Errorf("ffmpeg: %v (filler: %w)", err, ferr)
		}
		err = nil
	}
	if err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(string(outb)))
	}
	if err := os.Rename(tmp, out); err != nil {
		return "", err
	}
	return out, nil
}

// segmentCached reports whether path is a usable cached segment. An empty file
// is never a hit: older builds cached 0-byte output for undecodable ranges.
func segmentCached(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// unusableSegment is true when an encode left no playable output: ffmpeg
// reported corrupt input, or exited cleanly but wrote nothing.
func unusableSegment(tmp string, runErr error, output []byte) bool {
	if runErr != nil {
		return strings.Contains(string(output), "Invalid data found")
	}
	info, err := os.Stat(tmp)
	return err != nil || info.Size() == 0
}

// fillerSegment writes a black, silent MPEG-TS segment covering
// [start, start+segSec) to out.
func (a *API) fillerSegment(ctx context.Context, out string, start, segSec int) error {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "ffmpeg", "-nostdin", "-v", "error", "-y",
		"-f", "lavfi", "-i", "color=c=black:s=1280x720:r=24",
		"-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo",
		"-t", strconv.Itoa(segSec),
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-g", "48",
		"-c:a", "aac", "-b:a", "64k",
		"-output_ts_offset", strconv.Itoa(start), "-muxdelay", "0", "-f", "mpegts", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("filler: %w: %s", err, strings.TrimSpace(string(b)))
	}
	return nil
}

func (a *API) hlsSegment(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	n, nerr := strconv.Atoi(chi.URLParam(r, "n"))
	if err != nil || nerr != nil || n < 0 {
		writeError(w, http.StatusBadRequest, "invalid segment")
		return
	}
	src, status, msg := a.resolveMediaFile(r, id)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	src.Quality = qualityFromRequest(r)
	src.Audio = audioFromRequest(r)
	if n := burnFromRequest(r); n >= 0 {
		src.Burn, src.BurnIndex = true, n
	}
	segSec := a.segmentSeconds()
	if err := a.hlsDuration(r.Context(), &src); err != nil || float64(n*segSec) >= src.Duration {
		writeError(w, http.StatusNotFound, "segment out of range")
		return
	}
	a.pruneHLSCache()
	lastRequestSeg.Store(src.FileID, n)
	path, err := a.ensureSegment(r.Context(), src, n)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "transcode failed")
		return
	}
	// Warm the next segment so steady playback never waits on ffmpeg. Skip
	// it if a newer request for this file has already arrived by the time
	// the goroutine runs — a cheap signal that the viewer seeked elsewhere
	// and this segment won't be watched, which otherwise wastes one of the
	// few scarce transcode slots.
	next := n + 1
	if float64(next*segSec) < src.Duration {
		go func() {
			if v, ok := lastRequestSeg.Load(src.FileID); ok && v.(int) > next {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			_, _ = a.ensureSegment(ctx, src, next)
		}()
	}
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(a.countStream(w, r, id, "hls"), r, path)
}
