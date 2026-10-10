-- Hourly disk capacity readings, for the storage usage-over-time charts.
CREATE TABLE IF NOT EXISTS storage_capacity_sample (
    storage_location_id BIGINT NOT NULL REFERENCES storage_location(id) ON DELETE CASCADE,
    sampled_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    total_bytes         BIGINT NOT NULL,
    used_bytes          BIGINT NOT NULL,
    available_bytes     BIGINT NOT NULL,
    PRIMARY KEY (storage_location_id, sampled_at)
);

-- Paths a scan skips. (0042 moves rules from disks to libraries.)
-- Files already indexed under an excluded path are left as they are.
CREATE TABLE IF NOT EXISTS scan_rule (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    storage_location_id BIGINT REFERENCES storage_location(id) ON DELETE CASCADE,
    pattern             TEXT NOT NULL CHECK (pattern <> ''),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Rules moved from disks to libraries in 0042, which also creates the unique index.
