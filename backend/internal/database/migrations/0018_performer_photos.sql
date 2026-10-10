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
