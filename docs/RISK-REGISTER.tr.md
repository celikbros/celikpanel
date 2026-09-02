# Mühendislik ve Operasyon Risk Sicili

*Referans: 30 Ağustos 2026 · [English](RISK-REGISTER.md)*

Bu sicil, bilinen devir boşluklarını ve
[Alpha52 sürümüne](RELEASE-EVIDENCE-v0.1.0-alpha.52.tr.md) kadar `main` dalına
merge edilen azaltımları izler. Sır değeri veya gerçek custodian adı
içermez. Her sorumlu, hedef tarih, kabul ve harici kanıt referansı onaylı repo
dışı devir sisteminde atanmalıdır.

[#69](https://github.com/celikbros/celikpanel/pull/69),
[#70](https://github.com/celikbros/celikpanel/pull/70) ve
[#71](https://github.com/celikbros/celikpanel/pull/71) kapalı ve superseded'dır.
Bilinçli tutulan main dışı iki remote head; arşivlik PR
[#72](https://github.com/celikbros/celikpanel/pull/72) kaynağı
`agent/ssl-hostnames-hsts` ile arşivlik PR
[#73](https://github.com/celikbros/celikpanel/pull/73) kaynağı
`archive/alpha35-portal-tooling` dallarıdır. İkisi de olduğu hâliyle merge
edilmemeli veya çalıştırılmamalıdır. Bu referansta açık pull request yoktur.

## Durum ve önem

- AÇIK: Azaltma tamamlanmadı.
- YENİDEN DOĞRULA: Koşul değişmiş olabilir; güncel kanıt yoktur.
- ENGELLEYİCİ: Çıkış ölçütleri geçmeden ilgili işlemi yapmayın.
- MAIN ÜZERİNDE KAPALI: Yalnız repo içi çıkış ölçütleri `main` üzerinde
  karşılandı; bu durum canlı sunucu kanıtı değildir.
- KISMEN AZALTILDI / YENİDEN DOĞRULA: Sınırlı bir bileşen düzeltildi; kalan
  koşul için kabul kanıtı gerekir.
- KISMEN AZALTILDI / AÇIK: Repo kontrolleri vardır; hesap verebilir harici atama
  veya kabul koşulu açık kalır.
- Kritik: Geri döndürülemez duruma, güvensiz yetkili işleme veya
  release/rollback otoritesi kaybına yol açabilir.
- Yüksek: Kesintiye, güvenlik sınırı hatasına veya kanıtlanamayan canlı
  dağıtıma yol açabilir.
- Orta: Hata, drift veya onboarding riskini önemli ölçüde artırır.

## Risk özeti

| ID | Önem | Durum | Risk |
|---|---|---|---|
| R-001 | Kritik | MAIN ÜZERİNDE KAPALI | Operations artık snapshot v6'yı, güncel v4/v5 reddini ve tarihsel sürüm sınırını anlatıyor |
| R-002 | Yüksek | MAIN ÜZERİNDE KAPALI | README artık isteğe bağlı yerel GPG kullanımını canonical Ed25519 update otoritesinden ayırıyor |
| R-003 | Kritik | AÇIK / GERÇEK TENANT İÇİN ENGELLEYİCİ | Tam kontrol düzlemi disaster backup ve restore tatbikatı kanıtlanmadı |
| R-004 | Yüksek | KISMEN AZALTILDI / YENİDEN DOĞRULA | İki host exact Alpha52 ve terminal receipt ile tam kabulü geçti; snapshot kaynak provenance'ı `unknown` kaldı |
| R-005 | Yüksek | AÇIK | Boston/Frankfurt ortam sınıfı üretime-hazır-değil politikasıyla çelişkili |
| R-006 | Yüksek | AÇIK | Route/role ve API sözleşme borcu güvenlik sınırında sürüyor |
| R-007 | Yüksek | AÇIK | Zorunlu gerçek VM install/update/rollback/reboot kanıtı devirde yok |
| R-008 | Yüksek | KISMEN AZALTILDI / YENİDEN DOĞRULA | Alpha52 receipt'leri ve zone öncesi karma-motor katalog çifti doğrulandı; owner-zone ve zone sonrası otorite kanıtı yok |
| R-009 | Orta | AÇIK | Harici paket/repo/CA endpoint'leri canlı doğrulama kapısı olmadan bayatlayabilir |
| R-010 | Orta | MAIN ÜZERİNDE KAPALI | Mimari, onboarding ve uygulama-durumu belgeleri Alpha52'ye kadar uzlaştırıldı |
| R-011 | Yüksek | AÇIK | Erişim, signing key, provider ve olay custodian'ları devirde atanmadı |
| R-012 | Orta | MAIN ÜZERİNDE KAPALI/AZALTILMIŞ / YENİ EKİP TEMİZ CHECKOUT YENİDEN DOĞRULA | Root scaffold, kopya worktree/dallar ve listelenen kalıntılar kaldırıldı; yeni ekip temiz bir `main` checkout'unu doğrulamalıdır |
| R-013 | Orta | AÇIK | Tarayıcı golden-path, kritik endpoint ve latency kanıtı eksik |
| R-014 | Orta | KISMEN AZALTILDI / AÇIK | Olay şablonu ve ilk olay kaydı var; harici müdahale sahipliği atanmadı |
| R-015 | Yüksek | AÇIK / AÇIK DNS GEÇİŞİ İÇİN ENGELLEYİCİ | Parent delegation ve glue doğrulandı; `celikhost.com` child zone ve açık otorite yok |
| R-016 | Orta | AÇIK / PROVENANCE UYARISI | İki geçerli v6 snapshot kaynak kimliğini `unknown` yazar; terminal receipt'ler önceki Alpha51 commit'ini kanıtlar |
| R-017 | Yüksek | AÇIK | Üretim panelinin kalp atışı, paket kuran bir DNS motoru geçişini belirlenimci biçimde zehirler |
| R-018 | Orta | AÇIK | BIND mask ön denetimi, standart bir Arch kök dizinini reddeder; o imajda BIND'a ulaşılamaz |
| R-019 | Orta | AÇIK | Devralınmış dış PowerDNS, beslemesi beklenen BIND devri için geçişe hazır değildir |
| R-020 | Düşük | AÇIK | `cmd/panel` race paketi, açıkça verilmiş 30 dakikalık tavanın yüzde 87'sini tüketir |
| R-021 | Düşük | ÇÖZÜLDÜ / ENVANTER DÜZELTİLDİ | İki sunucu da yeniden kuruldu; kimlik doğrulandı ve envanter artık Ubuntu ile Debian 13 yazıyor. Envanterimizde Arch sunucu kalmadı |
| R-022 | Kritik | DALDA DÜZELTİLDİ / ATILABİLİR VM'LERDE KANITLANDI / MAIN'DE DEĞİL | `install.sh`, güvenilen sürüm kökü var olmadan sürüm işlem korumasını source ediyor; temiz kurulum başlamadan çıkıyor |
| R-023 | Yüksek | DALDA DÜZELTİLDİ / ATILABİLİR VM'LERDE KANITLANDI / MAIN'DE DEĞİL | Taze veritabanında `SKIP_ADMIN=1` sıfır kullanıcı bırakır; panel tasarımı gereği çıkar ve kurulum systemd yeniden başlatma döngüsüyle biter |
| R-024 | Orta | DALDA DÜZELTİLDİ / ATILABİLİR VM'LERDE KANITLANDI / MAIN'DE DEĞİL | Kurulum `systemctl enable` hatalarını yutar ve enable bağlarını hiç eşitlemez; taze bir sunucu iki birimi de devre dışı hâlde yeniden başlayabilir |
| R-025 | Düşük | KARAR VERİLDİ / BELGE DALDA DÜZELTİLDİ | Belgelenen `git clone && sudo ./install.sh` yolculuğu, kurtarma temelinin kullanıcıya ait üst dizinleri reddetmesiyle çelişir |
| R-026 | Yüksek | AÇIK / DALDA DÜZELTİLDİ, CANLI KANIT BEKLİYOR | PowerDNS geçiş geri alması yedek ana dosyayı atılan neslin WAL/SHM dosyalarının altına koydu ve canlı veritabanı bozuk kaldı |
| R-027 | Düşük | BELGELENMİŞ SINIR | PowerDNS yetkisi yalnız APT/Debian/systemd için onaylıdır; Arch PowerDNS'i devralamaz ya da ona geçemez; Arch kanıtları yalnız-BIND yolculukları kullanmalıdır |

## Ayrıntılı riskler

### R-001 — Snapshot sözleşmesi belge uyumsuzluğu

- Kanıt: `main` docs/OPERATIONS.md ve Türkçe eşini snapshot v6'ya günceller,
  güncel updater/rollback yolunun v4/v5'i reddettiğini belirtir ve eski
  snapshot'ı eşleşen değişmez tarihsel recovery sürümü ile rollback yardımcısıyla
  sınırlar.
- Etki: Yeni operatör uyumsuz rollback seçebilir veya restore edilemeyen
  snapshot'ın kabul edildiğini sanabilir.
- Kapanış dayanağı: İngilizce/Türkçe runbook'lar artık kaynak sözleşmesiyle
  uyumludur. Değişmez Alpha52 scriptleri ve sözleşme testleri binary otoritesi
  olarak kalır.
- Durum: MAIN ÜZERİNDE KAPALI. Bu yalnız belge kapanışıdır; canlı veya disposable
  restore kanıtı R-003 ve R-007 tarafından izlenmeye devam eder.

### R-002 — Release-signing otoritesi belirsizliği

- Kanıt: `main` README'yi; Ed25519 imzalı manifest v2, release sequence,
  sabitlenmiş public key ve tam altı ürünü etiketli sürüm update otoritesi olarak
  tanımlayacak şekilde günceller. İsteğe bağlı yerel GPG imzalama açıkça update
  otoritesi değildir.
- Etki: Ekip yalnız bütünlük sağlayan veya isteğe bağlı ürünleri yayınlayıp
  yetkili update otoritesinin oluştuğunu sanabilir.
- Kapanış dayanağı: README ve release-signing rehberi artık otorite sınırını
  açıklar; Alpha52 resmi manifest/imzası ile altı ürün kümesi doğrulanmıştır.
- Durum: MAIN ÜZERİNDE KAPALI. Portal/canlı eşitliği R-008'de izlenir ve bu belge
  kapanışından türetilemez.

### R-003 — Kontrol düzlemi disaster recovery kanıtsız

- Kanıt: ROADMAP.md panel-state disaster backup ve restore tatbikatını geleceğe
  koyar ve secret.key kaybının mühürlü sırları kurtarılamaz yaptığını söyler.
  Devirde başarılı clean-host restore kanıtı yoktur.
- Etki: Node kaybında domain dosyaları kalsa bile kimlik bilgileri,
  DKIM/WireGuard malzemesi, panel kimliği veya yönetilebilir durum kalıcı
  kaybolabilir.
- Acil kontrol: Gerçek tenant almayın veya disaster recovery iddiasında
  bulunmayın. Mevcut yetkili yedeklerin içeriğini repoya kopyalamadan koruyun.
- Çıkış ölçütü: Sürümlü ve şifreli yedek SQLite durumu, secret.key, ilgili
  kontrol düzlemi anahtarları ve sertifikaları içerir; retention ve off-host
  saklama tanımlıdır; temiz host restore tatbikatı servis ve kriptografik kimlik
  kurtarmayı kanıtlar; RPO/RTO repo dışında kabul edilir.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

### R-004 — Alpha52 canlı promotion kanıtlandı; artık kabul kanıtı korunmalı

- Kanıt: [Alpha52 sürüm kaydı](RELEASE-EVIDENCE-v0.1.0-alpha.52.tr.md) ve
  [tarihli canlı durum](LIVE-STATE-2026-08-30.tr.md), iki node'da terminal
  receipt, exact build `adb25d8ec487dcb76dd95304a551d8cb37565115`, aktif
  servisler, idle ledger'lar, kesintisiz şema 37, floor 52, byte-equal kurulu
  ürün/sunulan UI ile doğrulanmış v6 snapshot/rollback yardımcılarını kanıtlar.
- Etki: Bu rollout artık kanıtlanmamış canlı-sürüm eşitliği riski taşımaz; fakat
  kanıt sonraki sürümlerde ilişkilendirilebilir ve tekrarlanabilir kalmalıdır.
- Acil kontrol: Tarihli kaydı ve exact receipt'leri koruyun; gelecekteki host
  durumunu etiketten veya portaldan türetmeyin. Tarihsel
  [LIVE-STATE-2026-08-29.tr.md](LIVE-STATE-2026-08-29.tr.md) kaydını değiştirmeyin.
- Çıkış ölçütü: Yalnız R-016 snapshot provenance uyarısı daha sonraki incelenmiş
  sürümde düzeltilip yeni normal panel update'iyle kanıtlandıktan sonra kapatın.
- Durum: KISMEN AZALTILDI / YENİDEN DOĞRULA. Alpha52 canlı kimliği kanıtlandı;
  kalan provenance koşulu R-016 altında ayrıca izlenir.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

### R-005 — Ortam ve hazır olma belirsizliği

- Kanıt: SECURITY.md üretime hazır değil der; Operations production rollout
  dili kullanır; Roadmap Boston ve Frankfurt'a test sunucuları da der.
- Etki: Açık risk kararı olmadan Alpha sisteme müşteri verisi veya
  erişilebilirlik beklentisi yüklenebilir.
- Acil kontrol: İki node'u da sınıflandırılmamış, ürünü ön sürüm kabul edin.
- Çıkış ölçütü: Her node test, staging veya production olarak sınıflanır;
  müşteri verisi durumu, değişiklik otoritesi, izleme ve kabul ölçütleri repo
  dışında kaydedilir; açık ifadeler kararla uyumludur.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

### R-006 — API ve yetkilendirme sözleşme borcu

- Kanıt: docs/AUTOPSY.md OpenAPI/generated client işini ve route-plus-role
  tablo/matrisini eksik bırakır. Mevcut testler riski azaltır fakat beyan edilen
  yapısal borcu kapatmaz.
- Etki: Yeni rota veya frontend çağrısı tenant/role yetkilendirme
  beklentilerinden uzaklaşabilir.
- Acil kontrol: Değişen her endpoint için açık backend authorization incelemesi
  ve negatif rol testleri isteyin.
- Çıkış ölçütü: Tek route/role registry, tam role-by-endpoint matris testleri,
  üretilmiş API sözleşmesi/client ve yinelenen elle yazılmış otoritenin
  kaldırılması.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

### R-007 — Gerçek ortam sürüm kanıtı boşluğu

- Kanıt: Operations boot-kritik değişiklikler için disposable Debian 13,
  Ubuntu 24.04 ve güncel Arch Linux install/update/rollback/reboot kanıtı ister.
  Devirde tam kanıt kümesi yoktur. deploy/e2e/rhel9 açıkça yalnız blocked smoke
  probe'dur; başarılı kurulum sertifikasyonu değildir.
- Etki: Mock ve sözleşme testleri geçerken paketleme, systemd, firewall veya
  reboot davranışı gerçek host'ta bozulabilir.
- Acil kontrol: Bu alanlardaki değişiklikler zorunlu VM kanıtı olmadan dağıtım
  engelli kalır.
- Çıkış ölçütü: Her zorunlu OS ve durum için sır içermeyen kanıt, tam commit ve
  ürün digest'ine repo dışında bağlanmıştır.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

### R-008 — Alpha52 canlı otoritesi kanıtlandı; zone sonrası DNS kabulü açık

- Kanıt: [Alpha52 sürüm kaydı](RELEASE-EVIDENCE-v0.1.0-alpha.52.tr.md) ile
  [tarihli canlı durum](LIVE-STATE-2026-08-30.tr.md), iki exact kurulumu ve zone
  öncesi çifti kanıtlar: Frankfurt BIND primary, Boston PowerDNS secondary, boş
  katalog serisi `1` eşit ve source-bound AXFR iki yönde geçer. Parent delegation
  ve glue doğrulandı; owner zone yoktur.
- Etki: Kontrol düzlemi çifti sağlıklıdır fakat `celikhost.com` için otorite
  sunamaz; açık DNS hazır iddiası yanlış olur.
- Acil kontrol: Doğrulanmış karma çifti koruyun ve child zone'u yalnız normal
  panel akışıyla oluşturun. Başarısız açık kontrolleri atlamayın.
- Çıkış ölçütü: Child-zone yayını sonrasında iki motor eşit katalog
  üyeliği/serial, source-bound AXFR ve UDP/TCP AA/SOA kanıtlar; bağımsız recursive
  resolver'lar `SERVFAIL` döndürmeyi bırakır.
- Durum: KISMEN AZALTILDI / YENİDEN DOĞRULA. Kurulum ve zone öncesi eşleşme
  geçer; zone sonrası kabul R-015 altında açıktır.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

### R-009 — Harici endpoint güncelliği

- Kanıt: docs/AUTOPSY.md ölü bir ACME endpoint'inin ürüne girdiğini kaydeder ve
  periyodik canlı doğrulamayı açık kural olarak bırakır.
- Etki: Destekleniyor görünen CA, repo veya entegrasyon yalnız müşteri işlemi
  sırasında başarısız olabilir.
- Acil kontrol: Kendisine bağlı bir sürümden önce etkilenen resmi endpoint'leri
  elle doğrulayın.
- Çıkış ölçütü: Sınırlı zamanlanmış testler katalog/registry harici URL'lerini
  kapsar, geçici kesintiyle kalıcı kapanmayı ayırır ve uygulanabilir alarm üretir.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

### R-010 — Belge ve onboarding drift'i

- Kanıt: `main` README'yi Alpha52 ve imzalı update yoluyla uyumlu yapar, Roadmap
  metriklerini elle tutmak yerine generated yapar, iki UI mimarisi belgesinde
  role-aware web/src/nav.ts registry'sini açıklar ve genel web/README.md şablonunu
  ürüne özel onboarding ile değiştirir.
- Etki: Yeni mühendisler eski komut seçebilir, uygulanmış davranışı yanlış
  anlayabilir veya ürün yerine scaffolding'i yeniden kurabilir.
- Kapanış dayanağı: README, Roadmap durumu, mimari ve web onboarding Alpha52'ye
  kadar uzlaştırılmıştır; eskiyebilecek fact snapshot'ları tarihlidir
  veya generated'dır.
- Durum: MAIN ÜZERİNDE KAPALI. Gelecekte kaynak değiştiğinde ilgili bilgiler yine
  güncellenmeli veya üretilmelidir.

### R-011 — Custodian ve erişim sürekliliği

- Kanıt: Repo politikası sırları doğru biçimde dışlar; fakat GitHub, signing,
  release sequence, portal, VPS, registrar/DNS, yedek veya olay escalation
  custodian'ı atamaz.
- Etki: Yeni ekip release veya recovery yapamayabilir; paylaşılan erişimler
  hesap vermeden açık kalabilir.
- Acil kontrol: Yalnız özel hesap ve public key'leri onaylı harici sistemden
  devredin. Buraya değer eklemeyin.
- Çıkış ölçütü: HANDOFF.tr.md içindeki her kategorinin harici asıl/yedek
  custodian'ı, verme/inceleme/iptal tarihleri ve test edilmiş erişim yolu vardır.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

### R-012 — Yanlış ağaç ve repo çöpü riski

- Kanıt: `main` gereksiz root package.json/package-lock.json create-vite
  scaffold'unu kaldırır. Benzersiz ürünler PR #72 ve PR #73'te korunduktan sonra
  temizlik; 109 kayıtlı kopya worktree'yi, 105 eski yerel dalı, 56 eski remote
  dalı, `.attic`, `.worktrees`, `.claude/worktrees`, root `__pycache__` ve geçici
  devir worktree'sini kaldırmıştır. Yalnız primary kayıtlı worktree kalmıştır.
  Tracked `.design-sync` bilinçli olarak tutulmuştur.
- Etki: Değişiklik yanlış ağaçta yapılabilir veya kopyalar ürün kodu olarak
  yanlışlıkla commit edilip incelenebilir.
- Acil kontrol: Yeni ekip çalışmaya başlamadan önce taze bir `main` checkout'u
  kullanmalı, git status'u ve worktree listesini doğrulamalıdır.
- Çıkış ölçütü: Temizlik `main` üzerinde kapanmış/azaltılmıştır; yeni ekip kanıtı temiz
  bir `main` checkout'unda yalnız kasıtlı kayıtlı worktree'ler gösterir. Tracked
  `.design-sync` kalıntı değildir ve bilinçli olarak tutulur.
- Durum: MAIN ÜZERİNDE KAPALI/AZALTILMIŞ / YENİ EKİP TEMİZ CHECKOUT YENİDEN DOĞRULA. Bu
  temizlik canlı sunucuda değişiklik yapmamış ve canlı runtime kanıtı
  sağlamamıştır.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

### R-013 — Golden-path ve latency kanıtı

- Kanıt: docs/AUTOPSY.md browser render, kritik endpoint smoke ve 100 ms altı
  ölçümü eksik bırakır.
- Etki: Derleme ve unit sözleşmeleri geçerken müşteri yolculuğu veya performans
  hedefi bozulabilir.
- Acil kontrol: Etkilenen yolculuklarda hedefli UI sözleşme testleri ve elle
  kabul isteyin.
- Çıkış ölçütü: CI gerçek paneli açar, kritik kimliği doğrulanmış yolculukları
  çalıştırır, sınırlı tarayıcı kanıtı kaydeder ve belirtilen latency hedefini
  ölçer.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

### R-014 — Olay müdahalesi sahipliği

- Kanıt: SECURITY.md özel bildirimi tanımlar. Sır içermeyen
  [olay şablonu](INCIDENT-TEMPLATE.tr.md) ile
  [26 Ağustos olay kaydı](INCIDENT-2026-08-26-UPDATE-DNS-RECOVERY.tr.md) artık
  kanıt sözlüğü, zaman çizelgesi, kurtarma, düzeltici işlem ve kapanış yapısını
  sağlar. Harici on-call, severity sorumlusu, incident commander, escalation
  süresi veya postmortem action owner atanmamıştır.
- Etki: DNS, release veya güvenlik olayı takılabilir ya da güvensiz ad-hoc
  değişikliklerle ele alınabilir.
- Acil kontrol: Panel değişikliği ve salt okunur SSH sınırlarını koruyun; atanmış
  harici escalation kanalını kullanın.
- Çıkış ölçütü: Repo dışında atanmış severity modeli, kişiler, commander,
  iletişim yolu ve postmortem/action takibi; bir tatbikat veya gerçek olay,
  repo şablonuyla acknowledgement, devir ve action-owner kapanışını kanıtlar.
- Durum: KISMEN AZALTILDI / AÇIK. Repo şablonu tamamdır; hesap verebilir harici
  sorumlu atamasının yerini tutmaz.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

### R-015 — Child-zone ve açık-otorite engelleyicisi

- Kanıt: Parent delegation ve exact glue haricen doğrulandı:
  `ns1.celikhost.com → 72.62.38.15` ve `ns2.celikhost.com → 2.25.80.4`, TTL
  `172800`; DS yoktur. [Tarihli canlı durum](LIVE-STATE-2026-08-30.tr.md), child
  zone'un oluşturulmadığını kaydeder: doğrudan UDP/TCP sorguları `REFUSED`, AXFR
  `NOTAUTH`, açık recursive çözümleme `SERVFAIL` döndürür.
- Etki: Resolver'lar delegated sunuculara ulaşır fakat authoritative
  `celikhost.com` zone'u alamaz; açık çözümleme kullanılamaz.
- Acil kontrol: Açık DNS trafiğini bu çifte geçirmeyin ve açık authoritative
  availability iddiasında bulunmayın. Registrar kimlik bilgilerini repoya
  koymayın.
- Çıkış ölçütü: Child zone normal panel akışıyla yayımlanır; iki motor zone
  sonrası eş katalog üyeliği/serial, source-bound AXFR ve UDP/TCP AA/SOA
  kanıtlar; bağımsız resolver'lar zaten doğrulanmış delegation/glue üzerinden
  açık yanıtları kanıtlar.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

### R-016 — Snapshot kaynak kimliği `unknown`

- Kanıt: İki Alpha52 terminal receipt'i beklenen önceki Alpha51 commit'ini
  `45d01ffb29013b9457180072c3b25ab24d5ff7bd` olarak tanımlarken, iki doğrulanmış
  v6 snapshot dizin kimliği `from-unknown-to-adb25d8ec487-*` kullanır. Snapshot
  checksum, hedef kimliği ve rollback yardımcıları bütünüyle geçmiştir.
- Etki: Rollback malzemesi sağlamdır; fakat snapshot yolu tek başına kaynak
  build'i kanıtlayamaz ve forensic provenance ile otomatik devir kontrollerini
  zayıflatır.
- Acil kontrol: Terminal receipt'i snapshot kanıtıyla birlikte koruyun; canlı
  snapshot'ı yeniden adlandırmayın, yazmayın veya yeniden kurmayın.
- Çıkış ölçütü: İncelenmiş bir sürüm sonraki v6 snapshot kimliğine doğrulanmış
  kurulu kaynak version/commit'ini yazar ve normal panel update'i düzeltmeyi
  regresyonsuz kanıtlar.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

### R-017 - Panel kalp atışı, paket kuran DNS geçişini iptal ediyor

- Kanıt: S-1 kill-matrix run 8 (ara rapor SHA-256
  `85bde76952fe05ee7a7a47730b8242406bb5e0924aab739a1e8ea091b2798724`).
  `cmd/agent/dns_engine_rpc.go:1339-1361` mutasyonu arka uç işinden önce
  "sonlanıyor" olarak işaretler; oysa `:1597-1620,1648-1675` konumundaki
  sonlanma yüklemi `WorkerPID == 0` ve boş işçi kimliği şart koşar.
  `cmd/agent/service_mutation_worker.go:120-154,202-244` paket işçisini kalıcı
  olarak kaydeder ve `cmd/panel/service_mutation_agent.go:28,793-804` her beş
  saniyede kalp atışı gönderir. O geçerli kayıtlı aralığa denk gelen bir kalp
  atışı yüklemi düşürür, yöneticiyi zehirler, işlemi iptal eder ve ardından
  rollback işçi kaydını reddeder.
- Etki: Paket kurmak zorunda olan bir DNS motoru geçişi - yeni bir sunucuda
  olağan durum - panelin kendi canlılık denetimince iptal edilebilir. O çakışma
  koşuluyla ret belirlenimcidir, aralıklı değil. Run 8 geride kurulum sahiplik
  makbuzu ile kiralanmış işçi kimliği bıraktı; DNS durumu ve günlük yok. Bu
  kalıntı mevcut hiçbir denetime görünmez.
- Acil kontrol: Üründe kullanılabilir kontrol yok. Kalp atışını bastırmak ya da
  ertelemek bir kontrol değildir: yirmi saniyelik kiralama
  (`cmd/agent/service_mutation_rpc.go:39`) aynı bekçiden geçerek sona erer
  (`:1424-1443`), yani arıza yalnızca kiralama sonuna kayar.
- Çıkış ölçütü: Sonlanma yüklemi, rakip bir mutasyon ile sahip mutasyonun kendi
  kayıtlı işçisini birbirinden ayırır; sınırlı, işçiden haberdar bir bekçi
  incelemeyi geçer; ve paket kuran bir geçiş, üretim temposunda kalp atışı
  altında canlı bir düzenekte tamamlanır.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.
- S-2 güncellemesi: Yalnız inceleme amaçlı, işçiden haberdar bir bekçi yazılıp
  test edildi (`artifacts/s2-vm-acceptance/p1/`), ardından yazarı tarafından
  land edilmesi güvenli bulunmadı. Altında iki sıralama kusuru duruyor:
  korunan kalp atışı genel kiralama yenilemesinden önce dönüyor, bu yüzden bir
  işçi temizliği `UpdatedAt` değerini özgün yirmi saniyelik kiralamanın ötesine
  taşıyıp `LeaseExpiresAt < UpdatedAt` bırakabiliyor; ve `cmd.Wait()` çocuğu
  `tracker.clear()` kalıcı işçiyi silmeden önce topluyor, dolayısıyla o
  aralıktaki bir bekçi doğru biçimde ölmüş bir işçiyi görüp yine de reddediyor.
  Ne kiralamadan uzun süren canlı bir paket kurulumu ne de belirlenimci bir
  toplama-temizleme testi koşuldu. Yama HEAD'de değildir ve bu hâliyle land
  edilmemelidir.
- Düzeltme uygulandı (31 Ağustos 2026, `fix/alpha52-handoff-acceptance` dalı):
  sonlanma aralığı kanıtı artık sahibi olan mutasyonun kayıtlı işçisini
  biçimiyle kabul ediyor — bilerek canlılık sondası olmadan; bu,
  toplama-temizleme penceresini hoş görmek yerine kökten kaldırıyor. Korumalı
  kalp atışı erken dönmek yerine kiralamayı yeniliyor ve iki kalıcı işçi
  geçişi de kiralamayı yalnız iş Running durumundayken yeniliyor; böylece
  süresi-dolmuş-iptal kanıtının sıralaması korunuyor. Birim ve linux
  bütünleşme testleri koşu-8 defter biçimini (kayıtlı apt-get, duraklamış
  kiralama, kalp atışı, süre dolumu, iptal, temizleme) yeniden üretiyor ve
  geçiyor. Canlı kanıt — gerçek bir düzenekte, üretim temposundaki beş
  saniyelik kalp atışları altında paket kuran bir geçiş — hâlâ bekliyor ve bu
  kaydı AÇIK tutuyor.

### R-018 - BIND ön denetimi standart Arch kök dizinini reddediyor

- Kanıt: S-1 kill-matrix run 6. `cmd/agent/dns_engine_bind_mask_linux.go`, mask
  üst dizin zincirini yürürken `/` dahil olmak üzere tam `bindManagedRootMode`
  0755 beklentisini uygular; resmi Arch imajı `/` dizinini 0555 sunar. İstek
  intent günlük yazımına bile ulaşamaz. İlgili diğer üst dizinler 0755'ti, yani
  bu geniş bir düzenek bozulması değil tek ve belirli bir beklentidir.
- Etki: Standart bir Arch kurulumunda BIND motoruna ulaşılamaz. Arch,
  OPERATIONS.md 3. bölüm uyarınca zorunlu bir kabul dağıtımıdır; dolayısıyla bu
  aynı zamanda sürüm matrisinin bir bölümünü de engeller.
- Acil kontrol: Geçen bir koşu elde etmek için kök dizine `chmod` uygulamayın.
  Bu, reddi gizler ve düzeneği temsil edici olmaktan çıkarır.
- Çıkış ölçütü: `/` üzerindeki 0755'in mask politikası için taşıyıcı mı yoksa
  normal bir Linux kökü için baştan yanlış bir beklenti mi olduğuna dair yazılı
  bir karar ve buna karşılık gelen düzeltme ya da belgelenmiş dağıtım sınırı.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.
- S-2 kararı: `/` üzerindeki `0755` bu işlem için taşıyıcı **değildir**.
  Mutasyon doğrulanmış `/etc/systemd/system` dizinine iner ve `systemctl mask`
  çağırır; doğrudan `/` altında hiçbir girdi oluşturmaz, yeniden adlandırmaz ya
  da silmez. Gerçekten gereken özellikler - root sahipliği, güvensiz ACL veya
  sembolik bağ kaçışı olmaması, geçiş izni, grup/diğer yazma izni olmaması -
  `0555` altında da `0755` altında da sağlanır. Kusur politika
  karıştırmasıdır: `bindManagedRootMode` hem yönetilen dizinlere hem de var
  olan dosya sistemi güven çıpalarına hizmet ediyor. Düzeltme, yönetilen kök
  sabitini gevşetmek yerine üst güven çıpalarına kendi politikasını vermelidir.
  Mevcut testler sentetik mask üst dizinini zorla 0755 yapıp yalnız 0700 reddini
  sınıyor; standart Arch biçimi kapsanmıyor.
- Düzeltme uygulandı (1 Eylül 2026, `fix/alpha52-handoff-acceptance` dalı):
  önceden var olan dosya sistemi üst dizinlerinin artık kendi politikası var,
  `validateInheritedBINDAnchorFD`; bu ürünün oluşturduğu dizinler için
  kullanılan tam-kip dayatmasından ayrı. Devralınan bir çıpa; root'a ait bir
  dizin olmalı, grup ve diğer yazma izni bulunmamalı, setuid/setgid/sticky
  taşımamalı, ACL'siz olmalı ve herkesçe geçilebilmelidir. Bu, standart Arch'ın
  0555 kökünü ve olağan 0755'i kabul ederken yazılabilir, özel-bitli, yabancı
  sahipli ya da geçilemez her biçimi reddetmeyi sürdürür - 0700 dahil; mevcut
  bir test onu zaten ret olarak sabitlemişti ve bu politikanın ilk taslağı onu
  yanlışlıkla kabul ediyordu, test yakaladı. `/var/cache/bind/celikpanel` ve
  ürünün oluşturduğu diğer her dizin tam 0755 olarak kalır. Dört üst dizin
  yürüyücüsünün hepsine uygulandı (mask üst dizini, iki yönetilen kök yürüyüşü,
  satıcı unit yolu); bu önemlidir, çünkü mask üst dizini kanıtını PowerDNS yolu
  da paylaşır, yani Arch engeli hiçbir zaman yalnız BIND'a özgü değildi. Tam ve
  etiketli agent paketleri Debian 13 (WSL2) üzerinde geçiyor. Gerçek bir
  standart Arch sunucusundaki canlı kanıt hâlâ bekliyor ve bu kaydı AÇIK tutuyor.

### R-019 - Devralınmış PowerDNS, geçişe hazır bir BIND kaynağı değil

- Kanıt: S-1 kill-matrix run 5. Üretimdeki `pdns-adopt` bilerek salt-okunurdur
  ve bu yüzden CelikPanel'in özel eşitleme tablolarını oluşturmaz; oysa hemen
  ardından gelen BIND devri kaynağını `celikpanel_dns_zone_sync_v3_receipts`
  üzerinden doğrular. Tablo bulunmadığı için devir başarısız oldu.
- Etki: Mevcut bir dış PowerDNS'i devralıp sonra panel üzerinden BIND'a geçmek
  isteyen bir operatör bu geçişi tamamlayamayabilir. Müşteriye yansıyan sonuç
  henüz doğrulanmadı ve ilk saptanması gereken şey budur.
- Acil kontrol: Yok. Özel tabloları elle yazmayın; bu, üzerine kurulan her
  temeli geçersiz kılar.
- Çıkış ölçütü: Müşteriye yansıyan sonuç yazılı olarak saptanır; panel üzerinden
  ulaşılabiliyorsa devralma ya değiştirilmemiş
  `Agent.ConfigurePowerDNSSQLite` ve `Agent.SyncDNSZoneV3` yoluyla geçişe hazır
  bir kaynak üretir ya da devir, devralmanın sağlayamadığı şeyi şart koşmaktan
  vazgeçer.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.
- S-2 kararı: Sonuç müşteriye yansıyor ve doğrulandı. Bir yönetici geçerli bir
  mevcut PowerDNS'i devralabilir, onun yönetilen aktif motor olduğunu görebilir
  ve sonra onu kalıcı olarak BIND'a geçiremez; değiştirilmemiş her yeniden
  deneme, PowerDNS hizmet vermeye devam ederken intent günlüğünden önce
  başarısız olur. Kapalı arıza, kesinti yok; ama duyurulan motor devri işlemi
  kullanılamıyor ve müşteri PowerDNS'te mahsur kalıyor. Sonraki tek bölgelik
  bir mutasyon şemayı ve tek bir makbuzu tembelce oluşturabilir ama devralınan
  her bölgeyi güvenilir biçimde onaramaz. Devralma testleri bunu gizliyor,
  çünkü "dış" düzenekleri normal başlatıcıyı çağırıp özel makbuz şemasını
  önceden oluşturuyor. Bir mühendislik kusuru ve bir destek bilinen-sorun kaydı
  gerektirir.
- Teşhis ve düzeltme (1 Eylül 2026, `fix/alpha52-handoff-acceptance` dalı).
  S-5 7. deneme, devralma yarısının çalıştığını ve BIND geçişinin çalışmadığını
  kanıtladı. Beş mercekli düşman soruşturması üç bağımsız sebep saptadı, üçü de
  düzeltildi:
  (1) **Maskeli BIND "durdurulmuş" sayılmıyordu.** Ürün, bir paket yöneticisi
  arkasından BIND'ı başlatmasın diye named.service ve bind9.service birimlerini
  maskeler; sonra kendi PowerDNS-etkin kanıtı yalnız "not-found" ya da
  "loaded"+"disabled" kabul ediyordu, "masked" asla. BIND'ın kurulmuş olduğu
  hiçbir sunucuda geçiş kendi kaynağını kuramıyordu; aynı yüklem PowerDNS bölge
  yazımlarını da kapıya aldığı için kalıntı onları da engelliyordu. Maskeli ve
  etkin olmayan bir birim artık durdurulmuş sayılır; maskeli olmak devre dışı
  olmaktan zayıf değil güçlüdür.
  (2) **Beş saniyelik arıza otuz dakikalık sessizliğe dönüşüyordu.** Agent düşüp
  kendini zehirledikten sonra panel, DNS motoru geçişi için bütçesi otuz dakika
  olan ve bağlamı bilerek çağırandan koparılmış bir uç uzlaştırmaya girip her
  250 ms'de yokluyordu. `Agent.ServiceMutationStatus`, sağlık nöbetine hiç
  danışmayan tek giriş noktasıydı; bu yüzden donmuş işi doğru biçimde
  "çalışıyor" diye bildirmeye devam ediyordu. Durum yanıtı artık kararlı
  `MutationHold` kodunu taşıyor ve bekleyiş, agent tutulduğunu söylediği anda
  duruyor.
  (3) **Tıkanma kalıcıydı.** Paket kurup hiçbir günlük yazmadan düşen bir geçiş,
  tek sınıflandırması "hata" olan bir kurulum makbuzu bırakıyordu. Başlangıç
  kurtarması bunu zehirlenmiş bir mutasyon yöneticisine çeviriyordu ve zehir hiç
  değişmeyen kalıcı durumdan yeniden hesaplandığı için her açılış onu yeniden
  üretiyordu: DNS hizmet veriyor, panel cevap veriyor, ama sunucu bir daha
  hiçbir mutasyonu kabul edemiyordu. Aktif durum ile kendi sahiplik makbuzu
  uyuşuyorsa ve hedef hiçbir şeye sahip değilse yetki kanıtlanabilir biçimde hiç
  el değiştirmemiştir; bu artık kurtarılabilir sayılıyor ve defter işi temizce
  düşüyor. Başka bir motor etkinken yetkiye sahip olan bir hedef — Boston biçimi
  — kapalı arıza vermeyi sürdürüyor.
  Her düzeltmenin, düzeltilmemiş ağaçta olayın hata metnini birebir üreten bir
  testi var. Tam ve etiketli agent paketleri Debian 13 (WSL2) üzerinde geçiyor.
  Gerçek bir VM'de devralmadan BIND'a geçiş tamamlanana kadar R-019 AÇIK kalır.
- S-6 canlı kanıt (2 Eylül 2026): üç-sebep düzeltmesi gerçek makinelerle
  karşılaştı. 1. sebep tamamen tuttu - Debian 13 dış-PowerDNS devralmadan
  BIND'a yolculuğu sekiz denemede ilk kez uçtan uca geçti. 3. sebep özünde
  tuttu - intent-öncesi tıkanma artık zehirlemiyor, iş
  `agent_restarted_before_dns_engine_switch_commit` ile temizce düşüyor,
  yeniden başlatma hiçbir şeyi değiştirmiyor ve yeniden deneme sunucuyu
  yakınsatıyor - ama ilk yeniden denemenin çağıranına 75 dendi; bu, kill-matrix
  tetikleyicisinin kalp atışı sözleşmesinin geçiş ortasında `WorkerPID == 0`
  dayatmasına çıktı, R-017 öncesi hatanın harness'a taşınmış hâli; düzeltildi,
  S-6 hata metnini birebir üreten bir testle. 2. sebep gecikmede tuttu (30
  dakikadan 5,5 saniyeye, agent hatasından ilk bayta 9,8 ms) ama halka açık
  gövde tutulmayı adlandırmıyordu; artık tutulma kodunu ayrıntı olarak taşıyan
  `DNS_ENGINE_MUTATIONS_HELD` dönüyor. Hâlâ kanıtlanmamış olanlar: Boston
  negatifi (iki üretim kurulum zinciri de ölçülen hücreden önce düştü, biri
  R-026'yı açığa çıkardı) ve yolculuğa hiç giremeyen Arch (R-027). R-019,
  Boston negatifi ve gerçek sunucuda temiz-çağıranlı bir yeniden deneme için
  AÇIK kalır.
- Kalıntı, bilerek değiştirilmedi (1 Eylül 2026):
  `validateDNSEngineStateSnapshot`, kalıcı bir günlük anlık görüntüsünün
  kaydedilmiş GID'sini `serviceMutationRequiredOwnerGID` ile karşılaştırır; o
  değer ise süreç başına bir kez `/etc/group`'tan yeniden türetilir. Kalıcı bir
  kaydın çalışma anında türetilen bir beklentiye göre yargılanması, yukarıdaki
  3. sebebin aynı biçimidir; günlük yazımı ile doğrulama arasında bir celikpanel
  GID yeniden numaralandırması bütün kalıcı günlükleri bir anda geçersiz
  kılardı. Bağı koparmak denendi ve geri alındı:
  `TestDNSEngineSwitchJournalRequiresExactServiceOwnerForState`, denetimin aynı
  zamanda yakalama anında sahipliği yanlış olan bir durum dosyasından alınmış
  anlık görüntüyü de reddettiğini gösterdi; bu korunmaya değer ve tuğlalaşma
  senaryosu hem uzun süre takılı kalmış bir günlük hem de GID yeniden
  numaralandırması gerektiriyor — takılı günlükleri ise 3. sebebin düzeltmesi
  ortadan kaldırıyor. Temiz onarım, beklenen GID'yi günlüğe kaydedip ona göre
  doğrulamaktır; bu, göç sonuçları olan kalıcı bir şema değişikliğidir ve acil
  değildir.

### R-020 - Panel race paketi zaman aşımı tavanına yakın

- Kanıt: `go test ./cmd/panel/ -race -count=1 -timeout 30m`, Debian 13 WSL2
  üzerinde `f243304d1aadc94c0f26342d2d3270902ad43d4b` commit'inde 1800 saniyelik
  tavanın 1574.958 saniyesini kullanarak `ok` döndü; 225.042 saniye pay kaldı.
- Etki: Daha yavaş bir makinede paket tavanı aşar ve test hatası olarak değil
  zaman aşımı olarak düşer; bu "takıldı" gibi okunur ve orantısız inceleme
  süresine mal olur.
- Acil kontrol: Açık `-timeout 30m` korunsun; varsayılan daha kötü olurdu.
  Eğilim görünür kalsın diye her kabul koşusunda geçen süre raporlansın.
- Çıkış ölçütü: Ya paketin çalışma süresi düşürülür ya da tavan, gerekçesi
  kaydedilerek ve desteklenen en yavaş kabul makinesinde ölçülmüş bir payla
  yükseltilir.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

### R-021 - Kurulum sunucusu kimliği envanterle uyuşmuyor

- Kanıt: 31 Ağustos 2026'da iki kurulum hedefi de kayıtlı olandan farklı bir SSH
  sunucu anahtarı ve envanterde bulunmayan bir işletim sistemi adı taşıyan bir
  SSH afişi sundu: `2.25.80.4` Debian 13 olarak kayıtlı, Ubuntu bildiriyor;
  `72.62.38.15` Arch olarak kayıtlı, Debian 13 bildiriyor. Şu an görünür bir
  Arch sunucusu yok. Gözlenen parmak izleri operatörde tutulmaktadır ve bilerek
  buraya yazılmamıştır: doğrulanmamış bir parmak izi bu deftere girerse sonradan
  güvenilir bir temel sanılır.
- Etki: Üretim biçimli iki sunucunun da kimliği saptanmamıştır. Anahtar
  değişimi yeniden kurulumla tutarlıdır, ancak işletim sistemi değişimi tek
  başına temiz sunucu kuralıyla açıklanmaz; diğer açıklama, adreslerin artık
  başka makinelere çözümlendiğidir. Ayrıca zorunlu Arch kabul sunucusu ortada
  yoktur.
- Acil kontrol: Her iki adreste de kurulum, güncelleme ve yapılandırma değişimi
  yasak. Yalnızca salt-okunur teşhis, o da operatör sunulan anahtarları bant
  dışı doğruladıktan sonra. Kabul sanal makineleri, ekibin kendi denetimindeki
  atılabilir makineler olmalıdır.
- Çıkış ölçütü: Operatör her sunucu anahtarını sağlayıcı konsolu ya da sunucu
  üstü kanıtla doğrular; her adres için kayıtlı işletim sistemi düzeltilir ya da
  adres emekliye ayrılır; ve bir Arch kabul sunucusu var olur.
- Çözüm (1 Eylül 2026): Operatör iki sunucunun da yeniden kurulduğunu teyit
  etti; bu, üç değişikliği birden açıklar — yeni SSH sunucu anahtarları, yeni
  işletim sistemleri ve sağlayıcının enjekte ettiği yeni giriş anahtarları.
  Kimlik artık varsayımla değil kanıtla saptanmıştır: operatör dağıtım
  anahtarını sağlayıcı konsolundan yeniden ekledi (bu, gerçek makineye
  bağlanır) ve her sunucunun içeriden bildirdiği sunucu anahtarı
  (`/etc/ssh/ssh_host_ed25519_key.pub`) hat üzerinde sunulanla birebir tuttu —
  `2.25.80.4` için `SHA256:8Zje…` (hostname `boston`, Ubuntu 24.04.4) ve
  `72.62.38.15` için `SHA256:DV/e…` (hostname `frankfurt`, Debian 13).
- Düzeltilmiş envanter: `2.25.80.4` boston, Ubuntu 24.04.4, PowerDNS ikincil.
  `72.62.38.15` frankfurt, Debian 13, BIND birincil. Kayıtlı karma-motor ikilisi
  yeniden kurulumdan sağ çıktı ve iki sunucudaki kalıcı durum makbuzlarıyla
  uyuşuyor.
- Çözüm anındaki sağlık: CelikPanel iki sunucuda da kurulu ve tamamlanmış
  (`/etc/celikpanel/install.complete`, ikililer 30 Ağustos tarihli), panel ve
  agent aktif, aktif mutasyon isteği yok, on dört günde DEGRADED ya da kapalı
  arıza kaydı yok ve dört birimin hepsinde sıfır yeniden başlatma. Boston'ın
  eski sınırsız yeniden başlatma döngüsü yeniden kurulumdan sağ çıkmadı.
- Kalan, engel olarak değil burada izlenen konu: envanterimizde artık Arch
  sunucu yok. Bu, OPERATIONS.md 3. bölüm matrisini engellemez; o matris bu iki
  sunucuda değil, resmi Arch bulut imajından kurulan atılabilir QEMU/KVM
  konuklarında koşar. Ancak gözlenebilecek uzun ömürlü bir Arch makinesi
  bulunmadığı ve R-018 çıpa düzeltmesinin o atılabilir matris dışında gerçek bir
  Arch makinesiyle hâlâ karşılaşmadığı anlamına gelir.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

### R-022 - Temiz kurulum başlayamıyor

- Kanıt: S-2 kabul koşusu, atılabilir Debian 13 ve Arch sanal makineleri,
  `f243304d1aadc94c0f26342d2d3270902ad43d4b` commit'i. İkisi de tek bir hatayla
  1 döndü: `trusted release root is missing while sourcing release guard`;
  `/opt/celikpanel`, `/var/lib/celikpanel`, `/etc/celikpanel` ve her iki unit
  dosyası da oluşmadı. Kabul raporu SHA-256
  `7126f122e815ddda59ba7d8dd060b74c937c0bb7ab61d9a18ec93734d9a46eb3`.
  `install.sh:128` `SRC` değerini hesaplar, `:131`
  `deploy/release-transaction-guard.sh` dosyasını source eder; oradaki
  `:16-20` denetimi yalnız `TRUSTED_RELEASE_ROOT` ya da
  `CELIKPANEL_TRUSTED_RELEASE_ROOT` kabul eder. Temiz kurulum yolu
  `TRUSTED_RELEASE_ROOT=$SRC` atamasını ilk kez `install.sh:519` satırında,
  çok daha sonra ulaşılan `prepare_fresh_release_transaction_foundation()`
  içinde yapar. `:23` satırındaki `set -euo pipefail`, korumanın `return 1`
  değerini çıkışa çevirir. `download-portal/get.sh:1057-1063` kuruluma altı
  `CELIKPANEL_FIRST_INSTALL_*` değeri geçirir, iki kök değişkenini de geçirmez;
  dosyada ikisine de hiçbir atıf yoktur.
- Kapsam: Alpha53 adayının getirdiği bir hata değildir. Temel commit `0a5e849`
  aynı satır numaralarında aynı sırayı taşır ve sıra `45d01ff` (Alpha51
  kurtarma sağlamlaştırması, 28 Ağustos 2026) tarihine dayanır. Üç dosya aday
  dal boyunca bayt bayt değişmemiştir.
- Etki: Yeni hiçbir müşteri ürünü belgelenmiş hiçbir yoldan kuramaz - ne genel
  `get.sh` akışıyla ne de doğrudan `install.sh` çağrısıyla. Fark edilmemesinin
  sebebi, iki canlı sunucunun da Alpha52'ye bir Alpha51 güncellemesiyle
  ulaşmasıdır; eldeki Alpha52 kanıtı güncellemeyi kapsar, temiz kurulumu değil.
  Güncelleme ve geri alma yolları etkilenmez: kuruluma girmeden önce
  `CELIKPANEL_TRUSTED_RELEASE_ROOT` değişkenini verirler
  (`bootstrap-update.sh:389`).
- Acil kontrol: Yok. Geçici çözüm diye kimseye elle güvenilen kök değişkeni
  vermesini söylemeyin; koruma tam da çağıranın doğrulamadığı bir kökü
  reddetmek için vardır ve elle atamak koruduğu özelliği ortadan kaldırır.
- Çıkış ölçütü: Debian 13, Ubuntu 24.04 ve güncel Arch üzerinde hem genel
  imzalı `get.sh` akışından hem de belgelenmiş doğrudan çağrıdan temiz kurulum
  tamamlanır; koruma, çağıranın doğrulamadığı bir sürüm kökünü hâlâ reddeder;
  ve statik testlerle çıkarılmış-fonksiyon testlerinin ulaşamadığı
  bootstrap-kurulum giriş sınırını bir kabul testi kapsar.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.
- S-3 güncellemesi: Giriş sınırı kusurunun kendisi yerel
  `fix/r-022-fresh-install` dalında onarıldı ve kanıtlandı (`7fd5b30b`
  commit'i, üst commit tam olarak `0a5e8495…`): imzalı yolculuk artık `get.sh`
  içinden doğrulanmış çıkarma kökünü geçirir, apply-only olmayan doğrudan
  çağrı kendi kanonik kurulum dizinine düşer, apply-only eksik miras kökü hâlâ
  reddeder ve koruma dosyası tabanda ve adayda bayt bayt aynıdır. Yeni bir
  betikler-arası giriş-sınırı sözleşme testi tabanda düşer, düzeltmeyle geçer
  ve CI'da koşar. Dal itilmedi, operatör incelemesini bekliyor. Temiz kurulum
  bir bütün olarak arkasında kırık kalmaya devam ediyor - bkz. R-023 - bu
  yüzden düzeltme `main`'e inip eksiksiz yeşil bir kurulum var olana dek bu
  kayıt AÇIK kalır.

- Durum (2 Eylül 2026): giriş-sınırı düzeltmesi (`7fd5b30b`) entegrasyon dalı
  `fix/alpha52-handoff-acceptance` üzerinde. S-4 ve S-5'te atılabilir Debian
  13, Ubuntu 24.04 ve Arch konuklarında kanıtlandı (iki giriş yolu, R-022
  sözleşme testi tabanda kırmızı düzeltmeyle yeşil, CI'da koşuyor). `main`'de
  değil; engel dal indiğinde biter.

### R-023 - Atlanan ilk yönetici, panel yeniden başlatma döngüsüne dönüşüyor

- Kanıt: S-3 kabul koşusu; atılabilir Debian 13, Ubuntu 24.04 ve Arch sanal
  makinelerinde altı geçerli temiz kurulum yolculuğu (S-3 manifesti
  `815bf4adbf71c89f505d09293d3020b1964171485f900ab20e28089ec33eec09`). Altısı
  da tek ve belirlenimci bir zincirle düştü: `install.sh:1403`,
  `SKIP_ADMIN=1` altında `ensure_first_administrator` işlevinden hemen döner;
  `006_drop_placeholder_admin.sql` geçişi yer tutucu yöneticiyi siler; panel
  sıfır kullanıcı sayar ve ardına kadar açık hizmet vermeyi reddeder
  (`cmd/panel/main.go` sıfır-kullanıcı kapısı); `Restart=on-failure` bu reddi
  yeniden başlatma döngüsüne çevirir; kurulum
  `/etc/celikpanel/install.complete` yazılmadan çıkar.
- Kapsam: `main` üzerinde önceden var; sıfır-kullanıcı kapısı (3 Temmuz 2026)
  ile 006 geçişi `SKIP_ADMIN` ile birlikte yaşadığından beri. Hiçbir aday dalın
  getirdiği bir şey değil. Sıfır-kullanıcı reddi doğrudur ve kalmalıdır:
  kullanıcısı olmayan panel hizmet vermemelidir.
- Etki: Çalışan etkileşimsiz temiz kurulum yok. `SKIP_ADMIN=1` belgelenmiş bir
  kurulum seçeneğidir, apply-only sözleşmesi onu şart koşar ve tarihsel dağıtım
  reçetesi onu kullanıyordu. Etkileşimli yolculuk `--create-admin` komutuna
  dayanır; o da terminalden okur (`cmd/panel/admin_cli.go:27,133`) ve
  etkileşimsiz kipi yoktur - otomasyon ilk yöneticiyi hiç oluşturamaz.
  Etkileşimli TTY yolculuğu kanıtlanmadı.
- Acil kontrol: Temiz kurulumda `SKIP_ADMIN=1` kullanmayın. Etkilenmiş bir
  kurulumdan sonra servis kullanıcısıyla `--create-admin` çalıştırıp paneli
  yeniden başlatın; bir kullanıcı oluşunca döngü durur.
- Çıkış ölçütü: `SKIP_ADMIN=1` ile temiz kurulum, herhangi bir sunucu
  değişikliğinden ÖNCE açık bir mesajla erken reddeder, döngüyle bitmez;
  otomasyon için belgelenmiş, güvenli bir etkileşimsiz ilk-yönetici mekanizması
  vardır; etkileşimli yolculuk gerçek bir TTY'de kanıtlanır; ve altı S-3
  yolculuğu uçtan uca geçer.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

- Durum (2 Eylül 2026): kabul özelliği (kullanılabilir bir yönetici garanti
  edilmeden hiçbir sunucu değişikliği yok), stdin üzerinden kimlik yolu ve
  canlı-WAL toleranslı sonda (`7fbc4149`, S-5 `975a5e26`) entegrasyon dalında.
  S-5'te atılabilir VM'lerde kanıtlandı: 15/15 kabul yolculuğu, 45/45 ret
  hücresi, 72/72 kapsama kaydı ve üç dağıtımda etkileşimli TTY yolu. `main`'de
  değil.

### R-024 - Birim etkinleştirme açık-arızalı ve eşitlenmemiş

- Kanıt: S-3, Arch imzalı-genel yolculuğu. Tanı amaçlı yeniden başlatmadan
  sonra iki birim de devre dışı ve pasifti; Debian ile Ubuntu agent'ı geri
  getirdi. `install.sh` her iki `systemctl enable` çağrısının hatasını yutar
  (`>/dev/null 2>&1 || true`) ve yalnız aynı açılıştaki etkinliği kanıtlar;
  enable bağları panel arızasından önce hiç eşitlenmez ve düzenek sanal
  makineyi ani durdurdu. Kanıt, kalıcılık kaybını ve açık-arızalı
  etkinleştirmeyi kanıtlar; başarısız enable ile eşitlenmemiş bağı ayırt
  edemez.
- Etki: Taze bir sunucu, kurulum servisleri çalışıyor diye raporlamışken hiçbir
  şey hizmet vermeden yeniden başlayabilir.
- Acil kontrol: Her kurulumdan sonra, yeniden başlatmaya güvenmeden önce iki
  birim için `systemctl is-enabled` doğrulayın.
- Çıkış ölçütü: enable hataları kurulum için ölümcül olur, bağlar başarı
  raporlanmadan önce kalıcı eşitlenir ve bir yeniden başlatma testi iki birimin
  desteklenen her dağıtımda geri geldiğini kanıtlar.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

- Durum (2 Eylül 2026): `systemctl enable` hataları ölümcül ve enable durumu
  başarı raporlanmadan önce eşitleniyor (S-4, `7fbc4149`); S-4'te Debian 13,
  Ubuntu 24.04 ve Arch'ta 3/3 yeniden başlatmayla, S-5 ve S-6'da yine 0/3
  hatayla kanıtlandı. Entegrasyon dalında; `main`'de değil.

### R-025 - Klonla-kur iddiası kök zinciri politikasıyla çelişiyor

- Kanıt: S-3. `install.sh:1715-1721`, stok bir sistemde
  `git clone && sudo ./install.sh` çalışır diye belgeler; oysa sürüm kurtarma
  temeli, kullanıcıya ait bir üst dizinin altındaki sürümü reddeder - ki
  `/home/<kullanıcı>` altındaki bir klon tam olarak budur. S-3 düzeneği bu
  redde çarptı ve root'a ait korumalı depolamaya yeniden taşımak zorunda kaldı.
- Etki: Belgelenen geliştirici yolculuğu, ürünün kendi uyguladığı politikada
  düşer. Hangisi doğruysa öbürü değişmeli.
- Acil kontrol: Doğrudan kurulumları root'a ait bir dizin altına taşıyın.
- Çıkış ölçütü: Ya belge ev-dizini iddiasını bırakıp taşıma şartını yazar ya da
  politika bilinçli olarak tanımlı bir temiz-kurulum taşıma biçimini kabul
  eder; karar sürüklenmeyle değil yazılı verilir.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

### R-026 - PowerDNS geri alması veritabanı nesillerini karıştırıyor

- Kanıt: S-6, Boston negatif 2. deneme (S-6 manifesti
  `b5105623daa3fcebe444b3e6e293a0f6e9a5b3e7431ad0df35479d619d7e1701`).
  BIND'dan PowerDNS'e geçişin üretim geri alması PowerDNS'i durdurdu, canlı ana
  dosyayı sildi ve yedeği yerine taşıdı; ama hedefin aday veritabanına karşı
  oluşturduğu `-wal` ve `-shm` dosyalarına hiç dokunmadı. SQLite ardından başka
  bir neslin WAL'ını geri yüklenen ana dosyaya yeniden oynattı; özet
  `live_database_generation_mixed_with_retained_wal: true` ve
  `malformed_database: true` kaydediyor, kurulum işi `rolling-back` günlüğüyle
  `running`/`leased` kaldı ve mutasyon yöneticisi zehirlendi. İleri yol yan
  dosyaları hazırlıkta zaten reddediyor (`requireNoPDNSDatabaseSidecars`); geri
  almanın karşılığı yoktu.
- Etki: PowerDNS'e geri alınan her geçiş, sunucuyu bozuk bir canlı veritabanı ve
  tıkanmış bir defterle bırakabilir - R-019 biçimine öbür taraftan girilmesi.
- Daldaki düzeltme (2 Eylül 2026, `fix/alpha52-handoff-acceptance`):
  `restorePDNSDatabase` artık yedek yerine taşınmadan önce atılan neslin
  `-journal`, `-wal` ve `-shm` dosyalarını kaldırıyor; zaten-geri-yüklenmiş
  dalında da. Bir test, olayın reddini (`PowerDNS database has an unresolved
  SQLite sidecar`) düzeltilmemiş ağaçta birebir üretiyor.
- Çıkış ölçütü: Gerçek bir VM'de kasten düşürülen BIND'dan PowerDNS'e geçiş,
  temiz açılan bir veritabanına geri alınır, defter işi temizce düşer ve sonraki
  geçiş kabul edilir.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

### R-027 - PowerDNS yetkisi tasarım gereği yalnız APT

- Kanıt: `certifyAPTPDNSCapabilities` (`cmd/agent/dns_engine_pdns_unit.go`)
  `PackageManager == APT`, `DistroFamily == Debian` ve systemd şart koşar;
  `docs/DISTRO-SUPPORT.md` PowerDNS için pacman eşlemesi listelemez. S-6 Arch
  2. deneme sonucu doğruladı: üç bölge sunan depo paketi PowerDNS "yönetilmiyor"
  sınıflandı ve devralma `DNS_ENGINE_WORKFLOW_REQUIRED` döndürdü.
- Etki: Kusur değil, sınır. R-019 devralma yolculuğunun Arch'ta hiç
  koşamayacağı ve R-018 çıpa düzeltmesinin Arch'ta yalnız yalnız-BIND bir
  yolculukla (taze sunucudan BIND'a) kanıtlanabileceği, PowerDNS devralmasıyla
  asla kanıtlanamayacağı anlamına gelir.
- Acil kontrol: Gerekmez. APT'dekine eşdeğer bir pacman köken modeli olmadan
  onayı gevşetmeye kalkmayın.
- Çıkış ölçütü: Ya aynı köken güvencelerine sahip onaylı bir pacman PowerDNS
  profili ya da müşteriye dönük belgede yazılı sınır. İkisi de kabul edilir;
  sessizlik edilmez.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

- Karar (2 Eylül 2026): kök zinciri politikası kalıyor. Kullanıcıya ait bir üst
  dizinin altındaki sürüm, kurtarma temelinin tam da reddetmek için var olduğu
  şeydir ve hiçbir belge iddiası bunun üstüne çıkamaz. Stok bir sistemde
  `git clone && sudo ./install.sh` vaat eden kurulum yorumu artık gerçek şartı
  söylüyor - checkout root'a ait bir dizinin altına konur - ve kurtarma
  temelinin reddi nereye konulacağını zaten adlandırıyor (S-4). Ham checkout'un
  `bin/panel` içermeyip kaynaktan derlenmesi değişmedi.

## Kabul kuralı

Risk kabulü, hesap verebilir bir iş kararıdır ve harici sicilde tutulur.
AÇIK/ENGELLEYİCİ risk yalnız bu belgedeki ifadeyi değiştirerek sessizce kabul
edilmiş sayılamaz. Riski yalnız çıkış ölçütleri ve tarihli kanıtla kapatın.
