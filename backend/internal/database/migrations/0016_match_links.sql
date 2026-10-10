-- Provider-agnostic match links: a title, performer or studio can be linked to
-- one record at each metadata provider (TMDB, plugins, ...).
CREATE TABLE IF NOT EXISTS match_link (
    kind TEXT NOT NULL CHECK (kind IN ('media', 'performer', 'studio')),
    entity_id BIGINT NOT NULL,
    provider TEXT NOT NULL,
    external_id TEXT NOT NULL,
    matched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (kind, entity_id, provider)
);
CREATE INDEX IF NOT EXISTS match_link_external ON match_link (provider, external_id);


-- Links are polymorphic (no FK), so clean them up when the entity goes away.
CREATE OR REPLACE FUNCTION match_link_cleanup() RETURNS trigger AS $$
BEGIN
    DELETE FROM match_link WHERE kind = TG_ARGV[0] AND entity_id = OLD.id;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS media_item_match_link_cleanup ON media_item;
CREATE TRIGGER media_item_match_link_cleanup AFTER DELETE ON media_item
    FOR EACH ROW EXECUTE FUNCTION match_link_cleanup('media');
DROP TRIGGER IF EXISTS performer_match_link_cleanup ON performer;
CREATE TRIGGER performer_match_link_cleanup AFTER DELETE ON performer
    FOR EACH ROW EXECUTE FUNCTION match_link_cleanup('performer');
DROP TRIGGER IF EXISTS studio_match_link_cleanup ON studio;
CREATE TRIGGER studio_match_link_cleanup AFTER DELETE ON studio
    FOR EACH ROW EXECUTE FUNCTION match_link_cleanup('studio');

-- Rejected ("wrong match") memory is per provider.
ALTER TABLE media_rejected_match ADD COLUMN IF NOT EXISTS provider TEXT NOT NULL DEFAULT '';
ALTER TABLE media_rejected_match ALTER COLUMN provider DROP DEFAULT;
ALTER TABLE media_rejected_match DROP CONSTRAINT IF EXISTS media_rejected_match_pkey;
ALTER TABLE media_rejected_match ADD PRIMARY KEY (media_item_id, provider, scene_uuid);

-- Per-link provider metadata (what that provider said about the record), so
-- unlinking one provider can fall back to what another one supplied.
ALTER TABLE match_link ADD COLUMN IF NOT EXISTS remote_metadata JSONB;

-- One provider record can be linked to only one performer/studio. (Titles are
-- excluded: several files may legitimately share a scene.)
CREATE UNIQUE INDEX IF NOT EXISTS match_link_person_studio_unique
    ON match_link (kind, provider, external_id) WHERE kind IN ('performer', 'studio');

-- Wrong-match memory for performers is per provider too.
ALTER TABLE performer_rejected_match ADD COLUMN IF NOT EXISTS provider TEXT NOT NULL DEFAULT '';
ALTER TABLE performer_rejected_match ALTER COLUMN provider DROP DEFAULT;
ALTER TABLE performer_rejected_match DROP CONSTRAINT IF EXISTS performer_rejected_match_pkey;
ALTER TABLE performer_rejected_match ADD PRIMARY KEY (performer_id, provider, external_id);
