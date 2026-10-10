package scanner

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/names"
	"reelstash/backend/internal/slug"
)

// Third-party NFO interop: Jellyfin and Kodi scrapers for one adult-metadata
// service write its scene id as <uniqueid type="porndb">, <theporndbid> or
// <tpdbid>. ReelStash reads and writes those tags for the provider plugin that
// registers under nfoInteropProvider; every other provider uses its own id as
// the uniqueid type.
const (
	nfoInteropProvider = "x-porndb"
	nfoInteropType     = "porndb"
)

// nfoType is the NFO uniqueid type a provider's id is written under.
func nfoType(providerID string) string {
	if providerID == nfoInteropProvider {
		return nfoInteropType
	}
	return providerID
}

// nfoMovie mirrors the Kodi-style <movie> NFO ReelStash's existing namer
// pipeline already writes (backend/app/utils/moviexml.py): title, plot,
// premiered/year, studio, genre/tag, and <actor><name>.
type nfoMovie struct {
	XMLName     xml.Name   `xml:"movie"`
	Title       string     `xml:"title"`
	Plot        string     `xml:"plot"`
	Premiered   string     `xml:"premiered"`
	ReleaseDate string     `xml:"releasedate"`
	Year        string     `xml:"year"`
	Season      string     `xml:"season"`
	Episode     string     `xml:"episode"`
	Studio      string     `xml:"studio"`
	Genres      []string   `xml:"genre"`
	Tags        []string   `xml:"tag"`
	Actors      []nfoActor `xml:"actor"`
	// Provider ids: <uniqueid type="tmdb">movie/603</uniqueid> (ReelStash's own
	// form) plus Kodi/Jellyfin's plain <uniqueid type="tmdb">603</uniqueid> and
	// <tmdbid>603</tmdbid>.
	UniqueIDs []nfoUniqueID `xml:"uniqueid"`
	TMDBID    string        `xml:"tmdbid,omitempty"`
	PornDBID  string        `xml:"theporndbid,omitempty"`
	// TPDBID is the other spelling Jellyfin and Kodi
	// scrapers use for the same record id.
	TPDBID string `xml:"tpdbid,omitempty"`
}

type nfoUniqueID struct {
	Type    string `xml:"type,attr"`
	Default string `xml:"default,attr,omitempty"`
	Value   string `xml:",chardata"`
}

// NFOID is one provider record id written to / read from an NFO.
type NFOID struct {
	Provider   string // a provider id, such as "tmdb"
	ExternalID string
}

// ReelStashIDType is the <uniqueid type="..."> that carries a title's
// media_item.uid, so a file can be relinked to its title from the NFO alone.
const ReelStashIDType = "reelstash"

// ReelStashUID returns the title uid the NFO carries, or "" if it has none (or
// an invalid one).
func (m nfoMovie) ReelStashUID() string {
	for _, u := range m.UniqueIDs {
		if strings.EqualFold(strings.TrimSpace(u.Type), ReelStashIDType) {
			if id, err := uuid.Parse(strings.TrimSpace(u.Value)); err == nil {
				return id.String()
			}
		}
	}
	return ""
}

