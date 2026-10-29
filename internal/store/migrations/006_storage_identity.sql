-- Binds the database to the private object volume it was written against.
-- The single row holds the storage ID that must match <object root>/.storage-id
-- before the service starts. Idempotent because migrations run on each startup.
CREATE TABLE IF NOT EXISTS support_storage_identity (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    storage_id TEXT NOT NULL CHECK (storage_id ~ '^[0-9a-f]{64}$'),
    bound_at TIMESTAMPTZ NOT NULL
);
