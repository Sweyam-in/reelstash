-- The library the media app opens on for each user (NULL: the first one).
ALTER TABLE app_user ADD COLUMN IF NOT EXISTS default_library_id BIGINT REFERENCES library(id) ON DELETE SET NULL;

-- What the PIN lock covers: NULL is the whole app; otherwise only these
-- libraries lock, and the rest of the app stays open.
ALTER TABLE app_lock ADD COLUMN IF NOT EXISTS library_ids BIGINT[];
