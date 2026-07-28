# İşletim El Kitabı (Runbook)

*Son güncelleme: 11 Temmuz 2026 · [English](OPERATIONS.md)*

Bu belge, projeye sıfırdan katılan bir mühendisin üretimi anlaması, güvenle
dağıtım yapması ve bozulanı geri alması için gereken HER işletim bilgisini
taşır. Strateji [ROADMAP](../ROADMAP.tr.md)'te, mimari kararlar
[DECISIONS](DECISIONS.tr.md)'ta; burada yalnız "nasıl işletilir" var.

---

## 1. Üretim sunucusu

| Alan | Değer |
|---|---|
| Alan adı / IP | `celikpanel.cloud` → `2.25.80.4` (Hostinger KVM2, 2 vCPU / 8 GB) |
| İşletim sistemi | Debian 13 (trixie), hostname `boston.celikhost.com` (16 Tem: makine kimlikleri celikhost.com çatısında, konuma göre adlandırıldı — celikpanel.cloud kayıt firmasında askıda) |
| Erişim | `ssh root@2.25.80.4` — **yalnız anahtar** (parola girişi kullanılmaz; yetkili anahtarlar operatörde) |
| Panel | `https://2.25.80.4:2083` (LE kurulana dek self-signed; alan adı çözülmüyorsa `curl --resolve` ile test) |
| Dış engel (Tem 2026) | Alan adı kayıt operatöründe (Hostinger) askıda — çözümü operatör yürütüyor |

**Sunucudaki yerleşim:** binary'ler `/opt/celikpanel/bin/{agent,panel}` (⚠️ `/usr/local/bin` DEĞİL),
statik arayüz `/opt/celikpanel/web/`, systemd unit'leri `celikpanel-agent` (root) + `celikpanel-panel`
(düşük yetki). Veri yolları ve ilk kurulum tek kaynaktan: `install.sh` (okuyun — kendini belgeler).

**⚠️ Bir numaralı tuzak:** `celikpanel-agent`'ı durdurmak/yeniden başlatmak paneli de düşürür
(unit bağımlılığı). Agent'a dokunan her dağıtımın SONUNDA `systemctl start celikpanel-panel` çalıştırın.

## 1b. İkinci test sunucusu (Arch — taşınabilirlik muhafızı)

| Alan | Değer |
|---|---|
| Alan adı / IP | `frankfurt.celikhost.com` → `72.62.38.15` (Hostinger KVM8, 8 vCPU / 32 GB / 400 GB) |
| İşletim sistemi | Arch Linux (bilinçli — bkz. D-004 eki: geliştirme-test hedefi) |
| Panel | `https://72.62.38.15:2083` (16 Tem: tüm sunucular ve varsayılan tek portta — 2083) |
| Erişim | `ssh root@72.62.38.15` — yalnız anahtar |
| Rol | Her değişiklik İKİ sunucuda da test edilir (16 Tem operatör kararı). Arch'ta beklenen fark: servis kataloğu "otomatik kurulamıyor" der (apt'ye özgü) — bu hata değil, dürüst davranıştır |

## 2. Dağıtım ve geri alma

Üretimdeki tek normatif güncelleme yolu incelenmiş [update.sh](../update.sh), tek normatif geri
alma yolu da [rollback.sh](../rollback.sh) dosyasıdır. Bu scriptlerin snapshot, güven zinciri,
checksum, systemd durumu veya geri yükleme iç adımlarını SSH tek satırında ya da elle yazılmış
runbook'ta tekrarlamayın. `update.sh` snapshot sözleşmesi v3 üretir; `rollback.sh` yalnız bu
doğrulanmış sözleşmeyi kabul eder.

Sürümlenmiş bu ürün scriptlerini SSH üzerinden çalıştırmak izin verilen dağıtım işidir. Bu izin;
canlı panel ayarlarını, DNS, SSL, posta, firewall veya servis yapılandırmasını SSH üzerinden
değiştirme yetkisi vermez; bu değişiklikleri operatör yalnız panelden yapar.

Dağıtımdan önce temiz commit'i birleştirip push edin; geliştirme ortamında `go test ./...`,
`go vet ./...` ve `cd web && npm run build` ile kanıtlayın. İki sunuculuk dağıtım boyunca bu
release commit'ini sabitleyin. Önce Boston'ı güncelleyip tamamen doğrulayın; yalnız Boston
geçtikten sonra Frankfurt'u güncelleyin. Her sunucunun mevcut, root tarafından güvenilen
CelikPanel checkout'unda:

```bash
test -z "$(git status --porcelain)"
sudo ./update.sh
```

`update.sh`; root güven zinciri kontrollerini, mutasyon idle kanıtlarını ve ortak flock'u,
eş panel/agent/web/veritabanı/ledger/unit snapshot'ını, servis enabled/active durum defterini,
checksum'ları, saklama politikasını, fast-forward Git güncellemesini, yeniden derlemeyi, kurulumu
ve kurulum sonrası servis kontrollerini yönetir. Doğrulanmış geri alma snapshot'ının mutlak
yolunu yazdırır. Her ret release blocker'ıdır; snapshot veya koordinatör durumunu elle onarmayın.

Her sunucudan sonra panel ve agent'ın active olmasını; kimliği doğrulanmış
`/api/v1/panel/version` yanıtının panel ile agent için beklenen aynı tam commit'i ve beklenen
şemayı bildirmesini zorunlu tutun. Sunulan UI asset'ini yükleyin ve release zaman aralığı için iki
servisin journal'ını inceleyin. Herhangi bir kontrol farklıysa ikinci sunucuya geçmeyin.

