-- Downloads (qBittorrent): folders ReelStash never touches, move rules for
-- finished torrents, and what happened to each finished torrent.

-- Locked folders: scans, the watcher, the organizer, housekeeping and Files
-- leave everything inside one alone. owner says which feature added it.
CREATE TABLE IF NOT EXISTS protected_path (
    path       TEXT PRIMARY KEY,
    owner      TEXT NOT NULL DEFAULT 'downloads',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Checked top to bottom; the first enabled rule whose conditions all hold
-- moves the finished torrent (through qBittorrent, so it keeps seeding).
CREATE TABLE IF NOT EXISTS download_rule (
    id                       BIGSERIAL PRIMARY KEY,
    name                     TEXT NOT NULL,
    position                 INT NOT NULL DEFAULT 0,
    enabled                  BOOLEAN NOT NULL DEFAULT true,
    match_category           TEXT NOT NULL DEFAULT '',
    match_tag                TEXT NOT NULL DEFAULT '',
    match_tracker            TEXT NOT NULL DEFAULT '',
    match_name               TEXT NOT NULL DEFAULT '',
    min_size_mb              BIGINT NOT NULL DEFAULT 0,
    video_only               BOOLEAN NOT NULL DEFAULT false,
    dest_storage_location_id BIGINT NOT NULL REFERENCES storage_location(id) ON DELETE CASCADE,
    dest_path                TEXT NOT NULL DEFAULT '',
    subfolder                TEXT NOT NULL DEFAULT '',
    scan_after               BOOLEAN NOT NULL DEFAULT true,
    remove_after             TEXT NOT NULL DEFAULT 'never' CHECK (remove_after IN ('never', 'moved', 'seeded')),
    remove_ratio             DOUBLE PRECISION NOT NULL DEFAULT 0,
    remove_seed_minutes      BIGINT NOT NULL DEFAULT 0,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per finished torrent ReelStash has looked at, so a rule runs once.
CREATE TABLE IF NOT EXISTS download_move (
    hash         TEXT PRIMARY KEY,
    name         TEXT NOT NULL DEFAULT '',
    rule_id      BIGINT REFERENCES download_rule(id) ON DELETE SET NULL,
    rule_name    TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL CHECK (status IN ('MOVING', 'MOVED', 'FAILED', 'SKIPPED', 'REMOVED')),
    from_path    TEXT NOT NULL DEFAULT '',
    dest_path    TEXT NOT NULL DEFAULT '',
    message      TEXT NOT NULL DEFAULT '',
    scan_pending BOOLEAN NOT NULL DEFAULT false,
    library_id   BIGINT REFERENCES library(id) ON DELETE SET NULL,
    remove_after TEXT NOT NULL DEFAULT 'never',
    remove_ratio DOUBLE PRECISION NOT NULL DEFAULT 0,
    remove_seed_minutes BIGINT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    moved_at     TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS download_move_status_idx ON download_move (status, updated_at DESC);
