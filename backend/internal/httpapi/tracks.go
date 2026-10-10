package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"golang.org/x/text/encoding/charmap"

	"reelstash/backend/internal/lang"
	"reelstash/backend/internal/scanner"
)

// maxAudioTrack bounds ?audio=: no real file has this many audio streams.
const maxAudioTrack = 63

// audioFromRequest reads ?audio=N, the position of the wanted audio track
// among the file's audio tracks (0 is the first, which is also the default).
func audioFromRequest(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("audio"))
	if err != nil || n < 0 || n > maxAudioTrack {
		return 0
	}
	return n
}

// burnFromRequest reads ?burn=N, the picture subtitle to overlay. Absent means off.
func burnFromRequest(r *http.Request) int {
	raw := r.URL.Query().Get("burn")
	if raw == "" {
		return -1
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 || n > maxAudioTrack {
		return -1
	}
	return n
}

type audioTrack struct {
	Index         int    `json:"index"`
	Language      string `json:"language,omitempty"`
	LanguageLabel string `json:"languageLabel,omitempty"`
	Codec         string `json:"codec,omitempty"`
	Channels      int    `json:"channels,omitempty"`
	Title         string `json:"title,omitempty"`
	Default       bool   `json:"default"`
	Label         string `json:"label"`
}

type subtitleTrack struct {
	ID            string `json:"id"`
	Source        string `json:"source"` // "embedded" or "external"
	Language      string `json:"language,omitempty"`
	LanguageLabel string `json:"languageLabel,omitempty"`
	Codec         string `json:"codec,omitempty"`
	Title         string `json:"title,omitempty"`
	Default       bool   `json:"default"`
	Forced        bool   `json:"forced"`
	Label         string `json:"label"`
	// Burn is set for picture subtitles (PGS, VobSub). The player cannot show
	// them as text, so choosing one asks the server to draw them on the video.
	Burn      bool `json:"burn,omitempty"`
	BurnIndex int  `json:"burnIndex,omitempty"`
	// FileName is the subtitle file's name, for files beside the video.
	FileName string `json:"fileName,omitempty"`
}

func channelLayout(n int) string {
	switch n {
	case 1:
		return "Mono"
	case 2:
		return "Stereo"
	case 6:
		return "5.1"
	case 8:
		return "7.1"
	case 0:
		return ""
	}
	return strconv.Itoa(n) + "ch"
}

func audioLabel(t audioTrack) string {
	parts := []string{t.LanguageLabel}
	if parts[0] == "" {
		parts[0] = "Track " + strconv.Itoa(t.Index+1)
	}
	if t.Title != "" && !strings.EqualFold(t.Title, t.LanguageLabel) {
		parts = append(parts, t.Title)
	}
	var tech []string
	if t.Codec != "" {
		tech = append(tech, strings.ToUpper(t.Codec))
	}
	if l := channelLayout(t.Channels); l != "" {
		tech = append(tech, l)
	}
	if len(tech) > 0 {
		parts = append(parts, strings.Join(tech, " "))
	}
	return strings.Join(parts, " · ")
}

func subtitleLabel(t subtitleTrack, n int) string {
	label := t.LanguageLabel
	if label == "" {
		label = "Subtitles " + strconv.Itoa(n)
	}
	if t.Title != "" && !strings.EqualFold(t.Title, t.LanguageLabel) {
		label += " · " + t.Title
	}
	if t.Forced {
		label += " (forced)"
	}
	return label
}

// loadEmbeddedTracks returns a file's audio and subtitle tracks, reading them
// with ffprobe the first time they are needed (files scanned before tracks
// were recorded have none stored yet).
func (a *API) loadEmbeddedTracks(ctx context.Context, fileID int64, fullPath string) ([]scanner.Track, error) {
	var probed *time.Time
	if err := a.DB.QueryRow(ctx, `SELECT tracks_probed_at FROM media_file WHERE id = $1`, fileID).Scan(&probed); err != nil {
		return nil, err
	}
	if probed == nil {
		pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		pr, err := scanner.Probe(pctx, fullPath)
		if err != nil {
			return nil, err
		}
		if err := scanner.SaveTracks(ctx, a.DB, fileID, pr.Tracks, true); err != nil {
			return nil, err
		}
		_ = scanner.SaveRotation(ctx, a.DB, fileID, pr)
		return pr.Tracks, nil
	}
	rows, err := a.DB.Query(ctx, `
		SELECT kind, kind_index, stream_index, COALESCE(codec, ''), COALESCE(language, ''), COALESCE(title, ''), COALESCE(channels, 0), is_default, is_forced
		FROM media_track WHERE media_file_id = $1 ORDER BY kind, kind_index`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []scanner.Track
	for rows.Next() {
		var t scanner.Track
		if err := rows.Scan(&t.Kind, &t.KindIndex, &t.StreamIndex, &t.Codec, &t.Language, &t.Title, &t.Channels, &t.Default, &t.Forced); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// mediaTracks lists the audio tracks and text subtitles of a title's file.
func (a *API) mediaTracks(w http.ResponseWriter, r *http.Request) {
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
	audio := []audioTrack{}
	subs := []subtitleTrack{}
	hidden := 0
	tracks, err := a.loadEmbeddedTracks(r.Context(), src.FileID, src.FullPath)
	if err != nil {
		log.Printf("tracks: file %d: %v", src.FileID, err) // still offer sidecars
	}
	for _, t := range tracks {
		code := lang.Normalize(t.Language)
		switch t.Kind {
		case "audio":
			at := audioTrack{Index: t.KindIndex, Language: code, LanguageLabel: labelFor(code), Codec: t.Codec, Channels: t.Channels, Title: t.Title, Default: t.Default}
			at.Label = audioLabel(at)
			audio = append(audio, at)
		case "subtitle":
			st := subtitleTrack{ID: "e" + strconv.Itoa(t.KindIndex), Source: "embedded", Language: code, LanguageLabel: labelFor(code), Codec: t.Codec, Title: t.Title, Default: t.Default, Forced: t.Forced}
			if !scanner.TextSubtitleCodecs[t.Codec] {
				st.ID = "b" + strconv.Itoa(t.KindIndex)
				st.Burn = true
				st.BurnIndex = t.KindIndex
			}
			st.Label = subtitleLabel(st, len(subs)+1)
			if st.Burn {
				st.Label += " (burned in)"
			}
			subs = append(subs, st)
		}
	}
	sidecars := scanner.FindSidecars(src.FullPath)
	if err := scanner.SaveSidecarCount(r.Context(), a.DB, src.FileID, len(sidecars)); err != nil {
		log.Printf("tracks: file %d: save sidecar count: %v", src.FileID, err)
	}
	for _, sc := range sidecars {
		st := subtitleTrack{ID: sc.ID, Source: "external", Language: sc.Language, LanguageLabel: labelFor(sc.Language), Codec: strings.TrimPrefix(strings.ToLower(filepath.Ext(sc.Path)), "."), Title: sc.Title, Default: sc.Default, Forced: sc.Forced, FileName: filepath.Base(sc.Path)}
		st.Label = subtitleLabel(st, len(subs)+1)
		subs = append(subs, st)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"fileId": src.FileID, "videoCodec": src.VideoCodec, "audio": audio, "subtitles": subs, "hiddenSubtitles": hidden,
	})
}

func labelFor(code string) string {
	if code == "" {
		return ""
	}
	return lang.Label(code)
}

var (
	subtitleLocks sync.Map // cache path -> *sync.Mutex
	subtitleSem   = make(chan struct{}, 2)
)

func (a *API) subtitleCacheDir() string {
	if a.Cfg.SubtitleDir != "" {
		return a.Cfg.SubtitleDir
	}
	return filepath.Join(filepath.Dir(a.Cfg.ThumbnailDir), "subtitles")
}

// mediaSubtitle serves one subtitle track as WebVTT.
func (a *API) mediaSubtitle(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	track := chi.URLParam(r, "track")
	src, status, msg := a.resolveMediaFile(r, id)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	var data []byte
	switch {
	case strings.HasPrefix(track, "x"):
		var found *scanner.Sidecar
		for _, sc := range scanner.FindSidecars(src.FullPath) {
			if sc.ID == track {
				sc := sc
				found = &sc
				break
			}
		}
		if found == nil {
			writeError(w, http.StatusNotFound, "subtitle not found")
			return
		}
		data, err = a.sidecarVTT(r.Context(), src.FileID, *found)
	case strings.HasPrefix(track, "e"):
		n, perr := strconv.Atoi(track[1:])
		if perr != nil || n < 0 || n > maxAudioTrack {
			writeError(w, http.StatusBadRequest, "invalid subtitle track")
			return
		}
		data, err = a.embeddedVTT(r.Context(), src, n)
	default:
		writeError(w, http.StatusBadRequest, "invalid subtitle track")
		return
	}
	if err != nil {
		log.Printf("subtitles: file %d track %s: %v", src.FileID, track, err)
		writeError(w, http.StatusUnprocessableEntity, "could not read this subtitle track")
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(data)
}

// embeddedVTT extracts subtitle stream n with ffmpeg, caching the result.
func (a *API) embeddedVTT(ctx context.Context, src mediaSource, n int) ([]byte, error) {
	if !ffmpegAvailable() {
		return nil, errors.New("ffmpeg is not installed")
	}
	info, err := os.Stat(src.FullPath)
	if err != nil {
		return nil, err
	}
	dir := a.subtitleCacheDir()
	out := filepath.Join(dir, fmt.Sprintf("%d-e%d-%d.vtt", src.FileID, n, info.ModTime().Unix()))
	if b, err := os.ReadFile(out); err == nil && len(b) > 0 {
		touchCache(out)
		return b, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	lk, _ := subtitleLocks.LoadOrStore(out, &sync.Mutex{})
	mu := lk.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	if b, err := os.ReadFile(out); err == nil && len(b) > 0 {
		return b, nil
	}
	select {
	case subtitleSem <- struct{}{}:
		defer func() { <-subtitleSem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	tmp := out + ".part"
	if b, err := exec.CommandContext(cctx, "ffmpeg", "-nostdin", "-v", "error", "-y", "-i", src.FullPath,
		"-map", "0:s:"+strconv.Itoa(n), "-f", "webvtt", tmp).CombinedOutput(); err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(string(b)))
	}
	if err := os.Rename(tmp, out); err != nil {
		return nil, err
	}
	return os.ReadFile(out)
}

const maxSidecarBytes = 8 << 20

// sidecarVTT reads a subtitle file and returns it as WebVTT.
func (a *API) sidecarVTT(ctx context.Context, fileID int64, sc scanner.Sidecar) ([]byte, error) {
	f, err := os.Open(sc.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxSidecarBytes {
		return nil, errors.New("subtitle file is too large")
	}
	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	text := decodeSubtitleText(raw)
	switch strings.ToLower(filepath.Ext(sc.Path)) {
	case ".vtt":
		if !strings.HasPrefix(text, "WEBVTT") {
			text = "WEBVTT\n\n" + text
		}
		return []byte(text), nil
	case ".srt":
		return []byte(srtToVTT(text)), nil
	}
	// .ass / .ssa: let ffmpeg do the conversion, then cache it.
	if !ffmpegAvailable() {
		return nil, errors.New("ffmpeg is not installed")
	}
	dir := a.subtitleCacheDir()
	out := filepath.Join(dir, fmt.Sprintf("%d-%s-%d.vtt", fileID, sc.ID, info.ModTime().Unix()))
	if b, err := os.ReadFile(out); err == nil && len(b) > 0 {
		touchCache(out)
		return b, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	// Several players can request the same subtitle at once. Serialize the
	// conversion so they cannot overwrite or rename the shared .part file.
	lk, _ := subtitleLocks.LoadOrStore(out, &sync.Mutex{})
	mu := lk.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	if b, err := os.ReadFile(out); err == nil && len(b) > 0 {
		return b, nil
	}
	cctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	tmp := out + ".part"
	if b, err := exec.CommandContext(cctx, "ffmpeg", "-nostdin", "-v", "error", "-y", "-i", sc.Path, "-f", "webvtt", tmp).CombinedOutput(); err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(string(b)))
	}
	if info, err := os.Stat(tmp); err != nil || info.Size() == 0 {
		_ = os.Remove(tmp)
		return nil, errors.New("ffmpeg produced an empty subtitle")
	}
	if err := os.Rename(tmp, out); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return os.ReadFile(out)
}

// decodeSubtitleText returns UTF-8 text from a subtitle file's bytes: strips
// a byte-order mark and treats anything that is not valid UTF-8 as Windows-1252,
// the usual encoding of older Western .srt files.
func decodeSubtitleText(raw []byte) string {
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(raw) {
		return string(raw)
	}
	if dec, err := charmap.Windows1252.NewDecoder().Bytes(raw); err == nil {
		return string(dec)
	}
	return strings.ToValidUTF8(string(raw), "�")
}

var srtTimeLine = regexp.MustCompile(`(\d{1,2}:\d{2}:\d{2}),(\d{1,3})\s*-->\s*(\d{1,2}:\d{2}:\d{2}),(\d{1,3})`)

// srtToVTT converts SubRip text to WebVTT: a header, and "." instead of ","
// in the timestamps. Cue numbers are kept (WebVTT allows them as cue ids).
func srtToVTT(srt string) string {
	srt = strings.ReplaceAll(strings.ReplaceAll(srt, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(srt, "\n")
	for i, line := range lines {
		if m := srtTimeLine.FindStringSubmatch(line); m != nil {
			lines[i] = m[1] + "." + padMillis(m[2]) + " --> " + m[3] + "." + padMillis(m[4])
		}
	}
	return "WEBVTT\n\n" + strings.Join(lines, "\n")
}

func padMillis(ms string) string {
	for len(ms) < 3 {
		ms += "0"
	}
	return ms
}
