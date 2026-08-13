CREATE TABLE IF NOT EXISTS support_admin_sessions (
    token_hash BYTEA PRIMARY KEY,
    csrf_hash BYTEA NOT NULL,
    username TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS support_admin_sessions_expiry
    ON support_admin_sessions (expires_at);

CREATE TABLE IF NOT EXISTS support_admin_audit (
    id BIGSERIAL PRIMARY KEY,
    username TEXT NOT NULL,
    action TEXT NOT NULL,
    report_id UUID,
    remote_hash BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS support_admin_audit_created
    ON support_admin_audit (created_at DESC);
