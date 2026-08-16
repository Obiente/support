CREATE TABLE IF NOT EXISTS support_report_messages (
    id uuid PRIMARY KEY,
    report_id uuid NOT NULL REFERENCES support_reports(id) ON DELETE CASCADE,
    author text NOT NULL CHECK (author IN ('maintainer', 'reporter')),
    body_ciphertext bytea NOT NULL,
    created_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS support_report_messages_report_created_idx
    ON support_report_messages (report_id, created_at, id);
