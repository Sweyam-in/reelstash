-- Server-side sessions for plugin-authenticated (and emergency local) users.
CREATE TABLE IF NOT EXISTS web_session (
    id TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS web_session_expires ON web_session (expires_at);
