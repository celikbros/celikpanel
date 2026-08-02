-- Monotonic credential epoch used to invalidate in-flight authentication.
-- Password, suspension and two-factor mutations increment this value.
ALTER TABLE users ADD COLUMN auth_epoch INTEGER NOT NULL DEFAULT 0;
