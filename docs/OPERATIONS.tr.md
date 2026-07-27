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

**B) Anlık görüntülü eş-sürüm backend dağıtımı** (`cmd/panel`, `cmd/agent`, `internal`,
migration veya başka herhangi bir backend değişikliği):

Panel ve agent, hata durumunda kapalı kalan tek bir sürüm çiftidir. İki binary'den birini asla
tek başına derlemeyin, kurmayın veya geri almayın. İkisini de aynı temiz ve birleştirilmiş Git
commit'inden derleyin; tam 40 karakterlik commit SHA'sını iki binary'ye de gömün. Backend
dağıtımı aynı commit'in web derlemesini de taşır.

```bash
test -z "$(git status --porcelain)"
RELEASE_COMMIT="$(git rev-parse --verify HEAD)"
test "$(printf %s "$RELEASE_COMMIT" | wc -c)" -eq 40
RELEASE_VERSION="$(git describe --tags --always)"
RELEASE_DIR="/tmp/celikpanel-release-${RELEASE_COMMIT}"
mkdir -p "$RELEASE_DIR"
LDFLAGS="-s -w -X main.buildVersion=${RELEASE_VERSION} -X main.buildCommit=${RELEASE_COMMIT}"
go test ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "$RELEASE_DIR/agent" ./cmd/agent
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "$RELEASE_DIR/panel" ./cmd/panel
(cd web && npm run build)
tar -C web/dist -czf "$RELEASE_DIR/web.tar.gz" .
(cd "$RELEASE_DIR" && sha256sum agent panel web.tar.gz > SHA256SUMS)
```

Frankfurt'a başlamadan önce Boston dağıtımını bütünüyle tamamlayıp doğrulayın. Frankfurt'a
aynı artefakt baytlarını gönderin. Frankfurt tamamlanamazsa hemen düzeltin veya Boston'ı
anlık görüntüsünden geri alın; DNS çiftini farklı sürümlerde bırakmayın. Her sunucuda önce
benzersiz release dizinine yükleyip manifesti doğrulayın:

```bash
SERVER=root@2.25.80.4                  # sonra root@72.62.38.15
PANEL_HOST=boston.celikhost.com        # sonra frankfurt.celikhost.com
REMOTE_RELEASE="/opt/celikpanel/releases/${RELEASE_COMMIT}"
ssh "$SERVER" "install -d -m 0755 '$REMOTE_RELEASE'"
scp "$RELEASE_DIR/agent" "$RELEASE_DIR/panel" "$RELEASE_DIR/web.tar.gz" \
    "$RELEASE_DIR/SHA256SUMS" "$SERVER:$REMOTE_RELEASE/"
ssh "$SERVER" "cd '$REMOTE_RELEASE' && sha256sum -c SHA256SUMS"
```
Şema değiştiren sürümden önce, kimliği doğrulanmış yönetici oturumuyla mevcut sürüm JSON'unu
kaydedin; sonra paneli durdurup zorunlu anlık görüntüyü alın. `ADMIN_COOKIE_JAR`, normal panel
girişinden (açıksa TOTP dahil) gelmeli, repo dışında ve `0600` modunda tutulmalıdır.
`PANEL_HOST`, tarayıcıdaki alan adına bağlı oturum çerezinin gönderilmesi için IP adresi değil,
bu girişte kullanılan hostname olmalıdır.

```bash
curl -fsSk -b "$ADMIN_COOKIE_JAR" \
    "https://${PANEL_HOST}:2083/api/v1/panel/version" > "$RELEASE_DIR/${PANEL_HOST}.version-before.json"

DEPLOY_ID="$(date -u +%Y%m%dT%H%M%SZ)-${RELEASE_COMMIT}"
SNAPSHOT="/var/backups/celikpanel/releases/${DEPLOY_ID}"
ssh "$SERVER" "SNAPSHOT='$SNAPSHOT' bash -se" <<'REMOTE'
set -euo pipefail
systemctl stop celikpanel-panel
install -d -m 0700 "$SNAPSHOT"
cp -a /opt/celikpanel/bin/agent /opt/celikpanel/bin/panel "$SNAPSHOT/"
cp -a /opt/celikpanel/web "$SNAPSHOT/web"
cp -a /var/lib/celikpanel/celikpanel.db "$SNAPSHOT/"
for sidecar in -wal -shm; do
    source="/var/lib/celikpanel/celikpanel.db${sidecar}"
    if [ -f "$source" ]; then cp -a "$source" "$SNAPSHOT/"; fi
done
cp -a /etc/systemd/system/celikpanel-agent.service \
      /etc/systemd/system/celikpanel-panel.service "$SNAPSHOT/"
sha256sum "$SNAPSHOT/agent" "$SNAPSHOT/panel" > "$SNAPSHOT/BINARY_SHA256SUMS"
systemctl cat celikpanel-agent celikpanel-panel > "$SNAPSHOT/units.txt"
REMOTE
scp "$RELEASE_DIR/${PANEL_HOST}.version-before.json" "$SERVER:$SNAPSHOT/version-before.json"
```

