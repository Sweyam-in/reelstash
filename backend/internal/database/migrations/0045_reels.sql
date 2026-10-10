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
