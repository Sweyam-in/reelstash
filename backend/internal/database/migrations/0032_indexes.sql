-- Indexes for foreign keys and sort orders that were missing.

-- Studio pages, studio delete/merge (ON DELETE SET NULL) scanned media_item.
CREATE INDEX IF NOT EXISTS media_item_studio ON media_item (studio_id) WHERE studio_id IS NOT NULL;

-- Default "recently added" sort, scoped per library.
CREATE INDEX IF NOT EXISTS media_item_library_added ON media_item (library_id, date_added DESC);

-- Reverse lookups: items by tag, playlists containing an item.
CREATE INDEX IF NOT EXISTS media_item_tag_reverse ON media_item_tag (tag_id, media_item_id);
CREATE INDEX IF NOT EXISTS collection_media_item_reverse ON collection_media_item (media_item_id);

-- Artwork deletes (ON DELETE SET NULL) scanned these tables.
CREATE INDEX IF NOT EXISTS media_item_poster ON media_item (poster_image_id) WHERE poster_image_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS media_item_backdrop ON media_item (backdrop_image_id) WHERE backdrop_image_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS performer_profile_image ON performer (profile_image_id) WHERE profile_image_id IS NOT NULL;

-- Remaining foreign keys (series.studio_id is already covered by UNIQUE (studio_id, name)).
CREATE INDEX IF NOT EXISTS category_parent ON category (parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS duplicate_group_file_media_file ON duplicate_group_file (media_file_id);
