package scanner

import (
	"context"
	"encoding/xml"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// WriteNFO writes a Kodi-style <movie> NFO — the same schema ImportNFO
// reads (title/plot/premiered/releasedate/year/studio/genre/actor) — so
// anything this package writes round-trips through its own importer on a
// later rescan. ids are the provider records the title is linked to, written
// as <uniqueid> so a rescan restores the links.
func WriteNFO(path string, title, description, studio, releaseDate string, year int, tags, performers []string, ids []NFOID) error {
	movie := nfoMovie{
		Title:       title,
		Plot:        description,
		Premiered:   releaseDate,
		ReleaseDate: releaseDate,
		Studio:      studio,
		Genres:      tags,
	}
	if year > 0 {
		movie.Year = itoa(year)
	}
	hasDefault := false
	for _, id := range ids {
		u := nfoUniqueID{Type: nfoType(id.Provider), Value: id.ExternalID}
		// Kodi/Jellyfin scrape by the default id, so it's never ReelStash's own.
		if !hasDefault && nfoType(id.Provider) != ReelStashIDType {
			u.Default = "true"
			hasDefault = true
		}
		movie.UniqueIDs = append(movie.UniqueIDs, u)
		switch id.Provider {
		case nfoInteropProvider:
			movie.PornDBID = id.ExternalID
		case "tmdb":
			if number, ok := strings.CutPrefix(id.ExternalID, "movie/"); ok {
				movie.TMDBID = number
			}
		}
	}
	for _, name := range performers {
		movie.Actors = append(movie.Actors, nfoActor{Name: name})
	}

	out, err := xml.MarshalIndent(movie, "", "  ")
	if err != nil {
		return err
	}
	out = append([]byte(xml.Header), out...)
	return os.WriteFile(path, out, 0o644)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// MediaLinkIDs returns the provider records a title is linked to, oldest first,
// followed by the title's own ReelStash uid, in the form WriteNFO takes.
func MediaLinkIDs(ctx context.Context, db *pgxpool.Pool, mediaItemID int64) []NFOID {
	var out []NFOID
	rows, err := db.Query(ctx, `SELECT provider, external_id FROM match_link WHERE kind = 'media' AND entity_id = $1 ORDER BY matched_at`, mediaItemID)
	if err == nil {
		for rows.Next() {
			var id NFOID
			if rows.Scan(&id.Provider, &id.ExternalID) == nil {
				out = append(out, id)
			}
		}
		rows.Close()
	}
	var uid string
	if db.QueryRow(ctx, `SELECT uid::text FROM media_item WHERE id = $1`, mediaItemID).Scan(&uid) == nil {
		out = append(out, NFOID{Provider: ReelStashIDType, ExternalID: uid})
	}
	return out
}
