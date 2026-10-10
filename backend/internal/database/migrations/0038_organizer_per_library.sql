-- Organizer rules and layout settings belong to a library: each library has
-- its own studio/performer rules (and precedence) and its own folder layout.
-- An upgrade copies the old global rules and settings into every library.
ALTER TABLE organizer_rule ADD COLUMN IF NOT EXISTS library_id BIGINT REFERENCES library(id) ON DELETE CASCADE;

-- Drop the global uniqueness first: the copy below inserts one row per library.
ALTER TABLE organizer_rule DROP CONSTRAINT IF EXISTS organizer_rule_studio_id_key;
ALTER TABLE organizer_rule DROP CONSTRAINT IF EXISTS organizer_rule_performer_id_key;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM organizer_rule WHERE library_id IS NULL) THEN
        INSERT INTO organizer_rule (library_id, studio_id, performer_id, folder_name, enabled, position)
        SELECT l.id, r.studio_id, r.performer_id, r.folder_name, r.enabled, r.position
        FROM organizer_rule r CROSS JOIN library l
        WHERE r.library_id IS NULL;
        DELETE FROM organizer_rule WHERE library_id IS NULL;
    END IF;

    INSERT INTO app_setting (key, value, updated_at)
    SELECT 'organizer_settings:' || l.id, s.value, now()
    FROM library l CROSS JOIN app_setting s
    WHERE s.key = 'organizer_settings'
    ON CONFLICT (key) DO NOTHING;
    DELETE FROM app_setting WHERE key = 'organizer_settings';
END $$;

ALTER TABLE organizer_rule ALTER COLUMN library_id SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS organizer_rule_library_studio ON organizer_rule (library_id, studio_id);
CREATE UNIQUE INDEX IF NOT EXISTS organizer_rule_library_performer ON organizer_rule (library_id, performer_id);
DROP INDEX IF EXISTS organizer_rule_position_idx;
CREATE INDEX IF NOT EXISTS organizer_rule_library_position_idx ON organizer_rule (library_id, position);
