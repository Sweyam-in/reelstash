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
