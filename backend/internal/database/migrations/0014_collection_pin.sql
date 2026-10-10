-- Playlists can be pinned so they sort to the top of the Playlists page.
ALTER TABLE collection ADD COLUMN IF NOT EXISTS pinned BOOLEAN NOT NULL DEFAULT FALSE;
