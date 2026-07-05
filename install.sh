#!/usr/bin/env bash
#
# CelikPanel installer — one command from a fresh Ubuntu 24.04 to a login
# screen. Idempotent: safe to re-run to upgrade an existing install.
#
# CelikPanel kurulumu — temiz bir Ubuntu 24.04'ten giriş ekranına tek komut.
# Bağımsızdır: mevcut bir kurulumu yükseltmek için yeniden çalıştırmak
# güvenlidir.
#
#   sudo ./install.sh
#
# Environment knobs / Ortam ayarları:
#   SKIP_DEPS=1     do not apt-install nginx/php/mariadb/certbot
#   SKIP_ADMIN=1    do not prompt to create the first administrator
#   LISTEN=:1983    panel bind address

set -euo pipefail

PREFIX=/opt/celikpanel
DATA_DIR=/var/lib/celikpanel
CONF_DIR=/etc/celikpanel
SVC_USER=celikpanel
SVC_GROUP=celikpanel
LISTEN="${LISTEN:-:1983}"

SRC="$(cd "$(dirname "$(readlink -f "$0")")" && pwd)"

c() { printf '\033[%sm%s\033[0m\n' "$1" "$2"; }
step() { c '1;36' "==> $1"; }
ok() { c '32' "    ✓ $1"; }
die() { c '1;31' "HATA: $1" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "root olarak çalıştırın (sudo ./install.sh)"
command -v systemctl >/dev/null || die "systemd gerekli (Ubuntu/Debian)"
command -v apt-get >/dev/null || die "bu kurulum apt tabanlı dağıtımlar içindir"

# 1. System dependencies -----------------------------------------------------
if [ "${SKIP_DEPS:-0}" != "1" ]; then
    step "Sistem bağımlılıkları kuruluyor (nginx, php-fpm, mariadb, certbot)"
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get install -y -qq \
        nginx php-fpm mariadb-server certbot \
        tar xz-utils curl ca-certificates >/dev/null
    ok "bağımlılıklar hazır"
else
    step "Bağımlılık kurulumu atlandı (SKIP_DEPS=1)"
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

bootstrap_go() {
    command -v go >/dev/null && { echo go; return; }
    [ -x "$TOOLCHAIN/go/bin/go" ] && { echo "$TOOLCHAIN/go/bin/go"; return; }
    c '33' "    Go $GO_VERSION indiriliyor…" >&2
    mkdir -p "$TOOLCHAIN"
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-$(dpkg --print-architecture).tar.gz" \
        | tar -xz -C "$TOOLCHAIN" || die "Go indirilemedi"
    echo "$TOOLCHAIN/go/bin/go"
}

bootstrap_node() {
    command -v npm >/dev/null && { echo "$(command -v node | xargs dirname)"; return; }
    [ -x "$TOOLCHAIN/node/bin/npm" ] && { echo "$TOOLCHAIN/node/bin"; return; }
    c '33' "    Node $NODE_VERSION indiriliyor…" >&2
    local arch; arch=$(dpkg --print-architecture); [ "$arch" = "amd64" ] && arch=x64
    mkdir -p "$TOOLCHAIN/node"
    curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${arch}.tar.xz" \
        | tar -xJ -C "$TOOLCHAIN/node" --strip-components=1 || die "Node indirilemedi"
    echo "$TOOLCHAIN/node/bin"
}

if [ ! -x "$SRC/bin/panel" ] || [ ! -x "$SRC/bin/agent" ] || [ ! -f "$SRC/web/dist/index.html" ]; then
    step "Kaynaktan derleme (bin/panel, bin/agent, web/dist)"
    GO_BIN=$(bootstrap_go)
    NODE_BIN=$(bootstrap_node)
    ( cd "$SRC" && "$GO_BIN" build -ldflags "-s -w" -o bin/panel ./cmd/panel ) || die "panel derlenemedi"
    ( cd "$SRC" && "$GO_BIN" build -ldflags "-s -w" -o bin/agent ./cmd/agent ) || die "agent derlenemedi"
    ( cd "$SRC/web" && PATH="$NODE_BIN:$PATH" npm ci --no-audit --no-fund >/dev/null 2>&1 || PATH="$NODE_BIN:$PATH" npm install --no-audit --no-fund >/dev/null 2>&1 ) || die "npm kurulumu başarısız"
    ( cd "$SRC/web" && PATH="$NODE_BIN:$PATH" npm run build >/dev/null ) || die "frontend derlenemedi"
    ok "derlendi"
else
    ok "Mevcut derleme kullanılıyor (bin/ + web/dist)"
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
# The panel bind address is the one install-time override we bake in.
# Panel bağlanma adresi, kuruluma gömdüğümüz tek üst-geçersiz kılmadır.
sed "s|^Environment=CELIKPANEL_LISTEN=.*|Environment=CELIKPANEL_LISTEN=$LISTEN|" \
    "$SRC/deploy/systemd/celikpanel-panel.service" > /etc/systemd/system/celikpanel-panel.service
systemctl daemon-reload
ok "kuruldu"

# 7. Start the agent (generates the shared token on first run) ---------------
step "Agent başlatılıyor"
systemctl enable --now celikpanel-agent.service >/dev/null 2>&1 || true
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
systemctl enable --now celikpanel-panel.service >/dev/null 2>&1 || true
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
