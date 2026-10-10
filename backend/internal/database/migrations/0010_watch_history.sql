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
