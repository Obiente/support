CREATE TABLE IF NOT EXISTS support_reports (
    id UUID PRIMARY KEY,
    support_code TEXT NOT NULL UNIQUE,
    product_id TEXT NOT NULL,
    request_type TEXT NOT NULL CHECK (request_type IN ('bug', 'feature', 'support')),
    status TEXT NOT NULL CHECK (status IN ('new', 'needs_information', 'accepted', 'duplicate', 'resolved', 'rejected')),
    private_payload BYTEA NOT NULL,
    capability_ciphertext BYTEA NOT NULL,
    diagnostic_object_key TEXT,
    idempotency_hash BYTEA NOT NULL UNIQUE,
    request_hash BYTEA NOT NULL,
    capability_hash BYTEA NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    retention_until TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS support_reports_queue
    ON support_reports (product_id, status, created_at)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS support_reports_retention
    ON support_reports (retention_until)
    WHERE deleted_at IS NULL;
