-- Libraries group folders on the disks into what users browse. A library is a
-- set of roots (a disk, optionally narrowed to a subfolder); a file belongs to
-- the library whose root is the longest match for its path, and a title to the
-- library of its (first available) file. Titles outside every library are only
-- visible in the admin console.
CREATE TABLE IF NOT EXISTS library (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name           TEXT NOT NULL UNIQUE CHECK (name <> ''),
    -- 'entertainment': movies and TV shows, cast and crew.
    -- 'private': videos, performers, studios and series.
    mode           TEXT NOT NULL DEFAULT 'entertainment' CHECK (mode IN ('entertainment', 'private')),
    -- Granted to every user created from now on.
    default_access BOOLEAN NOT NULL DEFAULT FALSE,
    position       INTEGER NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- path is relative to the disk's mount path, without leading or trailing
-- slashes; '' is the whole disk.
CREATE TABLE IF NOT EXISTS library_root (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    library_id          BIGINT NOT NULL REFERENCES library(id) ON DELETE CASCADE,
    storage_location_id BIGINT NOT NULL REFERENCES storage_location(id) ON DELETE CASCADE,
    path                TEXT NOT NULL DEFAULT '' CHECK (path !~ '^/' AND path !~ '/$'),
    UNIQUE (storage_location_id, path)
);

CREATE TABLE IF NOT EXISTS library_access (
    user_id    BIGINT NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    library_id BIGINT NOT NULL REFERENCES library(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, library_id)
);
CREATE INDEX IF NOT EXISTS library_access_library ON library_access (library_id);

-- The metadata providers a library matches with, in priority order. A
-- provider without a row is off for that library.
CREATE TABLE IF NOT EXISTS library_provider (
    library_id BIGINT NOT NULL REFERENCES library(id) ON DELETE CASCADE,
    provider   TEXT NOT NULL,
    position   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (library_id, provider)
);

ALTER TABLE media_file ADD COLUMN IF NOT EXISTS library_id BIGINT REFERENCES library(id) ON DELETE SET NULL;
ALTER TABLE media_item ADD COLUMN IF NOT EXISTS library_id BIGINT REFERENCES library(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS media_file_library ON media_file (library_id);
CREATE INDEX IF NOT EXISTS media_item_library ON media_item (library_id);

CREATE OR REPLACE FUNCTION library_for_file(loc BIGINT, rel TEXT) RETURNS BIGINT AS $$
    SELECT lr.library_id FROM library_root lr
    WHERE lr.storage_location_id = loc
      AND (lr.path = '' OR rel = lr.path OR left(rel, length(lr.path) + 1) = lr.path || '/')
    ORDER BY length(lr.path) DESC, lr.id
    LIMIT 1
$$ LANGUAGE sql STABLE;

CREATE OR REPLACE FUNCTION library_for_item(item BIGINT) RETURNS BIGINT AS $$
    SELECT mf.library_id FROM media_file mf
    WHERE mf.media_item_id = item
    ORDER BY mf.available DESC, mf.library_id IS NULL, mf.id
    LIMIT 1
$$ LANGUAGE sql STABLE;

-- Keep the columns right whenever a file is added, moved, renamed, reassigned
-- to another title or goes missing, so scanner/organizer/rename code never has
-- to think about libraries.
CREATE OR REPLACE FUNCTION media_file_library_assign() RETURNS trigger AS $$
BEGIN
    NEW.library_id := library_for_file(NEW.storage_location_id, NEW.relative_path);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS media_file_library_assign ON media_file;
CREATE TRIGGER media_file_library_assign BEFORE INSERT OR UPDATE OF storage_location_id, relative_path ON media_file
    FOR EACH ROW EXECUTE FUNCTION media_file_library_assign();

CREATE OR REPLACE FUNCTION media_item_library_sync() RETURNS trigger AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        UPDATE media_item SET library_id = library_for_item(OLD.media_item_id)
        WHERE id = OLD.media_item_id AND library_id IS DISTINCT FROM library_for_item(OLD.media_item_id);
    END IF;
    IF TG_OP <> 'DELETE' THEN
        UPDATE media_item SET library_id = library_for_item(NEW.media_item_id)
        WHERE id = NEW.media_item_id AND library_id IS DISTINCT FROM library_for_item(NEW.media_item_id);
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS media_file_item_library_sync ON media_file;
-- (UPDATE OF only sees columns in the statement's SET list, not ones the
-- BEFORE trigger changed, so path changes are listed too.)
CREATE TRIGGER media_file_item_library_sync AFTER INSERT OR DELETE OR UPDATE OF library_id, media_item_id, available, storage_location_id, relative_path ON media_file
    FOR EACH ROW EXECUTE FUNCTION media_item_library_sync();

-- Recomputes every file and title; run after library roots change.
CREATE OR REPLACE FUNCTION reassign_libraries() RETURNS void AS $$
BEGIN
    UPDATE media_file SET library_id = library_for_file(storage_location_id, relative_path)
    WHERE library_id IS DISTINCT FROM library_for_file(storage_location_id, relative_path);
    UPDATE media_item SET library_id = library_for_item(id)
    WHERE library_id IS DISTINCT FROM library_for_item(id);
END;
$$ LANGUAGE plpgsql;

-- New users get the libraries marked for default access.
CREATE OR REPLACE FUNCTION app_user_default_libraries() RETURNS trigger AS $$
BEGIN
    INSERT INTO library_access (user_id, library_id)
    SELECT NEW.id, id FROM library WHERE default_access
    ON CONFLICT DO NOTHING;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS app_user_default_libraries ON app_user;
CREATE TRIGGER app_user_default_libraries AFTER INSERT ON app_user
    FOR EACH ROW EXECUTE FUNCTION app_user_default_libraries();

-- One-time upgrade of an existing install: every disk goes into a "Private"
-- library (the only kind there was), open to every user and to new ones, with
-- the providers that were configured. A fresh install starts with none.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM app_setting WHERE key = 'libraries.migrated') THEN
        IF EXISTS (SELECT 1 FROM storage_location) AND NOT EXISTS (SELECT 1 FROM library) THEN
            INSERT INTO library (name, mode, default_access) VALUES ('Private', 'private', TRUE);
            INSERT INTO library_root (library_id, storage_location_id, path)
            SELECT (SELECT id FROM library WHERE name = 'Private'), id, '' FROM storage_location;
            INSERT INTO library_access (user_id, library_id)
            SELECT u.id, l.id FROM app_user u CROSS JOIN library l WHERE l.name = 'Private';
            INSERT INTO library_provider (library_id, provider, position)
            SELECT (SELECT id FROM library WHERE name = 'Private'), p.id,
                   COALESCE((SELECT o.pos FROM app_setting s, jsonb_array_elements_text(s.value::jsonb) WITH ORDINALITY AS o(id, pos)
                             WHERE s.key = 'provider_order' AND o.id = p.id), p.pos + 100)
            FROM (VALUES ('tmdb', 0)) AS p(id, pos);
        END IF;
        INSERT INTO app_setting (key, value) VALUES ('libraries.migrated', 'true');
    END IF;
END $$;

SELECT reassign_libraries();
