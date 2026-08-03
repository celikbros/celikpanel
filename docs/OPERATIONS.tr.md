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

## 2. Dağıtım reçeteleri

İki dağıtım yolu vardır. Hepsi dev makinede derlenir; sunucuya yalnız ürün artefaktları kopyalanır
(sunucuda Go/Node YOKTUR, bilinçli — bkz. D-008 alfa modeli: sunucuda elle kurulum/ayar yapılmaz).
Sürümlenmiş ürün artefaktlarını SSH ile kopyalayıp atomik biçimde kurmak izin verilen ürün
dağıtımıdır. Bu izin; canlı panel ayarlarını, DNS, SSL, posta, güvenlik duvarı veya servis
yapılandırmasını SSH üzerinden değiştirme yetkisi vermez; bunlar yalnız arayüzden yapılır.

**A) Yalnız arayüz** (yalnız `web/src` değişti; Go, `internal` veya migration değişikliği yok —
restart gerekmez):
```bash
cd web && npm run build && cd ..
tar -C web/dist -czf /tmp/webdist.tar.gz .
scp /tmp/webdist.tar.gz root@2.25.80.4:/tmp/
ssh root@2.25.80.4 'mkdir -p /opt/celikpanel/web.new && tar -xzf /tmp/webdist.tar.gz -C /opt/celikpanel/web.new --no-same-owner && mv /opt/celikpanel/web /opt/celikpanel/web.old && mv /opt/celikpanel/web.new /opt/celikpanel/web && rm -rf /opt/celikpanel/web.old /tmp/webdist.tar.gz && echo TAMAM'
```
Yedekli geçiş: sorun anında `web.old` geri adlandırılır. `index.html` no-cache sunulur;
kullanıcı tarafında normal yenileme yeter.

**B) Eş-sürüm backend dağıtımı** (`cmd/panel`, `cmd/agent`, `internal`, migration veya başka
herhangi bir backend değişikliği):

Panel ve agent, hata durumunda kapalı kalan tek bir sürüm çiftidir. İki binary'den birini asla
tek başına derlemeyin, kurmayın veya geri almayın. İkisini de aynı temiz Git commit'inden ve
aynı `main.buildVersion` ile `main.buildCommit` bağlayıcı değerleriyle derleyin; backend
dağıtımı aynı commit'in web derlemesini de taşır. Commit değeri, dağıtım sonrasında doğrulanacak
tam SHA olmalıdır.

```bash
test -z "$(git status --porcelain)"
RELEASE_COMMIT="$(git rev-parse HEAD)"
RELEASE_VERSION="$(git describe --tags --always)"
RELEASE_FLAGS="-X main.buildVersion=${RELEASE_VERSION} -X main.buildCommit=${RELEASE_COMMIT}"
go build -trimpath -ldflags "-s -w ${RELEASE_FLAGS}" -o /tmp/celikpanel-agent ./cmd/agent
go build -trimpath -ldflags "-s -w ${RELEASE_FLAGS}" -o /tmp/celikpanel-panel ./cmd/panel
cd web && npm run build && cd ..
tar -C web/dist -czf /tmp/webdist.tar.gz .
```

Çalışan çifti değiştirmeden önce üç artefaktı da yükleyin. Her sunucuda dağıtım sırası:
**önce agent**, ardından **panel ve web**, son olarak paneli başlatma. Herhangi bir aşama
başarısız olursa durun; karışık bir çiftle panel mutasyonları yapmayın.

```bash
SERVER=root@2.25.80.4
scp /tmp/celikpanel-agent /tmp/celikpanel-panel /tmp/webdist.tar.gz "$SERVER":/tmp/
ssh "$SERVER" 'install -m 0755 /tmp/celikpanel-agent /opt/celikpanel/bin/agent.next && cp -a /opt/celikpanel/bin/agent /opt/celikpanel/bin/agent.previous && mv -f /opt/celikpanel/bin/agent.next /opt/celikpanel/bin/agent && systemctl restart celikpanel-agent'
ssh "$SERVER" 'install -m 0755 /tmp/celikpanel-panel /opt/celikpanel/bin/panel.next && cp -a /opt/celikpanel/bin/panel /opt/celikpanel/bin/panel.previous && rm -rf /opt/celikpanel/web.new /opt/celikpanel/web.previous && mkdir -p /opt/celikpanel/web.new && tar -xzf /tmp/webdist.tar.gz -C /opt/celikpanel/web.new --no-same-owner && mv /opt/celikpanel/web /opt/celikpanel/web.previous && mv /opt/celikpanel/web.new /opt/celikpanel/web && mv -f /opt/celikpanel/bin/panel.next /opt/celikpanel/bin/panel && systemctl start celikpanel-panel'
```
Aynı sürüm çiftini iki sunucuda da tekrarlayın. Doğrulama geçene kadar önceki çifti ve
web ağacını hazır tutun. Geri alma da eş-sürümdür: birbiriyle eşleşen agent, panel, web ve şema
anlık görüntüsünü birlikte geri yükleyin. Oturumlar SQLite'ta olduğundan normal kısa yeniden
başlatma kullanıcıların oturumunu kapatmaz.

**Anlık görüntülü tam sürüm:** yukarıdaki eş-sürüm artefakt reçetesi esas yöntemdir. Veritabanını
migrate edebilecek bir sürümden önce `update.sh` ile aynı DB + WAL + binary'ler + unit'ler +
commit anlık görüntüsünü alın (son 5 saklanır). `update.sh` yalnız önceden derlenmiş tam-commit
çiftini aynı agent-önce sırasıyla kuruyorsa kullanılabilir; aksi halde eş-sürüm artefakt
reçetesini kullanın. `rollback.sh`, DB ile eşleşen binary'leri birlikte geri yükler — eski binary
asla yeni şemaya karşı koşturulmamalıdır.

**Dağıtım doğrulaması** (her dağıtımdan sonra, dev makineden):
```bash
curl -sk https://2.25.80.4:2083/ | grep -oE 'assets/index[^"]*'   # sunulan hash == web/dist'teki mi?
curl -sk -o /dev/null -w '%{http_code}\n' https://2.25.80.4:2083/  # 200 mü?
curl -sk https://2.25.80.4:2083/api/v1/version
# Şart: commit == RELEASE_COMMIT, agent_commit == RELEASE_COMMIT, agent_matches == true.
```
Bu üç sürüm şartını iki sunucuda da doğrulayın. Eşleşmeyen bir çiftte arayüzün açılması yeterli
değildir; backend, tam commit'ler eşleşene dek yetkili mutasyonlarda kapalı kalır.

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
