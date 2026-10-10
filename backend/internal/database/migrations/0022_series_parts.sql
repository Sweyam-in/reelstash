-- Series hold several titles in order (the scenes of a movie, the episodes of
-- a show, the films of a collection). series_position is a title's number in
-- its series; titles without one sort after the numbered ones.
ALTER TABLE media_item ADD COLUMN IF NOT EXISTS series_position INTEGER CHECK (series_position >= 0);
CREATE INDEX IF NOT EXISTS media_item_series_position_idx ON media_item (series_id, series_position) WHERE series_id IS NOT NULL;

ALTER TABLE series ADD COLUMN IF NOT EXISTS release_date DATE;

-- A series can be linked to one record at each metadata provider, like titles,
-- performers and studios. The link's remote_metadata keeps that record's parts
-- (scenes or episodes) so they can be paired with the series' videos.
ALTER TABLE match_link DROP CONSTRAINT IF EXISTS match_link_kind_check;
ALTER TABLE match_link ADD CONSTRAINT match_link_kind_check CHECK (kind IN ('media', 'performer', 'studio', 'series'));

DROP TRIGGER IF EXISTS series_match_link_cleanup ON series;
CREATE TRIGGER series_match_link_cleanup AFTER DELETE ON series
    FOR EACH ROW EXECUTE FUNCTION match_link_cleanup('series');
