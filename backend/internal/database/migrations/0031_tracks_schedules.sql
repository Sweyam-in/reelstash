-- Audio and subtitle tracks found inside a media file (ffprobe), so the
-- player can offer language switching. Sidecar subtitle files are found on
-- disk when asked for and are not stored.
CREATE TABLE IF NOT EXISTS media_track (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_file_id BIGINT NOT NULL REFERENCES media_file(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('audio', 'subtitle')),
    -- Position among the streams of the same kind: ffmpeg's 0:a:N / 0:s:N.
    kind_index INTEGER NOT NULL CHECK (kind_index >= 0),
    stream_index INTEGER NOT NULL,
    codec TEXT,
    language TEXT,
    title TEXT,
    channels INTEGER,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    is_forced BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE (media_file_id, kind, kind_index)
);
-- NULL until the file's tracks were read; lets existing libraries fill in
-- lazily the first time a title is played instead of needing a rescan.
ALTER TABLE media_file ADD COLUMN IF NOT EXISTS tracks_probed_at TIMESTAMPTZ;

-- Per-user language defaults. Languages are 2-letter codes where one exists.
-- subtitle_mode: off, on (always show the preferred language) or foreign
-- (only when the chosen audio is not in the preferred subtitle language).
ALTER TABLE app_user ADD COLUMN IF NOT EXISTS preferred_audio_language TEXT;
ALTER TABLE app_user ADD COLUMN IF NOT EXISTS preferred_subtitle_language TEXT;
ALTER TABLE app_user ADD COLUMN IF NOT EXISTS subtitle_mode TEXT NOT NULL DEFAULT 'off';
DO $$ BEGIN
    ALTER TABLE app_user ADD CONSTRAINT app_user_subtitle_mode_check CHECK (subtitle_mode IN ('off', 'on', 'foreign'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- Recurring jobs. cron_expr is a 5-field cron expression in the server's
-- local time zone. A NULL storage location means every storage location.
CREATE TABLE IF NOT EXISTS scan_schedule (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    job_kind TEXT NOT NULL DEFAULT 'SCAN' CHECK (job_kind IN ('SCAN', 'DUPLICATE_SCAN')),
    storage_location_id BIGINT REFERENCES storage_location(id) ON DELETE CASCADE,
    cron_expr TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    last_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- File watching was per storage location here; 0042 moved it to libraries.
