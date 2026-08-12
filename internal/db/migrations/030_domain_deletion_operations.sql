-- Domain deletion is a durable, forward-only saga. This row preserves the
-- pre-deletion status while the domain ledger remains the retry handle.
CREATE TABLE domain_deletion_operations (
    domain_id INTEGER PRIMARY KEY REFERENCES domains(id) ON DELETE CASCADE,
    previous_status TEXT NOT NULL CHECK (
        previous_status IN ('active', 'suspended', 'pending')
    ),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
