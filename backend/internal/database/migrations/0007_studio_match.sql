-- Studio match state and the logo/metadata captured when a match is stored.
ALTER TABLE studio ADD COLUMN IF NOT EXISTS match_status TEXT NOT NULL DEFAULT 'unmatched';
ALTER TABLE studio ADD COLUMN IF NOT EXISTS remote_metadata JSONB;
ALTER TABLE studio ADD COLUMN IF NOT EXISTS matched_at TIMESTAMPTZ;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'studio_match_status_check') THEN
    ALTER TABLE studio ADD CONSTRAINT studio_match_status_check
      CHECK (match_status IN ('unmatched', 'matched', 'no_match', 'ambiguous'));
  END IF;
END $$;
