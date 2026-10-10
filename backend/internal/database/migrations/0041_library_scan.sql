-- A scan can cover one library's folders on a disk instead of the whole disk.
-- NULL is a whole-disk scan.
ALTER TABLE scan_job ADD COLUMN IF NOT EXISTS library_id BIGINT REFERENCES library(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS scan_job_library ON scan_job (library_id, completed_at);
