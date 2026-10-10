package scanner

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Track is one audio or subtitle stream inside a media file.
type Track struct {
	Kind        string // "audio" or "subtitle"
	KindIndex   int    // position among streams of the same kind (ffmpeg's 0:a:N / 0:s:N)
	StreamIndex int
	Codec       string
	Language    string // as tagged in the file (ISO 639-2, e.g. "eng")
	Title       string
	Channels    int
	Default     bool
	Forced      bool
}

func tracksFromStreams(streams []probeStream) []Track {
	var tracks []Track
	next := map[string]int{}
	for _, s := range streams {
		var kind string
		switch s.CodecType {
		case "audio":
			kind = "audio"
		case "subtitle":
			kind = "subtitle"
		default:
			continue
		}
		t := Track{
			Kind: kind, KindIndex: next[kind], StreamIndex: s.Index, Codec: strings.ToLower(s.CodecName),
			Language: strings.TrimSpace(s.Tags["language"]), Title: strings.TrimSpace(s.Tags["title"]), Channels: s.Channels,
			Default: s.Disposition["default"] == 1, Forced: s.Disposition["forced"] == 1,
		}
		// ffprobe lowercases tag keys in some builds and not in others.
		if t.Language == "" {
			t.Language = strings.TrimSpace(s.Tags["LANGUAGE"])
		}
		if t.Title == "" {
			t.Title = strings.TrimSpace(s.Tags["TITLE"])
		}
		next[kind]++
		tracks = append(tracks, t)
	}
	return tracks
}

// SaveTracks replaces a file's stored tracks with what ffprobe just found.
// When the probe failed (ok is false) the tracks are cleared and the file is
// marked as not probed, so they are read again the next time they are needed.
func SaveTracks(ctx context.Context, db *pgxpool.Pool, mediaFileID int64, tracks []Track, ok bool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM media_track WHERE media_file_id = $1`, mediaFileID); err != nil {
		return err
	}
	if !ok {
		if _, err := tx.Exec(ctx, `UPDATE media_file SET tracks_probed_at = NULL WHERE id = $1`, mediaFileID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	for _, t := range tracks {
		if _, err := tx.Exec(ctx, `
			INSERT INTO media_track (media_file_id, kind, kind_index, stream_index, codec, language, title, channels, is_default, is_forced)
			VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, 0), $9, $10)
		`, mediaFileID, t.Kind, t.KindIndex, t.StreamIndex, t.Codec, t.Language, t.Title, t.Channels, t.Default, t.Forced); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE media_file SET tracks_probed_at = now() WHERE id = $1`, mediaFileID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
