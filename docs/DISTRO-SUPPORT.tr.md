# Hangi dağıtımda ne sunuluyor?

<!-- BU DOSYA ÜRETİLİR — elle düzenlemeyin. -->
<!-- Kaynak: internal/core/managed_services.go · Üretmek için: go run ./tools/gen-distro-matrix -->
<!-- Bekçi: internal/core/distro_matrix_test.go (katalog değişip bu dosya üretilmezse test düşer) -->

Bu liste elle yazılmaz; bileşen kataloğundan üretilir. Panelin Bileşenler
sayfası aynı kataloğu okur — yani bu belge ile ekran birbirinden ayrı düşemez.

Kurallar:

- **Boş hücre (—) bir eksik değil, bir sözdür:** o dağıtımda "kurulunca çalışır"
  garantisi verilemiyorsa satır bilerek sunulmaz. Yarım kurulum kırıktır.
- **Koltuk:** aynı koltuktaki bileşenler aynı işi yapar; aynı anda yalnız biri
  kurulabilir (ör. iki web sunucusu 80 portunu paylaşamaz). Diğerini seçmek
  ekleme değil, değiştirmedir.
- **Gerekenler** ürünü değil ROLÜ adlandırır: "SMTP sunucusu" diyen bir satırı,
  o koltuğun kurulu herhangi bir üyesi tatmin eder.

## Dağıtım paketiyle kurulanlar

### Web

| Bileşen | Tür | Debian/Ubuntu (apt) | Arch (pacman) | Koltuk | Gerekenler |
|---|---|---|---|---|---|
| 🐘 PHP-FPM | Çalışma ortamı | `php-fpm` · sürüm seçimi: Sury PHP (packages.sury.org) | `php-fpm` | — | — |
| 🔄 Nginx | Servis | `nginx` | `nginx` | web sunucusu | — |
| 🪶 Apache | Servis | `apache2` | `apache` | web sunucusu | — |

### Veritabanı

| Bileşen | Tür | Debian/Ubuntu (apt) | Arch (pacman) | Koltuk | Gerekenler |
|---|---|---|---|---|---|
| 🐘 PostgreSQL | Servis | `postgresql` · sürüm seçimi: PostgreSQL Global Development Group (PGDG) | — | — | — |
| 🦭 MariaDB | Servis | `mariadb-server` | — | — | — |
| 🐬 phpMyAdmin | Araç | `phpmyadmin` | `phpmyadmin` | — | MariaDB, web sunucusu, PHP-FPM |
| 🐘 phpPgAdmin | Araç | `phppgadmin` | — | — | PostgreSQL, web sunucusu, PHP-FPM |

### E-posta

| Bileşen | Tür | Debian/Ubuntu (apt) | Arch (pacman) | Koltuk | Gerekenler |
|---|---|---|---|---|---|
| 📧 Postfix | Servis | `postfix` | `postfix` | SMTP sunucusu | — |
| 📮 Exim | Servis | `exim4-daemon-light` | `exim` | SMTP sunucusu | — |
| 🧹 Rspamd | Servis | `rspamd` | `rspamd` | spam filtresi | SMTP sunucusu |
| 📬 Dovecot | Servis | `dovecot-imapd` `dovecot-pop3d` `dovecot-lmtpd` | `dovecot` | IMAP sunucusu | — |
| 🛡️ SpamAssassin | Servis | `spamassassin` `spamd` `spamass-milter` | — | spam filtresi | SMTP sunucusu |

### Güvenlik

| Bileşen | Tür | Debian/Ubuntu (apt) | Arch (pacman) | Koltuk | Gerekenler |
|---|---|---|---|---|---|
| 🔐 WireGuard VPN | Servis | `wireguard` | `wireguard-tools` | — | — |
| 🚫 Fail2ban | Servis | `fail2ban` | `fail2ban` | — | — |
| 🧱 Firewall engine | Araç | `nftables` | `nftables` | — | — |
| 🦠 ClamAV | Servis | `clamav` `clamav-daemon` | `clamav` | — | — |

### DNS

| Bileşen | Tür | Debian/Ubuntu (apt) | Arch (pacman) | Koltuk | Gerekenler |
|---|---|---|---|---|---|
| 🌐 BIND | Servis | `bind9` | `bind` | DNS sunucusu | — |
| ⚡ PowerDNS | Servis | `pdns-server` `pdns-backend-sqlite3` | `powerdns` | DNS sunucusu | — |

### FTP

| Bileşen | Tür | Debian/Ubuntu (apt) | Arch (pacman) | Koltuk | Gerekenler |
|---|---|---|---|---|---|
| 📂 vsftpd | Servis | `vsftpd` | `vsftpd` | — | — |

### Önbellek

| Bileşen | Tür | Debian/Ubuntu (apt) | Arch (pacman) | Koltuk | Gerekenler |
|---|---|---|---|---|---|
| ⚡ Redis | Servis | `redis-server` | — | — | — |
| 💾 Memcached | Servis | `memcached` | `memcached` | — | — |

### İzleme

| Bileşen | Tür | Debian/Ubuntu (apt) | Arch (pacman) | Koltuk | Gerekenler |
|---|---|---|---|---|---|
| 📈 Netdata | Servis | `netdata` · sürüm seçimi: Netdata (repo.netdata.cloud) | `netdata` | — | — |

## Resmi sürümden kurulanlar (her dağıtımda aynı)

Bu bileşenler dağıtım paketine hiç bağlanmaz: panel, üreticinin yayınladığı
sürümü SHA-256 doğrulamasıyla indirir ve her Linux'ta aynı yoldan kurar
(D-018). Dağıtım sütunu yoktur çünkü cevap her sütunda aynıdır: evet.

| Bileşen | Tür | Koltuk | Gerekenler |
|---|---|---|---|
| 🟩 Node.js | Çalışma ortamı | — | web sunucusu |
| ✉️ Roundcube | Araç | — | IMAP sunucusu, web sunucusu, PHP-FPM |

