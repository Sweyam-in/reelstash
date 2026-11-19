package namer

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"reelstash/backend/internal/testdb"
)

// "Keep my edits": the locked fields of a title survive a match that replaced them, the unlocked
// ones keep the match's values, and the title stays marked as hand-edited.
func TestKeptFieldsSurviveAMatch(t *testing.T) {
	pool := testdb.Shared(t)
	ctx := context.Background()
	tag := uuid.NewString()[:8]
	e := &Engine{DB: pool}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var id, mine, theirs, catMine, catTheirs, perfMine, perfTheirs int64
	must(pool.QueryRow(ctx, `INSERT INTO studio (name) VALUES ($1) RETURNING id`, "Mine "+tag).Scan(&mine))
	must(pool.QueryRow(ctx, `INSERT INTO studio (name) VALUES ($1) RETURNING id`, "Theirs "+tag).Scan(&theirs))
	must(pool.QueryRow(ctx, `INSERT INTO media_item (title, sort_title, description, studio_id, metadata_source) VALUES ($1, $1, 'my words', $2, 'MANUAL') RETURNING id`, "My title "+tag, mine).Scan(&id))
	must(pool.QueryRow(ctx, `INSERT INTO category (name, slug) VALUES ($1, $2) RETURNING id`, "Mine "+tag, "mine-"+tag).Scan(&catMine))
	must(pool.QueryRow(ctx, `INSERT INTO category (name, slug) VALUES ($1, $2) RETURNING id`, "Theirs "+tag, "theirs-"+tag).Scan(&catTheirs))
	must(pool.QueryRow(ctx, `INSERT INTO performer (name) VALUES ($1) RETURNING id`, "Mine "+tag).Scan(&perfMine))
	must(pool.QueryRow(ctx, `INSERT INTO performer (name) VALUES ($1) RETURNING id`, "Theirs "+tag).Scan(&perfTheirs))
	t.Cleanup(func() {
		bg := context.Background()
		pool.Exec(bg, `DELETE FROM media_item WHERE id = $1`, id)
		pool.Exec(bg, `DELETE FROM studio WHERE id = ANY($1)`, []int64{mine, theirs})
		pool.Exec(bg, `DELETE FROM category WHERE id = ANY($1)`, []int64{catMine, catTheirs})
		pool.Exec(bg, `DELETE FROM performer WHERE id = ANY($1)`, []int64{perfMine, perfTheirs})
	})
	_, err := pool.Exec(ctx, `INSERT INTO media_item_category (media_item_id, category_id) VALUES ($1, $2)`, id, catMine)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO media_item_performer (media_item_id, performer_id, position) VALUES ($1, $2, 0)`, id, perfMine)
	must(err)

	// Nothing locked: nothing to keep.
	if kept, err := e.snapshotKept(ctx, id); err != nil || kept != nil {
		t.Fatalf("unlocked snapshot = %+v, %v", kept, err)
	}
	must(SetLockedFields(ctx, pool, id, []string{"title", "categories", "bogus", "title"}))
	locked, err := LockedFields(ctx, pool, id)
	must(err)
	if !reflect.DeepEqual(locked, []string{"title", "categories"}) {
		t.Fatalf("locked = %v", locked)
	}
	kept, err := e.snapshotKept(ctx, id)
	must(err)

	// A match replaces everything...
	_, err = pool.Exec(ctx, `UPDATE media_item SET title = 'Provider title', description = 'their words', studio_id = $2, metadata_source = 'NFO' WHERE id = $1`, id, theirs)
	must(err)
	_, err = pool.Exec(ctx, `DELETE FROM media_item_category WHERE media_item_id = $1`, id)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO media_item_category (media_item_id, category_id) VALUES ($1, $2)`, id, catTheirs)
	must(err)

	// ...and the kept fields come back.
	must(e.restoreKept(ctx, id, kept))
	var title, description, source string
	var studio int64
	must(pool.QueryRow(ctx, `SELECT title, description, studio_id, metadata_source FROM media_item WHERE id = $1`, id).Scan(&title, &description, &studio, &source))
	if title != "My title "+tag || source != "MANUAL" {
		t.Fatalf("title %q source %q", title, source)
	}
	if description != "their words" || studio != theirs {
		t.Fatalf("unlocked fields were restored: %q %d", description, studio)
	}
	var cats []int64
	rows, err := pool.Query(ctx, `SELECT category_id FROM media_item_category WHERE media_item_id = $1`, id)
	must(err)
	for rows.Next() {
		var c int64
		must(rows.Scan(&c))
		cats = append(cats, c)
	}
	rows.Close()
	if !reflect.DeepEqual(cats, []int64{catMine}) {
		t.Fatalf("categories = %v", cats)
	}

	must(SetLockedFields(ctx, pool, id, nil))
	if locked, _ := LockedFields(ctx, pool, id); len(locked) != 0 {
		t.Fatalf("locks not cleared: %v", locked)
	}
}