// LinkHints returns the provider records an NFO says its title matches.
func (m nfoMovie) LinkHints() []NFOID {
	var out []NFOID
	seen := map[string]bool{}
	add := func(provider, id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[provider] {
			return
		}
		seen[provider] = true
		out = append(out, NFOID{Provider: provider, ExternalID: id})
	}
	for _, u := range m.UniqueIDs {
		kind := strings.ToLower(strings.TrimSpace(u.Type))
		switch kind {
		case "porndb", "theporndb", "tpdb", "metadataapi", "theporndb.net":
			add(nfoInteropProvider, u.Value)
		case "tmdb", "themoviedb":
			id := strings.TrimSpace(u.Value)
			if !strings.Contains(id, "/") { // Kodi writes the bare number
				id = "movie/" + id
			}
			add("tmdb", id)
		default:
			// Provider plugins' ids start with "x-" and are written as their own type.
			if strings.HasPrefix(kind, "x-") {
				add(kind, u.Value)
			}
		}
	}
	if id := strings.TrimSpace(m.TMDBID); id != "" && !seen["tmdb"] {
		if !strings.Contains(id, "/") {
			id = "movie/" + id
		}
		add("tmdb", id)
	}
	if id := strings.TrimSpace(m.PornDBID); id != "" && !seen[nfoInteropProvider] {
		add(nfoInteropProvider, id)
	}
	if id := strings.TrimSpace(m.TPDBID); id != "" && !seen[nfoInteropProvider] {
		add(nfoInteropProvider, id)
	}
	// A scene uuid under a type we don't know (a Jellyfin plugin's own key) is
	// that service's id shape: nothing else a scene NFO carries looks like one.
	if !seen[nfoInteropProvider] {
		for _, u := range m.UniqueIDs {
			kind := strings.ToLower(strings.TrimSpace(u.Type))
			if kind == ReelStashIDType || kind == "tmdb" || kind == "themoviedb" || kind == "imdb" || kind == "tvdb" {
				continue
			}
			if id, err := uuid.Parse(strings.TrimSpace(u.Value)); err == nil {
				add(nfoInteropProvider, id.String())
				break
			}
		}
	}
	return out
}

// importNFOLinks records the provider links an NFO declares. It never replaces
// a link that already exists.
func importNFOLinks(ctx context.Context, db *pgxpool.Pool, mediaItemID int64, movie nfoMovie) error {
	hints := movie.LinkHints()
	for _, h := range hints {
		if _, err := db.Exec(ctx, `
			INSERT INTO match_link (kind, entity_id, provider, external_id) VALUES ('media', $1, $2, $3)
			ON CONFLICT (kind, entity_id, provider) DO NOTHING
		`, mediaItemID, h.Provider, h.ExternalID); err != nil {
			return err
		}
	}
	if len(hints) > 0 {
		_, err := db.Exec(ctx, `UPDATE media_item SET match_status = 'matched' WHERE id = $1 AND match_status = 'unmatched'`, mediaItemID)
		return err
	}
	return nil
}

// RefreshNFOLinksFromFile checks an already-imported NFO for provider IDs
// introduced by a newer parser without replacing the title's metadata.
func RefreshNFOLinksFromFile(ctx context.Context, db *pgxpool.Pool, mediaFileID, mediaItemID int64, path string) error {
	movie, err := parseNFO(path)
	if err != nil {
		return err
	}
	if err := importNFOLinks(ctx, db, mediaItemID, movie); err != nil {
		return err
	}
	return RefreshNFORecord(ctx, db, mediaFileID, path)
}

type nfoActor struct {
	Name string `xml:"name"`
}

// FindAdjacentNFO returns the .nfo path next to a media file, if any.
func FindAdjacentNFO(mediaPath string) (string, bool) {
	candidate := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath)) + ".nfo"
	if _, err := os.Stat(candidate); err == nil {
		return candidate, true
	}
	return "", false
}

func parseNFO(path string) (nfoMovie, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nfoMovie{}, err
	}
	var movie nfoMovie
	if err := xml.Unmarshal(data, &movie); err != nil {
		return nfoMovie{}, err
	}
	return movie, nil
}

