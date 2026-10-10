-- The file name a video had before ReelStash first renamed it after a metadata
-- match, so a wrong match can be undone without losing it.
ALTER TABLE media_file ADD COLUMN IF NOT EXISTS original_file_name TEXT;
