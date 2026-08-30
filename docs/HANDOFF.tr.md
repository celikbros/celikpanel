# Mühendislik Devri

*Referans güncellemesi: 30 Ağustos 2026 · [English](HANDOFF.md)*

Bu belge, CelikPanel'i devralacak mühendislik ekibinin başlangıç noktasıdır.
Dondurulmuş kaynak referansını, otorite taşıyan belgeleri, asgari geliştirme ve
sürüm akışını ve repo dışında devredilmesi gereken bilgileri tanımlar.

Bu belge herhangi bir sunucunun sağlıklı veya güncel olduğunun kanıtı değildir.
Tarihsel [LIVE-STATE-2026-08-29.tr.md](LIVE-STATE-2026-08-29.tr.md) kaydını
değiştirmeden koruyun. Güncel ürün sürümü Alpha52'dir ve incelenmiş sürüm/portal
kanıtı [RELEASE-EVIDENCE-v0.1.0-alpha.52.tr.md](RELEASE-EVIDENCE-v0.1.0-alpha.52.tr.md)
içindedir. Boston ve Frankfurt terminal başarılı Alpha52 receipt'leri üretmiştir.
Bkz. tarihli [canlı durum kaydı](LIVE-STATE-2026-08-30.tr.md) ve
[RISK-REGISTER.tr.md](RISK-REGISTER.tr.md).

## 1. Dondurulmuş devir referansı

