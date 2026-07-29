-- Store catalogue projection. This is product data only: commands, package
-- recipes, SQL fragments and filesystem paths never belong in metadata_json.
-- Mağaza katalog görünümü. Bu yalnız ürün verisidir: komutlar, paket
-- reçeteleri, SQL parçaları ve dosya sistemi yolları metadata_json'a girmez.
CREATE TABLE store_offerings (
    id TEXT PRIMARY KEY
        CHECK (length(id) BETWEEN 1 AND 80 AND id NOT GLOB '*[^a-z0-9_:-]*'),
    kind TEXT NOT NULL
        CHECK (kind IN ('component', 'addon', 'application', 'feature', 'integration')),
    category TEXT NOT NULL,
    vendor TEXT NOT NULL,
    release_state TEXT NOT NULL
        CHECK (release_state IN ('available', 'coming_soon', 'retired')),
    entitlement_mode TEXT NOT NULL
        CHECK (entitlement_mode IN ('included', 'grant')),
    manage_path TEXT CHECK (
        manage_path IS NULL OR (
            length(manage_path) BETWEEN 1 AND 240
            AND substr(manage_path, 1, 1) = '/'
            AND substr(manage_path, 1, 2) <> '//'
            AND instr(manage_path, '\') = 0
            AND instr(manage_path, '://') = 0
            AND instr(manage_path, char(9)) = 0
            AND instr(manage_path, char(10)) = 0
            AND instr(manage_path, char(13)) = 0
            AND instr(manage_path, '/../') = 0
            AND substr(manage_path, -3) <> '/..'
            AND instr(manage_path, '/./') = 0
            AND substr(manage_path, -2) <> '/.'
        )
    ),
    metadata_json TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(metadata_json) AND json_type(metadata_json) = 'object'),
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Product-to-component links are authorization-relevant and therefore typed.
-- They are deliberately not hidden inside presentation JSON.
-- Ürün-bileşen bağları yetkilendirmeyi etkiler ve bu yüzden tiplidir.
-- Bilerek sunum JSON'unun içine saklanmazlar.
CREATE TABLE store_offering_components (
    offering_id TEXT NOT NULL REFERENCES store_offerings(id) ON DELETE CASCADE,
    component_id TEXT NOT NULL
        CHECK (length(component_id) BETWEEN 1 AND 80 AND component_id NOT GLOB '*[^a-z0-9_-]*'),
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (offering_id, component_id)
);

CREATE INDEX idx_store_offerings_release
    ON store_offerings(release_state, sort_order, id);
CREATE INDEX idx_store_offering_components_component
    ON store_offering_components(component_id);

-- Two products have real feature gates today. They become grantable only when
-- their required components are freshly verified and usable on this host.
-- Bugün gerçek özellik kapısı olan iki ürün vardır. Yalnız gerekli bileşenleri
-- bu makinede güncel olarak doğrulanmış ve kullanılabilir ise hak verilebilir.
INSERT INTO store_offerings
    (id, kind, category, vendor, release_state, entitlement_mode, manage_path, metadata_json, sort_order)
VALUES
    (
        'app_installer', 'application', 'applications', 'WordPress.org',
        'available', 'grant', '/domains',
        '{"name":{"en":"One-click WordPress install","tr":"Tek tıkla WordPress kurulumu"},"description":{"en":"Grant a subscription access to the curated WordPress installer. Site deployment stays in Domain Apps.","tr":"Bir aboneliğe seçilmiş WordPress kurucusuna erişim verir. Site kurulumu Alan Adı Uygulamaları bölümünde kalır."},"icon":"wordpress","tags":["wordpress","cms"]}',
        10
    ),
    (
        'vpn', 'addon', 'network', 'WireGuard',
        'available', 'grant', '/services/wireguard',
        '{"name":{"en":"VPN access","tr":"VPN erişimi"},"description":{"en":"Grant a subscription permission to create private WireGuard peers.","tr":"Bir aboneliğe özel WireGuard eşleri oluşturma izni verir."},"icon":"shield","tags":["wireguard","vpn","network"]}',
        20
    );

INSERT INTO store_offering_components (offering_id, component_id, position) VALUES
    ('app_installer', 'nginx', 10),
    ('app_installer', 'php-fpm', 20),
    ('app_installer', 'mariadb', 30),
    ('vpn', 'wireguard', 10);

-- Included, working CelikPanel capabilities. "Included" is explicit commercial
-- state; it is never inferred from a zero or missing price.
-- Dahil, çalışan CelikPanel yetenekleri. "Dahil" açık ticari durumdur;
-- sıfır ya da eksik fiyattan çıkarılmaz.
INSERT INTO store_offerings
    (id, kind, category, vendor, release_state, entitlement_mode, manage_path, metadata_json, sort_order)
VALUES
    (
        'ssl_automation', 'component', 'security', 'Let''s Encrypt / Certbot',
        'available', 'included', '/services/certbot',
        '{"name":{"en":"SSL automation","tr":"SSL otomasyonu"},"description":{"en":"Issue and automatically renew trusted website certificates with ACME.","tr":"ACME ile güvenilir site sertifikaları alın ve otomatik yenileyin."},"icon":"lock","tags":["ssl","tls","acme"]}',
        100
    ),
    (
        'dnssec', 'feature', 'dns', 'CelikPanel / PowerDNS',
        'available', 'included', '/services/pdns',
        '{"name":{"en":"DNSSEC","tr":"DNSSEC"},"description":{"en":"Sign authoritative DNS zones and publish registrar DS records safely.","tr":"Yetkili DNS bölgelerini imzalayın ve kayıt kuruluşu DS kayıtlarını güvenle yayınlayın."},"icon":"key","tags":["dns","dnssec"]}',
        110
    ),
    (
        'backups', 'feature', 'backup', 'CelikPanel',
        'available', 'included', '/domains',
        '{"name":{"en":"Local backups","tr":"Yerel yedekler"},"description":{"en":"Create and schedule local file, database and full-domain backups.","tr":"Yerel dosya, veritabanı ve tam alan adı yedekleri oluşturun ve zamanlayın."},"icon":"archive","tags":["backup","restore"]}',
        120
    ),
    (
        'clamav', 'component', 'security', 'ClamAV',
        'available', 'included', '/services/clamav',
        '{"name":{"en":"ClamAV malware scanner","tr":"ClamAV zararlı yazılım tarayıcısı"},"description":{"en":"Scan hosted files with the open-source ClamAV engine.","tr":"Barındırılan dosyaları açık kaynak ClamAV motoruyla tarayın."},"icon":"shield-check","tags":["antivirus","malware"]}',
        130
    ),
    (
        'fail2ban', 'component', 'security', 'Fail2Ban',
        'available', 'included', '/services/fail2ban',
        '{"name":{"en":"Fail2Ban intrusion prevention","tr":"Fail2Ban saldırı önleme"},"description":{"en":"Block repeated abusive login attempts with managed jails.","tr":"Yönetilen jail kurallarıyla tekrarlanan kötü niyetli giriş denemelerini engelleyin."},"icon":"ban","tags":["security","brute-force"]}',
        140
    ),
    (
        'roundcube', 'application', 'email', 'Roundcube',
        'available', 'included', '/services/roundcube',
        '{"name":{"en":"Roundcube webmail","tr":"Roundcube web posta"},"description":{"en":"Browser-based mail for domains hosted on this server.","tr":"Bu sunucuda barındırılan alan adları için tarayıcı tabanlı e-posta."},"icon":"mail","tags":["email","webmail"]}',
        150
    ),
    (
        'rspamd', 'component', 'email', 'Rspamd',
        'available', 'included', '/services/rspamd',
        '{"name":{"en":"Rspamd spam protection","tr":"Rspamd spam koruması"},"description":{"en":"Modern spam filtering and DKIM integration for the local mail stack.","tr":"Yerel posta yığını için modern spam süzme ve DKIM entegrasyonu."},"icon":"mail-check","tags":["email","spam","dkim"]}',
        160
    ),
    (
        'netdata', 'component', 'monitoring', 'Netdata',
        'available', 'included', '/services/netdata',
        '{"name":{"en":"Netdata monitoring","tr":"Netdata izleme"},"description":{"en":"Real-time server metrics through the managed Netdata component.","tr":"Yönetilen Netdata bileşeni üzerinden gerçek zamanlı sunucu ölçümleri."},"icon":"activity","tags":["monitoring","metrics"]}',
        170
    );

INSERT INTO store_offering_components (offering_id, component_id, position) VALUES
    ('ssl_automation', 'certbot', 10),
    ('dnssec', 'pdns', 10),
    ('clamav', 'clamav', 10),
    ('fail2ban', 'fail2ban', 10),
    ('roundcube', 'roundcube', 10),
    ('rspamd', 'rspamd', 10),
    ('netdata', 'netdata', 10);

-- Honest roadmap entries: visible for discovery, but never grantable or
-- installable until a real backend and platform contract exists.
-- Dürüst yol haritası girdileri: keşif için görünür, fakat gerçek backend ve
-- platform sözleşmesi oluşana kadar hak verilemez ve kurulamaz.
INSERT INTO store_offerings
    (id, kind, category, vendor, release_state, entitlement_mode, manage_path, metadata_json, sort_order)
VALUES
    (
        'firewall', 'addon', 'security', 'CelikPanel',
        'coming_soon', 'grant', NULL,
        '{"name":{"en":"Managed per-site firewall","tr":"Yönetilen site güvenlik duvarı"},"description":{"en":"Customer-visible per-site firewall policy is not available yet.","tr":"Müşteriye açık site bazlı güvenlik duvarı ilkesi henüz hazır değil."},"icon":"shield","tags":["firewall","security"]}',
        500
    ),
    (
        'business_email', 'addon', 'email', 'CelikPanel',
        'coming_soon', 'grant', NULL,
        '{"name":{"en":"Business email","tr":"Kurumsal e-posta"},"description":{"en":"Larger enforced mailbox quotas and commercial mail features are in development.","tr":"Daha büyük zorunlu posta kutusu kotaları ve ticari posta özellikleri geliştiriliyor."},"icon":"briefcase","tags":["email","quota"]}',
        510
    ),
    (
        'extra_ip', 'addon', 'network', 'CelikPanel',
        'coming_soon', 'grant', NULL,
        '{"name":{"en":"Dedicated IP","tr":"Özel IP"},"description":{"en":"Subscription-level dedicated address allocation is in development.","tr":"Abonelik bazlı özel adres tahsisi geliştiriliyor."},"icon":"network","tags":["ip","network"]}',
        520
    ),
    (
        'ai_agent', 'integration', 'automation', 'CelikPanel',
        'coming_soon', 'grant', NULL,
        '{"name":{"en":"CelikPanel AI Agent","tr":"CelikPanel AI Agent"},"description":{"en":"A panel-scoped assistant that can explain and perform approved CelikPanel operations.","tr":"Yalnız CelikPanel işlemlerini açıklayan ve onaylanan panel işlemlerini yapabilen asistan."},"icon":"sparkles","tags":["ai","automation"]}',
        530
    ),
    (
        'git_deployment', 'integration', 'development', 'CelikPanel',
        'coming_soon', 'grant', NULL,
        '{"name":{"en":"Git deployment","tr":"Git ile yayınlama"},"description":{"en":"Deploy a site from an approved repository and branch.","tr":"Bir siteyi onaylanmış depo ve daldan yayınlayın."},"icon":"git-branch","tags":["git","deployment"]}',
        540
    ),
    (
        'remote_backup_destinations', 'integration', 'backup', 'CelikPanel',
        'coming_soon', 'grant', NULL,
        '{"name":{"en":"Remote backup destinations","tr":"Uzak yedek hedefleri"},"description":{"en":"Send encrypted backups to S3-compatible or FTP destinations.","tr":"Şifreli yedekleri S3 uyumlu veya FTP hedeflerine gönderin."},"icon":"cloud","tags":["backup","s3","ftp"]}',
        550
    ),
    (
        'wordpress_toolkit', 'application', 'applications', 'CelikPanel',
        'coming_soon', 'grant', NULL,
        '{"name":{"en":"WordPress Toolkit","tr":"WordPress Araç Seti"},"description":{"en":"Lifecycle, security and update management for installed WordPress sites.","tr":"Kurulu WordPress siteleri için yaşam döngüsü, güvenlik ve güncelleme yönetimi."},"icon":"wordpress","tags":["wordpress","updates"]}',
        560
    ),
    (
        'docker_manager', 'component', 'containers', 'Docker',
        'coming_soon', 'grant', NULL,
        '{"name":{"en":"Docker manager","tr":"Docker yöneticisi"},"description":{"en":"Policy-controlled container and Compose management is in development.","tr":"İlke kontrollü container ve Compose yönetimi geliştiriliyor."},"icon":"container","tags":["docker","containers"]}',
        570
    ),
    (
        'waf', 'addon', 'security', 'CelikPanel',
        'coming_soon', 'grant', NULL,
        '{"name":{"en":"Web application firewall","tr":"Web uygulama güvenlik duvarı"},"description":{"en":"Managed per-site WAF rules and audit events are in development.","tr":"Yönetilen site bazlı WAF kuralları ve denetim olayları geliştiriliyor."},"icon":"shield-alert","tags":["waf","security"]}',
        580
    ),
    (
        'dns_provider_sync', 'integration', 'dns', 'CelikPanel',
        'coming_soon', 'grant', NULL,
        '{"name":{"en":"External DNS sync","tr":"Harici DNS eşitleme"},"description":{"en":"Provider API integrations for controlled DNS publication are in development.","tr":"Kontrollü DNS yayını için sağlayıcı API entegrasyonları geliştiriliyor."},"icon":"refresh-cw","tags":["dns","api","sync"]}',
        590
    );
