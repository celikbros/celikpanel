#!/usr/bin/env bash
# CelikPanel rollback — return to the state captured by the last update.sh
# snapshot: previous binaries, previous panel database, matching source
# commit. For when an update makes things worse instead of better.
#
# The database restore intentionally comes with the binaries: migrations are
# additive, but a rolled-back binary must never run against a newer schema it
# does not know. Site files, mail, DNS and certificates are not part of the
# snapshot because updates never touch them.
#
# CelikPanel geri alma — son update.sh anlık görüntüsündeki duruma dön:
# önceki binary'ler, önceki panel veritabanı, eşleşen kaynak commit'i.
# Güncelleme işleri düzelteceğine bozduğunda içindir.
#
# Veritabanı geri dönüşü bilerek binary'lerle birlikte gelir: migration'lar
# ekleyicidir ama geri alınmış bir binary, tanımadığı daha yeni bir şemaya
# karşı asla çalışmamalıdır. Site dosyaları, posta, DNS ve sertifikalar
# anlık görüntüde yoktur; güncellemeler onlara zaten dokunmaz.
#
# Usage / Kullanım:
#   sudo ./rollback.sh              # latest snapshot / en son anlık görüntü
#   sudo ./rollback.sh <snapshot>   # a specific one / belirli biri
set -euo pipefail

SNAP_ROOT=/var/lib/celikpanel/update-snapshots
PANEL_DB=/var/lib/celikpanel/celikpanel.db
BIN_DIR=/opt/celikpanel/bin

if [[ $EUID -ne 0 ]]; then
    echo "Run as root: sudo ./rollback.sh" >&2
    exit 1
fi

snap="${1:-}"
if [[ -z "$snap" ]]; then
    snap=$(ls -1dt "$SNAP_ROOT"/*/ 2>/dev/null | head -1 || true)
fi
if [[ -z "$snap" || ! -d "$snap" ]]; then
    echo "No update snapshot found under $SNAP_ROOT — nothing to roll back to." >&2
    echo "Available snapshots / Mevcut anlık görüntüler:" >&2
    ls -1dt "$SNAP_ROOT"/*/ 2>/dev/null >&2 || echo "  (none / yok)" >&2
    exit 1
fi
snap="${snap%/}"

echo "==> Rolling back to / Geri dönülüyor: $snap"
systemctl stop celikpanel-panel 2>/dev/null || true
systemctl stop celikpanel-agent 2>/dev/null || true

if [[ -d "$snap/bin" ]]; then
    rm -rf "$BIN_DIR"
    cp -a "$snap/bin" "$BIN_DIR"
    echo "==> Binaries restored / Binary'ler geri yüklendi"
fi

if [[ -d "$snap/units" ]]; then
    cp -a "$snap/units/." /etc/systemd/system/
    systemctl daemon-reload
    echo "==> Service units restored / Servis unit'leri geri yüklendi"
fi

if [[ -f "$snap/$(basename "$PANEL_DB")" ]]; then
    # Remove current WAL/SHM too: a stale WAL against an older DB corrupts it.
    # Güncel WAL/SHM de kalksın: eski DB'ye karşı bayat WAL onu bozar.
    rm -f "$PANEL_DB" "$PANEL_DB-wal" "$PANEL_DB-shm"
    cp -a "$snap/$(basename "$PANEL_DB")" "$PANEL_DB"
    [[ -f "$snap/$(basename "$PANEL_DB")-wal" ]] && cp -a "$snap/$(basename "$PANEL_DB")-wal" "$PANEL_DB-wal"
    [[ -f "$snap/$(basename "$PANEL_DB")-shm" ]] && cp -a "$snap/$(basename "$PANEL_DB")-shm" "$PANEL_DB-shm"
    echo "==> Panel database restored / Panel veritabanı geri yüklendi"
fi

cd "$(dirname "$0")"
if [[ -d .git && -f "$snap/commit" ]]; then
    commit=$(cat "$snap/commit")
    if [[ "$commit" != "unknown" ]]; then
        git checkout --quiet "$commit" 2>/dev/null \
            && echo "==> Source checked out at $commit (git checkout main to return)" \
            || echo "==> Warning: could not check out $commit; sources left as-is"
    fi
fi

systemctl start celikpanel-agent 2>/dev/null || true
systemctl start celikpanel-panel 2>/dev/null || true

echo
echo "==> Rollback complete / Geri alma bitti."
echo "    Panel: $(systemctl is-active celikpanel-panel)  Agent: $(systemctl is-active celikpanel-agent)"
