-- Built-in username/password sign-in, for installs with no external identity
-- provider. Accounts created by a provider or plugin keep both columns NULL.
ALTER TABLE app_user ADD COLUMN IF NOT EXISTS username TEXT;
ALTER TABLE app_user ADD COLUMN IF NOT EXISTS password_hash TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS app_user_username_key ON app_user (lower(username)) WHERE username IS NOT NULL;
