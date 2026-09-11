-- Existing databases remain legacy; only a genuinely empty database is marked
-- fresh by NewSQLiteDB after migrations. / Mevcut veritabanlari eski kurulumdur.
CREATE TABLE server_setup_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version = 1),
    origin TEXT NOT NULL DEFAULT 'legacy' CHECK (origin IN ('fresh', 'legacy')),
    status TEXT NOT NULL DEFAULT 'legacy' CHECK (status IN ('new', 'legacy', 'draft', 'running', 'waiting', 'failed', 'ready')),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    draft_json TEXT NOT NULL DEFAULT '{}',
    completed_at TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO server_setup_state (id) VALUES (1);
