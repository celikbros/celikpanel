-- One row per file the Panel writes for a site (D-031). This step records the
-- nginx vhost; the PHP-FPM pool and the application unit follow under their
-- own kinds with the same states.
--
-- The file itself is the authority for "is this still CelikPanel's text": its
-- first line, `# celikpanel-render v2 sha256=<hex>`, declares the digest of the
-- body below it, and it survives a database restore. This row holds what the
-- Panel last wrote (body_sha256, written_release, written_at), what it last
-- observed (state, file_sha256, observed_at), where its own text is held
-- beside a kept file (pending_path) and the owner's last decision. A row that
-- disagrees with the file's header loses: the next render re-classifies from
-- the file. Rows of files written before D-031 are created at the first start
-- that classifies them; this migration creates no row and touches no file.
CREATE TABLE managed_site_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    site_id INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK(kind IN ('nginx_vhost', 'php_pool', 'app_unit')),
    path TEXT NOT NULL CHECK(length(path) BETWEEN 1 AND 4096),
    file_format TEXT NOT NULL DEFAULT '' CHECK(file_format IN ('', 'celikpanel-render v2')),
    body_sha256 TEXT NOT NULL DEFAULT '' CHECK(body_sha256 = '' OR length(body_sha256) = 64),
    written_release TEXT NOT NULL DEFAULT '',
    written_at INTEGER NOT NULL DEFAULT 0,
    adopted_from TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK(state IN (
        'absent', 'managed_unchanged', 'owner_edited', 'foreign',
        'unreadable', 'unknown_origin', 'missing', 'unknown'
    )),
    state_reason TEXT NOT NULL DEFAULT '',
    file_sha256 TEXT NOT NULL DEFAULT '' CHECK(file_sha256 = '' OR length(file_sha256) = 64),
    observed_at INTEGER NOT NULL DEFAULT 0,
    pending_path TEXT NOT NULL DEFAULT '',
    pending_sha256 TEXT NOT NULL DEFAULT '' CHECK(pending_sha256 = '' OR length(pending_sha256) = 64),
    backup_path TEXT NOT NULL DEFAULT '',
    decision TEXT NOT NULL DEFAULT '' CHECK(decision IN ('', 'keep_mine', 'take_celikpanel', 'recreate')),
    decision_file_sha256 TEXT NOT NULL DEFAULT '' CHECK(decision_file_sha256 = '' OR length(decision_file_sha256) = 64),
    decided_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    decided_at INTEGER NOT NULL DEFAULT 0,
    UNIQUE(site_id, kind, path)
);
CREATE INDEX managed_site_files_domain ON managed_site_files(domain_id);
