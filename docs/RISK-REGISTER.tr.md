# Mühendislik ve Operasyon Risk Sicili

*Referans: 29 Ağustos 2026 · [English](RISK-REGISTER.md)*

Bu sicil, bilinen devir boşluklarını ve `docs/alpha51-engineering-handoff`
dalında tamamlanan azaltımları izler. Sır değeri veya gerçek custodian adı
içermez. Her sorumlu, hedef tarih, kabul ve harici kanıt referansı onaylı repo
dışı devir sisteminde atanmalıdır.

Beş taslak PR Alpha51 referansının dışındadır: [#69](https://github.com/celikbros/celikpanel/pull/69)
(migration DDL canonicalization), [#70](https://github.com/celikbros/celikpanel/pull/70)
(restart acknowledgement UX/ürün kararı), [#71](https://github.com/celikbros/celikpanel/pull/71)
(`agent/ci-fast` dalındaki CI duplicate-release validation),
[#72](https://github.com/celikbros/celikpanel/pull/72) (`agent/ssl-hostnames-hsts`
dalındaki arşivlik SSL/backup WIP) ve [#73](https://github.com/celikbros/celikpanel/pull/73)
(beş benzersiz Alpha35 portal scripti ile yayımlanmamış PR72-follow-up patch'i;
head `archive/alpha35-portal-tooling`, commit
`0ef899f3cb96390c4ef3822f199eddc67bb0ee1f`). PR #72 ve PR #73 arşivliktir.
İkisi de olduğu hâliyle merge edilmemeli, PR #73 olduğu hâliyle
çalıştırılmamalıdır. Hiçbir taslak için bütün kontrollerin yeşil olduğu iddia
edilmez.

## Durum ve önem

- AÇIK: Azaltma tamamlanmadı.
- YENİDEN DOĞRULA: Koşul değişmiş olabilir; güncel kanıt yoktur.
- ENGELLEYİCİ: Çıkış ölçütleri geçmeden ilgili işlemi yapmayın.
- DEVİR DALINDA KAPALI: Yalnız repo içi çıkış ölçütleri bu dalda karşılandı;
  kapanışı `main` üzerinde geçerli saymadan önce inceleme ve merge'i doğrulayın.
- KISMEN AZALTILDI / YENİDEN DOĞRULA: Sınırlı bir bileşen düzeltildi; kalan
  koşul için kabul kanıtı gerekir.
- Kritik: Geri döndürülemez duruma, güvensiz yetkili işleme veya
  release/rollback otoritesi kaybına yol açabilir.
- Yüksek: Kesintiye, güvenlik sınırı hatasına veya kanıtlanamayan canlı
  dağıtıma yol açabilir.
- Orta: Hata, drift veya onboarding riskini önemli ölçüde artırır.

## Risk özeti

| ID | Önem | Durum | Risk |
|---|---|---|---|
| R-001 | Kritik | DEVİR DALINDA KAPALI | Operations artık snapshot v6'yı, güncel v4/v5 reddini ve tarihsel sürüm sınırını anlatıyor |
| R-002 | Yüksek | DEVİR DALINDA KAPALI | README artık isteğe bağlı yerel GPG kullanımını canonical Ed25519 update otoritesinden ayırıyor |
| R-003 | Kritik | AÇIK / GERÇEK TENANT İÇİN ENGELLEYİCİ | Tam kontrol düzlemi disaster backup ve restore tatbikatı kanıtlanmadı |
| R-004 | Yüksek | YENİDEN DOĞRULA | Frankfurt canlı kimliği bilinmiyor; Boston kanıtı yalnız kısmi |
| R-005 | Yüksek | AÇIK | Boston/Frankfurt ortam sınıfı üretime-hazır-değil politikasıyla çelişkili |
| R-006 | Yüksek | AÇIK | Route/role ve API sözleşme borcu güvenlik sınırında sürüyor |
| R-007 | Yüksek | AÇIK | Zorunlu gerçek VM install/update/rollback/reboot kanıtı devirde yok |
| R-008 | Yüksek | YENİDEN DOĞRULA | Alpha51 GitHub sürüm zinciri doğrulandı; portal eşitliği ve kurulu release floor'lar kanıtlanmadı |
| R-009 | Orta | AÇIK | Harici paket/repo/CA endpoint'leri canlı doğrulama kapısı olmadan bayatlayabilir |
| R-010 | Orta | DEVİR DALINDA KAPALI | Mimari, onboarding ve uygulama-durumu belgeleri Alpha51 ile uzlaştırıldı |
| R-011 | Yüksek | AÇIK | Erişim, signing key, provider ve olay custodian'ları devirde atanmadı |
| R-012 | Orta | DEVİR DALINDA KAPALI/AZALTILMIŞ / TEMİZ MAIN YENİDEN DOĞRULA | Root scaffold, kopya worktree/dallar ve listelenen kalıntılar kaldırıldı; yeni ekip temiz bir `main` checkout'unu doğrulamalıdır |
| R-013 | Orta | AÇIK | Tarayıcı golden-path, kritik endpoint ve latency kanıtı eksik |
| R-014 | Orta | AÇIK | Olay müdahalesi, escalation ve postmortem sahipliği tanımlı değil |

## Ayrıntılı riskler

### R-001 — Snapshot sözleşmesi belge uyumsuzluğu

- Kanıt: Bu dal docs/OPERATIONS.md ve Türkçe eşini snapshot v6'ya günceller,
  güncel updater/rollback yolunun v4/v5'i reddettiğini belirtir ve eski
  snapshot'ı eşleşen değişmez tarihsel recovery sürümü ile rollback yardımcısıyla
  sınırlar.
- Etki: Yeni operatör uyumsuz rollback seçebilir veya restore edilemeyen
  snapshot'ın kabul edildiğini sanabilir.
- Kapanış dayanağı: İngilizce/Türkçe runbook'lar artık kaynak sözleşmesiyle
  uyumludur. Merge edilene kadar değişmez Alpha51 scriptleri ve sözleşme testleri
  otorite olarak kalır.
- Durum: DEVİR DALINDA KAPALI. Bu yalnız belge kapanışıdır; canlı veya disposable
  restore kanıtı R-003 ve R-007 tarafından izlenmeye devam eder.

### R-002 — Release-signing otoritesi belirsizliği

- Kanıt: Bu dal README'yi; Ed25519 imzalı manifest v2, release sequence,
  sabitlenmiş public key ve tam altı ürünü etiketli sürüm update otoritesi olarak
  tanımlayacak şekilde günceller. İsteğe bağlı yerel GPG imzalama açıkça update
  otoritesi değildir.
- Etki: Ekip yalnız bütünlük sağlayan veya isteğe bağlı ürünleri yayınlayıp
  yetkili update otoritesinin oluştuğunu sanabilir.
- Kapanış dayanağı: README ve release-signing rehberi artık otorite sınırını
  açıklar; Alpha51 resmi manifest/imzası ile altı ürün kümesi doğrulanmıştır.
- Durum: DEVİR DALINDA KAPALI. Portal/canlı eşitliği R-008'de izlenir ve bu belge
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

### R-004 — Eksik canlı kimlik

- Kanıt: Boston için açık HTTP 200 kurtarma gözlemi ve Alpha51 bildirimi vardır;
  panel/agent commit'leri, şema ve rollback durumu alınmamıştır. Frankfurt'un
  güncel sürümü kanıtlanmamıştır.
- Etki: Rollout panel, agent, şema, UI veya eş DNS generation'larını
  karıştırabilir.
- Acil kontrol: Varsayılan eşitliğe göre değişiklik yapmayın.
  LIVE-STATE-2026-08-29.tr.md belgesini salt okunur tamamlayın.
- Çıkış ölçütü: İki node için tarihli panel/agent sürüm ve tam commit, şema, UI
  ürünü, unit, operation-idle, release-floor ve v6 snapshot kanıtı vardır.
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

### R-008 — Portal ve kurulu Alpha51 eşitliği tam kanıtlanmadı

- Kanıt: [Alpha51 GitHub sürümü](https://github.com/celikbros/celikpanel/releases/tag/v0.1.0-alpha.51),
  etiket/commit kimliği, etiketli sürüm CI'ı, tam altı değişmez ürün ve resmi
  Ed25519 manifest/imzası doğrulanmıştır. Manifest'in yetkilendirdiği arşiv
  22.644.115 bayttır ve SHA256 değeri
  `57d0321a13388392872bc3aef9af62646e2d700c23a4e0305d479df1e80ff365` şeklindedir.
  Git etiketinin kendisi imzasızdır ve update otoritesi değildir. Portal
  byte'ları ile iki sunucunun kurulu release-sequence floor'u kanıtlanmamıştır.
- Etki: Doğrulanmış GitHub sürümü portal veya kurulu durumdan yine de farklı
  olabilir.
- Acil kontrol: Sürüm otoritesi olarak resmi Ed25519 manifest'ini kullanın;
  yalnız etiket veya Boston sürüm metniyle hiçbir sunucuyu güncel saymayın.
- Çıkış ölçütü: Portal source/staged eşitliği ve iki kurulu floor, doğrulanmış
  sürüme karşı sınırlı ve sır içermeyen kanıtla kaydedilir.
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

- Kanıt: Bu dal README'yi Alpha51 ve imzalı update yoluyla uyumlu yapar, Roadmap
  metriklerini elle tutmak yerine generated yapar, iki UI mimarisi belgesinde
  role-aware web/src/nav.ts registry'sini açıklar ve genel web/README.md şablonunu
  ürüne özel onboarding ile değiştirir.
- Etki: Yeni mühendisler eski komut seçebilir, uygulanmış davranışı yanlış
  anlayabilir veya ürün yerine scaffolding'i yeniden kurabilir.
- Kapanış dayanağı: README, Roadmap durumu, mimari ve web onboarding Alpha51 ile
  uzlaştırılmıştır; eskiyebilecek fact snapshot'ları tarihlidir veya generated'dır.
- Durum: DEVİR DALINDA KAPALI. Gelecekte kaynak değiştiğinde ilgili bilgiler yine
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

- Kanıt: Bu dal gereksiz root package.json/package-lock.json create-vite
  scaffold'unu kaldırır. Benzersiz ürünler PR #72 ve PR #73'te korunduktan sonra
  temizlik; 109 kayıtlı kopya worktree'yi, 105 eski yerel dalı, 56 eski remote
  dalı, `.attic`, `.worktrees`, `.claude/worktrees`, root `__pycache__` ve geçici
  devir worktree'sini kaldırmıştır. Yalnız primary kayıtlı worktree kalmıştır.
  Tracked `.design-sync` bilinçli olarak tutulmuştur.
- Etki: Değişiklik yanlış ağaçta yapılabilir veya kopyalar ürün kodu olarak
  yanlışlıkla commit edilip incelenebilir.
- Acil kontrol: Yeni ekip çalışmaya başlamadan önce taze bir `main` checkout'u
  kullanmalı, git status'u ve worktree listesini doğrulamalıdır.
- Çıkış ölçütü: Temizlik bu dalda kapanmış/azaltılmıştır; yeni ekip kanıtı temiz
  bir `main` checkout'unda yalnız kasıtlı kayıtlı worktree'ler gösterir. Tracked
  `.design-sync` kalıntı değildir ve bilinçli olarak tutulur.
- Durum: DEVİR DALINDA KAPALI/AZALTILMIŞ / TEMİZ MAIN YENİDEN DOĞRULA. Bu
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

- Kanıt: SECURITY.md özel bildirimi, docs/AUTOPSY.md arızaları tanımlar; fakat
  on-call, severity, incident commander, escalation süresi veya postmortem
  action-owner süreci atanmaz.
- Etki: DNS, release veya güvenlik olayı takılabilir ya da güvensiz ad-hoc
  değişikliklerle ele alınabilir.
- Acil kontrol: Panel değişikliği ve salt okunur SSH sınırlarını koruyun; atanmış
  harici escalation kanalını kullanın.
- Çıkış ölçütü: Repo dışında atanmış severity modeli, kişiler, commander,
  iletişim yolu ve postmortem/action takibi; repoda sır içermeyen olay şablonu.
- Sorumlu / hedef / kanıt: REPO DIŞI / ATA.

## Kabul kuralı

Risk kabulü, hesap verebilir bir iş kararıdır ve harici sicilde tutulur.
AÇIK/ENGELLEYİCİ risk yalnız bu belgedeki ifadeyi değiştirerek sessizce kabul
edilmiş sayılamaz. Riski yalnız çıkış ölçütleri ve tarihli kanıtla kapatın.
