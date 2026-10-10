-- Which sign-in plugin opened a session, so sign-out can use that provider's
-- logout page. NULL for ReelStash's own accounts.
ALTER TABLE web_session ADD COLUMN IF NOT EXISTS auth_plugin TEXT;
