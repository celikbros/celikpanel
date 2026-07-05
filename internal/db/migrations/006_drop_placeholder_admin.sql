-- 006: Remove the placeholder admin seeded by 001.
-- 006: 001'in tohumladığı placeholder yöneticiyi kaldır.
--
-- Migration 001 seeds an 'admin' row with a fixed bcrypt hash that matches no
-- real password — a dead account nobody can log into. It exists only so the
-- schema has an owner for the default subscription, but it makes the panel's
-- "no users → run --create-admin" bootstrap think an admin already exists, so
-- a fresh install would skip creating a usable one.
--
-- We delete it by its exact known hash, so this touches ONLY the untouched
-- placeholder: any real admin (created via --create-admin, which rewrites the
-- hash) is left alone. Safe on both fresh and existing databases.
--
-- Migration 001, gerçek hiçbir parolayla eşleşmeyen sabit bir bcrypt hash'li
-- 'admin' satırı tohumlar — kimsenin giremeyeceği ölü bir hesap. Yalnızca
-- varsayılan aboneliğin bir sahibi olsun diye vardır, ama panelin "kullanıcı
-- yok → --create-admin çalıştır" önyüklemesini yanıltır; taze bir kurulum
-- kullanılabilir bir yönetici oluşturmayı atlar.
--
-- Onu tam bilinen hash'iyle sileriz; böylece YALNIZCA dokunulmamış
-- placeholder etkilenir: gerçek bir yönetici (hash'i yeniden yazan
-- --create-admin ile oluşturulmuş) korunur. Hem taze hem mevcut veritabanında
-- güvenlidir.

DELETE FROM users
WHERE username = 'admin'
  AND role = 'admin'
  AND password_hash = '$2a$10$rVQ8K5h6Z.Zg0qX7J3K3KuF7pB3vZ8mN9lD5qE0wY0kX0H0L0M0N0';
