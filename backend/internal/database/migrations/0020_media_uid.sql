-- A stable identity for each title that lives outside the database too: the
-- scanner writes it into every video's NFO as <uniqueid type="reelstash">, so a
-- file keeps its title (and watch history, ratings, links) through renames,
-- moves, remuxes and a rebuilt database.
ALTER TABLE media_item ADD COLUMN IF NOT EXISTS uid UUID NOT NULL DEFAULT gen_random_uuid();
CREATE UNIQUE INDEX IF NOT EXISTS media_item_uid ON media_item (uid);

-- The uid last confirmed in this file's NFO. When it differs from the title's
-- uid (or is NULL) the next scan writes/updates the NFO.
ALTER TABLE media_file ADD COLUMN IF NOT EXISTS nfo_uid UUID;
