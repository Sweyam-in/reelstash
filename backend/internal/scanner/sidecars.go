package scanner

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/lang"
)

// TextSubtitleCodecs are the embedded subtitle formats ffmpeg can turn into
// WebVTT. Picture-based formats (PGS, VobSub, ...) cannot be shown by a
// browser without burning them into the video.
var TextSubtitleCodecs = map[string]bool{
	"subrip": true, "srt": true, "ass": true, "ssa": true, "mov_text": true, "webvtt": true, "text": true, "ttml": true, "subviewer": true, "microdvd": true,
}

// TextSubtitleCodecList is TextSubtitleCodecs as a sorted list, for SQL.
func TextSubtitleCodecList() []string {
	out := make([]string, 0, len(TextSubtitleCodecs))
	for c := range TextSubtitleCodecs {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// Sidecar is a subtitle file lying next to a video.
type Sidecar struct {
	ID       string
	Path     string
	Language string
	Title    string
	Forced   bool
	Default  bool
}

// SidecarExts are the subtitle file types the player can show.
var SidecarExts = map[string]bool{".srt": true, ".vtt": true, ".ass": true, ".ssa": true}
var sidecarDirs = []string{"Subs", "subs", "Subtitles", "subtitles"}

// FindSidecars lists subtitle files that belong to a video: same folder (or a
// Subs/Subtitles folder beside it) and named like the video, optionally
// followed by a language, e.g. Movie.mkv -> Movie.en.srt, Movie.eng.forced.srt.
func FindSidecars(videoPath string) []Sidecar {
	dir := filepath.Dir(videoPath)
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	folders := []string{dir}
	for _, sub := range sidecarDirs {
		folders = append(folders, filepath.Join(dir, sub))
	}
	var out []Sidecar
	seen := map[string]bool{}
	var visited []os.FileInfo // "Subs" and "subs" are one folder on a case-insensitive disk
	for _, folder := range folders {
		info, err := os.Stat(folder)
		if err != nil || !info.IsDir() {
			continue
		}
		dup := false
		for _, v := range visited {
			if os.SameFile(v, info) {
				dup = true
			}
		}
		if dup {
			continue
		}
		visited = append(visited, info)
		entries, err := os.ReadDir(folder)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			ext := strings.ToLower(filepath.Ext(name))
			if !SidecarExts[ext] {
				continue
			}
			stem := strings.TrimSuffix(name, filepath.Ext(name))
			var rest string
			switch {
			case strings.EqualFold(stem, base):
			case len(stem) > len(base) && strings.EqualFold(stem[:len(base)], base) && stem[len(base)] == '.':
				rest = stem[len(base)+1:]
			default:
				continue
			}
			full := filepath.Join(folder, name)
			if seen[full] {
				continue
			}
			seen[full] = true
			sum := sha1.Sum([]byte(strings.TrimPrefix(full, dir)))
			sc := Sidecar{ID: "x" + hex.EncodeToString(sum[:4]), Path: full}
			var extra []string
			for _, tok := range strings.FieldsFunc(rest, func(r rune) bool { return r == '.' || r == '_' || r == ' ' }) {
				switch strings.ToLower(tok) {
				case "forced":
					sc.Forced = true
				case "default":
					sc.Default = true
				case "sdh", "cc", "hi":
					extra = append(extra, "SDH")
				default:
					if code := lang.Normalize(tok); code != "" && sc.Language == "" && len(tok) <= 12 {
						sc.Language = code
					} else {
						extra = append(extra, tok)
					}
				}
			}
			sc.Title = strings.Join(extra, " ")
			out = append(out, sc)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// SaveSidecarCount records how many subtitle files lie next to a media file,
// so titles can be filtered by whether they have subtitles.
func SaveSidecarCount(ctx context.Context, db *pgxpool.Pool, mediaFileID int64, n int) error {
	_, err := db.Exec(ctx, `UPDATE media_file SET sidecar_subtitles = $2 WHERE id = $1 AND sidecar_subtitles IS DISTINCT FROM $2`, mediaFileID, n)
	return err
}

// WriteSidecar saves subtitle data next to a video as <stem>.<tags>.<ext>
// (e.g. Movie.en.sdh.srt), where the player and the organizer pick it up like
// any other subtitle file. An existing file is never replaced: a number is
// added instead (Movie.en.2.srt). The new file gets the video's owner.
func WriteSidecar(videoPath string, tags []string, ext string, data []byte) (string, error) {
	dir := filepath.Dir(videoPath)
	stem := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	ext = "." + strings.TrimPrefix(strings.ToLower(ext), ".")
	if !SidecarExts[ext] {
		return "", fmt.Errorf("unsupported subtitle type %s", ext)
	}
	name := strings.Join(append([]string{stem}, tags...), ".")
	video, err := os.Stat(videoPath)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".reelstash-subtitle-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	_ = os.Chmod(tmp.Name(), 0o644)
	copyOwner(tmp.Name(), video)
	for n := 1; n < 100; n++ {
		target := filepath.Join(dir, name+ext)
		if n > 1 {
			target = filepath.Join(dir, name+"."+strconv.Itoa(n)+ext)
		}
		// A hard link fails if the target exists, so nothing is overwritten.
		if err := os.Link(tmp.Name(), target); err == nil {
			return target, nil
		} else if !os.IsExist(err) {
			// Some file systems (SMB, FAT) have no hard links.
			if _, serr := os.Stat(target); serr == nil {
				continue
			}
			if rerr := os.Rename(tmp.Name(), target); rerr != nil {
				return "", rerr
			}
			return target, nil
		}
	}
	return "", errors.New("too many subtitle files with this name")
}
