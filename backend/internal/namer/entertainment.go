package namer

import (
	"context"
	"log"
	"strings"

	"reelstash/backend/internal/names"
	"reelstash/backend/internal/provider"
	"reelstash/backend/internal/scanner"
)

// applyEntertainment stores what movie/TV providers add beyond a scene match:
// whether the title is a movie or an episode, the TV show and season/episode
// it belongs to, the roles its cast play, and its directors and writers.
// Scene providers leave these fields empty, so this is a no-op for them.
func (e *Engine) applyEntertainment(ctx context.Context, mediaItemID int64, scene provider.MediaResult) {
	if scene.Kind == "movie" || scene.Kind == "episode" {
		var season, episode *int
		if scene.Kind == "episode" && scene.Season > 0 && scene.Episode > 0 {
			season, episode = &scene.Season, &scene.Episode
		}
		if _, err := e.DB.Exec(ctx, `UPDATE media_item SET kind = $2, season_number = $3, episode_number = $4, updated_at = now() WHERE id = $1`,
			mediaItemID, scene.Kind, season, episode); err != nil {
			log.Printf("namer: set kind for media_item %d: %v", mediaItemID, err)
		}
	}
	if scene.Kind == "episode" && strings.TrimSpace(scene.Show) != "" {
		if err := e.attachShow(ctx, mediaItemID, scene); err != nil {
			log.Printf("namer: attach media_item %d to show %q: %v", mediaItemID, scene.Show, err)
		}
	}
	e.saveCharacters(ctx, mediaItemID, scene.Characters)
	e.saveCrew(ctx, mediaItemID, scene.Crew)
}

// attachShow puts an episode in its TV show's series: the series already
// linked to the show's provider record, else one with the show's name, else a
// new one.
func (e *Engine) attachShow(ctx context.Context, mediaItemID int64, scene provider.MediaResult) error {
	var seriesID int64
	if scene.ShowRef.ExternalID != "" {
		_ = e.DB.QueryRow(ctx, `SELECT entity_id FROM match_link WHERE kind = 'series' AND provider = $1 AND external_id = $2 ORDER BY entity_id LIMIT 1`,
			scene.ShowRef.Provider, scene.ShowRef.ExternalID).Scan(&seriesID)
	}
	if seriesID == 0 {
		_ = e.DB.QueryRow(ctx, `SELECT id FROM series WHERE lower(name) = lower($1) AND (kind = 'show' OR kind IS NULL) ORDER BY kind NULLS LAST, id LIMIT 1`, scene.Show).Scan(&seriesID)
	}
	if seriesID == 0 {
		if err := e.DB.QueryRow(ctx, `INSERT INTO series (name, kind) VALUES ($1, 'show') RETURNING id`, scene.Show).Scan(&seriesID); err != nil {
			return err
		}
	}
	if _, err := e.DB.Exec(ctx, `UPDATE series SET kind = 'show', updated_at = now() WHERE id = $1 AND kind IS NULL`, seriesID); err != nil {
		return err
	}
	if scene.ShowRef.ExternalID != "" {
		if _, err := e.DB.Exec(ctx, `
			INSERT INTO match_link (kind, entity_id, provider, external_id) VALUES ('series', $1, $2, $3)
			ON CONFLICT (kind, entity_id, provider) DO NOTHING
		`, seriesID, scene.ShowRef.Provider, scene.ShowRef.ExternalID); err != nil {
			return err
		}
	}
	_, err := e.DB.Exec(ctx, `UPDATE media_item SET series_id = $2, updated_at = now() WHERE id = $1`, mediaItemID, seriesID)
	return err
}

// saveCharacters records the role each cast member plays, matched by name.
func (e *Engine) saveCharacters(ctx context.Context, mediaItemID int64, characters map[string]string) {
	if len(characters) == 0 {
		return
	}
	byKey := map[string]string{}
	for name, role := range characters {
		byKey[names.Key(name)] = role
	}
	rows, err := e.DB.Query(ctx, `SELECT p.id, p.name FROM media_item_performer m JOIN performer p ON p.id = m.performer_id WHERE m.media_item_id = $1`, mediaItemID)
	if err != nil {
		return
	}
	type role struct {
		id   int64
		name string
	}
	var todo []role
	for rows.Next() {
		var id int64
		var name string
		if rows.Scan(&id, &name) == nil {
			if r, ok := byKey[names.Key(name)]; ok {
				todo = append(todo, role{id, r})
			}
		}
	}
	rows.Close()
	for _, r := range todo {
		_, _ = e.DB.Exec(ctx, `UPDATE media_item_performer SET character = $3 WHERE media_item_id = $1 AND performer_id = $2`, mediaItemID, r.id, r.name)
	}
}

// saveCrew replaces a title's directors and writers. Each is the performer
// already linked to their provider record, else one with the same name, else
// a new person.
func (e *Engine) saveCrew(ctx context.Context, mediaItemID int64, crew []provider.CrewCredit) {
	if len(crew) == 0 {
		return
	}
	if _, err := e.DB.Exec(ctx, `DELETE FROM media_item_crew WHERE media_item_id = $1`, mediaItemID); err != nil {
		return
	}
	for i, c := range crew {
		var id int64
		name := c.Name
		if c.Ref.ExternalID != "" {
			id, name = e.linkedPerformer(ctx, c.Ref, c.Name)
		}
		if id == 0 {
			if found, ok := scanner.PerformerByKey(ctx, e.DB, c.Name); ok {
				id = found
			}
		}
		if id == 0 {
			if err := e.DB.QueryRow(ctx, `
				INSERT INTO performer (name) VALUES ($1)
				ON CONFLICT (name) DO UPDATE SET updated_at = performer.updated_at
				RETURNING id
			`, name).Scan(&id); err != nil {
				log.Printf("namer: create crew member %q: %v", name, err)
				continue
			}
		}
		if c.Ref.ExternalID != "" {
			_ = e.linkPerformerRef(ctx, id, c.Ref)
		}
		if c.ImageURL != "" {
			_ = e.AddPerformerPhoto(ctx, id, c.Ref.Provider, c.ImageURL, false)
		}
		if _, err := e.DB.Exec(ctx, `
			INSERT INTO media_item_crew (media_item_id, performer_id, job, position) VALUES ($1, $2, $3, $4)
			ON CONFLICT DO NOTHING
		`, mediaItemID, id, c.Job, i); err != nil {
			log.Printf("namer: add crew %q to media_item %d: %v", name, mediaItemID, err)
		}
	}
}