// ImportNFO parses an adjacent NFO file, records it in nfo_metadata, and
// applies its fields to the media_item unless that item's metadata is
// protected (metadata_source = MANUAL), per the "never silently overwrite a
// manual edit" rule.
func ImportNFO(ctx context.Context, db *pgxpool.Pool, mediaFileID, mediaItemID int64, storageLocationID int64, relativePath, nfoPath string) error {
	movie, err := parseNFO(nfoPath)
	if err != nil {
		return err
	}
	// ReelStash's own identity-only stub: nothing to import, and recording it
	// would make the title look NFO-described to the auto-matcher.
	if !movie.hasMetadata() {
		return nil
	}
	contentHash, err := FullHash(nfoPath)
	if err != nil {
		return err
	}

	if err := recordNFO(ctx, db, mediaFileID, relativePath, movie, contentHash); err != nil {
		return err
	}

	if err := importNFOLinks(ctx, db, mediaItemID, movie); err != nil {
		return err
	}

	var metadataSource string
	if err := db.QueryRow(ctx, `SELECT metadata_source FROM media_item WHERE id = $1`, mediaItemID).Scan(&metadataSource); err != nil {
		return err
	}
	if metadataSource == "MANUAL" {
		return nil
	}

	var studioID *int64
	if movie.Studio != "" {
		id, err := findOrCreateStudio(ctx, db, movie.Studio)
		if err != nil {
			return err
		}
		studioID = &id
	}

	var releaseDate *string
	if movie.ReleaseDate != "" {
		releaseDate = &movie.ReleaseDate
	} else if movie.Premiered != "" {
		releaseDate = &movie.Premiered
	}
	var productionYear *int
	if y, err := strconv.Atoi(movie.Year); err == nil {
		productionYear = &y
	}

	var season, episode *int
	if n, err := strconv.Atoi(strings.TrimSpace(movie.Season)); err == nil && n >= 0 {
		season = &n
	}
	if n, err := strconv.Atoi(strings.TrimSpace(movie.Episode)); err == nil && n >= 0 {
		episode = &n
	}
	kind := ""
	if season != nil && episode != nil {
		kind = "episode"
	}
	if movie.Title != "" || kind == "episode" {
		if _, err := db.Exec(ctx, `
			UPDATE media_item SET
				title = CASE WHEN $2 = '' THEN title ELSE $2 END,
				sort_title = CASE WHEN $2 = '' THEN sort_title ELSE $2 END,
				description = COALESCE(NULLIF($3, ''), description),
				studio_id = COALESCE($4, studio_id),
				release_date = COALESCE($5::date, release_date),
				production_year = COALESCE($6, production_year),
				season_number = COALESCE($7, season_number),
				episode_number = COALESCE($8, episode_number),
				kind = CASE WHEN $9 = '' THEN kind ELSE $9 END,
				metadata_source = 'NFO',
				updated_at = now()
			WHERE id = $1
		`, mediaItemID, movie.Title, movie.Plot, studioID, releaseDate, productionYear, season, episode, kind); err != nil {
			return err
		}
	}

	performerNames := make([]string, 0, len(movie.Actors))
	for _, actor := range movie.Actors {
		if actor.Name != "" {
			performerNames = append(performerNames, actor.Name)
		}
	}
	if err := attachPerformers(ctx, db, mediaItemID, performerNames); err != nil {
		return err
	}

	categoryNames := append(append([]string{}, movie.Genres...), movie.Tags...)
	if err := attachCategories(ctx, db, mediaItemID, categoryNames); err != nil {
		return err
	}

	return nil
}

// recordNFO upserts the nfo_metadata row for a media file's NFO.
func recordNFO(ctx context.Context, db *pgxpool.Pool, mediaFileID int64, relativePath string, movie nfoMovie, contentHash string) error {
	parsed, _ := json.Marshal(movie)
	_, err := db.Exec(ctx, `
		INSERT INTO nfo_metadata (media_file_id, relative_path, parsed_metadata, content_hash, imported_at, updated_at)
		VALUES ($1, $2, $3, $4, now(), now())
		ON CONFLICT (media_file_id) DO UPDATE SET
			relative_path = EXCLUDED.relative_path,
			parsed_metadata = EXCLUDED.parsed_metadata,
			content_hash = EXCLUDED.content_hash,
			imported_at = CASE WHEN nfo_metadata.content_hash IS DISTINCT FROM EXCLUDED.content_hash THEN now() ELSE nfo_metadata.imported_at END,
			updated_at = now()
	`, mediaFileID, relativePath, parsed, contentHash)
	return err
}

