CREATE TABLE IF NOT EXISTS support_submission_states (
    idempotency_hash BYTEA PRIMARY KEY CHECK (octet_length(idempotency_hash) = 32),
    cancelled_at TIMESTAMPTZ
);

INSERT INTO support_submission_states (idempotency_hash)
SELECT idempotency_hash FROM support_reports
ON CONFLICT (idempotency_hash) DO NOTHING;
