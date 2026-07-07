-- Product entitlements: the ledger of what a subscription is allowed BEYOND
-- its plan quotas — the add-ons a provider grants or sells (a firewall, a
-- business-email tier, the app installer, an extra IP…). Products themselves
-- are a curated catalogue in code (like managed services and apps); only the
-- grants live here. Payment is deliberately out of scope — billing is an
-- external system (Stripe/WHMCS); this table records the RIGHT, not the money.
--
-- Ürün hakları (entitlement): bir aboneliğin plan kotalarının ÖTESİNDE neye
-- izinli olduğunun defteri — sağlayıcının verdiği ya da sattığı eklentiler
-- (firewall, iş-e-postası kademesi, uygulama kurucu, ek IP…). Ürünlerin
-- kendisi kodda kürlü bir katalogdur (yönetilen servisler ve uygulamalar
-- gibi); yalnız haklar burada yaşar. Ödeme bilerek kapsam dışıdır —
-- faturalandırma dış bir sistemdir (Stripe/WHMCS); bu tablo HAKKI kaydeder,
-- parayı değil.
CREATE TABLE subscription_entitlements (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    subscription_id INTEGER NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    product_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    granted_at TEXT DEFAULT (datetime('now')),
    expires_at TEXT,
    UNIQUE(subscription_id, product_id)
);

CREATE INDEX idx_entitlements_subscription ON subscription_entitlements(subscription_id);
