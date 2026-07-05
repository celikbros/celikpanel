-- 005: Project types on sites — roadmap 3A "runtimes done right".
-- 005: Sitelerde proje tipleri — yol haritası 3A "runtime'ları doğru yapmak".
--
-- A site is no longer implicitly PHP: project_type selects how the vhost is
-- generated and whether a supervised app runs behind it.
--   php        — PHP-FPM (the default, existing behaviour)
--   static     — files only, no runtime
--   node       — long-running app behind an nginx reverse proxy
--   proxy      — plain reverse proxy to an arbitrary upstream
--   forwarding — HTTP redirect to another URL
-- Validated in code, no CHECK (SQLite cannot alter a CHECK later; go/python
-- become possible without a table rebuild).
--
-- Bir site artık örtük olarak PHP değildir: project_type, vhost'un nasıl
-- üretileceğini ve arkasında gözetimli bir uygulama çalışıp çalışmayacağını
-- seçer. Kodda doğrulanır, CHECK yok (SQLite CHECK'i sonradan değiştiremez;
-- go/python tablo yeniden inşası olmadan eklenebilir).

ALTER TABLE sites ADD COLUMN project_type TEXT NOT NULL DEFAULT 'php';

-- node/proxy: the local port the app listens on (nginx proxies to it).
-- node/proxy: uygulamanın dinlediği yerel port (nginx ona proxy'ler).
ALTER TABLE sites ADD COLUMN app_port INTEGER;

-- node: how to start the app, relative to the document root's parent
-- (e.g. "node server.js" or "npm start").
-- node: uygulamanın nasıl başlatılacağı (ör. "node server.js", "npm start").
ALTER TABLE sites ADD COLUMN start_command TEXT;

-- node: which installed runtime version to use (e.g. "24.18.0");
-- empty = system default.
-- node: hangi kurulu runtime sürümü (ör. "24.18.0"); boş = sistem varsayılanı.
ALTER TABLE sites ADD COLUMN runtime_version TEXT;

-- forwarding/proxy: the target URL (forwarding: redirect target incl. code
-- choice below; proxy: upstream base URL).
-- forwarding/proxy: hedef URL (forwarding: yönlendirme hedefi; proxy: üst
-- kaynak taban URL'si).
ALTER TABLE sites ADD COLUMN forward_to TEXT;

-- forwarding: 301 (permanent) or 302 (temporary).
-- forwarding: 301 (kalıcı) ya da 302 (geçici).
ALTER TABLE sites ADD COLUMN forward_code INTEGER DEFAULT 301;
