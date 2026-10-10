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
