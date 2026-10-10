-- Device sessions: apps (Android, later iOS and TV) sign in with a bearer token
-- instead of a cookie. They are ordinary web_session rows with a device name, so
-- people can see and revoke them. public_id names a session without exposing
-- its secret id.
ALTER TABLE web_session ADD COLUMN IF NOT EXISTS public_id BIGINT GENERATED ALWAYS AS IDENTITY;
ALTER TABLE web_session ADD COLUMN IF NOT EXISTS device_name TEXT;
ALTER TABLE web_session ADD COLUMN IF NOT EXISTS device_platform TEXT;
ALTER TABLE web_session ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ;
CREATE UNIQUE INDEX IF NOT EXISTS web_session_public_id ON web_session (public_id);

-- Device pairing: an app shows a short code, a signed-in person enters it in
-- the web UI, and the app's next poll receives a device session. device_code
-- is the app's secret; user_code is what the person types.
CREATE TABLE IF NOT EXISTS device_pairing (
    device_code TEXT PRIMARY KEY,
    user_code TEXT NOT NULL UNIQUE,
    device_name TEXT NOT NULL,
    device_platform TEXT NOT NULL,
    approved_user_id BIGINT REFERENCES app_user(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS device_pairing_expires ON device_pairing (expires_at);
