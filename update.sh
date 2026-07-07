#!/usr/bin/env bash
# CelikPanel update — pull the latest source and re-run the idempotent
# installer. NEVER wipes anything: customer data (SQLite DB, site files,
# mail, DNS, certificates, DKIM keys) lives outside /opt/celikpanel/bin and
# /opt/celikpanel/web, which are the only things an update replaces. New
# database migrations apply automatically on panel start.
#
# CelikPanel güncelleme — son kaynağı çek ve idempotent kurucuyu yeniden
# koştur. HİÇBİR ŞEYİ silmez: müşteri verisi (SQLite DB, site dosyaları,
# posta, DNS, sertifikalar, DKIM anahtarları) /opt/celikpanel/bin ve
# /opt/celikpanel/web dışında yaşar; güncellemenin değiştirdiği yalnız bu
# ikisidir. Yeni veritabanı migration'ları panel açılışında kendiliğinden
# uygulanır.
#
# Usage / Kullanım:  sudo ./update.sh
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
    echo "Run as root: sudo ./update.sh" >&2
    exit 1
fi

cd "$(dirname "$0")"

if [[ -d .git ]]; then
    echo "==> Pulling latest source / Son kaynak çekiliyor"
    # --ff-only: an update must never silently merge local edits.
    # --ff-only: güncelleme yerel düzenlemeleri asla sessizce birleştirmemeli.
    git pull --ff-only
else
    echo "==> No git checkout here; using the sources as-is."
    echo "    (Release-tarball flow: unpack the new tarball over this directory first.)"
fi

echo "==> Re-running the installer (idempotent upgrade)"
./install.sh

echo
echo "==> Update complete. Data untouched; services restarted with the new build."
echo "    Güncelleme bitti. Veriye dokunulmadı; servisler yeni yapıyla yeniden başladı."
