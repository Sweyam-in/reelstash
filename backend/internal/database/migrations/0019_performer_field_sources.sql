-- Which provider each performer detail is taken from, e.g.
-- {"bio": "a-plugin", "height": "tmdb"}. A field that is not listed comes from
-- the most recent match, with gaps filled from the other providers.
ALTER TABLE performer ADD COLUMN IF NOT EXISTS field_sources JSONB NOT NULL DEFAULT '{}';
