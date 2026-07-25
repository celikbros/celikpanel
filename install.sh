#!/usr/bin/env bash
#
# CelikPanel installer — one command from a fresh Ubuntu 24.04 (first-class
# target) or Arch Linux (dev-test target) to a login screen. Idempotent: safe
# to re-run to upgrade an existing install.
#
# CelikPanel kurulumu — temiz bir Ubuntu 24.04'ten (birinci sınıf hedef) ya da
# Arch Linux'tan (geliştirme-test hedefi) giriş ekranına tek komut. Bağımsızdır:
# mevcut bir kurulumu yükseltmek için yeniden çalıştırmak güvenlidir.
#
#   sudo ./install.sh
#
# Environment knobs / Ortam ayarları:
#   SKIP_DEPS=1     do not apt-install the tiny prerequisites (tar, xz, curl)
#   SKIP_ADMIN=1    do not prompt to create the first administrator
#   LISTEN=:2083    panel bind address
#   DEMO=1          R&D mode: quick-login accounts on the login screen
#                   (admin/reseller/customer @ demo1234) and cookies that
#                   work over plain HTTP. NEVER on an internet-facing server.
#                   AR-GE modu: giriş ekranında hızlı-giriş hesapları ve düz
#                   HTTP'de çalışan çerezler. İnternete açık sunucuda ASLA.

set -euo pipefail

PREFIX=/opt/celikpanel
DATA_DIR=/var/lib/celikpanel
CONF_DIR=/etc/celikpanel
SVC_USER=celikpanel
SVC_GROUP=celikpanel
LISTEN="${LISTEN:-:2083}"

SRC="$(cd "$(dirname "$(readlink -f "$0")")" && pwd)"

c() { printf '\033[%sm%s\033[0m\n' "$1" "$2"; }
step() { c '1;36' "==> $1"; }
ok() { c '32' "    ✓ $1"; }
die() { c '1;31' "HATA: $1" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "root olarak çalıştırın (sudo ./install.sh)"
command -v systemctl >/dev/null || die "systemd gerekli"

# Package manager: apt (Ubuntu/Debian, the first-class tested target) and
# pacman (Arch, dev-test target since Jul 16) are supported. Anything else
# fails honestly instead of guessing.
# Paket yöneticisi: apt (Ubuntu/Debian, birinci sınıf test hedefi) ve pacman
# (Arch, 16 Tem'den beri geliştirme-test hedefi) desteklenir. Gerisi tahmin
# etmek yerine dürüstçe durur.
if command -v apt-get >/dev/null; then
    PKG_FAMILY=apt
elif command -v pacman >/dev/null; then
    PKG_FAMILY=pacman
else
    die "desteklenen paket yöneticisi yok (apt veya pacman gerekli)"
fi

# 1. Minimal prerequisites ---------------------------------------------------
# The panel and agent are self-contained (static Go binaries + embedded
# SQLite); we install NOTHING for hosting here. nginx / php / mariadb /
# postgresql / mail are added later from the panel, on demand, so the operator
# runs only what they actually want (constitution: what isn't installed is
# invisible). We ensure only the few tiny tools the agent itself uses.
#
# nftables belongs in this list, not in the on-demand catalog. It is the tool
# the agent shells out to for the firewall — plumbing, exactly like curl. The
# kernel packet filter (netfilter) is always present; only this userspace `nft`
# binary can be missing on a minimal image. Installing it changes nothing:
# it writes no rules and closes no ports until the operator hits "Turn on", and
# it never conflicts with ufw / firewalld / a cloud firewall (those drive the
# same nftables underneath). Having the tool ≠ enabling the firewall — so the
# firewall stays a clean on/off switch and this respects "never turn on a
# firewall by surprise."
#
# Panel ve agent kendi kendine yeter (statik Go binary + gömülü SQLite);
# barındırma için burada HİÇBİR ŞEY kurmayız. nginx / php / mariadb /
# postgresql / mail sonradan panelden, talep üzerine eklenir; böylece operatör
# yalnız gerçekten istediğini çalıştırır. Yalnız agent'ın kendi kullandığı
# birkaç küçük aracı sağlarız.
#
# nftables bu listede olmalı, talep-üzerine katalogda değil. Agent'ın firewall
# için çağırdığı araç budur — tıpkı curl gibi tesisat. Çekirdek paket süzgeci
# (netfilter) hep vardır; minimal imajda eksik olabilen yalnız bu kullanıcı-
# alanı `nft` ikilisidir. Kurmak hiçbir şeyi değiştirmez: operatör "Turn on"
# demeden tek kural yazmaz, tek port kapatmaz ve ufw / firewalld / bulut
# firewall ile ASLA çakışmaz (hepsi altta aynı nftables'ı sürer). Aracı kurmak
# ≠ firewall'u açmak — böylece firewall temiz bir aç/kapa düğmesi kalır ve bu,
# "firewall'u sürprizle açma" kuralına uyar.
if [ "${SKIP_DEPS:-0}" != "1" ]; then
    step "Küçük ön gereksinimler (curl, tar, xz, nftables)"
    case "$PKG_FAMILY" in
    apt)
        export DEBIAN_FRONTEND=noninteractive
        # A broken third-party repo must not abort the install; the packages we
        # need come from the base archives and may already be cached.
        # Bozuk bir üçüncü parti depo kurulumu iptal etmemeli; ihtiyacımız olan
        # paketler ana arşivlerden gelir ve zaten önbellekte olabilir.
        apt-get update -qq || c '33' "    apt-get update uyarı verdi — devam ediliyor"
        apt-get install -y -qq tar xz-utils curl ca-certificates nftables >/dev/null
        ;;
    pacman)
        # --needed skips what's already installed; we refresh the package
        # index but deliberately do NOT -Syu the whole system — a panel
        # installer upgrading every package would be exactly the kind of
        # surprise the constitution forbids.
        # --needed kuruluyu atlar; paket dizinini tazeleriz ama bilerek tüm
        # sistemi -Syu ile YÜKSELTMEYİZ — her paketi yükselten bir panel
        # kurucusu, anayasanın yasakladığı türden bir sürpriz olurdu.
        pacman -Sy --noconfirm --needed tar xz curl ca-certificates nftables >/dev/null
        ;;
    esac
    ok "hazır"
