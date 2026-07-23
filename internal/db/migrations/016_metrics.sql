-- Monitoring history (operator request, 23 Jul: "we should have a server
-- monitoring page"). The dashboard's health strip answers "how is it NOW";
-- this table answers "what happened while I slept". One row per 30s sample,
-- pruned at 48h — enough to see last night's spike, small enough to never
-- matter on disk (~5760 rows/day).
--
-- İzleme geçmişi (operatör isteği, 23 Tem: "sunucu monitöring sayfamız da
-- olsun"). Panonun sağlık şeridi "ŞU AN nasıl" sorusuna cevap verir; bu tablo
-- "ben uyurken ne oldu" sorusuna. 30 saniyede bir satır, 48 saatte budanır —
-- dün geceki sıçramayı gösterecek kadar uzun, diskte hiç hissedilmeyecek
-- kadar küçük (~günde 5760 satır).
CREATE TABLE IF NOT EXISTS metrics_samples (
    ts         TEXT    NOT NULL,  -- RFC3339 UTC
    cpu        REAL    NOT NULL,  -- yüzde 0-100
    mem_used   INTEGER NOT NULL,  -- bayt
    mem_total  INTEGER NOT NULL,
    disk_used  INTEGER NOT NULL,
    disk_total INTEGER NOT NULL,
    load1      REAL    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_metrics_ts ON metrics_samples(ts);
