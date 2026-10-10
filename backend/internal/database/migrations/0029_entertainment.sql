-- Entertainment libraries hold movies and TV shows. A title there is a movie
-- or an episode (with its season and episode number); a series is a TV show
-- or a collection of movies. Private libraries leave these NULL.
ALTER TABLE media_item ADD COLUMN IF NOT EXISTS kind TEXT;
ALTER TABLE media_item ADD COLUMN IF NOT EXISTS season_number INTEGER CHECK (season_number >= 0);
ALTER TABLE media_item ADD COLUMN IF NOT EXISTS episode_number INTEGER CHECK (episode_number >= 0);
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'media_item_kind_check') THEN
    ALTER TABLE media_item ADD CONSTRAINT media_item_kind_check CHECK (kind IN ('movie', 'episode'));
  END IF;
END $$;
CREATE INDEX IF NOT EXISTS media_item_kind ON media_item (library_id, kind);
CREATE INDEX IF NOT EXISTS media_item_episode_order ON media_item (series_id, season_number, episode_number) WHERE series_id IS NOT NULL;

ALTER TABLE series ADD COLUMN IF NOT EXISTS kind TEXT;
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'series_kind_check') THEN
    ALTER TABLE series ADD CONSTRAINT series_kind_check CHECK (kind IN ('show', 'collection'));
  END IF;
END $$;

-- The role an actor plays in a title.
ALTER TABLE media_item_performer ADD COLUMN IF NOT EXISTS character TEXT;

-- Crew (directors, writers) are people too, kept in the performer table so
-- one person can both act and direct; cast stays in media_item_performer.
CREATE TABLE IF NOT EXISTS media_item_crew (
    media_item_id BIGINT NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    performer_id  BIGINT NOT NULL REFERENCES performer(id) ON DELETE CASCADE,
    job           TEXT NOT NULL CHECK (job IN ('director', 'writer')),
    position      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (media_item_id, performer_id, job)
);
CREATE INDEX IF NOT EXISTS media_item_crew_person ON media_item_crew (performer_id, job);

-- Classifies unclassified titles in entertainment libraries from their file
-- name: "S01E02" / "1x02" is an episode, anything else a movie. A metadata
-- match later sets the kind for certain.
CREATE OR REPLACE FUNCTION classify_titles() RETURNS void AS $$
    WITH pending AS (
        SELECT mi.id, (
            SELECT COALESCE(
                regexp_match(f.file_name, '[Ss](\d{1,2})[ ._-]?[Ee](\d{1,3})'),
                regexp_match(f.file_name, '(?:^|[ ._-])(\d{1,2})x(\d{2,3})(?:[ ._-]|$)'))
            FROM media_file f WHERE f.media_item_id = mi.id ORDER BY f.id LIMIT 1) AS m
        FROM media_item mi JOIN library l ON l.id = mi.library_id
        WHERE l.mode = 'entertainment' AND mi.kind IS NULL
    )
    UPDATE media_item mi SET
        kind = CASE WHEN p.m IS NOT NULL THEN 'episode' ELSE 'movie' END,
        season_number = COALESCE(mi.season_number, (p.m)[1]::int),
        episode_number = COALESCE(mi.episode_number, (p.m)[2]::int)
    FROM pending p WHERE p.id = mi.id
$$ LANGUAGE sql;

-- New files are classified as soon as they are indexed. (Triggers fire in
-- name order: "post" runs after media_file_item_library_sync has given the
-- title its library.)
CREATE OR REPLACE FUNCTION media_file_classify() RETURNS trigger AS $$
BEGIN
    IF NEW.library_id IS NOT NULL AND EXISTS (SELECT 1 FROM library WHERE id = NEW.library_id AND mode = 'entertainment')
       AND EXISTS (SELECT 1 FROM media_item WHERE id = NEW.media_item_id AND kind IS NULL) THEN
        PERFORM classify_titles();
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS media_file_classify ON media_file;
DROP TRIGGER IF EXISTS media_file_post_classify ON media_file;
CREATE TRIGGER media_file_post_classify AFTER INSERT ON media_file
    FOR EACH ROW EXECUTE FUNCTION media_file_classify();

SELECT classify_titles();
