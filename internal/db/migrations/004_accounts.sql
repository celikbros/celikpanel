-- 004: Account management — service plans, plan-bound subscriptions,
-- user suspension (see docs/ROLES.md and task: account management sprint).
-- 004: Hesap yönetimi — servis planları, plana bağlı abonelikler, kullanıcı
-- askıya alma (bkz. docs/ROLES.md ve görev: hesap yönetimi sprinti).
--
-- Additive only: one new table plus nullable/defaulted columns, safe on a
-- live database. project note: no CHECK constraints on new enum-ish columns
-- (SQLite cannot alter a CHECK later); validation lives in code.
-- Yalnızca eklemeli: bir yeni tablo artı nullable/varsayılanlı kolonlar;
-- canlı veritabanında güvenli. Not: yeni enum-vari kolonlarda CHECK yok
-- (SQLite CHECK'i sonradan değiştiremez); doğrulama koddadır.

-- Service plans are reusable quota templates. owner_id NULL = a global plan
-- created by the administrator; reseller-owned plans become possible later
-- without a schema change.
-- Servis planları yeniden kullanılabilir kota şablonlarıdır. owner_id NULL =
-- yöneticinin oluşturduğu genel plan; bayiye ait planlar ileride şema
-- değişikliği olmadan mümkün olur.
CREATE TABLE service_plans (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_id INTEGER REFERENCES users(id),
    name TEXT NOT NULL,
    max_domains INTEGER NOT NULL DEFAULT 5,
    max_databases INTEGER NOT NULL DEFAULT 10,
    max_email_accounts INTEGER NOT NULL DEFAULT 50,
    disk_quota_mb INTEGER NOT NULL DEFAULT 10240,
    bandwidth_quota_mb INTEGER NOT NULL DEFAULT 102400,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX idx_service_plans_owner ON service_plans(owner_id);

-- A subscription may be born from a plan; NULL keeps ad-hoc subscriptions
-- (like the seeded admin one) valid.
-- Bir abonelik bir plandan doğabilir; NULL, elle oluşturulmuş abonelikleri
-- (seed'lenen admin aboneliği gibi) geçerli tutar.
ALTER TABLE subscriptions ADD COLUMN plan_id INTEGER REFERENCES service_plans(id);
CREATE INDEX idx_subscriptions_plan ON subscriptions(plan_id);

-- User lifecycle: active | suspended (validated in code).
-- Kullanıcı yaşam döngüsü: active | suspended (kodda doğrulanır).
ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'active';
