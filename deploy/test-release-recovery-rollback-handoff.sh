#!/usr/bin/env bash
# Exercise the production runner's retained-release dispatch into the real
# rollback entrypoint prefix, including environment capture and its lock gate.
# The fixture stops before snapshot restoration; no host services are changed.
# Gerçek kurtarma dağıtımı, geri almanın ortam ve kilit girişini birlikte sınar.
# Yedek geri yüklenmeden durur; sunucunun hizmetlerinde değişiklik yapmaz.
set -Eeuo pipefail
fail() { printf 'rollback recovery handoff: %s\n' "$*" >&2; exit 1; }
[[ $EUID -eq 0 ]] || fail 'run this contract as root'
[[ $# -le 1 ]] || fail 'expected at most one rollback source path'
REPO_ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
ROLLBACK_SOURCE=${1:-$REPO_ROOT/rollback.sh}
TEST_ROOT=$(mktemp -d /run/celikpanel-rollback-handoff.XXXXXXXX)
trap 'rm -rf -- "$TEST_ROOT"' EXIT
chmod 0700 "$TEST_ROOT"
TARGET_COMMIT=1111111111111111111111111111111111111111
SNAPSHOT=20260914T043142Z-from-unknown-to-$TARGET_COMMIT-db8813bd340936d749913d55dbcda68d
TRANSACTION_ROOT=$TEST_ROOT/var/lib/celikpanel-release-transaction
RELEASE=$TEST_ROOT/var/backups/celikpanel/releases/${TARGET_COMMIT:0:12}-aaaaaaaaaaaaaaaaaaaaaaaa
SNAPSHOT_PATH=$TEST_ROOT/var/backups/celikpanel/update-snapshots/$SNAPSHOT
install -d -m 0755 "$TEST_ROOT/var/lib" "$TEST_ROOT/var/backups/celikpanel"
install -d -m 0700 "$TRANSACTION_ROOT" "$(dirname -- "$RELEASE")" \
    "$(dirname -- "$SNAPSHOT_PATH")" "$RELEASE" "$SNAPSHOT_PATH"
install -d -m 0755 "$RELEASE/bin" "$RELEASE/deploy/systemd"
for source in release-transaction-guard.sh release-transaction-start-guard.sh \
    panel-tls-snapshot.sh release-recovery-runner.sh release-recovery-foundation.sh \
    release-recovery.protocol release-sequence-policy; do
    install -m 0644 "$REPO_ROOT/deploy/$source" "$RELEASE/deploy/$source"
done
for unit in celikpanel-release-recovery.service celikpanel-release-recovery.timer; do
    install -m 0644 "$REPO_ROOT/deploy/systemd/$unit" "$RELEASE/deploy/systemd/$unit"
done
for executable in install.sh update.sh bin/agent bin/panel; do
    printf '#!/bin/bash\nexit 98\n' >"$RELEASE/$executable"
    chmod 0755 "$RELEASE/$executable"
done
printf '1\n' >"$RELEASE/release.version"
printf '%s\n' "$TARGET_COMMIT" >"$RELEASE/release.commit"
printf '2222222222222222222222222222222222222222\n' >"$RELEASE/release.tree"
printf '%s\t%s\t%s\n' \
    celikpanel-agent.service enabled active \
    celikpanel-panel.service enabled active \
    celikpanel-firewall-restore.service disabled inactive \
    >"$SNAPSHOT_PATH/service-states.tsv"
chmod 0600 "$SNAPSHOT_PATH/service-states.tsv"
: >"$TRANSACTION_ROOT/transaction.lock"
chmod 0600 "$TRANSACTION_ROOT/transaction.lock"

# Preserve the complete production prefix and its actual top-level calls.
# Only fixed filesystem anchors and unrelated vendor-platform preflight are
# replaced. This consumes the runner's real env -i tuple, inherited FD9 and
# exact active marker; the child is not a substitute lock implementation.
python3 - "$ROLLBACK_SOURCE" "$RELEASE/rollback.sh" <<'PY'
from pathlib import Path
import sys
source = Path(sys.argv[1]).read_text(encoding="utf-8")
boundary = "# Every privileged path component must be root-owned and non-writable"
assert source.count(boundary) == 1, "rollback prefix boundary changed"
prefix = source.split(boundary, 1)[0]
def replace_once(old, new):
    global prefix
    assert prefix.count(old) == 1, f"rollback fixture boundary changed: {old}"
    prefix = prefix.replace(old, new, 1)
replace_once(
    "RELEASE_TRANSACTION_ROOT=/var/lib/celikpanel-release-transaction",
    'RELEASE_TRANSACTION_ROOT=${CELIKPANEL_RELEASE_RECOVERY_TEST_ROOT:?}/var/lib/celikpanel-release-transaction',
)
replace_once(
    '[[ "$parent" == /var/lib &&',
    '[[ "$parent" == "$CELIKPANEL_RELEASE_RECOVERY_TEST_ROOT/var/lib" &&',
)
replace_once("rollback_machine=$(vendor_machine_architecture)", "rollback_machine=fixture")
replace_once('preflight_rollback_platform "$SELINUX_OS_RELEASE" "$rollback_machine"', ": # Vendor preflight is outside this lock contract.")
faults = r"""
# Fault injection in the generated child, before actual environment capture.
case $(cat "${CELIKPANEL_RELEASE_RECOVERY_TEST_ROOT:?}/scenario") in
    valid) ;;
    shared) flock -s 9 ;;
    closed) exec 9>&- ;;
    wrong-identity) export CELIKPANEL_RECOVERY_LOCK_IDENTITY=0:0 ;;
    wrong-tuple) export CELIKPANEL_RECOVERY_EXPECTED_TOKEN=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb ;;
    *) exit 97 ;;
esac
"""
replace_once('RECOVER_EXISTING_TRANSACTION="${CELIKPANEL_RECOVER_EXISTING_TRANSACTION:-0}"', faults + '\nRECOVER_EXISTING_TRANSACTION="${CELIKPANEL_RECOVER_EXISTING_TRANSACTION:-0}"')
prefix += r"""
[[ $RECOVER_EXISTING_TRANSACTION == 1 && $RELEASE_TRANSACTION_FD == 9 ]] || die 'runner did not dispatch inherited rollback recovery'
[[ $RECOVERY_EXPECTED_OPERATION == update && $RECOVERY_EXPECTED_PHASE == active ]] || die 'runner selected the wrong recovery tuple'
[[ $# == 1 && $1 == "$CELIKPANEL_RELEASE_RECOVERY_TEST_ROOT/var/backups/celikpanel/update-snapshots/$RECOVERY_EXPECTED_SNAPSHOT" ]] || die 'runner did not pass the exact snapshot'
: >"$CELIKPANEL_RELEASE_RECOVERY_TEST_ROOT/rollback-gate-passed"
# Leave the active marker: a lock proof does not complete snapshot restoration.
exit 0
"""
Path(sys.argv[2]).write_text(prefix, encoding="utf-8", newline="\n")
PY
chmod 0755 "$RELEASE/rollback.sh"
(
    cd "$RELEASE"
    LC_ALL=C find . -xdev -type f ! -path './SHA256SUMS' -print0 \
        | LC_ALL=C sort -z | xargs -0 sha256sum >SHA256SUMS
)
chmod 0644 "$RELEASE/SHA256SUMS"
for scenario in valid shared closed wrong-identity wrong-tuple; do
    printf '%s\n' "$scenario" >"$TEST_ROOT/scenario"
    rm -f -- "$TEST_ROOT/rollback-gate-passed"
    printf '%s\n' version=1 \
        token=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
        operation=update "snapshot=$SNAPSHOT" >"$TRANSACTION_ROOT/active"
    chmod 0600 "$TRANSACTION_ROOT/active"
    before=$(sha256sum "$TRANSACTION_ROOT/active")
    status=0
    CELIKPANEL_RELEASE_RECOVERY_TESTING=1 \
        CELIKPANEL_RELEASE_RECOVERY_TEST_ROOT="$TEST_ROOT" \
        /bin/bash "$REPO_ROOT/deploy/release-recovery-runner.sh" \
        >"$TEST_ROOT/$scenario.log" 2>&1 || status=$?
    [[ $status -ne 0 ]] || fail "$scenario unexpectedly claimed completed recovery"
    [[ $(sha256sum "$TRANSACTION_ROOT/active") == "$before" ]] || fail "$scenario changed the active transaction"
    case "$scenario" in
        valid)
            [[ -f "$TEST_ROOT/rollback-gate-passed" ]] || {
                cat "$TEST_ROOT/$scenario.log" >&2
                fail 'real rollback entrypoint rejected the runner handoff'
            }
            expected='release recovery child returned success while a verified marker remains'
            ;;
        shared) expected='recovery transaction descriptor owns an unexpected lock' ;;
        closed) expected='recovery transaction descriptor is closed' ;;
        wrong-identity) expected='recovery transaction descriptor identity mismatch' ;;
        wrong-tuple) expected='recovery-only rollback transaction tuple changed before lock handoff' ;;
    esac
    if [[ $scenario != valid && -e "$TEST_ROOT/rollback-gate-passed" ]]; then
        fail "$scenario passed the actual rollback entrypoint"
    fi
    grep -F -- "$expected" "$TEST_ROOT/$scenario.log" >/dev/null || {
        cat "$TEST_ROOT/$scenario.log" >&2
        fail "$scenario did not reach its expected rejection"
    }
    printf 'PASS: recovery runner -> rollback entrypoint %s\n' "$scenario"
done
