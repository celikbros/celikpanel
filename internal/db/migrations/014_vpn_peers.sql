-- VPN peers: the panel-side ledger of issued WireGuard peers. The client
-- private key is never stored — it is shown once at creation. The preshared
-- key must be stored because the server config needs it on every sync.
-- VPN peer'ları: verilmiş WireGuard peer'larının panel tarafı defteri.
-- İstemci özel anahtarı asla saklanmaz — oluşturmada bir kez gösterilir.
-- Ön-paylaşımlı anahtar saklanmak zorundadır; sunucu config'i her senkronda
-- ona ihtiyaç duyar.
CREATE TABLE IF NOT EXISTS vpn_peers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    subscription_id INTEGER REFERENCES subscriptions(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    public_key TEXT NOT NULL UNIQUE,
    preshared_key TEXT NOT NULL,
    ip TEXT NOT NULL UNIQUE,
    created_by INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_vpn_peers_subscription ON vpn_peers(subscription_id);
