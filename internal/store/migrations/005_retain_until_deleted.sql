-- Reports are kept until a reporter or maintainer deletes them. The column is
-- kept for compatibility, but no report carries a time-based deletion date and
-- the purge only removes rows whose deleted_at is set. Every statement is
-- idempotent because migrations run on each startup.
ALTER TABLE support_reports ALTER COLUMN retention_until DROP NOT NULL;

UPDATE support_reports SET retention_until = NULL WHERE retention_until IS NOT NULL;

CREATE INDEX IF NOT EXISTS support_reports_deleted
    ON support_reports (deleted_at)
    WHERE deleted_at IS NOT NULL;
