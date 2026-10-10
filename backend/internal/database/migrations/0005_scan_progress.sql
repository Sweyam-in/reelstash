-- Live progress for a scan so the admin disk page can show how far
-- along it is and which files it is touching, instead of only the
-- final totals written when the scan completes.
ALTER TABLE scan_job ADD COLUMN IF NOT EXISTS files_seen INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scan_job ADD COLUMN IF NOT EXISTS files_processed INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scan_job ADD COLUMN IF NOT EXISTS current_path TEXT;
ALTER TABLE scan_job ADD COLUMN IF NOT EXISTS recent_paths JSONB NOT NULL DEFAULT '[]';
ALTER TABLE scan_job ADD COLUMN IF NOT EXISTS phase TEXT NOT NULL DEFAULT 'queued';
ALTER TABLE scan_job ADD COLUMN IF NOT EXISTS walk_complete BOOLEAN NOT NULL DEFAULT FALSE;
