#!/usr/bin/env bash
# Exercise the actual early updater lock gate with Linux flock/fdinfo.
# Güncelleyicinin ilk kilit kontrolünü gerçek Linux flock/fdinfo ile sına.
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo 'SKIP: recovery lock test requires root'; exit 0; }
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
UPDATE=${1:-$ROOT/update.sh}
TEST_ROOT=$(mktemp -d /var/lib/celikpanel-recovery-lock-test.XXXXXXXX)
trap 'rm -rf -- "$TEST_ROOT"' EXIT
chmod 0700 "$TEST_ROOT"
extract_function() {
    sed -n "/^$1() {$/,/^}$/p" "$UPDATE"
}
eval "$(extract_function prepare_and_acquire_release_transaction_lock)"
export -f prepare_and_acquire_release_transaction_lock
export TEST_ROOT
run_case() (
    set -euo pipefail
    scenario=$1
    export RELEASE_TRANSACTION_ROOT=$TEST_ROOT RECOVER_EXISTING_TRANSACTION=1 RECOVERY_LOCK_FD=9
    die() { echo "$*" >&2; exit 41; }
    export -f die
    : >"$TEST_ROOT/transaction.lock"
    chmod 0600 "$TEST_ROOT/transaction.lock"
    exec 9<>"$TEST_ROOT/transaction.lock"
    export RECOVERY_LOCK_IDENTITY
    RECOVERY_LOCK_IDENTITY=$(stat -Lc '%d:%i' "$TEST_ROOT/transaction.lock")
    case "$scenario" in
        exclusive) flock -x 9 ;;
        shared) flock -s 9 ;;
        unlocked) ;;
        closed) exec 9>&- ;;
        wrong-identity) flock -x 9; RECOVERY_LOCK_IDENTITY=0:0 ;;
        foreign-owner)
            exec 8<>"$TEST_ROOT/transaction.lock"
            flock -x 8
            ;;
        *) exit 99 ;;
    esac
    # The child inherits the held OFD, as it does from the recovery runner.
    # Kurtarma çalıştırıcısında olduğu gibi alt süreç kilitli OFD'yi devralır.
    bash -c 'prepare_and_acquire_release_transaction_lock'
)
for scenario in exclusive shared unlocked closed wrong-identity foreign-owner; do
    status=0
    run_case "$scenario" >"$TEST_ROOT/$scenario.log" 2>&1 || status=$?
    expected=41
    [[ $scenario != exclusive ]] || expected=0
    if [[ $status != "$expected" ]]; then
        cat "$TEST_ROOT/$scenario.log" >&2
        echo "FAIL: $scenario status=$status expected=$expected" >&2
        exit 1
    fi
    printf 'PASS: recovery lock %s\n' "$scenario"
done
