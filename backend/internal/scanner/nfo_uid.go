package scanner

import (
	"context"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	nfoReelStashID = regexp.MustCompile(`(?is)[ \t]*<uniqueid\b[^>]*\btype\s*=\s*["']` + ReelStashIDType + `["'][^>]*>.*?</uniqueid>[ \t]*\r?\n?`)

	errNotMovieNFO = errors.New("not a <movie> NFO")
)

// hasMetadata reports whether the NFO describes the title at all, as opposed
// to an identity-only stub carrying just ReelStash's uid.
func (m nfoMovie) hasMetadata() bool {
	return m.Title != "" || m.Plot != "" || m.Studio != "" || m.Premiered != "" || m.ReleaseDate != "" ||
		m.Year != "" || len(m.Genres) > 0 || len(m.Tags) > 0 || len(m.Actors) > 0 || len(m.LinkHints()) > 0
}

// SetNFOUID makes the NFO carry exactly one <uniqueid type="reelstash"> with
// uid, leaving every other byte of the file alone. It reports whether the file
// changed.
func SetNFOUID(path, uid string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	doc := string(data)
	if !strings.Contains(doc, "</movie>") {
		return false, errNotMovieNFO
	}
	element := `  <uniqueid type="` + ReelStashIDType + `">` + nfoEscape(uid) + "</uniqueid>\n"
	if nfoReelStashID.MatchString(doc) {
		done := false
		doc = nfoReelStashID.ReplaceAllStringFunc(doc, func(string) string {
			if done {
				return ""
			}
			done = true
			return element
		})
	} else {
		doc = insertBeforeMovieEnd(doc, element)
	}
	return writeIfChanged(path, string(data), doc)
}

// AddNFOProviderTags adds Jellyfin-style provider ID tags to an existing
// NFO without replacing its other metadata or an ID already in that tag.
func AddNFOProviderTags(path string, ids []NFOID) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	doc := string(data)
	if !strings.Contains(doc, "</movie>") {
		return false, errNotMovieNFO
	}
	var movie nfoMovie
	if err := xml.Unmarshal(data, &movie); err != nil {
		return false, err
	}
	for _, id := range ids {
		switch id.Provider {
		case nfoInteropProvider:
			if movie.PornDBID == "" && id.ExternalID != "" {
				doc = setNFOElement(doc, "theporndbid", id.ExternalID)
				movie.PornDBID = id.ExternalID
			}
		case "tmdb":
			if number, ok := strings.CutPrefix(id.ExternalID, "movie/"); ok && movie.TMDBID == "" && number != "" {
				doc = setNFOElement(doc, "tmdbid", number)
				movie.TMDBID = number
			}
		}
	}
	return writeIfChanged(path, string(data), doc)
}

// NFOUID returns the ReelStash uid in the NFO at path, or "".
func NFOUID(path string) string {
	movie, err := parseNFO(path)
	if err != nil {
		return ""
	}
	return movie.ReelStashUID()
}

// writeStubNFO writes an identity-only NFO: a title with no metadata yet (only
// its filename) still gets a uid on disk, without the NFO looking like
// metadata to the importer or the auto-matcher.
func writeStubNFO(path, uid string) error {
	movie := struct {
		XMLName  xml.Name    `xml:"movie"`
		UniqueID nfoUniqueID `xml:"uniqueid"`
	}{UniqueID: nfoUniqueID{Type: ReelStashIDType, Value: uid}}
	out, err := xml.MarshalIndent(movie, "", "  ")
	if err != nil {
		return err
	}
	out = append([]byte(xml.Header), out...)
	out = append(out, '\n')
	return os.WriteFile(path, out, 0o644)
}

