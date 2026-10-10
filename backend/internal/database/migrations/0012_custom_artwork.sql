-- Images the user set by hand. Automatic jobs (provider matches, syncs) must
-- never replace or remove an artwork row flagged custom.
ALTER TABLE artwork ADD COLUMN IF NOT EXISTS custom BOOLEAN NOT NULL DEFAULT false;
