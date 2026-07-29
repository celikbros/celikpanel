-- VPN peer mutations are desired-state operations. A revoked row remains in
-- the ledger until the agent confirms that the peer is absent from WireGuard.
-- VPN peer değişiklikleri istenen durum işlemleridir. İptal edilen bir satır,
-- agent peer'ın WireGuard'dan kaldırıldığını doğrulayana kadar defterde kalır.
ALTER TABLE vpn_peers
    ADD COLUMN desired_state TEXT NOT NULL DEFAULT 'active'
        CHECK (desired_state IN ('active', 'revoked'));

ALTER TABLE vpn_peers
    ADD COLUMN sync_state TEXT NOT NULL DEFAULT 'applied'
        CHECK (sync_state IN ('pending', 'applied', 'error'));

ALTER TABLE vpn_peers
    ADD COLUMN sync_error TEXT;

ALTER TABLE vpn_peers
    ADD COLUMN updated_at TEXT NOT NULL DEFAULT '';

-- Rows created before their one-time client configuration is delivered remain
-- provisioning. Startup recovery revokes them rather than leaving ghost access.
ALTER TABLE vpn_peers
    ADD COLUMN provisioning_state TEXT NOT NULL DEFAULT 'issued'
        CHECK (provisioning_state IN ('provisioning', 'issued'));

-- The panel never stores the one-time client configuration. It stores only a
-- short-lived hash used to prove that the browser received it.
-- Panel tek kullanımlık istemci yapılandırmasını asla saklamaz. Yalnızca
-- tarayıcının yapılandırmayı aldığını kanıtlayan kısa ömürlü özeti saklar.
ALTER TABLE vpn_peers
    ADD COLUMN delivery_token_hash TEXT;

ALTER TABLE vpn_peers
    ADD COLUMN delivery_expires_at TEXT;

CREATE INDEX idx_vpn_peers_desired_sync
    ON vpn_peers(desired_state, sync_state);

-- One durable row serializes whole-server WireGuard snapshots across panel
-- processes. Row-level triggers advance the generation whenever desired state
-- changes, so a stale RPC result can never be recorded as current.
-- Tek kalıcı satır, sunucu geneli WireGuard anlık görüntülerini panel süreçleri
-- arasında sıralar. Satır tetikleyicileri istenen durum değiştiğinde nesli
-- ilerletir; böylece eski bir RPC sonucu güncelmiş gibi kaydedilemez.
CREATE TABLE vpn_sync_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    desired_generation INTEGER NOT NULL DEFAULT 0,
    applied_generation INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'applied', 'error')),
    last_error TEXT,
    lease_token TEXT,
    lease_expires_at TEXT,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO vpn_sync_state (
    id, desired_generation, applied_generation, status, updated_at
) VALUES (1, 0, 0, 'applied', datetime('now'));

CREATE TRIGGER vpn_peers_sync_insert
AFTER INSERT ON vpn_peers
BEGIN
    UPDATE vpn_sync_state
    SET desired_generation = desired_generation + 1,
        status = 'pending',
        last_error = NULL,
        updated_at = datetime('now')
    WHERE id = 1;
END;

CREATE TRIGGER vpn_peers_sync_update
AFTER UPDATE OF public_key, preshared_key, ip, subscription_id, desired_state
ON vpn_peers
WHEN OLD.public_key IS NOT NEW.public_key
  OR OLD.preshared_key IS NOT NEW.preshared_key
  OR OLD.ip IS NOT NEW.ip
  OR OLD.subscription_id IS NOT NEW.subscription_id
  OR OLD.desired_state IS NOT NEW.desired_state
BEGIN
    UPDATE vpn_sync_state
    SET desired_generation = desired_generation + 1,
        status = 'pending',
        last_error = NULL,
        updated_at = datetime('now')
    WHERE id = 1;
END;

CREATE TRIGGER vpn_peers_sync_delete
AFTER DELETE ON vpn_peers
WHEN OLD.desired_state = 'active'
BEGIN
    UPDATE vpn_sync_state
    SET desired_generation = desired_generation + 1,
        status = 'pending',
        last_error = NULL,
        updated_at = datetime('now')
    WHERE id = 1;
END;

CREATE TRIGGER vpn_entitlements_sync_insert
AFTER INSERT ON subscription_entitlements
WHEN NEW.product_id = 'vpn'
BEGIN
    UPDATE vpn_sync_state
    SET desired_generation = desired_generation + 1,
        status = 'pending',
        last_error = NULL,
        updated_at = datetime('now')
    WHERE id = 1;
END;

CREATE TRIGGER vpn_entitlements_sync_update
AFTER UPDATE OF status, expires_at ON subscription_entitlements
WHEN NEW.product_id = 'vpn'
  AND (OLD.status IS NOT NEW.status OR OLD.expires_at IS NOT NEW.expires_at)
BEGIN
    UPDATE vpn_sync_state
    SET desired_generation = desired_generation + 1,
        status = 'pending',
        last_error = NULL,
        updated_at = datetime('now')
    WHERE id = 1;
END;

CREATE TRIGGER vpn_entitlements_sync_delete
AFTER DELETE ON subscription_entitlements
WHEN OLD.product_id = 'vpn'
BEGIN
    UPDATE vpn_sync_state
    SET desired_generation = desired_generation + 1,
        status = 'pending',
        last_error = NULL,
        updated_at = datetime('now')
    WHERE id = 1;
END;

CREATE TRIGGER vpn_offering_sync_update
AFTER UPDATE OF release_state, entitlement_mode ON store_offerings
WHEN NEW.id = 'vpn'
  AND (
    OLD.release_state IS NOT NEW.release_state
    OR OLD.entitlement_mode IS NOT NEW.entitlement_mode
  )
BEGIN
    UPDATE vpn_sync_state
    SET desired_generation = desired_generation + 1,
        status = 'pending',
        last_error = NULL,
        updated_at = datetime('now')
    WHERE id = 1;
END;
