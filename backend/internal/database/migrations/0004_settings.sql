-- Runtime-editable settings managed from Admin > Settings, distinct from
-- process env vars: these take effect immediately without a redeploy.
CREATE TABLE IF NOT EXISTS app_setting (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
