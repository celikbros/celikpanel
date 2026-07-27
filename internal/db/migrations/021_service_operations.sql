-- Durable package mutations. HTTP requests enqueue an operation and return;
-- the panel records every phase so reconnects can poll one source of truth.
-- A process restart cannot prove that an interrupted package command reached
-- its verified end state, so startup recovery marks active rows failed.
CREATE TABLE service_operations (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    service_id TEXT NOT NULL,
    package_name TEXT,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    phase TEXT NOT NULL,
    result_json TEXT,
    error_code TEXT,
    error_message TEXT,
    requested_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    request_ip TEXT,
    user_agent TEXT,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- There is one machine package database. Two concurrent mutations can corrupt
-- package-manager state even when they target different services.
CREATE UNIQUE INDEX idx_service_operations_one_active
    ON service_operations((1))
    WHERE status IN ('queued', 'running');

CREATE INDEX idx_service_operations_recent
    ON service_operations(started_at DESC);
