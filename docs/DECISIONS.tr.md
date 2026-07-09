# Stratejik Kararlar

*Proje kaydı · [English](DECISIONS.md)*

Büyük yön kararlarının **neden**inin kalıcı kaydı — soru her yeniden
gündeme geldiğinde sıfırdan türetmek istemediğimiz gerekçeler. Kod kararları
git'te yaşar; bu dosya strateji içindir. En yeni en üstte.

---

## D-009 · DNS sunucusu yoksa domain de yok: panel, domain'lerinin otoritesidir

*9 Temmuz 2026*

**Karar.** Bir domain ancak DNS sunucusu (PowerDNS/BIND) kuruluyken
eklenebilir ve domain'ler varken DNS sunucusu kaldırılamaz. CelikPanel
sunucusundaki her domain'in DNS'ini o sunucunun kendisi sunar — "ya da DNS'i
dış sağlayıcıda yönet" dalı yoktur.

**Neden.** Operatörün ya/ya-da mesajlarıyla yaşadıktan sonraki hükmü: "dns
yoksa domain de olmamalı — kafa karıştırıyor." Hiçbir şeyin sunmadığı bir
kayıt listesi, özellik kılığında bir tuzaktır; tek pencerede iki ayrı zihinsel
model, bir fazladır. Tek kural anında okunur: önce DNS kur, sonra domain. Bu,
D-008'in ilk-gün notlarındaki "DNS dışarıda da olabilir" duruşunu bilinçli
olarak geçersiz kılar — dış DNS, gerçek bir kullanıcı isterse ileride açık bir
gelişmiş mod olarak dönebilir; alfa tutarlılık için eniyilenir.

**Nasıl duruyor.** Ön-denetim her domain tipinin oluşturulmasını eyleme dönük
bir 409 ile engeller (pencere de tüm seçenekleri tek ve net bir bantla
kapatır); ayna koruması, herhangi bir domain varken dns-server grubu üyesinin
kaldırılmasını reddeder — yoksa her domain sessizce kararırdı; kuralın
önlediği tuzağın ta kendisi. Tüm "ya da dışarıda yönet" metinleri kaldırıldı.
Canlı kanıt: DNS'siz oluşturma → 409.

---

## D-008 · Alfa: paneli operatör sürer; her boşluk bir ürün özelliğine dönüşür

*9 Temmuz 2026*

**Karar.** Debian 13 yeniden kurulumundan itibaren operatör, CelikPanel'i
gerçek bir müşteri gibi kullanır: her kurulum, her ayar, her domain panelden
ve kendi eliyle geçer. Geliştirici sunucuyu asla yapılandırmaz — izinle bile.
Operatör bir duvara çarptığında suç ürünündür: eksik yetenek panele eklenir
(ya da bozuk olan düzeltilir) ve ürün güncellemesi olarak yayınlanır. SSH ile
teşhis salt-okunur kalır.

**Neden.** Sunucuyu SSH ile elle onarmak, ürünü olduğundan bitmiş gösterir —
boşluk üründen değil görüş alanından kaybolur. Bu, D-005 ile (hiçbir şey
kurma) başlayıp "sormadan asla kurma" ile keskinleşen çizginin son
basamağıdır: geliştirici artık *hiçbir şey* kurmaz ve ayarlamaz. Alfa
kalitesi duvarlara bilerek yürümekten gelir.