Korumasız her kopya zorunludur: DB, binary, web ağacı veya unit dosyası eksikse dağıtım durur.
WAL/SHM yalnız SQLite bunları oluşturmamış olabileceği için isteğe bağlıdır. Şema değiştiren
sürümlerde, aynı hata-kapalı DB yan dosyaları + iki binary + web + unit + önceki commit
kimliği snapshot/restore güvencesini sağlamadan `update.sh` veya `rollback.sh` kullanmayın.

Panel dururken doğrulanmış release dizininden önce agent'ı atomik değiştirip yeniden başlatın;
sonra panel ile web'i hazırlayıp paneli en son başlatın. Bu sırada anlık görüntüyü yeniden
kullanmayın veya silmeyin.

```bash
ssh "$SERVER" "REMOTE_RELEASE='$REMOTE_RELEASE' SNAPSHOT='$SNAPSHOT' RELEASE_COMMIT='$RELEASE_COMMIT' bash -se" <<'REMOTE'
set -euo pipefail
install -m 0755 "$REMOTE_RELEASE/agent" /opt/celikpanel/bin/agent.next
mv -f /opt/celikpanel/bin/agent.next /opt/celikpanel/bin/agent
systemctl restart celikpanel-agent

install -m 0755 "$REMOTE_RELEASE/panel" /opt/celikpanel/bin/panel.next
WEB_NEXT="/opt/celikpanel/web.${RELEASE_COMMIT}.next"
test ! -e "$WEB_NEXT"
install -d -m 0755 "$WEB_NEXT"
tar -xzf "$REMOTE_RELEASE/web.tar.gz" -C "$WEB_NEXT" --no-same-owner
mv /opt/celikpanel/web "$SNAPSHOT/web-before-swap"
mv "$WEB_NEXT" /opt/celikpanel/web
mv -f /opt/celikpanel/bin/panel.next /opt/celikpanel/bin/panel
systemctl start celikpanel-panel
REMOTE
```

**Dağıtım doğrulaması** (sonraki sunucuya geçmeden önce her sunucuda zorunlu):

```bash
EXPECTED_SCHEMA=20                    # sürümün migration hedefini yazın
curl -fsSk -b "$ADMIN_COOKIE_JAR" \
    "https://${PANEL_HOST}:2083/api/v1/panel/version" | \
    jq -e --arg commit "$RELEASE_COMMIT" --argjson schema "$EXPECTED_SCHEMA" \
      '.commit == $commit and
       .agent_commit == $commit and
       .agent_matches == true and
       .schema_version == $schema'

AGENT_PID="$(ssh "$SERVER" 'systemctl show -p MainPID --value celikpanel-agent')"
PANEL_PID="$(ssh "$SERVER" 'systemctl show -p MainPID --value celikpanel-panel')"
ssh "$SERVER" "systemctl is-active --quiet celikpanel-agent celikpanel-panel && \
    sha256sum /proc/$AGENT_PID/exe /proc/$PANEL_PID/exe"
curl -fsSk "https://${PANEL_HOST}:2083/" | grep -oE 'assets/index[^"]*'
```

Çalışan iki `/proc/.../exe` hash'i yüklenen `agent` ve `panel` hash'leriyle aynı olmalı;
API'nin `schema_version` değeri `EXPECTED_SCHEMA` değerine eşit olmalı; sunulan asset bu web
arşivine ait olmalı ve dağıtım zaman aralığında iki servis günlüğü de temiz kalmalıdır.
Yalnız arayüzün açılması yeterli değildir. Geri alma eş-sürümdür: paneli durdurun, başarısız DB dosyalarını
kenara taşıyın; snapshot'taki DB + yan dosyalar + agent + panel + web + unit'leri birlikte geri
yükleyin, önce agent'ı ve en son paneli başlatın.

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
