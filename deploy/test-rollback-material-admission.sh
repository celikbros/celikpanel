#!/usr/bin/env bash
# Native root filesystem/FD admission tests. Reader results are a narrow seam;
# material parsing and token authority are covered by the shared Go contract.
# No installed services, snapshots or customer state are accessed.
set -euo pipefail
[[ $EUID == 0 ]] || { echo 'SKIP: rollback material admission requires root'; exit 0; }
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
TEST_ROOT=$(mktemp -d /run/celikpanel-material-admission-test.XXXXXXXX)
trap 'rm -rf -- "$TEST_ROOT"' EXIT
chmod 0700 "$TEST_ROOT"
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
die() { printf '%s\n' "$*" >&2; exit 41; }
CODE_ROOT=$ROOT
source "$ROOT/deploy/release-transaction-guard.sh"
for name in validate_root_trusted_dir_chain verify_independent_recovery_material rollback_on_exit; do
    eval "$(sed -n "/^${name}() {$/,/^}$/p" "$ROOT/rollback.sh")"
done
path_source=$(sed -n '/^rollback_material_path_state() {$/,/^}$/p' "$ROOT/rollback.sh")
admission_source=$(sed -n '/^preflight_rollback_material_admission() {$/,/^}$/p' "$ROOT/rollback.sh")
[[ -n $path_source && -n $admission_source ]] || fail 'production admission helpers missing'

# Prove the actual entry invokes the tested gate before marker creation or
# takeover, stale authorization cleanup, unit changes and the first stop.
python3 - "$ROOT/rollback.sh" <<'PY'
import pathlib, sys
s = pathlib.Path(sys.argv[1]).read_text()
gate = s.index('\npreflight_rollback_material_admission\n')
for needle in ('\nunit_root_restore_identity=', '\nrelease_txn_clear_stale_start_authorization ',
               '\n    rollback_transaction_token=$(release_txn_takeover_active_for_rollback ',
               '\n    rollback_transaction_token=$(release_txn_generate_token)',
               '\nsystemctl stop celikpanel-panel.service ||'):
    if gate >= s.index(needle):
        raise SystemExit('material admission follows mutation: ' + needle)
PY

snapshot_name=20260914T120000Z-from-unknown-to-$(printf 'a%.0s' {1..40})-$(printf 'b%.0s' {1..32})
key=$(printf '%s' "$snapshot_name" | sha256sum); key=${key%% *}
case_number=0
make_case() {
    case_number=$((case_number + 1))
    case_root=$TEST_ROOT/$case_number-$1
    mkdir -m 0700 "$case_root"
    mkdir -p "$case_root/var/lib" "$case_root/usr/libexec/celikpanel" "$case_root/runtime/bin" "$case_root/transaction" "$case_root/owner"
    chmod -R 0700 "$case_root"
    printf '%s\n' 'unchanged TLS/DB/owner fixture sentinel' > "$case_root/owner/state"
    state_root=$case_root/var/lib/celikpanel-release-state
    material_base=$state_root/recovery-material/v1
    material_path=$material_base/$key
    reader=$case_root/usr/libexec/celikpanel/recovery
    cat > "$reader" <<'READER'
#!/usr/bin/env bash
set -euo pipefail
[[ $# == 3 && $1 == material-root && $2 == --snapshot && $3 == "$FIXTURE_SNAPSHOT" ]] || exit 90
[[ $(stat -Lc '%d:%i' "/proc/$BASHPID/fd/9") == $(stat -Lc '%d:%i' "$FIXTURE_LOCK") ]] || exit 91
exec 8<>"$FIXTURE_LOCK"
if flock -n 8; then exit 92; fi
exec 8>&-
printf '%s\n' 'exact material-root reader called' >> "$FIXTURE_READER_TRACE"
case $FIXTURE_READER_MODE in
    absent) exit 3 ;;
    corrupt) exit 1 ;;
    incompatible) exit 2 ;;
    dirty-absence) printf '%s\n' 'unexpected output'; exit 3 ;;
    material) printf '%s\n' "$FIXTURE_MATERIAL_ROOT" ;;
    *) exit 93 ;;
