-- Candidates a metadata provider suggested for a title that automatic matching
-- did not apply. They wait here for the Review Queue's Matches tab.
CREATE TABLE IF NOT EXISTS media_match_suggestion (
    media_item_id BIGINT PRIMARY KEY REFERENCES media_item(id) ON DELETE CASCADE,
    candidates    JSONB NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
