-- Reviewed plans are immutable. Executions carry their child identities before
-- any privileged work, so a lost HTTP response never creates a second install.
CREATE TABLE server_setup_plans (
    id TEXT PRIMARY KEY,
    revision INTEGER NOT NULL,
    plan_json TEXT NOT NULL,
    requested_by INTEGER NOT NULL REFERENCES users(id),
    created_at TEXT NOT NULL
);
CREATE TABLE server_setup_executions (
    id TEXT PRIMARY KEY,
    request_id TEXT NOT NULL UNIQUE,
    plan_id TEXT NOT NULL REFERENCES server_setup_plans(id),
    status TEXT NOT NULL CHECK(status IN ('running', 'waiting', 'succeeded', 'failed')),
    execution_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX server_setup_one_active_execution
    ON server_setup_executions((1)) WHERE status IN ('running', 'waiting');
