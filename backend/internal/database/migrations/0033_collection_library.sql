-- Playlists are owned by a library as well as a user. Existing playlists
-- inherit the library of their first title, or the owner's default library.
ALTER TABLE collection ADD COLUMN IF NOT EXISTS library_id BIGINT REFERENCES library(id) ON DELETE SET NULL;

UPDATE collection c SET library_id = COALESCE(
    (SELECT mi.library_id FROM collection_media_item cmi
     JOIN media_item mi ON mi.id = cmi.media_item_id
     WHERE cmi.collection_id = c.id AND mi.library_id IS NOT NULL
     ORDER BY cmi.position, cmi.media_item_id LIMIT 1),
    (SELECT default_library_id FROM app_user WHERE id = c.user_id),
    (SELECT la.library_id FROM library_access la WHERE la.user_id = c.user_id ORDER BY la.library_id LIMIT 1),
    (SELECT id FROM library ORDER BY position, id LIMIT 1)
) WHERE c.library_id IS NULL;

DROP INDEX IF EXISTS collection_user_name;
CREATE UNIQUE INDEX IF NOT EXISTS collection_user_library_name ON collection (user_id, library_id, name) WHERE library_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS collection_user_unassigned_name ON collection (user_id, name) WHERE library_id IS NULL;