Firewall açılış kalıcılığı da aynı açık-kullanıcı sözleşmesini izler. `install.sh` restore unit'ini
kurar; ardından `enable-firewall-restore-if-saved.sh`, güvenli kayıtlı snapshot yoksa kalıcı ve
runtime enable bağlantılarını kaldırır. Boş olmayan mevcut normal snapshot'ı ise başlatmadan veya
uygulamadan yeniden etkinleştirir. İlk snapshot'ı yalnız açık **Save for reboot** işlemi
oluşturabilir ve unit ancak dayanıklı yazma başarılı olduktan sonra etkinleşir. Arka plan
senkronizasyonu mevcut snapshot'ı yenileyebilir ama unit'i asla etkinleştirmez. Açık **Turn off**
snapshot'ı kaldırıp unit'i devre dışı bırakır. GET, rescan ve arka plan durum işleri unit'i asla
etkinleştirmez.

Doğrulama başarısızsa `update.sh` çıktısındaki kesin doğrulanmış snapshot yolunu kullanın:

```bash
sudo ./rollback.sh "$VERIFIED_SNAPSHOT"
```

`rollback.sh`, bir şeyi durdurmadan veya üzerine yazmadan önce v3 snapshot'ı ve bütün
checksum'ları doğrular. Eş artefaktları geri yükler ve sahip olduğu her unit'in kayıtlı enabled
ve active durumunu birebir geri getirir; firewall unit'inin yalnız var olması etkinleştirme
yetkisi vermez. Diğer sunucuyu denemeden önce güncellenmiş sunucuyu geri alın, sonra bütün
salt-okur kontrolleri tekrarlayın.

## 3. Geliştirme ve test

- **Derleme:** `go build ./... && go vet ./...` · `cd web && npm run build` (tsc + vite; ikisi de sıfır hata ile geçmeli).
- **Görsel doğrulama (canlı backend'siz):** `tools/dev-preview/preview-server.py` — gerçek API
  şemasını taklit eden stub ile `web/dist`'i sunar; `FRESH=1` taze sunucu, `FIREWALL=on/off` bant
  durumları. Ekran görüntüsü için playwright (chromium) herhangi bir kurulumdan kullanılabilir.
  **Kural: stub gerçek şemaya sadık kalır (tipler dahil)** — sadakatsizlik bir kez gerçek hatayı
  maskeledi (capabilities.mail_server BOOL'dur, metin değil).
- **i18n:** `web/src/i18n/en.ts` anahtar kaynağıdır (TranslationKey tipini o üretir); `tr.ts`
  anahtar kümesi birebir eş olmalı. Dizgi içinde kesme işareti derlemeyi bozar — cümleyi kesme
  işaretsiz kur.
- **Tasarım döngüsü:** tasarım sistemi claude.ai/design'da; ayrıntı ve tuzaklar
  `.design-sync/NOTES.md` (özellikle: her web derlemesinde `cssEntry` hash'i güncellenmeli).
- **Dev ortam kolaylıkları:** agent root olmadan koşarken `CELIKPANEL_*` env override'ları
  (BACKUP_DIR, DKIM_DIR, MAIL_DIR, RUNTIMES_DIR, NGINX_DIR, SYSTEMD_USER) — kodda arayın,
  her biri kullanım yerinde belgeli.

## 4. Çalışma modeli (D-008 — alfa)

Operatör paneli gerçek müşteri gibi kullanır; mühendis sunucuya **asla** elle servis/ayar kurmaz.
Canlı panel ayarları ile DNS, SSL, posta, güvenlik duvarı ve servis yapılandırmasının tamamını
yalnız operatör arayüzden değiştirir. SSH, yukarıda tanımlanan sürümlenmiş CelikPanel
artefaktlarının dar kapsamlı ürün dağıtımı ve geri alınması dışında yalnız salt-okur teşhis
içindir. Operatörün çarptığı her duvar bir ürün değişikliği olarak gelir: düzelt → derle →
stub'da ekran görüntüsüyle doğrula → commit+push → ürün artefaktlarını dağıt. Commit dili
Türkçedir; her commit'e `momerefe` + `celikalperen` co-author eklenir; ayrıntı
[CONVENTIONS](CONVENTIONS.tr.md).

## 5. Canlı durum fotoğrafı (11 Temmuz 2026)

- Panel v0.1.0 + panelden kurulmuş PowerDNS; başka servis YOK (operatör panelden kuracak).
- Güvenlik duvarı: kapalı (operatör açacak — pano/Servisler amber uyarı gösteriyor).
- Domain yok (D-009 gereği önce DNS gerekiyordu; PowerDNS artık kurulu → sıradaki adım domain).
- DNSSEC: eski anahtar ve kayıt operatöründeki DS kaydı GEÇERSİZ (9 Tem sıfırlamasında DS silindi).
  DNSSEC panelden etkinleştirilince YENİ anahtar üretilir; yeni DS kayıt operatörüne o zaman girilir.
- Sıradaki adımlar: [ROADMAP](../ROADMAP.tr.md) → v0.2 listesi.

## 6. Gizli bilgiler politikası

Repoda gizli bilgi YOKTUR ve olmayacaktır (bir kez sızdı, bir daha asla — bkz. Faz 0.8 dersi).
Panel yönetici parolası yalnız operatörde; sunucu SSH anahtarları operatörün makinelerinde;
servis parolalarını (DB, posta) panel üretir ve kendi SQLite'ında tutar. Yeni mühendis erişimi =
operatörün sunucuya yeni SSH açık anahtarı eklemesi; parola paylaşımı yapılmaz.
