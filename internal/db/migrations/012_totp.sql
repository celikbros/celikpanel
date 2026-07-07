-- Two-factor authentication (TOTP). A per-user base32 secret and an enabled
-- flag. The secret is set when a user starts enrollment but 2FA is not
-- enforced until they verify a code (totp_enabled = 1), so a half-finished
-- setup never locks anyone out.
--
-- İki faktörlü doğrulama (TOTP). Kullanıcı başına base32 gizli anahtar ve bir
-- etkin bayrağı. Anahtar, kullanıcı kaydolmaya başlayınca ayarlanır ama 2FA,
-- bir kod doğrulanana dek (totp_enabled = 1) zorlanmaz; böylece yarım kalmış
-- bir kurulum kimseyi dışarıda bırakmaz.
ALTER TABLE users ADD COLUMN totp_secret TEXT;
ALTER TABLE users ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0;
