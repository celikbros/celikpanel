# BSD Port Fizibilite Haritası

*Fizibilite çalışması · 8 Temmuz 2026 · [English](BSD_PORT_MAP.md)*

"FreeBSD portu gerçekte ne kadar iş?" sorusunun somut cevabı — agent'ın OS'e
dokunan her noktası, FreeBSD karşılığı ve dürüst bir tahmin. Bunu üretmek
için tek satır kod değiştirilmedi; herhangi bir yatırımdan önce riski ölçer.
Desteklediği karar için bkz. [DECISIONS.tr.md](DECISIONS.tr.md#d-004).

## Bugün kanıtlı

Kod tabanının tamamı **şu an**, güncel kaynakla FreeBSD'ye çapraz derleniyor:

```
make freebsd-cross   → ✓
```

Bu hedef önce tam Go 1.26.5 dışındaki her derleyiciyi reddeder, otomatik araç
zinciri indirmesini kapatır; ardından amd64 için panel ve agent'ı, arm64 için
paneli kanıtlar.

**Panel** (HTTP, SQLite, UI, iş mantığı, RPC sözleşmesi) OS-nötr Go'dur ve
**sıfır** değişiklik ister. Tüm BSD işi agent'ta — ve yalnız 36 dosyasından
OS'e komut gönderen ~13'ünde.

## Zaten taşınabilir olanlar (iş yok)

Bu araçlar FreeBSD'de (ports/pkg'den) **aynı komut satırı arayüzüyle** var —
yalnız config yolları değişir (aşağıda tek yerde çözülür):

- **Posta yığını**: `postconf`, `postmap`, `postsuper`, `postqueue` (Postfix),
  `doveadm`, `doveconf` (Dovecot), `opendkim` — birebir aynı çağrılar.
- **DNS**: `pdnsutil`, `pdns_control` (PowerDNS) — aynı.
- **TLS**: `certbot` — aynı.
- **VPN**: `wg`, `wg-quick` — WireGuard FreeBSD'de yerel, aynı.
- **POSIX**: `chown`, `chgrp`, `cp`, `tar`, `du`, `which`, `sudo` — aynı.
- **Çalışma zamanları**: Node.js zaten dağıtımdan bağımsız tarball — OS-nötr.

Agent bunların her birini zaten `exec.LookPath` ile koruyor; aracı olmayan
sistemde çökmek yerine dürüst hata döner — BSD portunu adım adım eklemeyi
güvenli kılan aynı disiplin.

## OS soyutlaması gerekenler (asıl port)

| Alan | Linux (bugün) | FreeBSD | Zorluk | Not |
|------|---------------|---------|--------|-----|
| **Servis kontrolü** | `systemctl` (21 çağrı) | `service(8)` + `sysrc` | **Orta** | Tek `serviceMgr` helper'a topla; çağrılar zaten birkaç fonksiyondan geçiyor |
| **Servis logu** | `journalctl` (1) | syslog / log dosyaları | Kolay | Tek okuma yolu |
| **Unit üretimi** | systemd unit (`celikapp-*`, drop-in) | `rc.d` script | Orta | Node gözetimi + posta/DNS drop-in → rc.d şablon |
| **Paketler** | `apt-get`, `apt-cache` (2) | `pkg` | Kolay | `detectPkgFamily` zaten çoklu-aile; `pkg` kolu ekle |
| **Kullanıcı/grup** | `useradd`/`usermod`/`groupadd` (3) | `pw` | Kolay | Tek `userMgr` helper |
| **Firewall/NAT** | `nftables` (1, VPN NAT) | `pf` | Orta | Tek nokta (VPN masquerade); pf.conf anchor |
| **Yönlendirme/IP** | `ip(8)` (3) | `route`/`ifconfig` | Kolay | Varsayılan rota + adres |
| **sysctl** | `sysctl` (1, ip_forward) | `sysctl` (aynı) | Yok | Birebir |
| **Dosya yolları** | `/etc/*`, `/var/www`, `/var/mail` | `/usr/local/etc/*`, `/usr/local/www` | Orta | ~30 sabit yol → GOOS'a göre tek `ospaths` tablosu |

## İşin şekli

Port bir **yeniden yazım değil** — agent'ın çoğunlukla zaten sahip olduğu
ince bir OS-soyutlama dikişini belirginleştirip arkasına FreeBSD gerçeklemesi
koymaktır:

1. **`serviceMgr` arayüzü** — `Start/Stop/Restart/Enable/Status/WriteUnit`.
   Linux arka ucu = systemd (var, sadece ayıkla); FreeBSD = service + sysrc + rc.d.
2. **`pkgMgr`** — mevcut `detectPkgFamily`'ye `pkg` ailesi ekle.
3. **`userMgr`** — useradd/pw sarmalı.
4. **`firewall`** — bugün nftables, BSD'de pf (tek metot: masquerade).
5. **`ospaths`** — GOOS'a göre seçilen config/veri konumları struct'ı.

Panel, UI, veritabanı, RPC yüzeyi, kurulum mantığı: **değişmez**. `install.sh`
bir FreeBSD kolu kazanır (apt yerine pkg, rc.d ile servis etkinleştirme) —
mevcut apt yolunun kardeşi, yeniden yazım değil.

## Dürüst tahmin

- **Odaklı tek geliştirici: ~2–4 hafta** — çekirdeği (servisler, paketler,
  kullanıcılar, web, posta, DNS, TLS, VPN) kapsayan çalışan bir FreeBSD
  agent'ı, gerçek sunucu testi dahil. Yığının çoğu (Postfix/Dovecot/PowerDNS/
  certbot/WireGuard) birebir aynı davranır; iş, yukarıdaki beş küçük dikiş +
  yol haritalama + rc.d şablonlarıdır.
- **Risk: düşük ve sınırlı.** Yüzey ölçüldü (13 dosya), araçlar BSD'de var,
  panel derlendiği kanıtlı. Bilinmeyen bir mimari engel yok — panel↔agent
  ayrımı zor kısımdı ve ilk günden güvenlik için yapılmıştı.

## Öneri

Seçeneği koru, henüz harcama. Yukarıdaki dikişleri agent geliştikçe
**fırsatçı** ayıklamaya değer (OS'e dokunan her yeni özellik ham `exec` değil
bir helper'dan geçmeli — zaten evin tarzı), ki gerçek bir BSD talebi
doğduğunda port haftalık bir arka-uç-doldurma işi olsun, asla fork.
Tam yürütme gerçek talebi bekler — [DECISIONS.tr.md D-004](DECISIONS.tr.md#d-004).
