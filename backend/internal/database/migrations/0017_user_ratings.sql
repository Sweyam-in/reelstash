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
