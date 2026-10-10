-- Optional per-user app-lock PIN. The PIN is stored only as a salted PBKDF2
-- hash; failed attempts are tracked here so brute-forcing a 6-digit PIN is
-- throttled server-side regardless of client behavior.
CREATE TABLE IF NOT EXISTS app_lock (
    user_id BIGINT PRIMARY KEY REFERENCES app_user(id) ON DELETE CASCADE,
    pin_hash TEXT NOT NULL,
    idle_minutes INTEGER NOT NULL DEFAULT 30,
    failed_attempts INTEGER NOT NULL DEFAULT 0,
    locked_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
