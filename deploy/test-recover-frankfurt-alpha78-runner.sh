#!/usr/bin/env bash
set -Eeuo pipefail

# Exercise the actual watchdog and start guard around a user recovery abort.
# Kullanici kurtarma iptalinde gercek watchdog ve baslatma korumasini sinar.
# Fixtures are isolated; no installed service, timer or release is changed.
# Fixture yalitilmistir; kurulu servis, zamanlayici veya surum degistirilmez.
fail() {
    printf 'Frankfurt recovery runner test: %s\n' "$*" >&2
    exit 1
}

[[ $EUID -eq 0 ]] || fail 'run this isolated test as root'
REPO_ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
TEST_ROOT=$(mktemp -d /var/lib/celikpanel-alpha78-runner-test.XXXXXXXX)
cleanup() {
    case "$TEST_ROOT" in
        /var/lib/celikpanel-alpha78-runner-test.*)
            [[ $(readlink -e -- "$TEST_ROOT") == "$TEST_ROOT" ]] || return 1
            rm -rf -- "$TEST_ROOT"
            ;;
        *) return 1 ;;
    esac
}
trap cleanup EXIT
umask 077

TRANSACTION_ROOT=$TEST_ROOT/var/lib/celikpanel-release-transaction
SNAPSHOT_ROOT=$TEST_ROOT/var/backups/celikpanel/update-snapshots
RUNTIME_ROOT=$TEST_ROOT/run/celikpanel-release-transaction
NAME=20260913T221708Z-from-unknown-to-ae881fc2d55b7bb6b6bd2674a1cf893d38e71539-7c4ac30c46ae27c9087fb3612c715284
STAGE=$SNAPSHOT_ROOT/.release-snapshot.incomplete.1225232.7c4ac30c46ae27c9087fb3612c715284
CHILD=$STAGE/$NAME
TOKEN=1111111111111111111111111111111111111111111111111111111111111111
install -d -m 0700 "$TRANSACTION_ROOT" "$CHILD"
chmod 0700 "$SNAPSHOT_ROOT" "$STAGE"
: >"$TRANSACTION_ROOT/transaction.lock"
printf 'version=1\ntoken=%s\noperation=update\nsnapshot=%s\n' \
    "$TOKEN" "$NAME" >"$TRANSACTION_ROOT/active"
printf 'celikpanel-agent.service\tenabled\tactive\ncelikpanel-panel.service\tenabled\tactive\ncelikpanel-firewall-restore.service\tenabled\tinactive\n' \
    >"$CHILD/service-states.tsv"
# Synthetic saved identities are data only; this test never starts services.
# Kayitli kimlikler sentetiktir; bu test servis baslatmaz.
printf 'celikpanel-agent.service\tactive\t12345\t123456\ncelikpanel-panel.service\tactive\t12346\t123457\n' \
    >"$CHILD/quiesce-coordinators.tsv"
