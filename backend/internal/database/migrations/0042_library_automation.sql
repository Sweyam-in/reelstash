-- Scan rules, scheduled scans and file watching belong to libraries instead of
-- disks. Every migration replays on startup, so each step checks whether the
-- old disk column is still there before converting it.

-- Scan rules: NULL library_id applies to every library. Patterns with a slash
-- are relative to each of the library's folders (they were relative to the
-- disk root). A disk rule becomes a rule of every library with a folder on
-- that disk; a path rule is rewritten relative to the folder it falls inside,
-- and dropped for folders it doesn't reach.
ALTER TABLE scan_rule ADD COLUMN IF NOT EXISTS library_id BIGINT REFERENCES library(id) ON DELETE CASCADE;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'scan_rule' AND column_name = 'storage_location_id') THEN
        DROP INDEX IF EXISTS scan_rule_unique;
        INSERT INTO scan_rule (library_id, pattern, created_at)
        SELECT DISTINCT ON (lr.library_id, p.rel) lr.library_id, p.rel, sr.created_at
        FROM scan_rule sr
        CROSS JOIN LATERAL (SELECT trim(both '/' from sr.pattern) AS pat) t
        JOIN library_root lr ON lr.storage_location_id = sr.storage_location_id
        CROSS JOIN LATERAL (SELECT CASE
            WHEN lr.path = '' OR strpos(t.pat, '/') = 0 THEN t.pat
            ELSE substr(t.pat, length(lr.path) + 2) END AS rel) p
        WHERE sr.storage_location_id IS NOT NULL
          AND (lr.path = '' OR strpos(t.pat, '/') = 0 OR left(t.pat, length(lr.path) + 1) = lr.path || '/')
          AND p.rel <> ''
        ORDER BY lr.library_id, p.rel, sr.created_at;
        DELETE FROM scan_rule WHERE storage_location_id IS NOT NULL;
        ALTER TABLE scan_rule DROP COLUMN storage_location_id;
    END IF;
END $$;
CREATE UNIQUE INDEX IF NOT EXISTS scan_rule_library_unique ON scan_rule (COALESCE(library_id, 0), pattern);

-- Schedules: NULL library_id scans every library. A disk schedule becomes one
-- schedule per library with a folder on that disk.
ALTER TABLE scan_schedule ADD COLUMN IF NOT EXISTS library_id BIGINT REFERENCES library(id) ON DELETE CASCADE;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'scan_schedule' AND column_name = 'storage_location_id') THEN
        INSERT INTO scan_schedule (job_kind, library_id, cron_expr, enabled, last_run_at, created_at)
        SELECT DISTINCT ON (lr.library_id, s.job_kind, s.cron_expr) s.job_kind, lr.library_id, s.cron_expr, s.enabled, s.last_run_at, s.created_at
        FROM scan_schedule s
        JOIN library_root lr ON lr.storage_location_id = s.storage_location_id
        WHERE s.storage_location_id IS NOT NULL
        ORDER BY lr.library_id, s.job_kind, s.cron_expr, s.enabled DESC;
        DELETE FROM scan_schedule WHERE storage_location_id IS NOT NULL;
        ALTER TABLE scan_schedule DROP COLUMN storage_location_id;
    END IF;
END $$;

-- File watching: a library watches its folders and scans itself shortly after
-- files change. Libraries on a watched disk keep being watched.
ALTER TABLE library ADD COLUMN IF NOT EXISTS watch_enabled BOOLEAN NOT NULL DEFAULT FALSE;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'storage_location' AND column_name = 'watch_enabled') THEN
        UPDATE library l SET watch_enabled = TRUE
        WHERE EXISTS (SELECT 1 FROM library_root lr JOIN storage_location sl ON sl.id = lr.storage_location_id
                      WHERE lr.library_id = l.id AND sl.watch_enabled);
        ALTER TABLE storage_location DROP COLUMN watch_enabled;
    END IF;
END $$;
