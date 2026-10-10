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
