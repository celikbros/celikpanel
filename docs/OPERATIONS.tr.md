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
| İşletim sistemi | Debian 13 (trixie), hostname `sunucu2.celikhost.com` (16 Tem: iki makine tek çatı altında — celikpanel.cloud kayıt firmasında askıda olduğundan makine kimlikleri celikhost.com'a taşındı) |
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
| Alan adı / IP | `sunucu1.celikhost.com` → `72.62.38.15` (Hostinger KVM8, 8 vCPU / 32 GB / 400 GB) |
| İşletim sistemi | Arch Linux (bilinçli — bkz. D-004 eki: geliştirme-test hedefi) |
| Panel | `https://72.62.38.15:2083` (16 Tem: tüm sunucular ve varsayılan tek portta — 2083) |
| Erişim | `ssh root@72.62.38.15` — yalnız anahtar |
| Rol | Her değişiklik İKİ sunucuda da test edilir (16 Tem operatör kararı). Arch'ta beklenen fark: servis kataloğu "otomatik kurulamıyor" der (apt'ye özgü) — bu hata değil, dürüst davranıştır |

## 2. Dağıtım reçeteleri

Üç değişiklik türü, üç reçete. Hepsi dev makinede derlenir; sunucuya yalnız ürün kopyalanır
(sunucuda Go/Node YOKTUR, bilinçli — bkz. D-008 alfa modeli: sunucuda elle kurulum/ayar yapılmaz).

**A) Yalnız arayüz** (web/src değişti — restart gerekmez):
```bash
cd web && npm run build && cd ..
tar -C web/dist -czf /tmp/webdist.tar.gz .
scp /tmp/webdist.tar.gz root@2.25.80.4:/tmp/
ssh root@2.25.80.4 'mkdir -p /opt/celikpanel/web.new && tar -xzf /tmp/webdist.tar.gz -C /opt/celikpanel/web.new --no-same-owner && mv /opt/celikpanel/web /opt/celikpanel/web.old && mv /opt/celikpanel/web.new /opt/celikpanel/web && rm -rf /opt/celikpanel/web.old /tmp/webdist.tar.gz && echo TAMAM'
```
Yedekli geçiş: sorun anında `web.old` geri adlandırılır. `index.html` no-cache sunulur;
kullanıcı tarafında normal yenileme yeter.

**B) Panel binary'si** (cmd/panel veya internal değişti):
```bash
go build -o /tmp/panel ./cmd/panel
scp /tmp/panel root@2.25.80.4:/tmp/
ssh root@2.25.80.4 'install -m 0755 /tmp/panel /opt/celikpanel/bin/panel && systemctl restart celikpanel-panel && rm /tmp/panel && echo TAMAM'
```
1-2 saniyelik kesinti; oturumlar SQLite'ta olduğundan giriş korunur.

**C) Agent binary'si** (cmd/agent veya internal/systemd|services değişti):
```bash
go build -o /tmp/agent ./cmd/agent
scp /tmp/agent root@2.25.80.4:/tmp/
ssh root@2.25.80.4 'install -m 0755 /tmp/agent /opt/celikpanel/bin/agent && systemctl restart celikpanel-agent && systemctl start celikpanel-panel && rm /tmp/agent && echo TAMAM'
```

**Tam sürüm yükseltme:** sunucuda `update.sh` (her koşumda anlık görüntü alır: DB + binary'ler +
unit'ler + commit, son 5 saklanır); geri dönüş tek komut: `rollback.sh` (DB bilerek binary ile
birlikte döner — eski binary yeni şemaya karşı koşturulmaz).

**Dağıtım doğrulaması** (her dağıtımdan sonra, dev makineden):
```bash
curl -sk https://2.25.80.4:2083/ | grep -oE 'assets/index[^"]*'   # sunulan hash == web/dist'teki mi?
curl -sk -o /dev/null -w '%{http_code}\n' https://2.25.80.4:2083/  # 200 mü?
```

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

Operatör paneli gerçek müşteri gibi kullanır; mühendis sunucuya **asla** elle servis/ayar kurmaz
(SSH teşhis için salt-okurdur). Operatörün çarptığı her duvar bir ürün değişikliği olarak gelir:
düzelt → derle → stub'da ekran görüntüsüyle doğrula → commit+push → dağıtım reçetesini operatöre ver.
Commit dili Türkçedir; her commit'e `momerefe` + `celikalperen` co-author eklenir; ayrıntı
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
