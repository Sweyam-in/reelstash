-- ReelStash PostgreSQL schema and upgrade path.
-- The sections below retain the original migration order and statements.
-- This lets new databases and existing installations reach the same schema
-- without discarding existing data, columns, tables, or final constraints.
-- Keep additions idempotent: this file is replayed on every API startup.

-- ===== 0001_init.sql =====
-- ReelStash catalog schema. Apply to a dedicated PostgreSQL database.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS app_user (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    auth_subject TEXT NOT NULL UNIQUE,
    email TEXT,
    display_name TEXT,
    avatar_url TEXT,
    role TEXT NOT NULL CHECK (role IN ('ADMIN', 'USER')),
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS artwork (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('POSTER', 'BACKDROP', 'PROFILE', 'LOGO')),
    storage_key TEXT NOT NULL UNIQUE,
    mime_type TEXT NOT NULL,
    width INTEGER,
    height INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS performer (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    display_name TEXT,
    profile_image_id BIGINT REFERENCES artwork(id) ON DELETE SET NULL,
    notes TEXT,
    favorite BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS performer_alias (
    performer_id BIGINT NOT NULL REFERENCES performer(id) ON DELETE CASCADE,
    alias TEXT NOT NULL,
    PRIMARY KEY (performer_id, alias)
);

CREATE TABLE IF NOT EXISTS studio (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    logo_image_id BIGINT REFERENCES artwork(id) ON DELETE SET NULL,
    description TEXT,
    website TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS studio_alias (
    studio_id BIGINT NOT NULL REFERENCES studio(id) ON DELETE CASCADE,
    alias TEXT NOT NULL,
    PRIMARY KEY (studio_id, alias)
);
CREATE TABLE IF NOT EXISTS series (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    studio_id BIGINT REFERENCES studio(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    description TEXT,
    poster_image_id BIGINT REFERENCES artwork(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (studio_id, name)
);

CREATE TABLE IF NOT EXISTS storage_location (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    type TEXT NOT NULL CHECK (type IN ('LOCAL', 'EXTERNAL', 'NAS', 'NETWORK_SHARE', 'ARCHIVE')),
    mount_path TEXT NOT NULL UNIQUE,
    total_capacity BIGINT CHECK (total_capacity >= 0),
    used_capacity BIGINT CHECK (used_capacity >= 0),
    available_capacity BIGINT CHECK (available_capacity >= 0),
    online BOOLEAN NOT NULL DEFAULT FALSE,
    last_seen_at TIMESTAMPTZ,
    last_scan_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS media_item (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title TEXT NOT NULL,
    original_title TEXT,
    sort_title TEXT,
    studio_id BIGINT REFERENCES studio(id) ON DELETE SET NULL,
    series_id BIGINT REFERENCES series(id) ON DELETE SET NULL,
    description TEXT,
    release_date DATE,
    production_year INTEGER,
    duration_seconds NUMERIC(12,3),
    favorite BOOLEAN NOT NULL DEFAULT FALSE,
    rating SMALLINT CHECK (rating BETWEEN 0 AND 10),
    notes TEXT,
    watched BOOLEAN NOT NULL DEFAULT FALSE,
    watch_count INTEGER NOT NULL DEFAULT 0 CHECK (watch_count >= 0),
    last_watched_at TIMESTAMPTZ,
    date_added TIMESTAMPTZ NOT NULL DEFAULT now(),
    poster_image_id BIGINT REFERENCES artwork(id) ON DELETE SET NULL,
    backdrop_image_id BIGINT REFERENCES artwork(id) ON DELETE SET NULL,
    metadata_source TEXT NOT NULL DEFAULT 'FILENAME' CHECK (metadata_source IN ('MANUAL','NFO','FILENAME','FOLDER','INFERRED')),
    protected_fields TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS media_item_performer (
    media_item_id BIGINT NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    performer_id BIGINT NOT NULL REFERENCES performer(id) ON DELETE CASCADE,
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (media_item_id, performer_id)
);

CREATE TABLE IF NOT EXISTS category (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    parent_id BIGINT REFERENCES category(id) ON DELETE SET NULL
);
CREATE TABLE IF NOT EXISTS media_item_category (
    media_item_id BIGINT NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    category_id BIGINT NOT NULL REFERENCES category(id) ON DELETE CASCADE,
    PRIMARY KEY (media_item_id, category_id)
);
CREATE TABLE IF NOT EXISTS tag (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    optional_color TEXT
);
CREATE TABLE IF NOT EXISTS media_item_tag (
    media_item_id BIGINT NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    tag_id BIGINT NOT NULL REFERENCES tag(id) ON DELETE CASCADE,
    PRIMARY KEY (media_item_id, tag_id)
);

CREATE TABLE IF NOT EXISTS media_file (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id BIGINT NOT NULL REFERENCES media_item(id) ON DELETE RESTRICT,
    storage_location_id BIGINT NOT NULL REFERENCES storage_location(id) ON DELETE RESTRICT,
    relative_path TEXT NOT NULL CHECK (relative_path <> '' AND relative_path !~ '^/'),
    file_name TEXT NOT NULL,
    extension TEXT NOT NULL,
    file_size BIGINT NOT NULL CHECK (file_size >= 0),
    content_hash TEXT,
    partial_hash TEXT,
    media_fingerprint TEXT,
    duration_seconds NUMERIC(12,3),
    width INTEGER,
    height INTEGER,
    frame_rate NUMERIC(10,3),
    video_codec TEXT,
    audio_codec TEXT,
    container TEXT,
    video_bitrate BIGINT,
    audio_bitrate BIGINT,
    overall_bitrate BIGINT,
    created_time TIMESTAMPTZ,
    modified_time TIMESTAMPTZ,
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_scanned_at TIMESTAMPTZ,
    available BOOLEAN NOT NULL DEFAULT TRUE,
    missing_since TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (storage_location_id, relative_path)
);

CREATE TABLE IF NOT EXISTS thumbnail (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id BIGINT REFERENCES media_item(id) ON DELETE CASCADE,
    media_file_id BIGINT REFERENCES media_file(id) ON DELETE CASCADE,
    storage_key TEXT NOT NULL UNIQUE,
    timestamp_seconds NUMERIC(12,3),
    width INTEGER,
    height INTEGER,
    preferred BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (media_item_id IS NOT NULL OR media_file_id IS NOT NULL)
);
CREATE TABLE IF NOT EXISTS nfo_metadata (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_file_id BIGINT NOT NULL UNIQUE REFERENCES media_file(id) ON DELETE CASCADE,
    relative_path TEXT NOT NULL,
    parsed_metadata JSONB NOT NULL DEFAULT '{}',
    content_hash TEXT,
    imported_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS collection (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    kind TEXT NOT NULL CHECK (kind IN ('MANUAL','SMART')),
    rules JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS collection_media_item (
    collection_id BIGINT NOT NULL REFERENCES collection(id) ON DELETE CASCADE,
    media_item_id BIGINT NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (collection_id, media_item_id)
);
CREATE TABLE IF NOT EXISTS playback_state (
    user_id BIGINT NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    media_item_id BIGINT NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    position_seconds NUMERIC(12,3) NOT NULL DEFAULT 0,
    completed BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, media_item_id)
);
CREATE TABLE IF NOT EXISTS duplicate_group (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('EXACT','PROBABLE','VERSION')),
    confidence NUMERIC(4,3) CHECK (confidence BETWEEN 0 AND 1),
    dismissed BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS duplicate_group_file (
    duplicate_group_id BIGINT NOT NULL REFERENCES duplicate_group(id) ON DELETE CASCADE,
    media_file_id BIGINT NOT NULL REFERENCES media_file(id) ON DELETE CASCADE,
    PRIMARY KEY (duplicate_group_id, media_file_id)
);
CREATE TABLE IF NOT EXISTS background_job (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('PENDING','RUNNING','COMPLETED','FAILED','CANCELLED')),
    payload JSONB NOT NULL DEFAULT '{}',
    result JSONB,
    error TEXT,
    attempts INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);
CREATE TABLE IF NOT EXISTS scan_job (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    storage_location_id BIGINT NOT NULL REFERENCES storage_location(id) ON DELETE RESTRICT,
    background_job_id BIGINT REFERENCES background_job(id) ON DELETE SET NULL,
    discovered_count INTEGER NOT NULL DEFAULT 0,
    changed_count INTEGER NOT NULL DEFAULT 0,
    missing_count INTEGER NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);
CREATE TABLE IF NOT EXISTS saved_filter (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    rules JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, name)
);

CREATE INDEX IF NOT EXISTS media_item_title_fts ON media_item USING GIN (to_tsvector('simple', coalesce(title,'') || ' ' || coalesce(description,'')));
CREATE INDEX IF NOT EXISTS media_item_title_trgm ON media_item USING GIN (title gin_trgm_ops);
CREATE INDEX IF NOT EXISTS performer_name_trgm ON performer USING GIN (name gin_trgm_ops);
CREATE INDEX IF NOT EXISTS performer_alias_trgm ON performer_alias USING GIN (alias gin_trgm_ops);
CREATE INDEX IF NOT EXISTS studio_name_trgm ON studio USING GIN (name gin_trgm_ops);
CREATE INDEX IF NOT EXISTS studio_alias_trgm ON studio_alias USING GIN (alias gin_trgm_ops);
CREATE INDEX IF NOT EXISTS series_name_trgm ON series USING GIN (name gin_trgm_ops);
CREATE INDEX IF NOT EXISTS media_file_name_trgm ON media_file USING GIN (file_name gin_trgm_ops);
CREATE INDEX IF NOT EXISTS media_file_content_hash ON media_file (content_hash) WHERE content_hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS media_file_partial_hash ON media_file (partial_hash, file_size) WHERE partial_hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS media_file_item ON media_file (media_item_id);
CREATE INDEX IF NOT EXISTS media_item_performer_reverse ON media_item_performer (performer_id, media_item_id);
CREATE INDEX IF NOT EXISTS media_item_category_reverse ON media_item_category (category_id, media_item_id);
CREATE INDEX IF NOT EXISTS background_job_pending ON background_job (created_at) WHERE status = 'PENDING';

-- ===== 0002_sessions.sql =====
-- Server-side sessions for plugin-authenticated (and emergency local) users.
CREATE TABLE IF NOT EXISTS web_session (
    id TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS web_session_expires ON web_session (expires_at);

-- ===== 0003_organizer.sql =====
-- Keyword-based organizer rules (Admin > Organizer > Keywords), distinct
-- from media categories/tags: these move files by filename match, they
-- don't describe a MediaItem.
CREATE TABLE IF NOT EXISTS organizer_keyword (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    keywords TEXT[] NOT NULL DEFAULT '{}',
    target_dir TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ===== 0004_settings.sql =====
-- Runtime-editable settings managed from Admin > Settings, distinct from
-- process env vars: these take effect immediately without a redeploy.
CREATE TABLE IF NOT EXISTS app_setting (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ===== 0005_scan_progress.sql =====
-- Live progress for a scan so the admin disk page can show how far
-- along it is and which files it is touching, instead of only the
-- final totals written when the scan completes.
ALTER TABLE scan_job ADD COLUMN IF NOT EXISTS files_seen INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scan_job ADD COLUMN IF NOT EXISTS files_processed INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scan_job ADD COLUMN IF NOT EXISTS current_path TEXT;
ALTER TABLE scan_job ADD COLUMN IF NOT EXISTS recent_paths JSONB NOT NULL DEFAULT '[]';
ALTER TABLE scan_job ADD COLUMN IF NOT EXISTS phase TEXT NOT NULL DEFAULT 'queued';
ALTER TABLE scan_job ADD COLUMN IF NOT EXISTS walk_complete BOOLEAN NOT NULL DEFAULT FALSE;

-- ===== 0006_performer_match.sql =====
-- Performer match state and the metadata captured when a match is stored.
ALTER TABLE performer ADD COLUMN IF NOT EXISTS match_status TEXT NOT NULL DEFAULT 'unmatched';
ALTER TABLE performer ADD COLUMN IF NOT EXISTS remote_metadata JSONB;
ALTER TABLE performer ADD COLUMN IF NOT EXISTS matched_at TIMESTAMPTZ;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'performer_match_status_check') THEN
    ALTER TABLE performer ADD CONSTRAINT performer_match_status_check
      CHECK (match_status IN ('unmatched', 'matched', 'no_match', 'ambiguous'));
  END IF;
END $$;

-- ===== 0007_studio_match.sql =====
-- Studio match state and the logo/metadata captured when a match is stored.
ALTER TABLE studio ADD COLUMN IF NOT EXISTS match_status TEXT NOT NULL DEFAULT 'unmatched';
ALTER TABLE studio ADD COLUMN IF NOT EXISTS remote_metadata JSONB;
ALTER TABLE studio ADD COLUMN IF NOT EXISTS matched_at TIMESTAMPTZ;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'studio_match_status_check') THEN
    ALTER TABLE studio ADD CONSTRAINT studio_match_status_check
      CHECK (match_status IN ('unmatched', 'matched', 'no_match', 'ambiguous'));
  END IF;
END $$;

-- ===== 0008_job_progress.sql =====
-- Generic progress for long-running background jobs so the Jobs page can
-- render a progress bar and a "what is it doing now" line for any kind.
ALTER TABLE background_job ADD COLUMN IF NOT EXISTS progress_done INTEGER NOT NULL DEFAULT 0;
ALTER TABLE background_job ADD COLUMN IF NOT EXISTS progress_total INTEGER NOT NULL DEFAULT 0;
ALTER TABLE background_job ADD COLUMN IF NOT EXISTS progress_label TEXT;
CREATE INDEX IF NOT EXISTS background_job_status_id ON background_job (status, id DESC);

-- ===== 0009_performer_distinct.sql =====
-- Pairs of performers the user confirmed are different people, so they stop
-- showing up as possible duplicates.
CREATE TABLE IF NOT EXISTS performer_distinct (
    a_id BIGINT NOT NULL REFERENCES performer(id) ON DELETE CASCADE,
    b_id BIGINT NOT NULL REFERENCES performer(id) ON DELETE CASCADE,
    PRIMARY KEY (a_id, b_id),
    CHECK (a_id < b_id)
);

-- ===== 0010_watch_history.sql =====
-- Per-play watch history. One row per viewing session so play counts,
-- watch time and per-performer/studio analytics can be derived.
CREATE TABLE IF NOT EXISTS watch_session (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    media_item_id BIGINT NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_activity_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    watched_seconds NUMERIC(12,3) NOT NULL DEFAULT 0,
    max_position_seconds NUMERIC(12,3) NOT NULL DEFAULT 0,
    duration_seconds NUMERIC(12,3),
    completed BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX IF NOT EXISTS watch_session_user_started ON watch_session (user_id, started_at DESC);
CREATE INDEX IF NOT EXISTS watch_session_user_item ON watch_session (user_id, media_item_id);
CREATE INDEX IF NOT EXISTS watch_session_item ON watch_session (media_item_id);

-- ===== 0011_rejected_matches.sql =====
-- "Wrong match" memory: a provider record the user rejected for a performer or
-- a title, so automatic jobs never propose or apply it again.
ALTER TABLE performer DROP CONSTRAINT IF EXISTS performer_match_status_check;
ALTER TABLE performer ADD CONSTRAINT performer_match_status_check
  CHECK (match_status IN ('unmatched', 'matched', 'no_match', 'ambiguous', 'rejected'));

-- Earlier installations named this column porndb_id. Rename it in place so
-- rejected matches survive the move to provider-neutral external IDs.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'performer_rejected_match' AND column_name = 'porndb_id')
     AND NOT EXISTS (SELECT 1 FROM information_schema.columns
                     WHERE table_schema = 'public' AND table_name = 'performer_rejected_match' AND column_name = 'external_id') THEN
    ALTER TABLE performer_rejected_match RENAME COLUMN porndb_id TO external_id;
  END IF;
END $$;
CREATE TABLE IF NOT EXISTS performer_rejected_match (
    performer_id BIGINT NOT NULL REFERENCES performer(id) ON DELETE CASCADE,
    external_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (performer_id, external_id)
);

ALTER TABLE media_item ADD COLUMN IF NOT EXISTS match_status TEXT NOT NULL DEFAULT 'unmatched';
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'media_item_match_status_check') THEN
    ALTER TABLE media_item ADD CONSTRAINT media_item_match_status_check
      CHECK (match_status IN ('unmatched', 'matched', 'rejected'));
  END IF;
END $$;

CREATE TABLE IF NOT EXISTS media_rejected_match (
    media_item_id BIGINT NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    scene_uuid TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (media_item_id, scene_uuid)
);

-- ===== 0012_custom_artwork.sql =====
-- Images the user set by hand. Automatic jobs (provider matches, syncs) must
-- never replace or remove an artwork row flagged custom.
ALTER TABLE artwork ADD COLUMN IF NOT EXISTS custom BOOLEAN NOT NULL DEFAULT false;

-- ===== 0013_app_lock.sql =====
-- Optional per-user app-lock PIN. The PIN is stored only as a salted PBKDF2
-- hash; failed attempts are tracked here so brute-forcing a 6-digit PIN is
-- throttled server-side regardless of client behavior.
CREATE TABLE IF NOT EXISTS app_lock (
    user_id BIGINT PRIMARY KEY REFERENCES app_user(id) ON DELETE CASCADE,
    pin_hash TEXT NOT NULL,
    idle_minutes INTEGER NOT NULL DEFAULT 30,
    failed_attempts INTEGER NOT NULL DEFAULT 0,
    locked_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ===== 0014_collection_pin.sql =====
-- Playlists can be pinned so they sort to the top of the Playlists page.
ALTER TABLE collection ADD COLUMN IF NOT EXISTS pinned BOOLEAN NOT NULL DEFAULT FALSE;

-- ===== 0015_player_features.sql =====
-- Scene markers, scrub-preview sprites, hover preview clips, watch later,
-- notifications and webhooks.

-- Per-user timestamped markers / bookmarks inside a title.
CREATE TABLE IF NOT EXISTS media_marker (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id BIGINT NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    seconds NUMERIC(12,3) NOT NULL CHECK (seconds >= 0),
    title TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS media_marker_item_user ON media_marker (media_item_id, user_id, seconds);

-- One tiled JPEG per file: a grid of small frames sampled every interval_seconds.
CREATE TABLE IF NOT EXISTS scrub_sprite (
    media_file_id BIGINT PRIMARY KEY REFERENCES media_file(id) ON DELETE CASCADE,
    storage_key TEXT NOT NULL UNIQUE,
    interval_seconds NUMERIC(8,3) NOT NULL,
    tile_width INTEGER NOT NULL,
    tile_height INTEGER NOT NULL,
    columns INTEGER NOT NULL,
    tile_count INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A short silent clip stitched from a few moments of the file, played on card hover.
CREATE TABLE IF NOT EXISTS preview_clip (
    media_file_id BIGINT PRIMARY KEY REFERENCES media_file(id) ON DELETE CASCADE,
    storage_key TEXT NOT NULL UNIQUE,
    duration_seconds NUMERIC(8,3),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS watch_later (
    user_id BIGINT NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    media_item_id BIGINT NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, media_item_id)
);
CREATE INDEX IF NOT EXISTS watch_later_user_added ON watch_later (user_id, added_at DESC);

-- Notifications are shared rows; each user only remembers how far they have read.
CREATE TABLE IF NOT EXISTS notification (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT,
    link TEXT,
    admin_only BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS notification_state (
    user_id BIGINT PRIMARY KEY REFERENCES app_user(id) ON DELETE CASCADE,
    last_read_id BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS webhook (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    secret TEXT,
    events TEXT[] NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    last_status INTEGER,
    last_error TEXT,
    last_delivery_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ===== 0016_match_links.sql =====
-- Provider-agnostic match links: a title, performer or studio can be linked to
-- one record at each metadata provider (TMDB, plugins, ...).
CREATE TABLE IF NOT EXISTS match_link (
    kind TEXT NOT NULL CHECK (kind IN ('media', 'performer', 'studio')),
    entity_id BIGINT NOT NULL,
    provider TEXT NOT NULL,
    external_id TEXT NOT NULL,
    matched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (kind, entity_id, provider)
);
CREATE INDEX IF NOT EXISTS match_link_external ON match_link (provider, external_id);


-- Links are polymorphic (no FK), so clean them up when the entity goes away.
CREATE OR REPLACE FUNCTION match_link_cleanup() RETURNS trigger AS $$
BEGIN
    DELETE FROM match_link WHERE kind = TG_ARGV[0] AND entity_id = OLD.id;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS media_item_match_link_cleanup ON media_item;
CREATE TRIGGER media_item_match_link_cleanup AFTER DELETE ON media_item
    FOR EACH ROW EXECUTE FUNCTION match_link_cleanup('media');
DROP TRIGGER IF EXISTS performer_match_link_cleanup ON performer;
CREATE TRIGGER performer_match_link_cleanup AFTER DELETE ON performer
    FOR EACH ROW EXECUTE FUNCTION match_link_cleanup('performer');
DROP TRIGGER IF EXISTS studio_match_link_cleanup ON studio;
CREATE TRIGGER studio_match_link_cleanup AFTER DELETE ON studio
    FOR EACH ROW EXECUTE FUNCTION match_link_cleanup('studio');

-- Rejected ("wrong match") memory is per provider.
ALTER TABLE media_rejected_match ADD COLUMN IF NOT EXISTS provider TEXT NOT NULL DEFAULT '';
ALTER TABLE media_rejected_match ALTER COLUMN provider DROP DEFAULT;
ALTER TABLE media_rejected_match DROP CONSTRAINT IF EXISTS media_rejected_match_pkey;
ALTER TABLE media_rejected_match ADD PRIMARY KEY (media_item_id, provider, scene_uuid);

-- Per-link provider metadata (what that provider said about the record), so
-- unlinking one provider can fall back to what another one supplied.
ALTER TABLE match_link ADD COLUMN IF NOT EXISTS remote_metadata JSONB;

-- One provider record can be linked to only one performer/studio. (Titles are
-- excluded: several files may legitimately share a scene.)
CREATE UNIQUE INDEX IF NOT EXISTS match_link_person_studio_unique
    ON match_link (kind, provider, external_id) WHERE kind IN ('performer', 'studio');

-- Wrong-match memory for performers is per provider too.
ALTER TABLE performer_rejected_match ADD COLUMN IF NOT EXISTS provider TEXT NOT NULL DEFAULT '';
ALTER TABLE performer_rejected_match ALTER COLUMN provider DROP DEFAULT;
ALTER TABLE performer_rejected_match DROP CONSTRAINT IF EXISTS performer_rejected_match_pkey;
ALTER TABLE performer_rejected_match ADD PRIMARY KEY (performer_id, provider, external_id);

-- ===== 0017_user_ratings.sql =====
-- Ratings are per user (0-10 scale, like the old column). media_item.rating is
-- kept as a cached, rounded average of these rows so filters and the
-- recommender keep reading a single column.
CREATE TABLE IF NOT EXISTS media_item_rating (
    user_id BIGINT NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    media_item_id BIGINT NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    rating SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 10),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, media_item_id)
);
CREATE INDEX IF NOT EXISTS idx_media_item_rating_item ON media_item_rating(media_item_id);

ALTER TABLE media_item ADD COLUMN IF NOT EXISTS rating_count INTEGER NOT NULL DEFAULT 0;

-- Attribute pre-existing single ratings to the first admin so they survive.
INSERT INTO media_item_rating (user_id, media_item_id, rating)
SELECT (SELECT id FROM app_user WHERE role = 'ADMIN' ORDER BY id LIMIT 1), mi.id, mi.rating
FROM media_item mi
WHERE mi.rating BETWEEN 1 AND 10
  AND EXISTS (SELECT 1 FROM app_user WHERE role = 'ADMIN')
ON CONFLICT DO NOTHING;

UPDATE media_item mi SET rating_count = 1
WHERE EXISTS (SELECT 1 FROM media_item_rating r WHERE r.media_item_id = mi.id);

-- ===== 0018_performer_photos.sql =====
-- A performer can keep several photos: one from each metadata provider plus
-- any the user uploads. performer.profile_image_id stays the one displayed, so
-- every existing list/detail query keeps working.
CREATE TABLE IF NOT EXISTS performer_photo (
    performer_id BIGINT NOT NULL REFERENCES performer(id) ON DELETE CASCADE,
    artwork_id BIGINT NOT NULL REFERENCES artwork(id) ON DELETE CASCADE,
    -- 'upload', a provider id ('tmdb', ...), or 'provider' when unknown.
    source TEXT NOT NULL,
    -- The provider image URL, so the same photo is never downloaded twice.
    source_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (performer_id, artwork_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS performer_photo_source_url
    ON performer_photo (performer_id, source_url) WHERE source_url IS NOT NULL;

-- True once the user picked (or uploaded) the displayed photo; automatic
-- matches then add photos to the gallery without switching to them.
ALTER TABLE performer ADD COLUMN IF NOT EXISTS photo_chosen BOOLEAN NOT NULL DEFAULT false;

INSERT INTO performer_photo (performer_id, artwork_id, source)
SELECT p.id, p.profile_image_id, CASE WHEN ar.custom THEN 'upload' ELSE 'provider' END
FROM performer p JOIN artwork ar ON ar.id = p.profile_image_id
ON CONFLICT DO NOTHING;

UPDATE performer p SET photo_chosen = true
FROM artwork ar WHERE ar.id = p.profile_image_id AND ar.custom AND NOT p.photo_chosen;

-- ===== 0019_performer_field_sources.sql =====
-- Which provider each performer detail is taken from, e.g.
-- {"bio": "a-plugin", "height": "tmdb"}. A field that is not listed comes from
-- the most recent match, with gaps filled from the other providers.
ALTER TABLE performer ADD COLUMN IF NOT EXISTS field_sources JSONB NOT NULL DEFAULT '{}';

-- ===== 0020_media_uid.sql =====
-- A stable identity for each title that lives outside the database too: the
-- scanner writes it into every video's NFO as <uniqueid type="reelstash">, so a
-- file keeps its title (and watch history, ratings, links) through renames,
-- moves, remuxes and a rebuilt database.
ALTER TABLE media_item ADD COLUMN IF NOT EXISTS uid UUID NOT NULL DEFAULT gen_random_uuid();
CREATE UNIQUE INDEX IF NOT EXISTS media_item_uid ON media_item (uid);

-- The uid last confirmed in this file's NFO. When it differs from the title's
-- uid (or is NULL) the next scan writes/updates the NFO.
ALTER TABLE media_file ADD COLUMN IF NOT EXISTS nfo_uid UUID;

-- ===== 0021_organizer_rules.sql =====
-- Metadata-driven organizer rules (Admin > Organizer). Each rule sends the
-- videos of one studio or one performer to categorized/<folder>/; the rule
-- list's position is its precedence (lowest position wins when a video
-- matches several rules). Videos matching no rule go to uncategorized/.
-- Replaces the filename-keyword rules in organizer_keyword, which are no
-- longer read (the table is kept so nothing the user entered is lost).
CREATE TABLE IF NOT EXISTS organizer_rule (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    studio_id BIGINT UNIQUE REFERENCES studio(id) ON DELETE CASCADE,
    performer_id BIGINT UNIQUE REFERENCES performer(id) ON DELETE CASCADE,
    folder_name TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((studio_id IS NULL) <> (performer_id IS NULL))
);

CREATE INDEX IF NOT EXISTS organizer_rule_position_idx ON organizer_rule (position);

-- ===== 0022_series_parts.sql =====
-- Series hold several titles in order (the scenes of a movie, the episodes of
-- a show, the films of a collection). series_position is a title's number in
-- its series; titles without one sort after the numbered ones.
ALTER TABLE media_item ADD COLUMN IF NOT EXISTS series_position INTEGER CHECK (series_position >= 0);
CREATE INDEX IF NOT EXISTS media_item_series_position_idx ON media_item (series_id, series_position) WHERE series_id IS NOT NULL;

ALTER TABLE series ADD COLUMN IF NOT EXISTS release_date DATE;

-- A series can be linked to one record at each metadata provider, like titles,
-- performers and studios. The link's remote_metadata keeps that record's parts
-- (scenes or episodes) so they can be paired with the series' videos.
ALTER TABLE match_link DROP CONSTRAINT IF EXISTS match_link_kind_check;
ALTER TABLE match_link ADD CONSTRAINT match_link_kind_check CHECK (kind IN ('media', 'performer', 'studio', 'series'));

DROP TRIGGER IF EXISTS series_match_link_cleanup ON series;
CREATE TRIGGER series_match_link_cleanup AFTER DELETE ON series
    FOR EACH ROW EXECUTE FUNCTION match_link_cleanup('series');

-- ===== 0023_media_file_original_name.sql =====
-- The file name a video had before ReelStash first renamed it after a metadata
-- match, so a wrong match can be undone without losing it.
ALTER TABLE media_file ADD COLUMN IF NOT EXISTS original_file_name TEXT;

-- ===== 0024_media_match_suggestion.sql =====
-- Candidates a metadata provider suggested for a title that automatic matching
-- did not apply. They wait here for the Review Queue's Matches tab.
CREATE TABLE IF NOT EXISTS media_match_suggestion (
    media_item_id BIGINT PRIMARY KEY REFERENCES media_item(id) ON DELETE CASCADE,
    candidates    JSONB NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ===== 0025_storage_insights.sql =====
-- Hourly disk capacity readings, for the storage usage-over-time charts.
CREATE TABLE IF NOT EXISTS storage_capacity_sample (
    storage_location_id BIGINT NOT NULL REFERENCES storage_location(id) ON DELETE CASCADE,
    sampled_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    total_bytes         BIGINT NOT NULL,
    used_bytes          BIGINT NOT NULL,
    available_bytes     BIGINT NOT NULL,
    PRIMARY KEY (storage_location_id, sampled_at)
);

-- Paths a scan skips. (0042 moves rules from disks to libraries.)
-- Files already indexed under an excluded path are left as they are.
CREATE TABLE IF NOT EXISTS scan_rule (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    storage_location_id BIGINT REFERENCES storage_location(id) ON DELETE CASCADE,
    pattern             TEXT NOT NULL CHECK (pattern <> ''),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Rules moved from disks to libraries in 0042, which also creates the unique index.

-- ===== 0026_stream_stats.sql =====
-- Daily streaming totals for admin analytics: bytes sent by direct play and
-- by HLS transcode, and the most streams playing at once that day.
CREATE TABLE IF NOT EXISTS stream_daily (
    day          DATE PRIMARY KEY,
    direct_bytes BIGINT NOT NULL DEFAULT 0,
    hls_bytes    BIGINT NOT NULL DEFAULT 0,
    peak_streams INTEGER NOT NULL DEFAULT 0
);

-- ===== 0027_libraries.sql =====
-- Libraries group folders on the disks into what users browse. A library is a
-- set of roots (a disk, optionally narrowed to a subfolder); a file belongs to
-- the library whose root is the longest match for its path, and a title to the
-- library of its (first available) file. Titles outside every library are only
-- visible in the admin console.
CREATE TABLE IF NOT EXISTS library (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name           TEXT NOT NULL UNIQUE CHECK (name <> ''),
    -- 'entertainment': movies and TV shows, cast and crew.
    -- 'private': videos, performers, studios and series.
    mode           TEXT NOT NULL DEFAULT 'entertainment' CHECK (mode IN ('entertainment', 'private')),
    -- Granted to every user created from now on.
    default_access BOOLEAN NOT NULL DEFAULT FALSE,
    position       INTEGER NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- path is relative to the disk's mount path, without leading or trailing
-- slashes; '' is the whole disk.
CREATE TABLE IF NOT EXISTS library_root (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    library_id          BIGINT NOT NULL REFERENCES library(id) ON DELETE CASCADE,
    storage_location_id BIGINT NOT NULL REFERENCES storage_location(id) ON DELETE CASCADE,
    path                TEXT NOT NULL DEFAULT '' CHECK (path !~ '^/' AND path !~ '/$'),
    UNIQUE (storage_location_id, path)
);

CREATE TABLE IF NOT EXISTS library_access (
    user_id    BIGINT NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    library_id BIGINT NOT NULL REFERENCES library(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, library_id)
);
CREATE INDEX IF NOT EXISTS library_access_library ON library_access (library_id);

-- The metadata providers a library matches with, in priority order. A
-- provider without a row is off for that library.
CREATE TABLE IF NOT EXISTS library_provider (
    library_id BIGINT NOT NULL REFERENCES library(id) ON DELETE CASCADE,
    provider   TEXT NOT NULL,
    position   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (library_id, provider)
);

ALTER TABLE media_file ADD COLUMN IF NOT EXISTS library_id BIGINT REFERENCES library(id) ON DELETE SET NULL;
ALTER TABLE media_item ADD COLUMN IF NOT EXISTS library_id BIGINT REFERENCES library(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS media_file_library ON media_file (library_id);
CREATE INDEX IF NOT EXISTS media_item_library ON media_item (library_id);

CREATE OR REPLACE FUNCTION library_for_file(loc BIGINT, rel TEXT) RETURNS BIGINT AS $$
    SELECT lr.library_id FROM library_root lr
    WHERE lr.storage_location_id = loc
      AND (lr.path = '' OR rel = lr.path OR left(rel, length(lr.path) + 1) = lr.path || '/')
    ORDER BY length(lr.path) DESC, lr.id
    LIMIT 1
$$ LANGUAGE sql STABLE;

CREATE OR REPLACE FUNCTION library_for_item(item BIGINT) RETURNS BIGINT AS $$
    SELECT mf.library_id FROM media_file mf
    WHERE mf.media_item_id = item
    ORDER BY mf.available DESC, mf.library_id IS NULL, mf.id
    LIMIT 1
$$ LANGUAGE sql STABLE;

-- Keep the columns right whenever a file is added, moved, renamed, reassigned
-- to another title or goes missing, so scanner/organizer/rename code never has
-- to think about libraries.
CREATE OR REPLACE FUNCTION media_file_library_assign() RETURNS trigger AS $$
BEGIN
    NEW.library_id := library_for_file(NEW.storage_location_id, NEW.relative_path);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS media_file_library_assign ON media_file;
CREATE TRIGGER media_file_library_assign BEFORE INSERT OR UPDATE OF storage_location_id, relative_path ON media_file
    FOR EACH ROW EXECUTE FUNCTION media_file_library_assign();

CREATE OR REPLACE FUNCTION media_item_library_sync() RETURNS trigger AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        UPDATE media_item SET library_id = library_for_item(OLD.media_item_id)
        WHERE id = OLD.media_item_id AND library_id IS DISTINCT FROM library_for_item(OLD.media_item_id);
    END IF;
    IF TG_OP <> 'DELETE' THEN
        UPDATE media_item SET library_id = library_for_item(NEW.media_item_id)
        WHERE id = NEW.media_item_id AND library_id IS DISTINCT FROM library_for_item(NEW.media_item_id);
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS media_file_item_library_sync ON media_file;
-- (UPDATE OF only sees columns in the statement's SET list, not ones the
-- BEFORE trigger changed, so path changes are listed too.)
CREATE TRIGGER media_file_item_library_sync AFTER INSERT OR DELETE OR UPDATE OF library_id, media_item_id, available, storage_location_id, relative_path ON media_file
    FOR EACH ROW EXECUTE FUNCTION media_item_library_sync();

-- Recomputes every file and title; run after library roots change.
CREATE OR REPLACE FUNCTION reassign_libraries() RETURNS void AS $$
BEGIN
    UPDATE media_file SET library_id = library_for_file(storage_location_id, relative_path)
    WHERE library_id IS DISTINCT FROM library_for_file(storage_location_id, relative_path);
    UPDATE media_item SET library_id = library_for_item(id)
    WHERE library_id IS DISTINCT FROM library_for_item(id);
END;
$$ LANGUAGE plpgsql;

-- New users get the libraries marked for default access.
CREATE OR REPLACE FUNCTION app_user_default_libraries() RETURNS trigger AS $$
BEGIN
    INSERT INTO library_access (user_id, library_id)
    SELECT NEW.id, id FROM library WHERE default_access
    ON CONFLICT DO NOTHING;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS app_user_default_libraries ON app_user;
CREATE TRIGGER app_user_default_libraries AFTER INSERT ON app_user
    FOR EACH ROW EXECUTE FUNCTION app_user_default_libraries();

-- One-time upgrade of an existing install: every disk goes into a "Private"
-- library (the only kind there was), open to every user and to new ones, with
-- the providers that were configured. A fresh install starts with none.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM app_setting WHERE key = 'libraries.migrated') THEN
        IF EXISTS (SELECT 1 FROM storage_location) AND NOT EXISTS (SELECT 1 FROM library) THEN
            INSERT INTO library (name, mode, default_access) VALUES ('Private', 'private', TRUE);
            INSERT INTO library_root (library_id, storage_location_id, path)
            SELECT (SELECT id FROM library WHERE name = 'Private'), id, '' FROM storage_location;
            INSERT INTO library_access (user_id, library_id)
            SELECT u.id, l.id FROM app_user u CROSS JOIN library l WHERE l.name = 'Private';
            INSERT INTO library_provider (library_id, provider, position)
            SELECT (SELECT id FROM library WHERE name = 'Private'), p.id,
                   COALESCE((SELECT o.pos FROM app_setting s, jsonb_array_elements_text(s.value::jsonb) WITH ORDINALITY AS o(id, pos)
                             WHERE s.key = 'provider_order' AND o.id = p.id), p.pos + 100)
            FROM (VALUES ('tmdb', 0)) AS p(id, pos);
        END IF;
        INSERT INTO app_setting (key, value) VALUES ('libraries.migrated', 'true');
    END IF;
END $$;

SELECT reassign_libraries();

-- ===== 0028_user_library_prefs.sql =====
-- The library the media app opens on for each user (NULL: the first one).
ALTER TABLE app_user ADD COLUMN IF NOT EXISTS default_library_id BIGINT REFERENCES library(id) ON DELETE SET NULL;

-- What the PIN lock covers: NULL is the whole app; otherwise only these
-- libraries lock, and the rest of the app stays open.
ALTER TABLE app_lock ADD COLUMN IF NOT EXISTS library_ids BIGINT[];

-- ===== 0029_entertainment.sql =====
-- Entertainment libraries hold movies and TV shows. A title there is a movie
-- or an episode (with its season and episode number); a series is a TV show
-- or a collection of movies. Private libraries leave these NULL.
ALTER TABLE media_item ADD COLUMN IF NOT EXISTS kind TEXT;
ALTER TABLE media_item ADD COLUMN IF NOT EXISTS season_number INTEGER CHECK (season_number >= 0);
ALTER TABLE media_item ADD COLUMN IF NOT EXISTS episode_number INTEGER CHECK (episode_number >= 0);
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'media_item_kind_check') THEN
    ALTER TABLE media_item ADD CONSTRAINT media_item_kind_check CHECK (kind IN ('movie', 'episode'));
  END IF;
END $$;
CREATE INDEX IF NOT EXISTS media_item_kind ON media_item (library_id, kind);
CREATE INDEX IF NOT EXISTS media_item_episode_order ON media_item (series_id, season_number, episode_number) WHERE series_id IS NOT NULL;

ALTER TABLE series ADD COLUMN IF NOT EXISTS kind TEXT;
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'series_kind_check') THEN
    ALTER TABLE series ADD CONSTRAINT series_kind_check CHECK (kind IN ('show', 'collection'));
  END IF;
END $$;

-- The role an actor plays in a title.
ALTER TABLE media_item_performer ADD COLUMN IF NOT EXISTS character TEXT;

-- Crew (directors, writers) are people too, kept in the performer table so
-- one person can both act and direct; cast stays in media_item_performer.
CREATE TABLE IF NOT EXISTS media_item_crew (
    media_item_id BIGINT NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    performer_id  BIGINT NOT NULL REFERENCES performer(id) ON DELETE CASCADE,
    job           TEXT NOT NULL CHECK (job IN ('director', 'writer')),
    position      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (media_item_id, performer_id, job)
);
CREATE INDEX IF NOT EXISTS media_item_crew_person ON media_item_crew (performer_id, job);

-- Classifies unclassified titles in entertainment libraries from their file
-- name: "S01E02" / "1x02" is an episode, anything else a movie. A metadata
-- match later sets the kind for certain.
CREATE OR REPLACE FUNCTION classify_titles() RETURNS void AS $$
    WITH pending AS (
        SELECT mi.id, (
            SELECT COALESCE(
                regexp_match(f.file_name, '[Ss](\d{1,2})[ ._-]?[Ee](\d{1,3})'),
                regexp_match(f.file_name, '(?:^|[ ._-])(\d{1,2})x(\d{2,3})(?:[ ._-]|$)'))
            FROM media_file f WHERE f.media_item_id = mi.id ORDER BY f.id LIMIT 1) AS m
        FROM media_item mi JOIN library l ON l.id = mi.library_id
        WHERE l.mode = 'entertainment' AND mi.kind IS NULL
    )
    UPDATE media_item mi SET
        kind = CASE WHEN p.m IS NOT NULL THEN 'episode' ELSE 'movie' END,
        season_number = COALESCE(mi.season_number, (p.m)[1]::int),
        episode_number = COALESCE(mi.episode_number, (p.m)[2]::int)
    FROM pending p WHERE p.id = mi.id
$$ LANGUAGE sql;

-- New files are classified as soon as they are indexed. (Triggers fire in
-- name order: "post" runs after media_file_item_library_sync has given the
-- title its library.)
CREATE OR REPLACE FUNCTION media_file_classify() RETURNS trigger AS $$
BEGIN
    IF NEW.library_id IS NOT NULL AND EXISTS (SELECT 1 FROM library WHERE id = NEW.library_id AND mode = 'entertainment')
       AND EXISTS (SELECT 1 FROM media_item WHERE id = NEW.media_item_id AND kind IS NULL) THEN
        PERFORM classify_titles();
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS media_file_classify ON media_file;
DROP TRIGGER IF EXISTS media_file_post_classify ON media_file;
CREATE TRIGGER media_file_post_classify AFTER INSERT ON media_file
    FOR EACH ROW EXECUTE FUNCTION media_file_classify();

SELECT classify_titles();

-- ===== 0030_per_user_favorites_playlists.sql =====
-- Favorites and playlists belong to one user. media_item.favorite and
-- performer.favorite are no longer read; existing favorites and playlists are
-- given to the first admin (like pre-existing ratings in 0017) so none are lost.

CREATE TABLE IF NOT EXISTS media_item_favorite (
    user_id       BIGINT NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    media_item_id BIGINT NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, media_item_id)
);
CREATE INDEX IF NOT EXISTS media_item_favorite_item ON media_item_favorite (media_item_id);

CREATE TABLE IF NOT EXISTS performer_favorite (
    user_id      BIGINT NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    performer_id BIGINT NOT NULL REFERENCES performer(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, performer_id)
);
CREATE INDEX IF NOT EXISTS performer_favorite_performer ON performer_favorite (performer_id);

-- A playlist is private to the user who made it; names are unique per user.
ALTER TABLE collection ADD COLUMN IF NOT EXISTS user_id BIGINT REFERENCES app_user(id) ON DELETE CASCADE;
ALTER TABLE collection DROP CONSTRAINT IF EXISTS collection_name_key;
CREATE UNIQUE INDEX IF NOT EXISTS collection_user_name ON collection (user_id, name);

DO $$
DECLARE owner BIGINT;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM app_setting WHERE key = 'per_user.migrated') THEN
        SELECT id INTO owner FROM app_user WHERE role = 'ADMIN' ORDER BY id LIMIT 1;
        IF owner IS NOT NULL THEN
            INSERT INTO media_item_favorite (user_id, media_item_id)
            SELECT owner, id FROM media_item WHERE favorite ON CONFLICT DO NOTHING;
            INSERT INTO performer_favorite (user_id, performer_id)
            SELECT owner, id FROM performer WHERE favorite ON CONFLICT DO NOTHING;
            UPDATE collection SET user_id = owner WHERE user_id IS NULL;
        END IF;
        INSERT INTO app_setting (key, value) VALUES ('per_user.migrated', 'true');
    END IF;
END $$;

-- ===== 0031_tracks_schedules.sql =====
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

-- ===== 0032_indexes.sql =====
-- Indexes for foreign keys and sort orders that were missing.

-- Studio pages, studio delete/merge (ON DELETE SET NULL) scanned media_item.
CREATE INDEX IF NOT EXISTS media_item_studio ON media_item (studio_id) WHERE studio_id IS NOT NULL;

-- Default "recently added" sort, scoped per library.
CREATE INDEX IF NOT EXISTS media_item_library_added ON media_item (library_id, date_added DESC);

-- Reverse lookups: items by tag, playlists containing an item.
CREATE INDEX IF NOT EXISTS media_item_tag_reverse ON media_item_tag (tag_id, media_item_id);
CREATE INDEX IF NOT EXISTS collection_media_item_reverse ON collection_media_item (media_item_id);

-- Artwork deletes (ON DELETE SET NULL) scanned these tables.
CREATE INDEX IF NOT EXISTS media_item_poster ON media_item (poster_image_id) WHERE poster_image_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS media_item_backdrop ON media_item (backdrop_image_id) WHERE backdrop_image_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS performer_profile_image ON performer (profile_image_id) WHERE profile_image_id IS NOT NULL;

-- Remaining foreign keys (series.studio_id is already covered by UNIQUE (studio_id, name)).
CREATE INDEX IF NOT EXISTS category_parent ON category (parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS duplicate_group_file_media_file ON duplicate_group_file (media_file_id);

-- ===== 0033_collection_library.sql =====
-- Playlists are owned by a library as well as a user. Existing playlists
-- inherit the library of their first title, or the owner's default library.
ALTER TABLE collection ADD COLUMN IF NOT EXISTS library_id BIGINT REFERENCES library(id) ON DELETE SET NULL;

UPDATE collection c SET library_id = COALESCE(
    (SELECT mi.library_id FROM collection_media_item cmi
     JOIN media_item mi ON mi.id = cmi.media_item_id
     WHERE cmi.collection_id = c.id AND mi.library_id IS NOT NULL
     ORDER BY cmi.position, cmi.media_item_id LIMIT 1),
    (SELECT default_library_id FROM app_user WHERE id = c.user_id),
    (SELECT la.library_id FROM library_access la WHERE la.user_id = c.user_id ORDER BY la.library_id LIMIT 1),
    (SELECT id FROM library ORDER BY position, id LIMIT 1)
) WHERE c.library_id IS NULL;

DROP INDEX IF EXISTS collection_user_name;
CREATE UNIQUE INDEX IF NOT EXISTS collection_user_library_name ON collection (user_id, library_id, name) WHERE library_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS collection_user_unassigned_name ON collection (user_id, name) WHERE library_id IS NULL;

-- ===== 0034_series_backdrop.sql =====
-- A series can carry its own backdrop image (uploaded in the series settings),
-- shown behind the series page header.
ALTER TABLE series ADD COLUMN IF NOT EXISTS backdrop_image_id BIGINT REFERENCES artwork(id) ON DELETE SET NULL;

-- ===== 0035_native_auth.sql =====
-- Built-in username/password sign-in, for installs with no external identity
-- provider. Accounts created by a provider or plugin keep both columns NULL.
ALTER TABLE app_user ADD COLUMN IF NOT EXISTS username TEXT;
ALTER TABLE app_user ADD COLUMN IF NOT EXISTS password_hash TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS app_user_username_key ON app_user (lower(username)) WHERE username IS NOT NULL;

-- ===== 0037_session_plugin.sql =====
-- Which sign-in plugin opened a session, so sign-out can use that provider's
-- logout page. NULL for ReelStash's own accounts.
ALTER TABLE web_session ADD COLUMN IF NOT EXISTS auth_plugin TEXT;

-- ===== 0038_organizer_per_library.sql =====
-- Organizer rules and layout settings belong to a library: each library has
-- its own studio/performer rules (and precedence) and its own folder layout.
-- An upgrade copies the old global rules and settings into every library.
ALTER TABLE organizer_rule ADD COLUMN IF NOT EXISTS library_id BIGINT REFERENCES library(id) ON DELETE CASCADE;

-- Drop the global uniqueness first: the copy below inserts one row per library.
ALTER TABLE organizer_rule DROP CONSTRAINT IF EXISTS organizer_rule_studio_id_key;
ALTER TABLE organizer_rule DROP CONSTRAINT IF EXISTS organizer_rule_performer_id_key;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM organizer_rule WHERE library_id IS NULL) THEN
        INSERT INTO organizer_rule (library_id, studio_id, performer_id, folder_name, enabled, position)
        SELECT l.id, r.studio_id, r.performer_id, r.folder_name, r.enabled, r.position
        FROM organizer_rule r CROSS JOIN library l
        WHERE r.library_id IS NULL;
        DELETE FROM organizer_rule WHERE library_id IS NULL;
    END IF;

    INSERT INTO app_setting (key, value, updated_at)
    SELECT 'organizer_settings:' || l.id, s.value, now()
    FROM library l CROSS JOIN app_setting s
    WHERE s.key = 'organizer_settings'
    ON CONFLICT (key) DO NOTHING;
    DELETE FROM app_setting WHERE key = 'organizer_settings';
END $$;

ALTER TABLE organizer_rule ALTER COLUMN library_id SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS organizer_rule_library_studio ON organizer_rule (library_id, studio_id);
CREATE UNIQUE INDEX IF NOT EXISTS organizer_rule_library_performer ON organizer_rule (library_id, performer_id);
DROP INDEX IF EXISTS organizer_rule_position_idx;
CREATE INDEX IF NOT EXISTS organizer_rule_library_position_idx ON organizer_rule (library_id, position);

-- ===== 0039_mp4_data_boxes.sql =====
-- How many top-level 'mdat' boxes an MP4/MOV file has (NULL: not checked yet,
-- or not an MP4). Some downloaders write one per few seconds of video; a
-- browser opening such a file seeks to every box header first, which pulls
-- nearly the whole file before the first frame. MP4_REPAIR rewrites them.
ALTER TABLE media_file ADD COLUMN IF NOT EXISTS mp4_data_boxes INTEGER;

-- ===== 0040_subtitle_counts.sql =====
-- How many subtitle files lie next to each video (Movie.en.srt and the like),
-- so titles can be filtered by whether they have subtitles. NULL until the
-- folder was looked at; the TRACK_SCAN job fills it in for the whole library.
ALTER TABLE media_file ADD COLUMN IF NOT EXISTS sidecar_subtitles INTEGER;

-- ===== 0041_library_scan.sql =====
-- A scan can cover one library's folders on a disk instead of the whole disk.
-- NULL is a whole-disk scan.
ALTER TABLE scan_job ADD COLUMN IF NOT EXISTS library_id BIGINT REFERENCES library(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS scan_job_library ON scan_job (library_id, completed_at);

-- ===== 0042_library_automation.sql =====
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

-- ===== 0043_linked_not_ambiguous.sql =====
-- A performer or studio linked to at least one provider is matched; an earlier
-- run could leave it flagged "choose match" because another provider had
-- several candidates.
UPDATE performer SET match_status = 'matched'
WHERE match_status IN ('ambiguous', 'unmatched', 'no_match')
  AND EXISTS (SELECT 1 FROM match_link l WHERE l.kind = 'performer' AND l.entity_id = performer.id);
UPDATE studio SET match_status = 'matched'
WHERE match_status IN ('ambiguous', 'unmatched', 'no_match')
  AND EXISTS (SELECT 1 FROM match_link l WHERE l.kind = 'studio' AND l.entity_id = studio.id);

-- ===== 0044_housekeeping.sql =====
-- Housekeeping can run on a schedule like scans and duplicate checks.
-- Migrations replay on every start, so only swap the check once.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                   WHERE conrelid = 'scan_schedule'::regclass
                     AND conname = 'scan_schedule_job_kind_check'
                     AND pg_get_constraintdef(oid) LIKE '%HOUSEKEEPING%') THEN
        ALTER TABLE scan_schedule DROP CONSTRAINT IF EXISTS scan_schedule_job_kind_check;
        ALTER TABLE scan_schedule ADD CONSTRAINT scan_schedule_job_kind_check
            CHECK (job_kind IN ('SCAN', 'DUPLICATE_SCAN', 'HOUSEKEEPING'));
    END IF;
END $$;

-- ===== 0045_reels.sql =====
-- Reels: portrait (vertical) videos, detected from the probed frame size. Phones
-- often record a landscape frame plus a rotation flag, so the rotation is kept
-- too; NULL means the file was probed before rotation was read (the next
-- TRACK_SCAN reads it).
ALTER TABLE media_file ADD COLUMN IF NOT EXISTS rotation SMALLINT;
ALTER TABLE media_file ADD COLUMN IF NOT EXISTS portrait BOOLEAN GENERATED ALWAYS AS (
    CASE WHEN rotation IN (90, 270) THEN width > height ELSE height > width END
) STORED;
CREATE INDEX IF NOT EXISTS idx_media_file_portrait ON media_file (media_item_id) WHERE portrait AND available;

-- An admin can override the detection per title: TRUE always a reel, FALSE
-- never, NULL decided by the video's shape.
ALTER TABLE media_item ADD COLUMN IF NOT EXISTS reel_override BOOLEAN;

-- ===== 0046_device_sessions.sql =====
-- Device sessions: apps (Android, later iOS and TV) sign in with a bearer token
-- instead of a cookie. They are ordinary web_session rows with a device name, so
-- people can see and revoke them. public_id names a session without exposing
-- its secret id.
ALTER TABLE web_session ADD COLUMN IF NOT EXISTS public_id BIGINT GENERATED ALWAYS AS IDENTITY;
ALTER TABLE web_session ADD COLUMN IF NOT EXISTS device_name TEXT;
ALTER TABLE web_session ADD COLUMN IF NOT EXISTS device_platform TEXT;
ALTER TABLE web_session ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ;
CREATE UNIQUE INDEX IF NOT EXISTS web_session_public_id ON web_session (public_id);

-- Device pairing: an app shows a short code, a signed-in person enters it in
-- the web UI, and the app's next poll receives a device session. device_code
-- is the app's secret; user_code is what the person types.
CREATE TABLE IF NOT EXISTS device_pairing (
    device_code TEXT PRIMARY KEY,
    user_code TEXT NOT NULL UNIQUE,
    device_name TEXT NOT NULL,
    device_platform TEXT NOT NULL,
    approved_user_id BIGINT REFERENCES app_user(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS device_pairing_expires ON device_pairing (expires_at);

-- ===== 0047_downloads.sql =====
-- Downloads (qBittorrent): folders ReelStash never touches, move rules for
-- finished torrents, and what happened to each finished torrent.

-- Locked folders: scans, the watcher, the organizer, housekeeping and Files
-- leave everything inside one alone. owner says which feature added it.
CREATE TABLE IF NOT EXISTS protected_path (
    path       TEXT PRIMARY KEY,
    owner      TEXT NOT NULL DEFAULT 'downloads',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Checked top to bottom; the first enabled rule whose conditions all hold
-- moves the finished torrent (through qBittorrent, so it keeps seeding).
CREATE TABLE IF NOT EXISTS download_rule (
    id                       BIGSERIAL PRIMARY KEY,
    name                     TEXT NOT NULL,
    position                 INT NOT NULL DEFAULT 0,
    enabled                  BOOLEAN NOT NULL DEFAULT true,
    match_category           TEXT NOT NULL DEFAULT '',
    match_tag                TEXT NOT NULL DEFAULT '',
    match_tracker            TEXT NOT NULL DEFAULT '',
    match_name               TEXT NOT NULL DEFAULT '',
    min_size_mb              BIGINT NOT NULL DEFAULT 0,
    video_only               BOOLEAN NOT NULL DEFAULT false,
    dest_storage_location_id BIGINT NOT NULL REFERENCES storage_location(id) ON DELETE CASCADE,
    dest_path                TEXT NOT NULL DEFAULT '',
    subfolder                TEXT NOT NULL DEFAULT '',
    scan_after               BOOLEAN NOT NULL DEFAULT true,
    remove_after             TEXT NOT NULL DEFAULT 'never' CHECK (remove_after IN ('never', 'moved', 'seeded')),
    remove_ratio             DOUBLE PRECISION NOT NULL DEFAULT 0,
    remove_seed_minutes      BIGINT NOT NULL DEFAULT 0,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per finished torrent ReelStash has looked at, so a rule runs once.
CREATE TABLE IF NOT EXISTS download_move (
    hash         TEXT PRIMARY KEY,
    name         TEXT NOT NULL DEFAULT '',
    rule_id      BIGINT REFERENCES download_rule(id) ON DELETE SET NULL,
    rule_name    TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL CHECK (status IN ('MOVING', 'MOVED', 'FAILED', 'SKIPPED', 'REMOVED')),
    from_path    TEXT NOT NULL DEFAULT '',
    dest_path    TEXT NOT NULL DEFAULT '',
    message      TEXT NOT NULL DEFAULT '',
    scan_pending BOOLEAN NOT NULL DEFAULT false,
    library_id   BIGINT REFERENCES library(id) ON DELETE SET NULL,
    remove_after TEXT NOT NULL DEFAULT 'never',
    remove_ratio DOUBLE PRECISION NOT NULL DEFAULT 0,
    remove_seed_minutes BIGINT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    moved_at     TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS download_move_status_idx ON download_move (status, updated_at DESC);

-- ===== 0048_reliability.sql =====
-- Password lockouts are shared by every API process, and a duplicate moved
-- aside can be put back.

CREATE TABLE IF NOT EXISTS auth_throttle (
    bucket TEXT PRIMARY KEY,
    fails INTEGER NOT NULL,
    until_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS trash_item (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    storage_location_id BIGINT NOT NULL REFERENCES storage_location(id) ON DELETE CASCADE,
    original_path TEXT NOT NULL,
    trash_path TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    file_size BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ===== 0049_admin_audit.sql =====
-- Admin audit trail: one row per successful admin change (who, what, to what, and why),
-- and a short history of webhook deliveries.

CREATE TABLE IF NOT EXISTS admin_audit_event (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_id BIGINT,
    actor_name TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL,
    method TEXT NOT NULL DEFAULT '',
    route TEXT NOT NULL DEFAULT '',
    target_kind TEXT NOT NULL DEFAULT '',
    target_id BIGINT,
    target_label TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    detail JSONB
);
CREATE INDEX IF NOT EXISTS admin_audit_event_at ON admin_audit_event (at DESC, id DESC);
CREATE INDEX IF NOT EXISTS admin_audit_event_target ON admin_audit_event (target_kind, target_id);
CREATE INDEX IF NOT EXISTS admin_audit_event_actor ON admin_audit_event (actor_id);

CREATE TABLE IF NOT EXISTS webhook_delivery (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    webhook_id BIGINT NOT NULL REFERENCES webhook(id) ON DELETE CASCADE,
    at TIMESTAMPTZ NOT NULL DEFAULT now(),
    kind TEXT NOT NULL DEFAULT '',
    status INTEGER,
    error TEXT,
    duration_ms INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS webhook_delivery_hook ON webhook_delivery (webhook_id, id DESC);

-- DVD VIDEO_TS folders a scan has noticed. A scan only records them; an admin
-- converts a chosen disc to MP4 afterwards. The VOB files stay on disk.
CREATE TABLE IF NOT EXISTS dvd_disc (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    storage_location_id BIGINT NOT NULL REFERENCES storage_location(id) ON DELETE CASCADE,
    relative_path TEXT NOT NULL,
    title_count INTEGER NOT NULL DEFAULT 0,
    file_size BIGINT NOT NULL DEFAULT 0,
    available BOOLEAN NOT NULL DEFAULT TRUE,
    missing_since TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (storage_location_id, relative_path)
);
CREATE INDEX IF NOT EXISTS dvd_disc_storage ON dvd_disc (storage_location_id);

-- ===== keep my edits =====
-- Fields of a title a person edited by hand and asked to keep: a metadata match or rescan
-- writes them back afterwards instead of replacing them.
CREATE TABLE IF NOT EXISTS media_item_field_lock (
    media_item_id BIGINT NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    field TEXT NOT NULL CHECK (field IN ('title', 'description', 'studio', 'categories', 'performers')),
    locked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (media_item_id, field)
);

-- ===== performer_notify =====
-- People a user wants to hear about: titles with them in the cast appear in that user's new-title notifications.
CREATE TABLE IF NOT EXISTS performer_notify (
    user_id      BIGINT NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    performer_id BIGINT NOT NULL REFERENCES performer(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, performer_id)
);
CREATE INDEX IF NOT EXISTS performer_notify_performer ON performer_notify (performer_id);
