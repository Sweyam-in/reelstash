package housekeeping

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
)

// rows adds one finding per row of a query returning a label per record.
func (r *runner) rows(f *Finding, query string, args ...any) error {
	rows, err := r.db().Query(r.ctx, query, args...)
	if err != nil {
		return err
	}
	labels, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, l := range labels {
		f.add(l, 0)
	}
	return nil
}

// sweep deletes from table the rows matching where (or, in a dry run, just
// lists them), labelling each with label.
func (r *runner) sweep(f *Finding, table, label, where string, args ...any) error {
	t := pgx.Identifier{table}.Sanitize()
	if r.apply {
		return r.rows(f, fmt.Sprintf(`DELETE FROM %s WHERE %s RETURNING %s`, t, where, label), args...)
	}
	return r.rows(f, fmt.Sprintf(`SELECT %s FROM %s WHERE %s`, label, t, where), args...)
}

// missingArtifacts drops rows for thumbnails, previews and artwork whose file
// is gone, so they are made again (or the title falls back to a default)
// instead of showing a broken image. A folder that is unreadable, or that
// seems to have lost most of its files at once, is left alone: that looks
// like a missing volume rather than files cleaned away.
func missingArtifacts(r *runner, f *Finding) error {
	managed := r.s.managedDirs()
	for _, a := range artifacts {
		dir := managed[a.role]
		if err := readableDir(dir); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				f.skip(fmt.Sprintf("%s folder unreadable: %v", a.label, err))
			} else if dir != "" {
				// Never created: fine when the table is empty too.
				var n int
				_ = r.db().QueryRow(r.ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s`, pgx.Identifier{a.table}.Sanitize())).Scan(&n)
				if n > 0 {
					f.skip(fmt.Sprintf("%s folder %s does not exist", a.label, dir))
				}
			}
			continue
		}
		keys, err := r.keys(a.table)
		if err != nil {
			return err
		}
		var missing []string
		for k := range keys {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(k))); errors.Is(err, fs.ErrNotExist) {
				missing = append(missing, k)
			}
		}
		if len(missing) > 20 && len(missing)*2 > len(keys) {
			f.skip(fmt.Sprintf("%d of %d %s files are missing; check that the folder is mounted", len(missing), len(keys), a.label))
			continue
		}
		if len(missing) == 0 {
			continue
		}
		label := fmt.Sprintf(`%s || ' ' || storage_key`, quoteLiteral(a.label))
		if err := r.sweep(f, a.table, label, `storage_key = ANY($1)`, missing); err != nil {
			return err
		}
	}
	return nil
}

func quoteLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// unusedArtwork removes artwork (posters, logos, photos, backdrops) that no
// title, person, studio or collection uses any more, with its file. The
// referencing columns are read from the schema's foreign keys, so a new one
// is covered without changing this.
func unusedArtwork(r *runner, f *Finding) error {
	rows, err := r.db().Query(r.ctx, `
		SELECT c.conrelid::regclass::text, a.attname
		FROM pg_constraint c
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = ANY (c.conkey)
		WHERE c.contype = 'f' AND c.confrelid = 'artwork'::regclass`)
	if err != nil {
		return err
	}
	var refs []string
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			rows.Close()
			return err
		}
		refs = append(refs, fmt.Sprintf(`NOT EXISTS (SELECT 1 FROM %s x WHERE x.%s = ar.id)`, table, pgx.Identifier{column}.Sanitize()))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(refs) == 0 {
		return errors.New("found no references to artwork in the schema")
	}
	// A day's grace: artwork is inserted before the row that uses it.
	where := `ar.created_at < now() - interval '1 day' AND ` + strings.Join(refs, " AND ")
	q := `SELECT ar.id, ar.storage_key FROM artwork ar WHERE ` + where
	if r.apply {
		q = `DELETE FROM artwork ar WHERE ` + where + ` RETURNING ar.id, ar.storage_key`
	}
	res, err := r.db().Query(r.ctx, q)
	if err != nil {
		return err
	}
	type art struct {
		id  int64
		key string
	}
	list, err := pgx.CollectRows(res, func(row pgx.CollectableRow) (art, error) {
		var a art
		return a, row.Scan(&a.id, &a.key)
	})
	if err != nil {
		return err
	}
	dir := r.s.managedDirs()["posters"]
	for _, a := range list {
		var size int64
		p := ""
		if dir != "" && a.key != "" {
			p = filepath.Join(dir, filepath.FromSlash(a.key))
			if info, err := os.Stat(p); err == nil {
				size = info.Size()
			}
		}
		f.add("artwork "+a.key, size)
		if p != "" {
			r.remove(f, p, false)
		}
	}
	return nil
}

// finishedJobs clears background jobs that finished more than JobDays ago,
// and old scan records (keeping each disk's and library's latest, which
// other checks rely on).
func finishedJobs(r *runner, f *Finding) error {
	cutoff := r.days(r.settings.JobDays)
	if err := r.sweep(f, "background_job", `kind || ' #' || id`,
		`status IN ('COMPLETED','FAILED','CANCELLED') AND COALESCE(finished_at, created_at) < $1`, cutoff); err != nil {
		return err
	}
	return r.sweep(f, "scan_job", `'scan record #' || id`, `
		completed_at IS NOT NULL AND completed_at < $1
		AND id NOT IN (SELECT DISTINCT ON (storage_location_id, library_id) id FROM scan_job
		               WHERE completed_at IS NOT NULL ORDER BY storage_location_id, library_id, completed_at DESC)`, cutoff)
}

func expiredSessions(r *runner, f *Finding) error {
	return r.sweep(f, "web_session", `'session of user #' || user_id`, `expires_at < now()`)
}

func oldNotifications(r *runner, f *Finding) error {
	return r.sweep(f, "notification", `title`, `created_at < $1`, r.days(r.settings.NotificationDays))
}

func capacityHistory(r *runner, f *Finding) error {
	return r.sweep(f, "storage_capacity_sample", `'disk #' || storage_location_id || ' at ' || to_char(sampled_at, 'YYYY-MM-DD HH24:MI')`,
		`sampled_at < $1`, r.days(r.settings.CapacityDays))
}

// orphanLinks removes provider links (which have no foreign key, being shared
// by titles, people and studios) whose title, person or studio is gone.
func orphanLinks(r *runner, f *Finding) error {
	return r.sweep(f, "match_link", `kind || ' #' || entity_id || ' → ' || provider`, `
		(kind = 'media' AND NOT EXISTS (SELECT 1 FROM media_item x WHERE x.id = entity_id))
		OR (kind = 'performer' AND NOT EXISTS (SELECT 1 FROM performer x WHERE x.id = entity_id))
		OR (kind = 'studio' AND NOT EXISTS (SELECT 1 FROM studio x WHERE x.id = entity_id))`)
}

// emptyTitles removes titles with no file at all (not even a missing one).
func emptyTitles(r *runner, f *Finding) error {
	return r.sweep(f, "media_item", `title`, `
		created_at < now() - interval '1 day'
		AND NOT EXISTS (SELECT 1 FROM media_file mf WHERE mf.media_item_id = media_item.id)`)
}

// pluginSettings removes saved settings of plugins whose folder is gone.
func pluginSettings(r *runner, f *Finding) error {
	root := r.s.managedDirs()["plugins"]
	rows, err := r.db().Query(r.ctx, `SELECT key FROM app_setting WHERE key LIKE 'plugin\_config\_%'`)
	if err != nil {
		return err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(keys) == 0 {
		return err
	}
	// An unmounted plugin folder would make every plugin look removed.
	if err := readableDir(root); err != nil {
		f.skip(fmt.Sprintf("the plugin folder %s is not readable", root))
		return nil
	}
	var gone []string
	for _, k := range keys {
		id := strings.TrimPrefix(k, "plugin_config_")
		if id == "" || strings.ContainsAny(id, `/\`) || id == "." || id == ".." {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, id)); errors.Is(err, fs.ErrNotExist) {
			gone = append(gone, k)
		}
	}
	if len(gone) == 0 {
		return nil
	}
	return r.sweep(f, "app_setting", `'settings of ' || substr(key, 15)`, `key = ANY($1)`, gone)
}

// vacuum reclaims the space deleted rows leave and refreshes the planner's
// statistics. It never locks tables against reads or writes.
func vacuum(r *runner, f *Finding) error {
	var dead int64
	var tables int
	if err := r.db().QueryRow(r.ctx, `SELECT COALESCE(SUM(n_dead_tup), 0), COUNT(*) FROM pg_stat_user_tables`).Scan(&dead, &tables); err != nil {
		return err
	}
	f.Samples = append(f.Samples, fmt.Sprintf("%d dead rows across %d tables", dead, tables))
	if r.apply {
		_, err := r.db().Exec(r.ctx, `VACUUM (ANALYZE)`)
		return err
	}
	return nil
}
