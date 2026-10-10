-- Metadata-driven organizer rules (Admin > Organizer). Each rule sends the
-- videos of one studio or one performer to categorized/<folder>/; the rule
-- list's position is its precedence (lowest position wins when a video
-- matches several rules). Videos matching no rule go to uncategorized/.
-- Replaces the filename-keyword rules in organizer_keyword, which are no
-- longer read (the table is kept so nothing the user entered is lost).
CREATE TABLE IF NOT EXISTS organizer_rule (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    studio_id BIGINT UNIQUE REFERENCES studio(id) ON DELETE CASCADE,
    performer_id BIGINT UNIQUE REFERENCES performer(id) ON DELETE CASCADE,
    folder_name TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((studio_id IS NULL) <> (performer_id IS NULL))
);

CREATE INDEX IF NOT EXISTS organizer_rule_position_idx ON organizer_rule (position);
