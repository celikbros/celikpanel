-- Catch-all: one address per domain that receives every message sent to an
-- otherwise-unknown mailbox at that domain. Stored as a plain destination;
-- pushed to postfix as a "@domain destination" virtual-alias line alongside
-- the explicit forwardings.
--
-- Catch-all: bir domain'de başka türlü tanınmayan bir posta kutusuna
-- gönderilen her iletiyi alan, domain başına tek adres. Düz bir hedef olarak
-- saklanır; açık yönlendirmelerin yanında postfix'e "@domain hedef" sanal
-- takma-ad satırı olarak itilir.
CREATE TABLE mail_catch_all (
    domain_id INTEGER PRIMARY KEY REFERENCES domains(id) ON DELETE CASCADE,
    destination TEXT NOT NULL,
    created_at TEXT DEFAULT (datetime('now'))
);
