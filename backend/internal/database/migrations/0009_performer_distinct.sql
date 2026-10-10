-- Pairs of performers the user confirmed are different people, so they stop
-- showing up as possible duplicates.
CREATE TABLE IF NOT EXISTS performer_distinct (
    a_id BIGINT NOT NULL REFERENCES performer(id) ON DELETE CASCADE,
    b_id BIGINT NOT NULL REFERENCES performer(id) ON DELETE CASCADE,
    PRIMARY KEY (a_id, b_id),
    CHECK (a_id < b_id)
);
