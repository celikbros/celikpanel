-- Service scan results are cached: opening any page must never trigger a
-- full system probe (version execs + config scans across every unit). The
-- list endpoints serve this cache instantly; a fresh scan runs only when the
-- user asks for one, and the row records when that was.
--
-- Servis tarama sonuçları önbelleklenir: herhangi bir sayfayı açmak asla tam
-- sistem taraması tetiklememeli (her unit için sürüm çalıştırmaları + config
-- taramaları). Liste uç noktaları bu önbelleği anında sunar; taze tarama
-- yalnız kullanıcı istediğinde koşar ve satır bunun ne zaman olduğunu tutar.
CREATE TABLE service_scan_cache (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    data TEXT NOT NULL,
    scanned_at TEXT NOT NULL
);