else
    step "Ön gereksinim kurulumu atlandı (SKIP_DEPS=1)"
fi

# 1b. Automatic security patches --------------------------------------------
# Every package the operator later installs from the panel (nginx, postfix,
# PowerDNS…) is attack surface; unattended-upgrades keeps that surface patched
# without anyone remembering to. Security origin only — never feature upgrades,
# so a hosting box is never surprised by a behaviour change, only by a fix.
# SKIP_SECURITY_UPDATES=1 opts out.
#
# 1b. Otomatik güvenlik yamaları. Operatörün panelden kurduğu her paket
# (nginx, postfix, PowerDNS…) saldırı yüzeyidir; unattended-upgrades bu yüzeyi
# kimse hatırlamak zorunda kalmadan yamalı tutar. Yalnız güvenlik kaynağı —
# asla özellik yükseltmesi; barındırma kutusu davranış değişikliğiyle
# şaşırmaz, yalnız düzeltmeyle. SKIP_SECURITY_UPDATES=1 devre dışı bırakır.
if [ "${SKIP_DEPS:-0}" != "1" ] && [ "${SKIP_SECURITY_UPDATES:-0}" != "1" ] && [ "$PKG_FAMILY" = "apt" ]; then
    step "Otomatik güvenlik yamaları (unattended-upgrades)"
    export DEBIAN_FRONTEND=noninteractive
    if apt-get install -y -qq unattended-upgrades >/dev/null 2>&1; then
        # Enable the periodic timer: update lists + apply security upgrades daily.
        # Periyodik zamanlayıcıyı aç: listeleri güncelle + günlük güvenlik yaması.
        cat > /etc/apt/apt.conf.d/20celikpanel-auto-upgrades <<'AUTOCONF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
AUTOCONF
        systemctl enable --now unattended-upgrades >/dev/null 2>&1 || true
        ok "güvenlik yamaları etkin"
    else
        c '33' "    unattended-upgrades kurulamadı — atlandı (elle kurulabilir)"
    fi