esac
READER
    chmod 0755 "$reader"
    cp -p "$reader" "$case_root/runtime/bin/recovery"
    eval "${path_source//parent=\/var\/lib/parent=$case_root\/var\/lib}"
    eval "${admission_source//\/usr\/libexec\/celikpanel\/recovery/$reader}"
    RELEASE_TRANSACTION_ROOT=$case_root/transaction
    exec 9<>"$RELEASE_TRANSACTION_ROOT/transaction.lock"
    chmod 0600 "$RELEASE_TRANSACTION_ROOT/transaction.lock"
    flock -x 9
    RELEASE_TRANSACTION_FD=9
    RECOVERY_MATERIAL_ROOT= RECOVERY_RUNTIME_ROOT= TRUSTED_RELEASE_ROOT= CODE_ROOT=$case_root/runtime
    rollback_active_present=0 rollback_pending_resume=0 rollback_scheduler_only_resume=0
    legacy_agent_frozen=0 rollback_completion_verified=0 rollback_completion_removing=0
    rollback_scheduler_restore_pending=0 rollback_scheduler_restore_completed=0
    rollback_transaction_started=0 rollback_mutation_started=0 rollback_service_state_recorded=0
    rollback_transaction_token= rollback_verified_snapshot=
    FIXTURE_READER_TRACE=$TEST_ROOT/$case_number.reader.trace
    FIXTURE_READER_MODE=absent FIXTURE_SNAPSHOT=$snapshot_name
    FIXTURE_LOCK=$RELEASE_TRANSACTION_ROOT/transaction.lock FIXTURE_MATERIAL_ROOT=$material_path/data
    export FIXTURE_READER_TRACE FIXTURE_READER_MODE FIXTURE_SNAPSHOT FIXTURE_LOCK FIXTURE_MATERIAL_ROOT
}
make_material() { mkdir -p -m 0700 "$material_path/data"; chmod -R 0700 "$state_root"; }
make_active() {
    rollback_active_present=1
    rollback_transaction_token=$(release_txn_generate_token)
    release_txn_create_active_marker "$RELEASE_TRANSACTION_ROOT" 9 "$rollback_transaction_token" update "$snapshot_name"
}
fingerprint() {
    {
        find "$case_root" -printf '%P %y %m %U:%G %n %s %T@ %C@ %l\n' | LC_ALL=C sort
        find "$case_root" -type f -print0 | LC_ALL=C sort -z | xargs -0 -r sha256sum
    } | sha256sum
}
systemctl() { printf 'unexpected systemctl %s\n' "$*" >> "$TEST_ROOT/mutations.trace"; return 99; }
expect_refused() {
    local before status=0
    before=$(fingerprint)
    (trap rollback_on_exit EXIT; preflight_rollback_material_admission; : > "$case_root/unexpected-mutation") \
        > "$TEST_ROOT/$case_number.stdout" 2> "$TEST_ROOT/$case_number.stderr" || status=$?
    [[ $status == 41 ]] || fail "case $case_number reached mutation or failed unexpectedly: $status"
    [[ $(fingerprint) == "$before" ]] || fail "case $case_number changed state/evidence on refusal"
    [[ ! -e $TEST_ROOT/mutations.trace ]] || fail 'rejection invoked a service action'
}
expect_allowed() {
    local before
    before=$(fingerprint)
    (trap rollback_on_exit EXIT; preflight_rollback_material_admission) \
        > "$TEST_ROOT/$case_number.stdout" 2> "$TEST_ROOT/$case_number.stderr" \
        || { cat "$TEST_ROOT/$case_number.stderr" >&2; fail "case $case_number rejected supported admission"; }
    [[ $(fingerprint) == "$before" ]] || fail "case $case_number changed state/evidence during admission"
    [[ ! -e $TEST_ROOT/mutations.trace ]] || fail 'admission invoked a service action'
}

