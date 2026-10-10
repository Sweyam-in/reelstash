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