// WriteItemNFO writes a full NFO for a title from what the database holds.
func WriteItemNFO(ctx context.Context, db *pgxpool.Pool, mediaItemID int64, nfoPath string) error {
	var title, description, studio, releaseDate string
	var year int
	var season, episode *int
	if err := db.QueryRow(ctx, `
		SELECT mi.title, COALESCE(mi.description, ''), COALESCE(s.name, ''),
		       COALESCE(mi.release_date::text, ''), COALESCE(mi.production_year, 0),
		       mi.season_number, mi.episode_number
		FROM media_item mi LEFT JOIN studio s ON s.id = mi.studio_id WHERE mi.id = $1
	`, mediaItemID).Scan(&title, &description, &studio, &releaseDate, &year, &season, &episode); err != nil {
		return err
	}
	names := func(query string) []string {
		var out []string
		rows, err := db.Query(ctx, query, mediaItemID)
		if err != nil {
			return nil
		}
		defer rows.Close()
		for rows.Next() {
			var n string
			if rows.Scan(&n) == nil {
				out = append(out, n)
			}
		}
		return out
	}
	categories := names(`SELECT c.name FROM media_item_category m JOIN category c ON c.id = m.category_id WHERE m.media_item_id = $1 ORDER BY c.name`)
	performers := names(`SELECT p.name FROM media_item_performer m JOIN performer p ON p.id = m.performer_id WHERE m.media_item_id = $1 ORDER BY m.position`)
	if err := WriteNFO(nfoPath, title, description, studio, releaseDate, year, categories, performers, MediaLinkIDs(ctx, db, mediaItemID)); err != nil {
		return err
	}
	if season == nil && episode == nil {
		return nil
	}
	if season != nil {
		if _, err := EditNFO(nfoPath, NFOFields{Season: season}); err != nil {
			return err
		}
	}
	if episode != nil {
		if _, err := EditNFO(nfoPath, NFOFields{Episode: episode}); err != nil {
			return err
		}
	}
	return nil
}

// SyncFileNFO makes sure the video at absPath has an adjacent NFO carrying its
// title's uid: an existing NFO gets the <uniqueid type="reelstash"> added or
// corrected; a missing one is created — in full when the title has metadata,
// or as an identity-only stub when all it has is its filename (so it stays
// eligible for auto-matching). It then records the uid as synced on the file.
func SyncFileNFO(ctx context.Context, db *pgxpool.Pool, mediaFileID, mediaItemID int64, relPath, absPath string) error {
	var uid, source string
	var linked bool
	if err := db.QueryRow(ctx, `
		SELECT uid::text, metadata_source,
		       EXISTS (SELECT 1 FROM match_link WHERE kind = 'media' AND entity_id = mi.id)
		FROM media_item mi WHERE id = $1
	`, mediaItemID).Scan(&uid, &source, &linked); err != nil {
		return err
	}

	if nfoPath, ok := FindAdjacentNFO(absPath); ok {
		changed, err := SetNFOUID(nfoPath, uid)
		if err != nil {
			return err
		}
		providerChanged, err := AddNFOProviderTags(nfoPath, MediaLinkIDs(ctx, db, mediaItemID))
		if err != nil {
			return err
		}
		if changed || providerChanged || linked {
			if err := RefreshNFORecord(ctx, db, mediaFileID, nfoPath); err != nil {
				return err
			}
		}
	} else {
		nfoPath = strings.TrimSuffix(absPath, filepath.Ext(absPath)) + ".nfo"
		if source == "FILENAME" && !linked {
			if err := writeStubNFO(nfoPath, uid); err != nil {
				return err
			}
		} else {
			if err := WriteItemNFO(ctx, db, mediaItemID, nfoPath); err != nil {
				return err
			}
			movie, err := parseNFO(nfoPath)
			if err != nil {
				return err
			}
			hash, err := FullHash(nfoPath)
			if err != nil {
				return err
			}
			if err := recordNFO(ctx, db, mediaFileID, relPath, movie, hash); err != nil {
				return err
			}
		}
	}

	_, err := db.Exec(ctx, `UPDATE media_file SET nfo_uid = $2 WHERE id = $1`, mediaFileID, uid)
	return err
}
