-- Authentication sessions.
-- Kimlik doğrulama oturumları.
--
-- We store only a hash of the session token, never the token itself, so a
-- read of this table does not let an attacker impersonate a live session.
--
-- Oturum jetonunun yalnızca özetini saklarız, asla jetonun kendisini değil;
-- böylece bu tablonun okunması bir saldırganın canlı bir oturumu taklit
-- etmesine izin vermez.
CREATE TABLE sessions (
    token_hash  TEXT PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);
