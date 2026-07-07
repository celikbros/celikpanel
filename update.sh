#!/usr/bin/env bash
# CelikPanel update — pull the latest source and re-run the idempotent
# installer. NEVER wipes anything: customer data (SQLite DB, site files,
# mail, DNS, certificates, DKIM keys) lives outside /opt/celikpanel/bin and
# /opt/celikpanel/web, which are the only things an update replaces. New
# database migrations apply automatically on panel start.
#
# Before touching anything, a rollback snapshot is taken (panel database +
# current binaries + source commit). If an update makes things worse instead
# of better, `sudo ./rollback.sh` returns to the previous working state in
# seconds.
#
# CelikPanel güncelleme — son kaynağı çek ve idempotent kurucuyu yeniden
# koştur. HİÇBİR ŞEYİ silmez: müşteri verisi (SQLite DB, site dosyaları,
# posta, DNS, sertifikalar, DKIM anahtarları) /opt/celikpanel/bin ve
# /opt/celikpanel/web dışında yaşar; güncellemenin değiştirdiği yalnız bu
# ikisidir. Yeni veritabanı migration'ları panel açılışında kendiliğinden
# uygulanır.
#
# Hiçbir şeye dokunmadan önce bir geri-alma anlık görüntüsü alınır (panel
# veritabanı + mevcut binary'ler + kaynak commit'i). Güncelleme işleri
# düzelteceğine bozarsa `sudo ./rollback.sh` saniyeler içinde önceki çalışan
# duruma döner.
#
# Usage / Kullanım:  sudo ./update.sh
set -euo pipefail

SNAP_ROOT=/var/lib/celikpanel/update-snapshots
PANEL_DB=/var/lib/celikpanel/celikpanel.db
BIN_DIR=/opt/celikpanel/bin
KEEP_SNAPSHOTS=5

if [[ $EUID -ne 0 ]]; then
    echo "Run as root: sudo ./update.sh" >&2
    exit 1
fi

cd "$(dirname "$0")"

# ---- 1. Rollback snapshot / Geri-alma anlık görüntüsü --------------------
commit="unknown"
if [[ -d .git ]]; then
    commit=$(git rev-parse --short HEAD)
fi
snap="$SNAP_ROOT/$(date +%Y%m%d_%H%M%S)-$commit"
mkdir -p "$snap"

echo "==> Snapshot before update / Güncelleme öncesi anlık görüntü: $snap"
# Stop the panel so the SQLite copy is consistent (WAL included). The agent
# never writes the panel DB, so it can keep running.
# SQLite kopyası tutarlı olsun diye paneli durdur (WAL dahil). Agent panel
# DB'sine hiç yazmaz; çalışmaya devam edebilir.
systemctl stop celikpanel-panel 2>/dev/null || true
if [[ -f "$PANEL_DB" ]]; then
    cp -a "$PANEL_DB" "$snap/" 2>/dev/null || true
    cp -a "$PANEL_DB-wal" "$snap/" 2>/dev/null || true
    cp -a "$PANEL_DB-shm" "$snap/" 2>/dev/null || true
fi
if [[ -d "$BIN_DIR" ]]; then
    cp -a "$BIN_DIR" "$snap/bin"
fi
# Unit files carry the install-time configuration (port, TLS); snapshot them
# so a rollback restores configuration, not just code.
# Unit dosyaları kurulum anı yapılandırmasını taşır (port, TLS); geri alma
# yalnız kodu değil yapılandırmayı da geri getirsin diye anlık görüntüye al.
mkdir -p "$snap/units"
cp -a /etc/systemd/system/celikpanel-*.service "$snap/units/" 2>/dev/null || true
echo "$commit" > "$snap/commit"

# Keep only the newest snapshots. / Yalnız en yeni anlık görüntüler kalsın.
ls -1dt "$SNAP_ROOT"/*/ 2>/dev/null | tail -n +$((KEEP_SNAPSHOTS + 1)) | xargs -r rm -rf

# ---- 2. Pull + reinstall / Çek + yeniden kur ------------------------------
if [[ -d .git ]]; then
    echo "==> Pulling latest source / Son kaynak çekiliyor"
    # --ff-only: an update must never silently merge local edits. The pull
    # runs as the checkout's owner so their SSH deploy key is used — root's
    # SSH environment usually has neither the key nor the known_hosts entry.
    # --ff-only: güncelleme yerel düzenlemeleri asla sessizce birleştirmemeli.
    # Pull, çalışma kopyasının sahibi olarak koşar ki onun SSH deploy
    # anahtarı kullanılsın — root'un SSH ortamında çoğu kez ne anahtar ne
    # known_hosts kaydı vardır.
    repo_owner=$(stat -c %U .git)
    if [[ "$repo_owner" != "root" ]]; then
        pull_ok=0
        sudo -u "$repo_owner" -H git pull --ff-only && pull_ok=1
    else
        pull_ok=0
        git pull --ff-only && pull_ok=1
    fi
    if [[ "$pull_ok" != 1 ]]; then
        echo "!! git pull failed — continuing with the sources already here."
        echo "!! git pull başarısız — buradaki mevcut kaynakla devam ediliyor."
        echo "   (Fetch manually and re-run / Elle çekip yeniden koşun: git pull && sudo ./update.sh)"
    fi
else
    echo "==> No git checkout here; using the sources as-is."
    echo "    (Release-tarball flow: unpack the new tarball over this directory first.)"
fi

echo "==> Re-running the installer (idempotent upgrade)"
# Preserve the installed panel's listen address: the installer's default
# must never override what this server was set up with.
# Kurulu panelin dinleme adresini koru: kurucunun varsayılanı bu sunucunun
# kurulduğu ayarı asla ezmemeli.
current_listen=$(grep -h "^Environment=CELIKPANEL_LISTEN=" /etc/systemd/system/celikpanel-panel.service 2>/dev/null | cut -d= -f3- || true)
if [[ -n "$current_listen" ]]; then
    echo "    (keeping listen address / dinleme adresi korunuyor: $current_listen)"
    LISTEN="$current_listen" ./install.sh
else
    ./install.sh
fi

echo
echo "==> Update complete. Data untouched; services restarted with the new build."
echo "    Güncelleme bitti. Veriye dokunulmadı; servisler yeni yapıyla yeniden başladı."
echo "    Sorun çıkarsa / If this made things worse:  sudo ./rollback.sh"