elif [ "${SKIP_DEPS:-0}" != "1" ] && [ "${SKIP_SECURITY_UPDATES:-0}" != "1" ] && [ "$PKG_FAMILY" = "pacman" ]; then
    # Arch is rolling release: there is no security-only patch channel to
    # subscribe to, so we say so instead of pretending.
    # Arch yuvarlanan sürümdür: abone olunacak güvenlik-yalnız yama kanalı
    # yoktur; öyleymiş gibi yapmak yerine bunu söyleriz.
    step "Otomatik güvenlik yamaları"
    c '33' "    Arch'ta güvenlik-yalnız kanal yok — otomatik yama kurulmadı; sistemi 'pacman -Syu' ile güncel tutun"
fi

# 2. Service user & group ----------------------------------------------------
step "Servis kullanıcısı ve grubu"
getent group "$SVC_GROUP" >/dev/null || groupadd --system "$SVC_GROUP"
if ! id "$SVC_USER" >/dev/null 2>&1; then
    useradd --system --gid "$SVC_GROUP" --home-dir "$DATA_DIR" \
        --shell /usr/sbin/nologin "$SVC_USER"
fi
ok "$SVC_USER:$SVC_GROUP"

# 3. Build if artifacts are missing ------------------------------------------
# A prebuilt release tarball already contains bin/ and web/dist, so this whole
# step is skipped there. From a bare git checkout we build from source,
# bootstrapping the Go and Node toolchains if the system lacks them — so
# "git clone && sudo ./install.sh" works on a stock Ubuntu with nothing else.
#
# Önceden derlenmiş bir release tarball zaten bin/ ve web/dist içerir; orada bu
# adım tümüyle atlanır. Çıplak bir git checkout'tan kaynaktan derleriz;
# sistemde yoksa Go ve Node araç zincirlerini indiririz — böylece stok bir
# Ubuntu'da başka hiçbir şey olmadan "git clone && sudo ./install.sh" çalışır.
GO_VERSION=1.25.0
NODE_VERSION=24.18.0
TOOLCHAIN=/opt/celikpanel/.toolchain

# Toolchain download architecture, in Go/Node naming (amd64/arm64). uname -m
# instead of dpkg so this works on every distro.
# Araç zinciri indirme mimarisi, Go/Node adlandırmasıyla (amd64/arm64).
# dpkg yerine uname -m — böylece her dağıtımda çalışır.
dl_arch() {
    case "$(uname -m)" in
        x86_64)  echo amd64 ;;
        aarch64) echo arm64 ;;
        *) die "desteklenmeyen mimari: $(uname -m)" ;;
    esac
}

bootstrap_go() {
    command -v go >/dev/null && { echo go; return; }
    [ -x "$TOOLCHAIN/go/bin/go" ] && { echo "$TOOLCHAIN/go/bin/go"; return; }
    c '33' "    Go $GO_VERSION indiriliyor…" >&2
    mkdir -p "$TOOLCHAIN"
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-$(dl_arch).tar.gz" \
        | tar -xz -C "$TOOLCHAIN" || die "Go indirilemedi"
    echo "$TOOLCHAIN/go/bin/go"
}

bootstrap_node() {
    command -v npm >/dev/null && { echo "$(command -v node | xargs dirname)"; return; }
    [ -x "$TOOLCHAIN/node/bin/npm" ] && { echo "$TOOLCHAIN/node/bin"; return; }
    c '33' "    Node $NODE_VERSION indiriliyor…" >&2
    local arch; arch=$(dl_arch); [ "$arch" = "amd64" ] && arch=x64
    mkdir -p "$TOOLCHAIN/node"
    curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${arch}.tar.xz" \
        | tar -xJ -C "$TOOLCHAIN/node" --strip-components=1 || die "Node indirilemedi"
    echo "$TOOLCHAIN/node/bin"
}

