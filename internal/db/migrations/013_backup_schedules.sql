-- Automatic scheduled backups. Until now a backup only ran when someone
-- clicked "back up"; a real host takes them on a schedule so a lost site can
-- be recovered even if nobody remembered. One schedule per domain: how often,
-- what to include, and how many copies to keep (older ones are pruned).
--
-- Otomatik zamanlanmış yedekler. Şimdiye dek yedek yalnız biri "yedekle"
-- deyince koşuyordu; gerçek bir barındırıcı bunları bir zamanlamayla alır ki
-- kaybolan bir site kimse hatırlamasa bile geri getirilebilsin. Domain başına
-- tek zamanlama: ne sıklıkla, neyi içerecek ve kaç kopya tutulacak (eskiler
-- budanır).
CREATE TABLE backup_schedules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    frequency TEXT NOT NULL CHECK (frequency IN ('daily', 'weekly')),
    backup_type TEXT NOT NULL DEFAULT 'files' CHECK (backup_type IN ('files', 'full')),
    retention INTEGER NOT NULL DEFAULT 7,
    enabled INTEGER NOT NULL DEFAULT 1,
    last_run TEXT,
    created_at TEXT DEFAULT (datetime('now')),
    UNIQUE(domain_id)
);
