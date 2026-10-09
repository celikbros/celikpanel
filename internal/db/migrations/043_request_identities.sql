-- One row per state-changing request that carried an identity (D-029). A
-- browser that re-sends a request by itself, or a page that asks again after
-- a lost answer, is answered from this row; the request never runs twice.
--
-- The row never holds the request body, only its SHA-256. An answer that
-- carries a one-time secret, or one larger than 64 KiB, is not kept:
-- response_retained stays 0 and only the status code is recorded.
--
-- Rows expire after 24 hours and are removed by the hourly sweep. The table
-- holds no owner data: a snapshot restore that loses it is harmless.
CREATE TABLE request_identities (
    id TEXT PRIMARY KEY CHECK(length(id) = 32),
    actor_user_id INTEGER NOT NULL,
    method TEXT NOT NULL,
    route TEXT NOT NULL,
    request_sha256 TEXT NOT NULL CHECK(length(request_sha256) = 64),
    status TEXT NOT NULL CHECK(status IN ('running', 'done', 'interrupted')),
    response_status INTEGER NOT NULL DEFAULT 0,
    response_retained INTEGER NOT NULL DEFAULT 0 CHECK(response_retained IN (0, 1)),
    response_content_type TEXT NOT NULL DEFAULT '',
    response_body BLOB,
    created_at INTEGER NOT NULL,
    finished_at INTEGER NOT NULL DEFAULT 0,
    expires_at INTEGER NOT NULL
);
CREATE INDEX request_identities_expiry ON request_identities(expires_at);
