#!/usr/bin/env bash
set -euo pipefail

snapshot_path="${1:?firewall snapshot path is required}"
unit_name="celikpanel-firewall-restore.service"

# Absence is the fresh-install state: remove stale persistent and runtime
# activation links without starting or stopping the unit. This also makes an
# upgrade converge to the same state when no explicit saved policy exists.
# Yokluk temiz kurulum durumudur: unit'i başlatmadan veya durdurmadan bayat
# kalıcı ve runtime etkinleştirme bağlantılarını kaldır. Böylece açıkça kaydedilmiş
# politika yokken yükseltme de aynı duruma yakınsar.
if [[ ! -e "$snapshot_path" && ! -L "$snapshot_path" ]]; then
    systemctl disable "$unit_name" >/dev/null
    systemctl disable --runtime "$unit_name" >/dev/null
    exit 0
fi
if [[ -L "$snapshot_path" || ! -f "$snapshot_path" || ! -s "$snapshot_path" ]]; then
    printf 'invalid firewall snapshot path: %s\n' "$snapshot_path" >&2
    exit 1
fi

# Re-enable updates links from an older [Install] topology without starting or
# reapplying the firewall during the upgrade.
# Yeniden etkinleştirme eski [Install] topolojisinin bağlantılarını günceller;
# yükseltme sırasında firewall'u başlatmaz veya yeniden uygulamaz.
systemctl reenable "$unit_name" >/dev/null
