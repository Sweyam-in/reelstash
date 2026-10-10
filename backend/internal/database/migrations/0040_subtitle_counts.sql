-- How many subtitle files lie next to each video (Movie.en.srt and the like),
-- so titles can be filtered by whether they have subtitles. NULL until the
-- folder was looked at; the TRACK_SCAN job fills it in for the whole library.
ALTER TABLE media_file ADD COLUMN IF NOT EXISTS sidecar_subtitles INTEGER;
