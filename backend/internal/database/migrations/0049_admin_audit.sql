-- Admin audit trail: one row per successful admin change (who, what, to what, and why),
-- and a short history of webhook deliveries.

CREATE TABLE IF NOT EXISTS admin_audit_event (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_id BIGINT,
    actor_name TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL,
    method TEXT NOT NULL DEFAULT '',
    route TEXT NOT NULL DEFAULT '',
    target_kind TEXT NOT NULL DEFAULT '',
    target_id BIGINT,
    target_label TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    detail JSONB
);
CREATE INDEX IF NOT EXISTS admin_audit_event_at ON admin_audit_event (at DESC, id DESC);
CREATE INDEX IF NOT EXISTS admin_audit_event_target ON admin_audit_event (target_kind, target_id);
CREATE INDEX IF NOT EXISTS admin_audit_event_actor ON admin_audit_event (actor_id);

CREATE TABLE IF NOT EXISTS webhook_delivery (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    webhook_id BIGINT NOT NULL REFERENCES webhook(id) ON DELETE CASCADE,
    at TIMESTAMPTZ NOT NULL DEFAULT now(),
    kind TEXT NOT NULL DEFAULT '',
    status INTEGER,
    error TEXT,
    duration_ms INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS webhook_delivery_hook ON webhook_delivery (webhook_id, id DESC);
