-- Generic progress for long-running background jobs so the Jobs page can
-- render a progress bar and a "what is it doing now" line for any kind.
ALTER TABLE background_job ADD COLUMN IF NOT EXISTS progress_done INTEGER NOT NULL DEFAULT 0;
ALTER TABLE background_job ADD COLUMN IF NOT EXISTS progress_total INTEGER NOT NULL DEFAULT 0;
ALTER TABLE background_job ADD COLUMN IF NOT EXISTS progress_label TEXT;
CREATE INDEX IF NOT EXISTS background_job_status_id ON background_job (status, id DESC);