// findOrCreateStudio resolves an NFO studio name. A name that was merged into
// another studio is stored as an alias, so it lands on the surviving record
// instead of resurrecting the duplicate.
func findOrCreateStudio(ctx context.Context, db *pgxpool.Pool, name string) (int64, error) {
	var id int64
	if db.QueryRow(ctx, `SELECT id FROM studio WHERE name = $1`, name).Scan(&id) == nil {
		return id, nil
	}
	var owner int64
	var owners int
	if db.QueryRow(ctx, `
		SELECT COALESCE(MIN(studio_id), 0), COUNT(DISTINCT studio_id)
		FROM studio_alias WHERE lower(alias) = lower($1)
	`, name).Scan(&owner, &owners) == nil && owners == 1 {
		return owner, nil
	}
	err := db.QueryRow(ctx, `
		INSERT INTO studio (name) VALUES ($1)
		ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, name).Scan(&id)
	return id, err
}

// findOrCreatePerformer resolves an NFO actor name to a performer. A name that
// was merged into another performer is stored as an alias, so it lands on the
// surviving record instead of resurrecting the duplicate.
func findOrCreatePerformer(ctx context.Context, db *pgxpool.Pool, name string) (int64, error) {
	var id int64
	err := db.QueryRow(ctx, `SELECT id FROM performer WHERE name = $1`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	var owner int64
	var owners int
	if db.QueryRow(ctx, `
		SELECT COALESCE(MIN(performer_id), 0), COUNT(DISTINCT performer_id)
		FROM performer_alias WHERE lower(alias) = lower($1)
	`, name).Scan(&owner, &owners) == nil && owners == 1 {
		return owner, nil
	}
	// Providers spell the same person differently ("Riley-Mitchel", "MITCHEL,
	// Riley", accents), so fall back to a normalized comparison before creating
	// a duplicate. Only an unambiguous hit is used.
	if id, ok := PerformerByKey(ctx, db, name); ok {
		return id, nil
	}
	err = db.QueryRow(ctx, `
		INSERT INTO performer (name) VALUES ($1)
		ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, name).Scan(&id)
	return id, err
}

// PerformerByKey finds the one performer whose name or alias normalizes to the
// same key as name.
func PerformerByKey(ctx context.Context, db *pgxpool.Pool, name string) (int64, bool) {
	key := names.Key(name)
	if key == "" {
		return 0, false
	}
	rows, err := db.Query(ctx, `
		SELECT id, name FROM performer
		UNION ALL
		SELECT performer_id, alias FROM performer_alias
	`)
	if err != nil {
		return 0, false
	}
	defer rows.Close()
	found := map[int64]bool{}
	for rows.Next() {
		var id int64
		var candidate string
		if rows.Scan(&id, &candidate) == nil && names.Key(candidate) == key {
			found[id] = true
		}
	}
	if len(found) != 1 {
		return 0, false
	}
	for id := range found {
		return id, true
	}
	return 0, false
}

func attachPerformers(ctx context.Context, db *pgxpool.Pool, mediaItemID int64, names []string) error {
	for i, name := range names {
		performerID, err := findOrCreatePerformer(ctx, db, name)
		if err != nil {
			return err
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO media_item_performer (media_item_id, performer_id, position)
			VALUES ($1, $2, $3)
			ON CONFLICT (media_item_id, performer_id) DO NOTHING
		`, mediaItemID, performerID, i); err != nil {
			return err
		}
	}
	return nil
}

func attachCategories(ctx context.Context, db *pgxpool.Pool, mediaItemID int64, names []string) error {
	for _, name := range names {
		if name == "" {
			continue
		}
		categorySlug := slug.Name(name)
		var categoryID int64
		err := db.QueryRow(ctx, `
			INSERT INTO category (name, slug) VALUES ($1, $2)
			ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
			RETURNING id
		`, name, categorySlug).Scan(&categoryID)
		if err != nil {
			return err
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO media_item_category (media_item_id, category_id)
			VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, mediaItemID, categoryID); err != nil {
			return err
		}
	}
	return nil
}
