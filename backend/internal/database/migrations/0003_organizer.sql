-- Keyword-based organizer rules (Admin > Organizer > Keywords), distinct
-- from media categories/tags: these move files by filename match, they
-- don't describe a MediaItem.
CREATE TABLE IF NOT EXISTS organizer_keyword (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    keywords TEXT[] NOT NULL DEFAULT '{}',
    target_dir TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