**İlk günde nasıl işledi.** Bir öğleden sonralık gerçek operatör kullanımı
sırayla şunları çıkardı ve düzelttirdi: domain'in yanlışlıkla PHP zorunlu
sayması (oluşturma yolu tip sisteminden eskiydi) → php/statik/**yalnız-DNS**
tipli, canlı önkoşul denetimli rol-farkında Alan Adı Ekle (Plesk'in "no web
hosting" karşılığı); kurulu olmayan servislerin hayalet ayar sayfaları → her
sekme, motor listesi ve sürüm açılır kutusu artık sunucunun gerçek yetenek
kümesini okur (tek uç nokta: `/api/v1/hosting/capabilities`); ölü
phpMyAdmin/pgAdmin bağlantıları → genel bir `Requires` katalog kavramı
(çakışma gruplarının tersi): bağımlı araçlar üst servisleri kurulana dek
kilitlidir, kurulunca yalnız-loopback'te, admin-kapılı panel vekilinin
arkasında sunulur; panele gerçek sertifika almanın yolu yoktu → Ayarlar'da
tek tık Let's Encrypt + otomatik yenileme; ve en sessiz, en büyüğü — panel
`index.html`'i önbellek başlıksız veriyordu; tarayıcılar her güncellemeden
sonra ESKİ arayüzü göstermeye devam etti. Operatör, çoktan düzeltilmiş
hayaletleri gördü; artık giriş noktası `no-cache`, parmak izli asset'ler
immutable.

---

## D-007 · Sürüm seçimi, dağıtımı fork'lamadan yönetilen vendor depolarıyla

*9 Temmuz 2026*

**Karar.** Bir servis resmi bir yukarı-akış deposu bildirebilir (PostgreSQL →
PGDG). Onu açmak, kurulumda sürüm seçimini açan birinci-sınıf bir panel
eylemidir: dağıtımın dondurduğu tek major yerine, yönetici vendor'ın sunduğu
tüm güncel major'lardan seçer. Opt-in, seçilmiş, imzalı ve geri alınabilirdir —
asla varsayılan açık değil.

**Neden.** Bir dağıtım sürümü her veritabanı/çalışma zamanının tek major'unu
sabitler (Ubuntu noble yalnız PostgreSQL 16; Debian bookworm yalnız 15).
Müşteriler belirli bir sürüme inip orada kalmak ister — bu, operatörün açık
kaygısıydı ("PG'nin bir sürü sürümü var; install hangisini kurar, bizi tek
sürüme kilitler mi?"). İki kötü yanıt: *dağıtımı fork'la* (tüm paket ağacına
sonsuza dek sahip ol) ya da *OS karar versin* (hiç seçim yok). Sektör-standardı
orta yol, tüm güncel major'ları yan yana taşıyan vendor'ın kendi imzalı apt
deposudur.

**Minimal kurulumla (D-005/D-006) neden tutarlı.** Üçüncü-parti depo güven
sınırını genişletir; bu yüzden servis kurmakla aynı disipline tabidir: yalnız
katalogda tanımlı depolar açılabilir (UI asla URL geçirmez); imza anahtarı depo
başına apt `signed-by=` ile sabitlenir (küresel `apt-key` güveni yok); kapatmak
kaynağı + anahtarı temizce kaldırır. Zırhlı anahtar doğrudan kullanılır, böylece
**`gpg` çekilmez** — minimal ayak izi korunur. Sürüm seçimi deponun paket
desenine bağlıdır; asla keyfi paket kurulumuna dönüşemez.

**Nasıl doğrulandı.**
- Katalog: `ManagedRepo` (id, ad, anahtar URL'si, `{codename}` yer tutuculu
  kaynak şablonu, paket deseni); `postgresql` PGDG taşır. Agent `EnableRepo`/
  `DisableRepo`/`RepoStatus`/`RepoPackages`; hangi sürümlerin var olduğunun
  kaynağı kodumuz değil depodur (`apt-cache` ile keşfedilir, en yeni major önce).
- Panel `GET/POST /api/v1/repo` (yalnız admin, audit'li); izin listesi burada
  (depoyu katalogdan servis id'siyle çöz). Kurulum, bir sürüm paketini yalnız o
  servisin depo desenine uyuyorsa kabul eder.
- Üretim VPS'te kanıtlandı: baseline **yalnız `postgresql-16`** sunuyordu; PGDG
  açılınca **9 major (10–18)** açıldı ve `postgresql-17` seçimi PGDG'den
  `17.10-1.pgdg24.04+1`'e çözüldü; kapatmak baseline'a döndürdü.
- Testte yakalanan hata: agent `UMask=0027` ile koşar; `0644` keyring
  `0640 root:celikpanel` olarak indi — apt'ın yetkisiz `_apt` doğrulayıcısı
  okuyamadı, "imzasız" hatası verdi. Yazdıktan sonra `chmod 0644` ile düzeltildi.

---

## D-006 · Saldırı yüzeyi yönetimi: her kurulum geri alınabilir

*8 Temmuz 2026*

**Karar.** Panel neyi kurabiliyorsa, onu kaldırabilir de. Servis kaldırma
(durdur + devre dışı + `apt purge --auto-remove`) kurulumun yanında birinci
sınıf bir eylemdir, elle SSH işi değil. Bu, üç parçalı "saldırı yüzeyi"
hattının ilkidir: **geri alınabilir kurulumlar**, otomatik güvenlik yaması ve
yalnız kullanılan portları açan bir güvenlik duvarı.

**Neden.** Operatörün ilkesi, açıkça: *kurulu her servis ya da paket bir
güvenlik riskidir — bekleyen bir CVE, açık bir port.* Minimalizm derli
topluluk değil, daha küçük saldırı yüzeyidir. Ama minimalizm ancak yüzey hem
büyüyüp hem küçülebiliyorsa gerçektir: nginx ekleyip asla kaldıramıyorsan,
yanlış ya da artık gereksiz bir kurulum kalıcı risktir. O yüzden kurulumun bir
aynası var.

**Nasıl korunuyor.**
- `Agent.UninstallService`: her unit'i durdurur + devre dışı bırakır, sonra
  `removePackages` purge eder (config gider, `--auto-remove` öksüz bağımlılığı
  düşürür). `InstallService`'in aynası; aynı whitelist, aynı dürüst "bu
  dağıtımda henüz yok".
- Panel `POST /service/uninstall` (yalnız admin, audit'li); UI tam paket
  listesini ve bağımlı site/postanın duracağı uyarısını içeren yıkıcı-onay
  modalı gösterir.
- Üretim VPS'inde kanıtlı: SpamAssassin kuruldu (dpkg var) sonra kaldırıldı
  (dpkg yok, unit gitti) — yüzey ölçülebilir şekilde küçüldü.

**Bu hattın ilerlemesi (tamam):** geri alınabilir kurulumlar ✅ · otomatik
güvenlik güncellemeleri ✅ · güvenlik duvarı ✅. Duvar varsayılan-reddet
gelen; panel tam olarak panel portu + kurulu her servisin bildirdiği portları
açar, agent ise SSH'ı (canlı dinleyicilerden tespit) + loopback + kurulu
bağlantıları daima açık tutar; bir kural operatörü asla kilitleyemez.
Kurulum/kaldırma yeniden senkronlar; açık-port kümesi daima koşan-servis
kümesine eşittir. VPS'te kanıtlı: açınca SSH, panel, web ve DNS erişilebilir
kaldı, politika gerisini düşürdü.

---

## D-005 · Kurulum panelden başka hiçbir şey kurmaz; sunucu yalnız-panel olabilir

*8 Temmuz 2026*

**Karar.** `install.sh` yalnız panel + agent'ı (kendine yeten Go binary'leri +
SQLite) ve dört minik indirme aracını (tar, xz, curl, ca-certificates) kurar —
barındırma için **hiçbir şey**. nginx, PHP, MariaDB, Postfix, PowerDNS, posta:
hepsi sonradan, panelden, talep üzerine eklenir. Bir sunucu yalnız-panel
kalabilir (örn. sadece yerleşik VPN isteyen biri) ve hiç domain olmadan IP
üzerinden yönetilebilir.

**Neden.** Anayasa: "kurulu olmayan görünmez." Taze kurulmuş bir sunucu, panel
dışında sıfır saldırı yüzeyi ve sıfır fazlalık taşır. Operatör tam istediği
sunucuyu kurar — tam bir barındırma kutusu, ya da sadece VPN ucu, ya da sadece
DNS. İstenmeyen hiçbir şey asla bulunmaz.

**Nasıl korunuyor.**
- Doğrulandı: `install.sh` yalnız `apt-get install tar xz-utils curl
  ca-certificates` koşar. Üretim VPS'inde nginx/PHP/vb. yalnız golden path'i
  kanıtlamak için *panelden* kurulduğu için var — çıplak kurulumda hiçbiri yok.
- Domainsiz erişim tasarım gereği çalışır: panelin self-signed sertifikası
  makinenin IP'lerini SAN'a koyar (`tmpl.IPAddresses = hostIPs()`), böylece IP
  erişimi eşleşen bir sertifika sunar (yalnız self-signed uyarısı, asla isim
  uyuşmazlığı). Domain + Let's Encrypt bir yükseltmedir, asla zorunluluk değil.

---

## D-004 · İşletim sistemi: önce Ubuntu LTS, BSD seçeneği korunur (asla fork)

*8 Temmuz 2026*

**Karar.** Ubuntu 24.04 LTS birinci sınıf, yalnızca test edilen hedeftir. BSD
**şimdilik** bilinçli bir hedef-dışıdır — ama ileride desteklemek seçeneği
mimariyle korunur ve **asla** ayrı bir ürün olmaz.

**Neden Ubuntu, BSD değil.**
- Ürünün tamamı Linux'a özgü parçaların üstünde: systemd (agent'ın kalbi —
  servisler, `celikapp-*`, `wg-quick@`), apt + paket whitelist'leri, nftables
  (VPN NAT) ve postfix/dovecot/opendkim'in Ubuntu yerleşimi. OS değiştirmek =
  agent'ın "ellerini" yeniden yazmak, kullanıcıya sıfır fark için aylarca iş.
- Kullanıcının beğendiği kararlılık ("versiyonlar kilitli, kararlı") zaten
  **Ubuntu LTS'nin kendisi**: 5 yıl yalnız güvenlik yaması. BSD, elimizde
  olmayan bir kararlılık katmaz.
- Pazar gerçeği: hiçbir rakip BSD'de değil (cPanel = RHEL ailesi, Plesk =
  Linux+Windows, aaPanel = Linux). VPS imajları, müşteri alışkanlıkları,
  WordPress/PHP ekosistemi, Let's Encrypt araçları hep Linux-öncelikli.
  Müşteri "WordPress'im çalışıyor mu, mailim spam'e düşüyor mu?" diye sorar —
  "BSD'de misiniz?" diye değil.

**Seçeneği ucuz tutan şey — ve neden fork DEĞİL.**
- Panel↔agent RPC ayrımı (güvenlik için kuruldu) aynı zamanda OS
  bağımsızlığıdır. **Panel** (HTTP, SQLite, UI, iş mantığı) taşınabilir Go;
  OS'e dokunan tek katman **agent**. Panel *ne* yapılacağını der ("site kur");
  *nasıl*ını yalnız agent bilir (systemctl/apt/nftables).
- İddia değil kanıt: bu tarih itibarıyla `GOOS=freebsd go build` **hem** paneli
  hem agent'ı derliyor. Tek bir taşınabilirlik düzeltmesi gerekti (Statfs_t
  alan tipleri Linux ile BSD'de farklı — `cmd/panel/system_stats.go`'da açık
  `uint64` dönüşümleri) ve yapıldı. Kod tabanının tamamı bugün FreeBSD'ye
  çapraz derleniyor.
- İki CelikPanel, kullanıcının korktuğu hantallığın ta kendisi olurdu: her
  özellik iki kez, her hata iki kez, iki vasat ürün. Bunun yerine olası bir
  BSD geleceği = aynı RPC yüzeyinin arkasına bir **BSD agent arka ucu** (rc.d,
  pkg, pf). Tahmin: yıllar değil haftalar — tüm yığın (nginx, postfix,
  dovecot, opendkim, PowerDNS, MariaDB, WireGuard) FreeBSD ports'ta var;
  yalnız "eller" değişir. Panel, UI, DB, iş mantığı: sıfır değişiklik.

**Bugün ne yapıyoruz.** Neredeyse hiçbir şey — ve asıl nokta bu. Tek bedel
disiplin: yeni agent özellikleri *ne*yi (RPC yüzeyi) *nasıl*dan (exec
çağrıları) ayrı tutar, ki kod zaten böyle yazılıyor. Spekülatif soyutlama
katmanı yok, "belki lazım olur" kodu yok. Karar, gerçek talep doğduğunda
verilir (örn. Linux'ta bir güven/güvenlik krizi barındırıcıları BSD'ye
iterse).

**Diğer dağıtımlar.** `detectPkgFamily` apt / dnf / pacman tanır. Bugün yalnız
apt (Ubuntu/Debian) tam destekli ve test edilmiş; dnf/pacman tanınır ama
kurulum tahmin etmek yerine dürüstçe "bu dağıtımda henüz otomatik kurulamıyor"
der. Not: dnf module stream'leri (`postgresql:16` vs `:18`) orada gerçek bir
sürüm seçiciyi anlamlı kılar — dnf desteği eklersek UI buna hazır.

---

## D-003 · Servis kataloğu: her şeyi göster, çakışmayla engelle — "yönetilmiyor" ile değil

*8 Temmuz 2026*

**Karar.** Servisler sayfası (yalnız admin) tüm kataloğu listeler, kurulu olsun
olmasın, tek tıkla Kur ile. Derinlemesine yönetmediğimiz servisleri
**gizlemeyiz** ve "yönetilmiyor" etiketleriyle **karmaşıklaştırmayız**. Bir
kurulumu engelleyen tek şey **gerçek bir çakışma**dır.

**Neden.** Kullanıcının sezgisi: "yönetiyor muyuz?" üzerinden kapılamak
gereksiz karmaşıklık. Asıl kısıt, iki servisin yan yana çalışıp
çalışamayacağıdır.
- **Çakışma grupları** (katalogda `ConflictGroup`): aynı rol/portu tutanlar
  karşılıklı dışlar — :80'de web sunucusu (Nginx ↔ Apache), :53'te DNS (BIND ↔
  PowerDNS). Biri kuruluysa diğerinin Kur düğmesi "X ile çakışır" olur.
- **Grup yoksa = yan yana**: MariaDB + PostgreSQL birlikte koşar (farklı
  portlar), ikisi de kurulabilir. Redis + Memcached de öyle.

**Sonuç.** Her kurulum admin'in açık, tek-tıklık onayıdır ("yalnız izinle kur"
ilkesine uygun). Katalog kategoriye göre akordiyonla gruplu (tümünü aç/kapat);
düzinelerce servise doğru büyüdükçe taranabilir kalır.

---

## D-002 · Servis sürümleri: dürüst tek sürüm, gerçek çoklu-sürüm yalnız var olan yerde

*8 Temmuz 2026*

**Karar.** Kurulum modalı apt'ın kuracağı **gerçek** sürümü gösterir (canlı
`apt-cache policy` ile okunur), sürüm *seçici* değil. Seçici yalnız birden
fazla sürümün gerçekten var olduğu yerde sunulur.

**Neden.** Bir Debian/Ubuntu sürümü her paketin tam olarak tek sürümünü verir
ve dondurur (yalnız güvenlik yaması) — kararlılık modeli bu, bilerek. Tek
seçenekli bir açılır liste, kullanıcıyı yanıltan bir yalandır. Dolayısıyla:
- **Tek-sürümlü servisler** (PostgreSQL, MariaDB, nginx…): ne ineceğini göster
  ("Kurulacak sürüm: 16"), seçici yok.
- **Gerçekten çoklu-sürüm, hosting-kritik** → dağıtımdan bağımsız, gerçek
  seçiciyle:
  - **PHP** — yan yana paketler (`php8.1-fpm`…`php8.4-fpm`), site başına
    seçilir. ✅ yapıldı.
  - **Node.js** — dağıtımdan bağımsız resmi tarball'lar, proje başına seçilir.
    ✅ yapıldı.
- **Gelecek seçeneği**: Ubuntu 24.04'te örn. PostgreSQL 18 isteyen müşteri,
  üreticinin kendi apt deposunu (PGDG) bilinçli bir özellik olarak eklemek
  demektir — gerçek depo yönetimi, asla sahte seçici. Backlog'da, yapılmadı.

Desen: **dağıtımın tek sürümü = güvenli taban; gerçek çoklu-sürüm (PHP, Node)
kendi mekanizmamızla, dağıtımdan bağımsız.** cPanel/Plesk gibi (PHP'yi
kendileri derler, gerisini dağıtıma bırakır).

---

## D-001 · Güncelleme ve geri alma: sunucuyu asla sıfırdan kurma

*8 Temmuz 2026*

**Karar.** Üretim güncellemeleri `sudo ./update.sh` (git pull + idempotent
`install.sh`), asla sil-baştan-kur değil. Her güncelleme önce bir geri-alma
anlık görüntüsü alır; `sudo ./rollback.sh` önceki çalışan duruma döner.

**Neden.** Kutuda müşteri olması, verinin her güncellemeden sağ çıkması
gerektiği anlamına gelir. Müşteri verisi (SQLite DB, site dosyaları, posta,
DNS, sertifikalar, DKIM anahtarları) değişen yolların (`bin/`, `web/`) dışında
yaşar; migration'lar panel açılışında uygulanır. Anlık görüntü (panel DB +
binary'ler + unit dosyaları + kaynak commit'i) "bir güncelleme işleri bozdu"yu
felaket değil, kurtarılabilir bir olay yapar. Düzeltme akışı: dev kutusunda
(VPS'in aynası) yeniden üret → kanıtla → push → VPS'te `update.sh` → gerekirse
`rollback.sh`. Bkz. [update.sh](../update.sh), [rollback.sh](../rollback.sh).
