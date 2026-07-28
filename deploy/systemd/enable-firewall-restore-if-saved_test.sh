#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
helper="$root/deploy/systemd/enable-firewall-restore-if-saved.sh"
tmp="$(mktemp -d)"
trap 'rm -rf -- "$tmp"' EXIT

fail() {
    printf 'FAIL: %s\n' "$1" >&2
    exit 1
}

mkdir -p "$tmp/bin"
printf '%s\n' \
    '#!/usr/bin/env bash' \
    'printf "%s\n" "$*" >> "$SYSTEMCTL_LOG"' \
    'exit "${SYSTEMCTL_EXIT_CODE:-0}"' > "$tmp/bin/systemctl"
chmod 0755 "$tmp/bin/systemctl"
export PATH="$tmp/bin:$PATH"
export SYSTEMCTL_LOG="$tmp/systemctl.log"
snapshot="$tmp/firewall.nft"

# A missing snapshot removes stale links but never creates first-install links.
# Eksik snapshot bayat bağlantıları kaldırır ama ilk kurulum bağlantısı oluşturmaz.
: > "$SYSTEMCTL_LOG"
bash "$helper" "$snapshot"
expected_missing=$'disable celikpanel-firewall-restore.service\ndisable --runtime celikpanel-firewall-restore.service'
[[ "$(<"$SYSTEMCTL_LOG")" == "$expected_missing" ]] || \
    fail "missing snapshot did not remove every stale activation link"

# A disable failure must fail installation instead of leaving stale persistence.
# Disable hatası, bayat kalıcılığı bırakmak yerine kurulumu düşürmelidir.
: > "$SYSTEMCTL_LOG"
export SYSTEMCTL_EXIT_CODE=1
if bash "$helper" "$snapshot"; then
    fail "missing-snapshot disable failure was ignored"
fi
unset SYSTEMCTL_EXIT_CODE

# A durable non-empty snapshot reconciles the current RequiredBy topology.
# Dayanıklı ve boş olmayan snapshot güncel RequiredBy topolojisini uzlaştırır.
: > "$SYSTEMCTL_LOG"
printf '{"version":2}\n' > "$snapshot"
bash "$helper" "$snapshot"
[[ "$(<"$SYSTEMCTL_LOG")" == "reenable celikpanel-firewall-restore.service" ]] || \
    fail "saved snapshot did not reenable the restore unit"

# Re-enable failure must fail the installer instead of claiming persistence.
# Yeniden etkinleştirme hatası kalıcılık varmış gibi davranmak yerine kurulumu düşürmeli.
: > "$SYSTEMCTL_LOG"
export SYSTEMCTL_EXIT_CODE=1
if bash "$helper" "$snapshot"; then
    fail "systemctl failure was ignored"
fi
unset SYSTEMCTL_EXIT_CODE

# Symlink and empty-file snapshots are abnormal and fail before systemctl.
# Sembolik bağ ve boş dosya snapshot'ları anormaldir, systemctl öncesi düşer.
: > "$SYSTEMCTL_LOG"
rm -f -- "$snapshot"
ln -s "$tmp/missing-target" "$snapshot"
if bash "$helper" "$snapshot"; then
    fail "dangling snapshot symlink was accepted"
fi
[[ ! -s "$SYSTEMCTL_LOG" ]] || fail "invalid snapshot invoked systemctl"
rm -f -- "$snapshot"
: > "$snapshot"
if bash "$helper" "$snapshot"; then
    fail "empty snapshot was accepted"
fi
[[ ! -s "$SYSTEMCTL_LOG" ]] || fail "empty snapshot invoked systemctl"

# Keep the installer invariant explicit: only the guarded helper may re-enable.
# Kurulum değişmezini açık tut: yalnız korumalı yardımcı yeniden etkinleştirebilir.
grep -Fq 'enable-firewall-restore-if-saved.sh" "$CONF_DIR/firewall.nft"' "$root/install.sh" || \
    fail "install.sh does not call the guarded helper"
if grep -Eq '^[[:space:]]*systemctl[[:space:]]+reenable[[:space:]]+celikpanel-firewall-restore' "$root/install.sh"; then
    fail "install.sh still re-enables the restore unit unconditionally"
fi

# Deployment guides must delegate to the verified v3 scripts instead of
# duplicating a privileged pseudo-runbook that can drift from their contract.
# Dağıtım kılavuzları ayrıcalıklı ve sözleşmeden sapabilecek sahte runbook'u
# kopyalamak yerine doğrulanmış v3 scriptlerine yetki vermelidir.
for runbook in "$root/docs/OPERATIONS.md" "$root/docs/OPERATIONS.tr.md"; do
    grep -Fq 'sudo ./update.sh' "$runbook" || \
        fail "$runbook does not use the canonical update script"
    grep -Fq 'sudo ./rollback.sh "$VERIFIED_SNAPSHOT"' "$runbook" || \
        fail "$runbook does not use the canonical verified rollback snapshot"
    grep -Fq 'enable-firewall-restore-if-saved.sh' "$runbook" || \
        fail "$runbook does not document the no-snapshot disable invariant"
    if grep -Fq 'systemctl reenable celikpanel-firewall-restore.service' "$runbook"; then
        fail "$runbook still duplicates privileged firewall activation commands"
    fi
done
grep -Fq 'enabled_states[$unit]' "$root/rollback.sh" || \
    fail "rollback.sh does not restore snapshotted unit enablement"
grep -Fq 'systemctl enable --runtime "$unit"' "$root/rollback.sh" || \
    fail "rollback.sh does not preserve runtime-only enablement"

bash -n "$root/install.sh" "$helper" "$0"
printf 'PASS: guarded firewall restore activation\n'