# Version stamped into BOTH binaries and the frontend, from one source: the
# git description of the commit being installed. Before this the footer showed
# a hand-typed "v0.1.0" that no build could change, so "which release is this
# server running?" had no honest answer.
# Sürüm, kurulan commit'in git tanımından TEK kaynaktan alınır ve HER İKİ
# binary ile ön yüze gömülür. Bundan önce footer, hiçbir derlemenin
# değiştiremediği elle yazılmış bir "v0.1.0" gösteriyordu; yani "bu sunucu
# hangi sürümü koşuyor?" sorusunun dürüst bir cevabı yoktu.
CP_VERSION=$(cd "$SRC" && git describe --tags --always --dirty 2>/dev/null || echo dev)
CP_COMMIT=$(cd "$SRC" && git rev-parse --short HEAD 2>/dev/null || echo unknown)
VER_FLAGS="-X main.buildVersion=$CP_VERSION -X main.buildCommit=$CP_COMMIT"

# A git checkout ALWAYS rebuilds. bin/ and web/dist are gitignored, so they
# survive `git pull` — and the old "skip the build if artifacts exist" test
# then installed the PREVIOUS build while update.sh printed "services
# restarted with the new build". A stale binary that reports the new release
# is worse than no version at all, especially when the new release is a
# security fix. Only a prebuilt release tarball (no .git) may skip.
# Bir git checkout HER ZAMAN yeniden derler. bin/ ve web/dist git'te olmadığı
# için `git pull`dan sağ çıkar — ve eski "ürünler varsa derlemeyi atla"
# denetimi, update.sh "servisler yeni yapıyla yeniden başladı" yazarken BİR
# ÖNCEKİ yapıyı kuruyordu. Yeni sürümü bildiren bayat bir binary, hiç sürüm
# olmamasından kötüdür; hele yeni sürüm bir güvenlik düzeltmesiyse. Yalnız
# önceden derlenmiş release tarball'ı (.git yok) atlayabilir.
if [ -d "$SRC/.git" ] || [ ! -x "$SRC/bin/panel" ] || [ ! -x "$SRC/bin/agent" ] || [ ! -f "$SRC/web/dist/index.html" ]; then
    step "Kaynaktan derleme (bin/panel, bin/agent, web/dist) — sürüm $CP_VERSION"
    GO_BIN=$(bootstrap_go)
    NODE_BIN=$(bootstrap_node)
    ( cd "$SRC" && "$GO_BIN" build -ldflags "-s -w $VER_FLAGS" -o bin/panel ./cmd/panel ) || die "panel derlenemedi"
    ( cd "$SRC" && "$GO_BIN" build -ldflags "-s -w $VER_FLAGS" -o bin/agent ./cmd/agent ) || die "agent derlenemedi"
    ( cd "$SRC/web" && PATH="$NODE_BIN:$PATH" npm ci --no-audit --no-fund >/dev/null 2>&1 || PATH="$NODE_BIN:$PATH" npm install --no-audit --no-fund >/dev/null 2>&1 ) || die "npm kurulumu başarısız"
    ( cd "$SRC/web" && PATH="$NODE_BIN:$PATH" npm run build >/dev/null ) || die "frontend derlenemedi"
    ok "derlendi ($CP_VERSION · $CP_COMMIT)"
else
    ok "Önceden derlenmiş release kullanılıyor (bin/ + web/dist)"
fi

