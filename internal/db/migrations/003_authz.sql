-- 003: Ownership hierarchy for role-based authorization (see docs/ROLES.md)
-- 003: Rol tabanlı yetkilendirme için sahiplik hiyerarşisi (bkz. docs/ROLES.md)
--
-- Adds the single hierarchy edge the ownership model needs: who created/owns
-- each user. Resellers own the customers they create (parent_id = reseller).
-- This is purely additive (one nullable column + index), so it is safe to
-- apply to a live database.
--
-- Sahiplik modelinin ihtiyaç duyduğu tek hiyerarşi kenarını ekler: her
-- kullanıcıyı kimin oluşturduğu/sahiplendiği. Bayiler oluşturdukları
-- müşterilerin sahibidir (parent_id = bayi). Tamamen eklemeli (bir nullable
-- sütun + indeks) olduğundan canlı veritabanına uygulamak güvenlidir.

ALTER TABLE users ADD COLUMN parent_id INTEGER REFERENCES users(id);

CREATE INDEX idx_users_parent ON users(parent_id);
