-- One-click application installation is a durable saga. The row is written
-- before any privileged RPC and survives ambiguous transport failures so an
-- operator can see and reconcile every reserved database and published site.
CREATE TABLE application_install_operations (
    operation_id TEXT PRIMARY KEY
        CHECK (
            length(operation_id) = 32
            AND operation_id NOT GLOB '*[^a-f0-9]*'
        ),
    app_id TEXT NOT NULL CHECK (app_id = 'wordpress'),
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    subscription_id INTEGER NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    site_id INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    database_server_id INTEGER NOT NULL REFERENCES database_servers(id),
    database_name TEXT NOT NULL,
    database_user TEXT NOT NULL,
    cleanup_token_encrypted TEXT NOT NULL,
    status TEXT NOT NULL CHECK (
        status IN (
            'reserved',
            'database_creating',
            'database_ready',
            'files_installing',
            'applied',
            'failed',
            'needs_review'
        )
    ),
    last_error TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_application_install_operations_status
    ON application_install_operations(status, updated_at);

CREATE INDEX idx_application_install_operations_domain
    ON application_install_operations(domain_id, app_id, created_at);