# 4. Install files -----------------------------------------------------------
step "Dosyalar $PREFIX altına kuruluyor"
install -d -m 0755 "$PREFIX/bin" "$PREFIX/web" "$PREFIX/runtimes"
install -m 0755 "$SRC/bin/panel" "$PREFIX/bin/panel"
install -m 0755 "$SRC/bin/agent" "$PREFIX/bin/agent"
rm -rf "$PREFIX/web"/*
cp -r "$SRC/web/dist/." "$PREFIX/web/"
# Runtimes dir is where the agent installs Node versions; group-owned so the
# root agent writes and the panel can stat.
# Runtimes dizini agent'ın Node sürümlerini kurduğu yerdir; grup-sahipli.
chown -R root:"$SVC_GROUP" "$PREFIX/runtimes"
chmod 0775 "$PREFIX/runtimes"
ok "kuruldu"

# 5. Data directory (SQLite lives here; StateDirectory also ensures it) ------
step "Veri dizini $DATA_DIR"
install -d -m 0750 -o "$SVC_USER" -g "$SVC_GROUP" "$DATA_DIR"
install -d -m 0750 -o root -g "$SVC_GROUP" "$CONF_DIR"
ok "hazır"

# 6. systemd units -----------------------------------------------------------
step "systemd servisleri"
install -m 0644 "$SRC/deploy/systemd/celikpanel-agent.service" /etc/systemd/system/
# Install-time overrides baked into the unit: bind address, and in R&D mode
# the demo flags (quick-login accounts + cookies usable over plain HTTP).
# Kuruluma gömülen üst-geçersiz kılmalar: bağlanma adresi ve AR-GE modunda
# demo bayrakları (hızlı-giriş hesapları + düz HTTP'de kullanılabilir çerez).
# A normal install serves HTTPS (self-signed) so credentials never cross the
# wire in the clear. R&D mode (DEMO=1) stays on plain HTTP with insecure
# cookies + demo accounts. TLS and demo are opposite ends of the same switch.
# Normal kurulum HTTPS sunar (kendinden-imzalı); kimlik bilgileri asla açık
# geçmez. AR-GE modu (DEMO=1) güvensiz çerez + demo hesaplarla düz HTTP'de
# kalır. TLS ve demo aynı anahtarın iki ucudur.
PANEL_ARGS=""
TLS_ENV="Environment=CELIKPANEL_TLS=1"
if [ "${DEMO:-0}" = "1" ]; then
    PANEL_ARGS=" --insecure-cookies --demo"
    TLS_ENV=""
    c '33' "    AR-GE modu: demo hesaplar açık, çerezler düz HTTP'de çalışır — internete açmayın"
fi
sed -e "s|^Environment=CELIKPANEL_LISTEN=.*|Environment=CELIKPANEL_LISTEN=$LISTEN|" \
    -e "s|^ExecStart=/opt/celikpanel/bin/panel.*|${TLS_ENV:+$TLS_ENV\n}ExecStart=/opt/celikpanel/bin/panel$PANEL_ARGS|" \
    "$SRC/deploy/systemd/celikpanel-panel.service" > /etc/systemd/system/celikpanel-panel.service
systemctl daemon-reload
ok "kuruldu"

# 7. Start the agent (generates the shared token on first run) ---------------
# restart, not enable --now: an upgrade must actually load the new binary;
# --now is a no-op when the service is already running.
# enable --now değil restart: yükseltme yeni binary'yi gerçekten yüklemeli;
# servis zaten çalışıyorsa --now hiçbir şey yapmaz.
step "Agent başlatılıyor"
systemctl enable celikpanel-agent.service >/dev/null 2>&1 || true
systemctl restart celikpanel-agent.service || true
for _ in $(seq 1 20); do
    [ -S /run/celikpanel/agent.sock ] && break
    sleep 0.3
done
[ -S /run/celikpanel/agent.sock ] || die "agent socket oluşmadı — 'journalctl -u celikpanel-agent' inceleyin"
ok "agent çalışıyor"

# 8. First administrator -----------------------------------------------------
if [ "${SKIP_ADMIN:-0}" != "1" ]; then
    if sudo -u "$SVC_USER" CELIKPANEL_DATA_DIR="$DATA_DIR" "$PREFIX/bin/panel" --count-users 2>/dev/null | grep -q '^0$'; then
        step "İlk yönetici oluşturuluyor"
        sudo -u "$SVC_USER" CELIKPANEL_DATA_DIR="$DATA_DIR" "$PREFIX/bin/panel" --create-admin || \
            die "yönetici oluşturma başarısız"
        ok "yönetici hazır"
    else
        ok "Yönetici zaten var — atlandı"
    fi
fi

# 9. Start the panel ---------------------------------------------------------
step "Panel başlatılıyor"
systemctl enable celikpanel-panel.service >/dev/null 2>&1 || true
systemctl restart celikpanel-panel.service || true
sleep 1
systemctl is-active --quiet celikpanel-panel.service || \
    die "panel başlamadı — 'journalctl -u celikpanel-panel' inceleyin"
ok "panel çalışıyor"

# 10. Done -------------------------------------------------------------------
IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
PORT="${LISTEN##*:}"
echo
c '1;32' "CelikPanel kuruldu."
echo "    Panel:  http://${IP:-SUNUCU_IP}:${PORT}"
echo "    Servisler: systemctl status celikpanel-agent celikpanel-panel"
echo "    Günlükler: journalctl -u celikpanel-panel -f"