for missing in state base version snapshot; do
    make_case "legacy-absent-$missing"
    case $missing in
        state) ;;
        base) mkdir -m 0700 "$state_root" ;;
        version) mkdir -p -m 0700 "$state_root/recovery-material" ;;
        snapshot) mkdir -p -m 0700 "$material_base"; mkdir -m 0700 "$material_base/unrelated-malformed-record"; printf bad > "$material_base/unrelated-malformed-record/bad" ;;
    esac
    expect_allowed
    [[ ! -e $FIXTURE_READER_TRACE ]] || fail 'fresh legacy absence required a transaction reader'
done
for position in state base version snapshot; do
    for fault in symlink dangling file fifo writable owner; do
        make_case "$position-$fault"
        case $position in
            state) selected=$state_root ;;
            base) selected=$state_root/recovery-material ;;
            version) selected=$material_base ;;
            snapshot) selected=$material_path ;;
        esac
        mkdir -p -m 0700 "${selected%/*}"
        case $fault in
            symlink) ln -s "$case_root/owner" "$selected" ;;
            dangling) ln -s "$case_root/absent" "$selected" ;;
            file) printf bad > "$selected" ;;
            fifo) mkfifo -m 0600 "$selected" ;;
            writable) mkdir -m 0777 "$selected" ;;
            owner) mkdir -m 0700 "$selected"; chown 1:0 "$selected" ;;
        esac
        expect_refused
    done
done
for mode in fresh active pending scheduler; do
    make_case "material-direct-$mode"
    make_material
    case $mode in
        fresh) ;;
        active) make_active ;;
        pending) make_active; release_txn_mark_completion_pending "$RELEASE_TRANSACTION_ROOT" 9 "$rollback_transaction_token" update "$snapshot_name"; rollback_active_present=0; rollback_pending_resume=1 ;;
        scheduler) rollback_scheduler_only_resume=1 ;;
    esac
    expect_refused
    [[ ! -e $FIXTURE_READER_TRACE ]] || fail 'direct material presence executed a reader before refusal'
done
for result in absent corrupt incompatible dirty-absence material; do
    make_case "existing-missing-$result"
    make_active
    FIXTURE_READER_MODE=$result
    if [[ $result == absent ]]; then expect_allowed; else expect_refused; fi
    [[ -s $FIXTURE_READER_TRACE ]] || fail 'existing transaction did not use the shared reader'
done
for fault in symlink hardlink writable missing; do
    make_case "unsafe-reader-$fault"
    make_active
    case $fault in
        symlink) mv "$reader" "$reader.original"; ln -s "$reader.original" "$reader" ;;
        hardlink) ln "$reader" "$reader.second" ;;
        writable) chmod 0777 "$reader" ;;
        missing) rm "$reader" ;;
    esac
    expect_refused
    [[ ! -e $FIXTURE_READER_TRACE ]] || fail 'unsafe reader was executed'
done
for result in material corrupt absent; do
    make_case "independent-$result"
    make_active
    make_material
    RECOVERY_MATERIAL_ROOT=$material_path/data TRUSTED_RELEASE_ROOT=$material_path/data
    FIXTURE_READER_MODE=$result
    if [[ $result == material ]]; then expect_allowed; else expect_refused; fi
    [[ -s $FIXTURE_READER_TRACE ]] || fail 'independent same-token material was not reverified'
done
make_case failed-parent-inspection
find() { return 74; }
status=0
(rollback_material_path_state) > "$TEST_ROOT/find.stdout" 2> "$TEST_ROOT/find.stderr" || status=$?
unset -f find
[[ $status == 41 ]] || fail 'directory inspection failure became absence'
printf 'rollback material admission: PASS (%s native filesystem/FD cases)\n' "$case_number"
