package namer

import (
	"context"
	"fmt"

	"reelstash/backend/internal/names"
	"reelstash/backend/internal/provider"
	"reelstash/backend/internal/scanner"
)

// resolveCast rewrites a record's performer names to the library's own
// performers, so applying it never creates a second copy of someone who is
// already there under another spelling. A performer already linked to the
// provider's person record is found first; otherwise an unambiguous
// normalized-name match is used. Photos and person refs follow the rename.
func (e *Engine) resolveCast(ctx context.Context, scene provider.MediaResult) provider.MediaResult {
	if len(scene.Performers) == 0 {
		return scene
	}
	out := scene
	out.Performers = make([]string, 0, len(scene.Performers))
	out.PerformerImages = map[string]string{}
	out.PerformerRefs = map[string]provider.Ref{}
	if scene.Characters != nil {
		out.Characters = map[string]string{}
	}
	seen := map[int64]bool{}
	for _, name := range scene.Performers {
		ref, hasRef := scene.PerformerRefs[name]
		local := name
		var id int64
		if hasRef {
			id, local = e.linkedPerformer(ctx, ref, name)
		}
		if id == 0 {
			if found, ok := scanner.PerformerByKey(ctx, e.DB, name); ok {
				id = found
				e.DB.QueryRow(ctx, `SELECT name FROM performer WHERE id = $1`, id).Scan(&local)
			}
		}
		if id != 0 {
			if seen[id] { // two provider names for one local performer
				continue
			}
			seen[id] = true
		}
		out.Performers = append(out.Performers, local)
		if img, ok := scene.PerformerImages[name]; ok {
			out.PerformerImages[local] = img
		}
		if hasRef {
			out.PerformerRefs[local] = ref
		}
		if role, ok := scene.Characters[name]; ok {
			out.Characters[local] = role
		}
	}
	return out
}

// linkedPerformer returns the performer linked to ref, or 0 and the given name.
func (e *Engine) linkedPerformer(ctx context.Context, ref provider.Ref, name string) (int64, string) {
	var id int64
	local := name
	err := e.DB.QueryRow(ctx, `
		SELECT p.id, p.name FROM match_link l JOIN performer p ON p.id = l.entity_id
		WHERE l.kind = 'performer' AND l.provider = $1 AND l.external_id = $2
		ORDER BY p.id LIMIT 1
	`, ref.Provider, ref.ExternalID).Scan(&id, &local)
	if err != nil {
		return 0, name
	}
	return id, local
}

// linkCast links the title's performers to the provider person records the
// applied record named, so the next title with them is matched by id. An
// existing link, a record already linked to someone else, and a record the
// user rejected for that performer are all left alone.
func (e *Engine) linkCast(ctx context.Context, mediaItemID int64, scene provider.MediaResult) {
	if len(scene.PerformerRefs) == 0 {
		return
	}
	byKey := map[string]provider.Ref{}
	for name, ref := range scene.PerformerRefs {
		if k := names.Key(name); k != "" {
			byKey[k] = ref
		}
	}
	rows, err := e.DB.Query(ctx, `
		SELECT p.id, p.name FROM media_item_performer m JOIN performer p ON p.id = m.performer_id
		WHERE m.media_item_id = $1
	`, mediaItemID)
	if err != nil {
		return
	}
	type attached struct {
		id  int64
		ref provider.Ref
	}
	var todo []attached
	for rows.Next() {
		var id int64
		var name string
		if rows.Scan(&id, &name) != nil {
			continue
		}
		if ref, ok := byKey[names.Key(name)]; ok {
			todo = append(todo, attached{id, ref})
		}
	}
	rows.Close()
	for _, a := range todo {
		if err := e.linkPerformerRef(ctx, a.id, a.ref); err != nil {
			fmt.Printf("namer: link performer %d to %s/%s: %v\n", a.id, a.ref.Provider, a.ref.ExternalID, err)
		}
	}
}

func (e *Engine) linkPerformerRef(ctx context.Context, performerID int64, ref provider.Ref) error {
	if e.RejectedPerformers(ctx, performerID)[rejectKey(ref.Provider, ref.ExternalID)] {
		return nil
	}
	tag, err := e.DB.Exec(ctx, `
		INSERT INTO match_link (kind, entity_id, provider, external_id) VALUES ('performer', $1, $2, $3)
		ON CONFLICT DO NOTHING
	`, performerID, ref.Provider, ref.ExternalID)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	return nil
}