printf 'normal\n' >"$CHILD/snapshot-transition.state"
chmod 0600 "$TRANSACTION_ROOT/transaction.lock" "$TRANSACTION_ROOT/active" "$CHILD"/*
cp -- "$TRANSACTION_ROOT/active" "$TEST_ROOT/active.before"

run_watchdog() {
    (
        # A systemd watchdog does not inherit the owner's descriptor.
        # Systemd watchdog, kullanicinin descriptor'unu miras almaz.
        exec 8>&-
        env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C \
            CELIKPANEL_RELEASE_RECOVERY_TESTING=1 \
            CELIKPANEL_RELEASE_RECOVERY_TEST_ROOT="$TEST_ROOT" \
            /bin/bash "$REPO_ROOT/deploy/release-recovery-runner.sh"
    )
}

# No retained release or foundation exists in this fixture. Reaching recovery
# dispatch must therefore fail; successful calls below prove early no-op paths.
# Fixture'da sakli surum/foundation yoktur; dagitim yoluna ulasmak hata vermelidir.
if run_watchdog >"$TEST_ROOT/unlocked.stdout" 2>"$TEST_ROOT/unlocked.stderr"; then
    fail 'an unlocked active transaction unexpectedly skipped release validation'
fi
grep -F 'retained release storage is missing or unsafe' "$TEST_ROOT/unlocked.stderr" >/dev/null \
    || fail 'negative control did not reach retained-release validation'
cmp -s "$TRANSACTION_ROOT/active" "$TEST_ROOT/active.before" \
    || fail 'negative control changed the active marker'
printf 'PASS: unlocked active transaction reaches real release validation\n'

exec 8<>"$TRANSACTION_ROOT/transaction.lock"
flock -n -x 8 || fail 'cannot acquire fixture transaction lock'
run_watchdog >"$TEST_ROOT/busy.stdout" 2>"$TEST_ROOT/busy.stderr" \
    || fail 'watchdog did not yield to the owner recovery lock'
[[ ! -s $TEST_ROOT/busy.stdout && ! -s $TEST_ROOT/busy.stderr ]] \
    || fail 'busy watchdog unexpectedly dispatched or reported a failure'
cmp -s "$TRANSACTION_ROOT/active" "$TEST_ROOT/active.before" \
    || fail 'busy watchdog changed the active marker'
printf 'PASS: real transaction flock prevents watchdog dispatch\n'

if /bin/bash "$REPO_ROOT/deploy/release-transaction-start-guard.sh" \
    "$TRANSACTION_ROOT" "$RUNTIME_ROOT" >"$TEST_ROOT/start-blocked.stdout" \
    2>"$TEST_ROOT/start-blocked.stderr"; then
    fail 'start guard accepted active transaction without authorization'
fi
printf 'PASS: start guard blocks ordinary starts while active remains\n'

# Invoke the standard exact marker primitive, not a test implementation.
# Test taklidi yerine standart tam marker iptal korumasi kullanilir.
TRUSTED_RELEASE_ROOT=$TEST_ROOT/guard-source
install -D -m 0644 "$REPO_ROOT/deploy/release-transaction-guard.sh" \
    "$TRUSTED_RELEASE_ROOT/deploy/release-transaction-guard.sh"
cmp -s "$REPO_ROOT/deploy/release-transaction-guard.sh" \
    "$TRUSTED_RELEASE_ROOT/deploy/release-transaction-guard.sh" \
    || fail 'fixture transaction guard differs from production source'
source "$TRUSTED_RELEASE_ROOT/deploy/release-transaction-guard.sh"
release_txn_remove_pre_mutation_active_marker \
    "$TRANSACTION_ROOT" 8 "$TOKEN" update "$NAME" "$SNAPSHOT_ROOT" "$STAGE" \
    || fail 'standard pre-mutation marker abort failed'
[[ ! -e $TRANSACTION_ROOT/active ]] || fail 'standard abort left active behind'
[[ -d $STAGE && -f $CHILD/service-states.tsv ]] || fail 'standard abort removed snapshot evidence'
run_watchdog >"$TEST_ROOT/aborted-held.stdout" 2>"$TEST_ROOT/aborted-held.stderr" \
    || fail 'watchdog interfered between marker removal and lock release'
flock -u 8
exec 8>&-

run_watchdog >"$TEST_ROOT/none.stdout" 2>"$TEST_ROOT/none.stderr" \
    || fail 'marker-free watchdog attempted retained-release or foundation dispatch'
[[ ! -s $TEST_ROOT/none.stdout && ! -s $TEST_ROOT/none.stderr ]] \
    || fail 'marker-free watchdog unexpectedly dispatched or reported a failure'
[[ $(find "$TRANSACTION_ROOT" -mindepth 1 -maxdepth 1 -printf '%f\n') == transaction.lock ]] \
    || fail 'marker-free watchdog created transaction state'
printf 'PASS: after exact abort and unlock, watchdog exits without dispatch\n'

[[ ! -e $RUNTIME_ROOT ]] || fail 'test unexpectedly created start authorization'
/bin/bash "$REPO_ROOT/deploy/release-transaction-start-guard.sh" \
    "$TRANSACTION_ROOT" "$RUNTIME_ROOT" >"$TEST_ROOT/start-allowed.stdout" \
    2>"$TEST_ROOT/start-allowed.stderr" \
    || fail 'marker-free start guard required runtime authorization'
printf 'PASS: marker-free start guard permits ordinary service starts\n'