| Konu | Değer | Durum |
|---|---|---|
| Güncel canonical kaynak etiketi ve sürümü | [v0.1.0-alpha.52](https://github.com/celikbros/celikpanel/releases/tag/v0.1.0-alpha.52) | DOĞRULANMIŞ ürün referansı; canlı kurulum kanıtı değil |
| Güncel canonical kaynak commit'i | adb25d8ec487dcb76dd95304a551d8cb37565115 | DOĞRULANMIŞ ürün referansı |
| Güncel yayımlanmış binary | v0.1.0-alpha.52 | GitHub, portal ve iki canlı kurulumda DOĞRULANMIŞ |
| Alpha52 etiketli sürüm CI'ı | [Run 33283088681](https://github.com/celikbros/celikpanel/actions/runs/33283088681) geçti | DOĞRULANMIŞ |
| Alpha52 sürüm ürünleri | Tam 6 değişmez ürün; resmi Ed25519 manifest'i ve ayrık imza doğrulandı | DOĞRULANMIŞ |
| Alpha52 manifest'inin yetkilendirdiği arşiv | SHA256 `9a604bf0f58855f53997a1adeb44a24cc76c4ff062fd8068ee6a66be66a28304`; 22.672.364 bayt | DOĞRULANMIŞ |
| Tarihsel canlı referans etiketi | [v0.1.0-alpha.51](https://github.com/celikbros/celikpanel/releases/tag/v0.1.0-alpha.51) | Alpha52 öncesi DOĞRULANMIŞ canlı referansı |
| Tarihsel canlı referans commit'i | 45d01ffb29013b9457180072c3b25ab24d5ff7bd | Alpha52 öncesi DOĞRULANMIŞ canlı referansı |
| Promosyon öncesi son kanıtlanmış canlı binary | v0.1.0-alpha.51 | DOĞRULANMIŞ tarihsel promosyon öncesi durum |
| Alpha51 etiketli sürüm CI'ı | Yayımlanmış sürüm için geçti | DOĞRULANMIŞ |
| Alpha51 sürüm ürünleri | Tam 6 değişmez ürün; resmi Ed25519 manifest'i ve ayrık imza doğrulandı | DOĞRULANMIŞ |
| Manifest'in yetkilendirdiği arşiv | SHA256 `57d0321a13388392872bc3aef9af62646e2d700c23a4e0305d479df1e80ff365`; 22.644.115 bayt | DOĞRULANMIŞ |
| Git etiketi imzası | Git etiketi imzasızdır | DOĞRULANMIŞ; update otoritesi etiket imzası değil Ed25519 manifest'idir |
| Tarihsel devir belgeleri | [PR #74](https://github.com/celikbros/celikpanel/pull/74), merge commit `e29df589594b2b5929d067a0174ab98d8182e4b5` | DOĞRULANMIŞ tarihsel belge referansı |
| GitHub açık pull request'leri | Yok | 30.08.2026 itibarıyla DOĞRULANMIŞ |
| Bilinçli tutulan arşivlik remote head'ler | `agent/ssl-hostnames-hsts` (PR #72) ve `archive/alpha35-portal-tooling` (PR #73) | Yalnız kaynak arkeolojisi için koruyun; olduğu hâliyle merge etmeyin veya çalıştırmayın |
| Ürün yaşam evresi | Alpha / ön sürüm | REPO TARAFINDAN BEYAN EDİLMİŞ |
| Üretime hazır olma durumu | Üretime hazır değil | SECURITY.md içinde REPO BEYANI |
| Boston canlı kimliği | panel/agent Alpha52 commit `adb25d8ec487dcb76dd95304a551d8cb37565115`; terminal receipt `b6fd0052b2c4a04b117a753637d68798`; servisler aktif; floor 52 | 30.08.2026 salt okunur DOĞRULANDI |
| Frankfurt canlı kimliği | panel/agent Alpha52 commit `adb25d8ec487dcb76dd95304a551d8cb37565115`; terminal receipt `b85dee68b54a01689333112ae8ccaa5f`; servisler aktif; floor 52 | 30.08.2026 salt okunur DOĞRULANDI |

Bir etiket, kaynak commit'i ve açık HTTP yanıtı; kurulu panel commit'ini, agent
commit'ini, veritabanı şemasını, DNS rolünü, firewall durumunu, sertifika
durumunu veya rollback hazırlığını kanıtlamaz. Bu değerleri türetmeyin. Herhangi
bir dağıtım veya panel değişikliğinden önce salt okunur canlı durum listesini
tamamlayın.

PR #69, #70 ve #71 kapalı ve superseded'dır. PR #72 ile PR #73 arşivliktir; iki
remote head yalnız kaynak arkeolojisi için tutulur ve olduğu hâliyle merge
edilmemeli veya çalıştırılmamalıdır. Alpha52 canonical ürün kaynağı ve yayımlanmış
binary referansıdır. Devir hazırlanırken tek açık dal, bu paketi kendi pull
request'i üzerinden taşıyacak incelenmiş belge dalıdır.

## 2. Hukuk ve yetki sınırı

CelikPanel, CELIKBROS'a ait proprietary bir yazılımdır. Sistemi devralacak
şirket, yüklenici veya mühendisin kaynağa, sürüm ürünlerine ya da operasyonel
erişime ulaşmadan önce CELIKBROS'tan açık yazılı yetki alması gerekir. Yetki
kaydı bu repoda değil, harici devir sicilinde tutulur.

Canlı panel değişikliklerinin sahibi panel kullanıcısıdır. DNS, nameserver,
DNSSEC, SSL, mail, firewall, servis, domain, kullanıcı ve veritabanı işlemleri
panelden yapılır. Dağıtım araçları incelenmiş CelikPanel ürünlerini kurabilir
veya geri alabilir; panel ayarlarını sessizce değiştiremez. Açıkça incelenmiş
ürün güncellemesi, bootstrap veya rollback yolu dışında SSH yalnız salt okunur
teşhis içindir.

## 3. Doğruluk kaynakları

Bilgiler çeliştiğinde şu sırayı kullanın:

1. Tam incelenmiş kaynak commit'i, testler ve paketlenmiş sürüm sözleşmeleri.
2. Kaynak sürüm kimliği için deploy/release-sequence-policy ve sürüme sabit
   download-portal/get.sh.
3. İmzalı ürün üretimi ve güveni için docs/release-signing.tr.md ile sürüm
   sözleşme testleri.
4. Operasyonel sahiplik ve rollout kuralları için docs/OPERATIONS.md.
5. Kalıcı ürün ve mimari kararlar için docs/DECISIONS.md.
6. Gerçek sunucu gözlemleri için tarihli canlı durum belgesi.
7. Plan, borç ve tarihsel bağlam için ROADMAP.md ve docs/AUTOPSY.md; ikisi de
   güncel runtime durumunun kanıtı değildir.

Artık `main` üzerinde bulunan devir paketi, daha önce saptanan kaynak-belge
drift'ini uzlaştırır:

- docs/OPERATIONS.md ve Türkçe eşi snapshot v6'yı, güncel rollback yolunun
  v4/v5'i reddettiğini ve eski snapshot'lar için eşleşen tarihsel sürüm sınırını
  artık açıklar.
- README.md etiketli sürüm update otoritesini Ed25519 imzalı manifest akışı
  olarak tanımlar ve GPG imzalamayı isteğe bağlı yerel kullanımla sınırlar.
- README, Roadmap, UI mimarisi ve web onboarding Alpha52'ye kadar uyumludur; gereksiz
  root create-vite scaffold'u kaldırılmıştır.

Bu nedenle R-001, R-002 ve R-010 `main` üzerinde kapanmıştır. R-012 de root
scaffold ve kopya worktree/kalıntı temizliği için `main` üzerinde
kapanmış/azaltılmıştır:
benzersiz işler PR #72 ve PR #73'te korunduktan sonra 109 kayıtlı kopya worktree,
105 eski yerel dal, 56 eski remote dal, `.attic`, `.worktrees`,
`.claude/worktrees`, root `__pycache__` ve geçici devir worktree'si
kaldırılmıştır. Yalnız primary kayıtlı worktree kalmıştır. Tracked
`.design-sync` bilinçli olarak tutulmuştur. Yeni ekip kabul sırasında taze ve
temiz bir `main` checkout'unu yine doğrulamalıdır. Bu repo temizliği canlı
sunucuda değişiklik yapmamıştır ve canlı durum kanıtı değildir. Binary release
ve rollback otoritesi için değişmez Alpha52 scriptlerini ve sözleşme testlerini
kullanmaya devam edin; yalnız belge içeren `main` değişiklikleri bunların yerini
almaz.

## 4. Runtime mimarisi

~~~text
Tarayıcı
  │ HTTPS :2083
  ▼
Panel — yetkisiz celikpanel kullanıcısı
  │ SQLite durumu + mühürlü uygulama sırları
  │ kimliği doğrulanmış yerel Unix-socket RPC
  ▼
Agent — root süreci, celikpanel grubu
  │ host değişikliklerinin tek otoritesi
  ▼
Yönetilen servisler ve host yapılandırması
~~~

Önemli kurulu yollar:

| Amaç | Yol |
|---|---|
| Panel ve agent binary'leri | /opt/celikpanel/bin/panel ve /opt/celikpanel/bin/agent |
| Gömülü web ürünleri | /opt/celikpanel/web/ |
| Panel veritabanı | /var/lib/celikpanel/celikpanel.db |
| Panel şifreleme anahtarı | /var/lib/celikpanel/secret.key |
| Agent özel durumu | /var/lib/celikpanel-agent-private/ |
| Agent RPC socket'i | /run/celikpanel/agent.sock |
| Agent RPC token'ı | /etc/celikpanel/agent.token |
| Panel yapılandırması | /etc/celikpanel/panel.env |
| Kurulu imzalı updater | /usr/libexec/celikpanel/get.sh |
| Sürüm güven durumu | /var/lib/celikpanel-release-state/ |
| Değişmez sürümler ve update snapshot'ları | /var/backups/celikpanel/ |

Yollar sorumluluğu tanımlar; içeriklerini okuma veya kopyalama izni vermez.
Veritabanı satırlarını, secret.key, agent.token, özel anahtarları, kimlik
bilgilerini veya ham üretim loglarını commit'e, issue'ya, sohbete ya da devir
belgesine koymayın.

## 5. Repo haritası

| Alan | Sorumluluk |
|---|---|
| cmd/panel | HTTP sunucusu, UI sunumu, kimlik doğrulama, yetkilendirme ve panel orkestrasyonu |
| cmd/agent | Yetkili RPC daemon'ı ve host/servis işlemleri |
| internal/db/migrations | Sıralı SQLite şema migration'ları |
| internal/transport | Panel-agent RPC sözleşmeleri ve Unix-socket taşıması |
| internal/core | Ortak katalog ve domain kuralları |
| internal/services | Servis yapılandırma ve orkestrasyon yardımcıları |
| internal/secrets | Mühürlü sır uygulaması |
| web | React/TypeScript arayüzü ve UI sözleşme testleri |
| deploy | Sürüm, kurtarma, imzalama, yayın, systemd ve sözleşme testleri |
| download-portal | Açık bootstrap ve indirme portalı |
| docs | Kararlar, operasyonlar, güvenlik sınırları ve ürün sözleşmeleri |
| .github/workflows | Zorunlu CI, paket ve etiketli sürüm işleri |

## 6. Geliştirme ve doğrulama

İncelenmiş backend derleyicisi tam olarak Go 1.26.5'tir ve otomatik toolchain
indirme kapalıdır. Etiketli sürüm CI'ı Node 24.18.0 kullanır. Shell sürüm
sözleşmeleri için Linux veya Linux uyumlu bir ortam kullanın; üretim portalı
publisher'ının ayrıca Windows PowerShell 5.1 sözleşme testi vardır.

Olağan bir değişiklik için asgari yerel kontroller:

~~~bash
make test vet web
cd web
npm ci --no-audit --no-fund
npm test
npm run build
~~~

Mevcut repo CI kapısı GitHub CI workflow'udur. Go biçimlendirme, build, race-test parçaları,
shell sözdizimi, sürüm/kurtarma sözleşmeleri, web testleri, cross-compile ve
tekrarlanabilir paketleme bunun içindedir. Yerel bir alt kümenin geçmesi, tam
itilmiş commit üzerindeki yeşil workflow'un yerine geçmez.
Tarayıcı render'ı, kritik-endpoint smoke ve latency kabulü ayrı açık kanıt
gereksinimleri olarak kalır.

Sürüm paketleme, olağan geliştirme döngüsü değil release-engineering işidir:

~~~bash
make dist VERSION=<tam-sürüm> COMMIT=<tam-commit> SOURCE_DATE_EPOCH=<commit-epoch>
~~~

Kirli checkout'tan sürüm derlemeyin veya yayımlamayın. Üretilmiş bin, web/dist
veya dist içeriğini kaynak otoritesi saymayın.

## 7. Değişiklik ve inceleme kuralları

- Kopyalanmış alpha*-wt dizininden veya kayıt dışı yerel worktree'den değil,
  canonical reponun temiz checkout'undan çalışın.
- İngilizce ve Türkçe belgeleri eşzamanlı tutun.
- Kimlikler, UI metinleri ve commit mesajları için docs/CONVENTIONS.md'ye uyun.
- Panel/Agent yetki sınırını koruyun. İstekle gelen yol, URL, paket bilgisi veya
  servis kimliği root otoritesine dönüşmemelidir.
- Değişen sınırda hata ve kurtarma yollarını da içeren testler ekleyin.
- Kaynak ağacı testi, canlı dağıtım kanıtı değildir.
- Git etiketi, release sequence veya indirme portalını birbirinden bağımsız
  ilerletmeyin.
- Canlı veritabanını, işlem işaretini, mutation journal'ını, release floor'u
  veya DNS engine durumunu elle düzenlemeyin.

## 8. Sürüm ve rollout devri

Alpha52 güncel ürün sürümüdür ve iki normal panel update'i de sunucu-otoriteli
terminal başarıyla tamamlanmıştır. Exact kaynak, imzalı ürünler, portal yayını ve
node receipt'leri [Alpha52 sürüm kanıtında](RELEASE-EVIDENCE-v0.1.0-alpha.52.tr.md)
ve tarihli [canlı durum kaydında](LIVE-STATE-2026-08-30.tr.md) kayıtlıdır. İki node
da build, servis, idle-ledger, şema-37, floor-52, kurulu ürün, sunulan UI, v6
snapshot ve rollback-yardımcısı kabulünü geçti. Buna karşın iki v6 snapshot'ın
kaynak bileşeni `unknown` değerindedir; receipt'ler önceki Alpha51 commit'ini
bağımsız kanıtladığından bunu canlı servis arızası değil açık provenance uyarısı
olarak ele alın.

Zone öncesi kasıtlı karma çift de doğrulanmıştır: Frankfurt BIND birincil, Boston
PowerDNS ikincildir; boş katalog serisi `1` iki tarafta aynıdır ve kaynağa bağlı
katalog AXFR iki yönde de geçer. Parent `.com` delegation ile in-bailiwick glue;
`ns1.celikhost.com → 72.62.38.15` ve `ns2.celikhost.com → 2.25.80.4`, TTL `172800`
olarak haricen doğrulandı; DS yoktur. Bu, açık otorite kanıtı değildir.
`celikhost.com` child zone henüz oluşturulmamıştır: doğrudan UDP/TCP sorguları
`REFUSED`, AXFR `NOTAUTH`, açık recursive çözümleme `SERVFAIL` döndürür. Child
zone'u normal panel akışıyla yayımlayın; DNS geçişini tamamlanmış ilan etmeden
önce zone sonrası katalog, AXFR, UDP/TCP authoritative ve açık-recursive kabul
matrisini bütünüyle tekrarlayın.

Mevcut kurulumda public bootstrap veya SSH updater kullanmayın; yeni request ID
uydurmayın, marker düzenlemeyin ve tarihsel canlı durum kaydını yeniden yazmayın.
Yeni tarihli kabul kaydı ekleyin. Kurtarma/eskalasyon bağlantıları:
[işletim](OPERATIONS.tr.md),
[26 Ağustos olayı](INCIDENT-2026-08-26-UPDATE-DNS-RECOVERY.tr.md) ve
[olay şablonu](INCIDENT-TEMPLATE.tr.md).

[Alpha51 GitHub sürümü](https://github.com/celikbros/celikpanel/releases/tag/v0.1.0-alpha.51),
etiket/commit kimliği, etiketli sürüm CI'ı, tam altı değişmez ürün ve resmi
Ed25519 manifest/imzası bu devir için doğrulanmıştır. Manifest'in yetkilendirdiği
arşiv 22.644.115 bayttır ve SHA256 değeri
`57d0321a13388392872bc3aef9af62646e2d700c23a4e0305d479df1e80ff365` şeklindedir.
Altı ürün; genel arşiv ve checksum, Linux/amd64 arşiv ve checksum, imzalı
manifest ve ayrık imzadır.

Git etiketinin kendisi imzasızdır. Kurulum/update otoritesi Git etiketi imzası
değil, sabitlenmiş Ed25519 güven kökü ile doğrulanmış imzalı manifest v2'dir.
Alpha52 güncel ürün sürümüdür ve iki node'daki exact kanıtlanmış canlı kurulumdur.
Açık DNS geçişi child zone yokluğu nedeniyle engellidir.

Sonraki sürümden önce:

1. Bundan sonraki devir/belge değişikliklerini binary sürüm kimliğinden bağımsız
   inceleyin; Alpha51 uzlaştırması zaten merge edilmiştir.
2. Yeni etiketin incelenmiş main commit'ini gösterdiğini ve CI'ın yeşil olduğunu
   kanıtlayın.
3. Altı GitHub release ürününü ve checksum/imzalarını doğrulayın.
4. Portal adayını yalnız izlenen publisher ile oluşturup yayımlayın.
5. Sır içermeyen sürüm kanıtını ve değişmez ürün özetlerini kaydedin.
6. Önce Boston'u doğrulayın.
7. Boston kapılarının tamamı geçmeden Frankfurt'a dokunmayın.
8. Yalnız eşleşen değişmez sürüm rollback yardımcısını ve v6 snapshot'ını
   kullanın.

Güncel canlı sürümler, şema ve rollback snapshot'ları bu belgeyle kanıtlanmaz.
Canlı durum kaydına bakın.

## 9. Erişim ve sır devri

Gerçek hesap adları, anahtar yolları, kurtarma kodları, kimlik bilgileri,
token'lar, sağlayıcı kimlikleri ve custodian adları onaylı harici parola
yöneticisi veya erişim sicilinde aktarılmalıdır. Repo yalnız gerekli
kategorileri kaydeder:

Chat, ticket, ekran görüntüsü veya shell history içinde paylaşılmış her kimlik
bilgisini ele geçirilmiş sayın. Yeni ekip erişiminden önce yenileyin ve kimlik
bilgisi değerini kopyalamadan yenileme kanıtını repo dışında kaydedin.

| Erişim kategorisi | Repodaki değer |
|---|---|
| CELIKBROS yazılı yetkisi | REPO DIŞI / CUSTODIAN ATA |
| GitHub organizasyon ve repo yönetimi | REPO DIŞI / CUSTODIAN ATA |
| GitHub release-signing secret yönetimi | REPO DIŞI / CUSTODIAN ATA |
| Monotonik release-sequence yönetimi | REPO DIŞI / CUSTODIAN ATA |
| Ed25519 özel anahtar ve kurtarma kopyası emaneti | REPO DIŞI / CUSTODIAN ATA |
| İndirme portalı host'u ve publisher SSH kimliği | REPO DIŞI / CUSTODIAN ATA |
| Boston ve Frankfurt VPS yönetimi | REPO DIŞI / CUSTODIAN ATA |
| celikpanel.net ve celikhost.com registrar/DNS kontrolü | REPO DIŞI / CUSTODIAN ATA |
| Panel yönetici hesapları | REPO DIŞI / CUSTODIAN ATA |
| Yedek şifreleme ve kurtarma malzemesi | REPO DIŞI / CUSTODIAN ATA |
| Güvenlik bildirimi ve olay escalation kanalı | REPO DIŞI / CUSTODIAN ATA |

Özel hesaplar ve SSH public key'leri verin; parola veya özel anahtar
paylaşmayın. Verme, inceleme, rotation ve iptal tarihlerini repo dışında
kaydedin. Eski ekibin erişimini ancak yeni ekip doğrulanmış salt okunur
envanteri ve kontrollü erişim testini tamamladıktan sonra kaldırın.

## 10. Yeni ekibin ilk gün listesi

- Yazılı yetkiyi ve harici custodian atamalarını doğrulayın.
- Temiz bir checkout alın; etiketi, commit'i, origin'i ve temiz durumu kanıtlayın.
- Alpha52 sürüm kanıtını ve tarihli Alpha52 canlı durum kaydını inceleyin. PR #72
  ve PR #73 arşivliktir; ikisini de olduğu hâliyle merge etmeyin ve PR #73'ü
  olduğu hâliyle çalıştırmayın.
- Sunucuya erişmeden önce SECURITY.md, OPERATIONS.md, release-signing.md,
  DECISIONS.md ve bu devir belgelerini okuyun.
- Canlı durum kaydındaki tüm BİLİNMİYOR / YENİDEN DOĞRULA alanlarını salt
  okunur tamamlayın.
- Boston ve Frankfurt'un test, staging veya production olduğunu kesinleştirin.
- Güncel yedek kapsamını ve secret.key ile diğer kontrol düzlemi anahtarlarının
  kurtarılabilirliğini kanıtlayın.
- Her açık riske repo dışında sorumlu ve hedef tarih atayın.
- Kaynak referansı, canlı referans ve rollback yolu bağımsız kanıtlanmadan canlı
  değişiklik yapmayın.

## 11. Devrin tamamlanma ölçütleri

Mühendislik devri ancak şu koşullarda tamamlanır:

- paylaşılan repo checkout'u temizdir ve kopya worktree içermez;
- canonical etiket, tam commit, CI çalışması ve altı release ürünü uyuşur;
- operasyon belgeleri snapshot v6 ve Ed25519 otoritesiyle uyumludur;
- iki sunucu envanterinin tarihli salt okunur kanıtı vardır;
- ortam sınıfı ve kurulu kimlikler açıktır;
- disaster-recovery kararı ve restore kanıtı vardır;
- harici erişim, hukuki yetki, custodian ve escalation atanmıştır;
- hiçbir sır değeri repoya veya devir belgelerine girmemiştir.
