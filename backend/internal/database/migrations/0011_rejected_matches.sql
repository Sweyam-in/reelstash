-- "Wrong match" memory: a provider record the user rejected for a performer or
-- a title, so automatic jobs never propose or apply it again.
ALTER TABLE performer DROP CONSTRAINT IF EXISTS performer_match_status_check;
ALTER TABLE performer ADD CONSTRAINT performer_match_status_check
  CHECK (match_status IN ('unmatched', 'matched', 'no_match', 'ambiguous', 'rejected'));

CREATE TABLE IF NOT EXISTS performer_rejected_match (
    performer_id BIGINT NOT NULL REFERENCES performer(id) ON DELETE CASCADE,
    external_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (performer_id, external_id)
);

ALTER TABLE media_item ADD COLUMN IF NOT EXISTS match_status TEXT NOT NULL DEFAULT 'unmatched';
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'media_item_match_status_check') THEN
    ALTER TABLE media_item ADD CONSTRAINT media_item_match_status_check
      CHECK (match_status IN ('unmatched', 'matched', 'rejected'));
  END IF;
END $$;

CREATE TABLE IF NOT EXISTS media_rejected_match (
    media_item_id BIGINT NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    scene_uuid TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (media_item_id, scene_uuid)
);
