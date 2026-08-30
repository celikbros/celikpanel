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

## Kabul kuralı

Risk kabulü, hesap verebilir bir iş kararıdır ve harici sicilde tutulur.
AÇIK/ENGELLEYİCİ risk yalnız bu belgedeki ifadeyi değiştirerek sessizce kabul
edilmiş sayılamaz. Riski yalnız çıkış ölçütleri ve tarihli kanıtla kapatın.
