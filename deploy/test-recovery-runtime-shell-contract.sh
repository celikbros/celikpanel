#!/usr/bin/env bash
# Local glue tests for independent CODE/DATA separation and the exact capture
# handoff. This is not a full snapshot, installed executor or native VM drill.
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo 'SKIP: recovery runtime shell contract requires root'; exit 0; }
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
TEST_ROOT=$(mktemp -d /var/lib/celikpanel-runtime-shell-test.XXXXXXXX)
trap 'rm -rf -- "$TEST_ROOT"' EXIT
chmod 0700 "$TEST_ROOT"
extract_function() { sed -n "/^$1() {$/,/^}$/p" "$ROOT/update.sh"; }
fail() { echo "FAIL: $*" >&2; exit 1; }
die() { echo "$*" >&2; exit 41; }
# Source pinning is real: target DATA and running CODE may differ, but a sibling
# source file is still refused. Full kit hashes are exercised by recoveryruntime.
TRUSTED_RELEASE_ROOT=$TEST_ROOT/data
CODE_ROOT=$ROOT
mkdir -m 0700 "$TRUSTED_RELEASE_ROOT"
source "$ROOT/deploy/release-transaction-guard.sh"
source "$ROOT/deploy/release-unit-transition.sh"
if (CODE_ROOT=$TRUSTED_RELEASE_ROOT; source "$ROOT/deploy/release-transaction-guard.sh") >/dev/null 2>&1; then
    fail 'guard accepted source outside pinned CODE root'
fi
if (CODE_ROOT=$TRUSTED_RELEASE_ROOT; source "$ROOT/deploy/release-unit-transition.sh") >/dev/null 2>&1; then
    fail 'unit helper accepted source outside pinned CODE root'
fi
(CODE_ROOT=; TRUSTED_RELEASE_ROOT=$ROOT; source "$ROOT/deploy/release-transaction-guard.sh")
printf 'PASS: separated CODE/DATA roots retain source pinning\n'

eval "$(extract_function run_update_idle_probe)"
eval "$(extract_function check_bind_update_compatibility)"
eval "$(extract_function run_panel_migrations_offline)"
eval "$(extract_function release_release_mutation_lock)"
eval "$(extract_function handoff_independent_capture_rollback)"
# Exact probe output makes accidental main/migrate/install execution observable.
mkdir -m 0700 "$TEST_ROOT/checkers"
cat > "$TEST_ROOT/checkers/agent" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*"
[[ $# == 1 && $1 == --check-bind-signed-update-compatible-under-external-lock ||
   $# == 1 && $1 == --check-pre-ledger-bind-signed-update-compatible-under-external-lock ]]
SH
cat > "$TEST_ROOT/checkers/panel" <<'SH'
#!/usr/bin/env bash
[[ $# == 1 && $1 == --check-service-operations-idle-wal-aware ]] || exit 91
printf 'independent-wal-proof\n'
SH
chmod 0755 "$TEST_ROOT/checkers/agent" "$TEST_ROOT/checkers/panel"
PREFLIGHT_AGENT=$TEST_ROOT/checkers/agent
AGENT_STATE_DIR=$TEST_ROOT MUTATION_LOCK=$TEST_ROOT/mutation.lock MUTATION_LOCK_FD=8
RECOVERY_RUNTIME_ROOT= BOOTSTRAP_PRE_LEDGER=0
[[ $(check_bind_update_compatibility) == --check-bind-signed-update-compatible-under-external-lock ]] || fail 'normal BIND probe changed'
BOOTSTRAP_PRE_LEDGER=1
[[ $(check_bind_update_compatibility) == --check-pre-ledger-bind-signed-update-compatible-under-external-lock ]] || fail 'pre-ledger BIND probe changed'
RECOVERY_RUNTIME_ROOT=$TEST_ROOT/kit
PREFLIGHT_AGENT=$TEST_ROOT/never-execute-candidate
[[ -z $(check_bind_update_compatibility) ]] || fail 'rollback capture ran install compatibility probe'
PREFLIGHT_PANEL=$TEST_ROOT/checkers/panel PANEL_DB=$TEST_ROOT/celikpanel.db BIN_DIR=$TEST_ROOT/never-execute-installed
[[ $(run_panel_migrations_offline) == independent-wal-proof ]] || fail 'kit completion did not use independent WAL proof'
PREFLIGHT_PANEL=/usr/bin/false
status=0
(run_panel_migrations_offline) >"$TEST_ROOT/completion-failed.log" 2>&1 || status=$?
[[ $status == 41 ]] || fail 'rejected completion proof did not stop recovery'
printf 'PASS: normal preflights retained; independent completion never runs candidate migration\n'

run_handoff_case() (
    set -euo pipefail
    local_case=$1
    case_root=$TEST_ROOT/$local_case
    mkdir -m 0700 "$case_root" "$case_root/transaction" "$case_root/kit" "$case_root/snapshots"
    RELEASE_TRANSACTION_ROOT=$case_root/transaction
    RELEASE_TRANSACTION_FD=9
    exec 9<>"$RELEASE_TRANSACTION_ROOT/transaction.lock"
    chmod 0600 "$RELEASE_TRANSACTION_ROOT/transaction.lock"
    flock -x 9
    release_transaction_token=$(release_txn_generate_token)
    snapshot_name=20260914T120000Z-from-unknown-to-$(printf 'a%.0s' {1..40})-$(printf 'b%.0s' {1..32})
    snap=$case_root/snapshots/$snapshot_name
    mkdir -m 0700 "$snap"
    release_txn_create_active_marker "$RELEASE_TRANSACTION_ROOT" 9 "$release_transaction_token" update "$snapshot_name"
    MUTATION_LOCK=$case_root/mutation.lock
    exec 8<>"$MUTATION_LOCK"
    chmod 0600 "$MUTATION_LOCK"
    flock -x 8
    MUTATION_LOCK_FD=8
    RECOVERY_LOCK_IDENTITY=$(stat -Lc '%d:%i' "$RELEASE_TRANSACTION_ROOT/transaction.lock")
    RECOVERY_RUNTIME_ROOT=$case_root/kit
    TRUSTED_RELEASE_ROOT=$TEST_ROOT/data
    RECOVER_EXISTING_TRANSACTION=1 resume_active_update=1
    # The entry identity gate has its separate protected fixed-root Go tests.
    # Replace only that gate in this private path fixture, after recording its
    # required argument. Marker producer/validator, flocks and exec are real.
    validate_recovery_code_root() {
        [[ $# == 1 && $1 == update.sh ]] || die 'wrong handoff entry gate'
        CODE_ROOT=$RECOVERY_RUNTIME_ROOT
    }
    cat > "$case_root/kit/rollback.sh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[[ $# == 1 && -d $1 ]] || exit 81
base=$(dirname -- "$(dirname -- "$1")")
[[ ${CELIKPANEL_RECOVERY_EXPECTED_OPERATION:-} == update &&
   ${CELIKPANEL_RECOVERY_EXPECTED_PHASE:-} == active &&
   ${CELIKPANEL_RECOVER_EXISTING_TRANSACTION:-} == 1 &&
   ${CELIKPANEL_RELEASE_TRANSACTION_FD:-} == 9 &&
   ${CELIKPANEL_RECOVERY_EXPECTED_SNAPSHOT:-} == "${1##*/}" &&
   ${CELIKPANEL_RECOVERY_RUNTIME_ROOT:-} == "$base/kit" &&
   ${SHOULD_NOT_LEAK:-} == '' ]] || exit 82
[[ $(stat -Lc '%d:%i' /proc/self/fd/9) == "$CELIKPANEL_RECOVERY_LOCK_IDENTITY" ]] || exit 83
# Reopening the release lock must conflict, while mutation lock is released.
status=0
flock -n -E 75 "$base/transaction/transaction.lock" true || status=$?
[[ $status == 75 ]] || exit 84
flock -n "$base/mutation.lock" true || exit 85
grep -Fx "token=$CELIKPANEL_RECOVERY_EXPECTED_TOKEN" "$base/transaction/active" >/dev/null || exit 86
printf 'exact-independent-rollback\n' > "$base/handoff.result"
SH
    chmod 0755 "$case_root/kit/rollback.sh"
    export SHOULD_NOT_LEAK=fixture-private-value
    case $local_case in
        exact) ;;
        not-recovery) RECOVER_EXISTING_TRANSACTION=0 ;;
        not-resuming) resume_active_update=0 ;;
        wrong-token) release_transaction_token=$(release_txn_generate_token) ;;
        wrong-snapshot) snapshot_name=another-safe-snapshot ;;
        wrong-operation) sed -i 's/^operation=update$/operation=rollback/' "$RELEASE_TRANSACTION_ROOT/active" ;;
        missing-marker) rm -- "$RELEASE_TRANSACTION_ROOT/active" ;;
        normal-update) RECOVERY_RUNTIME_ROOT= ;;
        *) exit 99 ;;
    esac
    handoff_independent_capture_rollback
    [[ $local_case == normal-update ]] || fail 'independent handoff returned to installer'
    printf 'normal-update-unmodified\n' > "$case_root/handoff.result"
)
for scenario in exact not-recovery not-resuming wrong-token wrong-snapshot wrong-operation missing-marker normal-update; do
    status=0
    run_handoff_case "$scenario" >"$TEST_ROOT/$scenario.log" 2>&1 || status=$?
    expected=41
    [[ $scenario != exact && $scenario != normal-update ]] || expected=0
    if [[ $status != "$expected" ]]; then cat "$TEST_ROOT/$scenario.log" >&2; fail "$scenario: $status, expected $expected"; fi
    if [[ $expected == 41 && -e $TEST_ROOT/$scenario/handoff.result ]]; then fail "$scenario reached rollback"; fi
    printf 'PASS: capture handoff %s\n' "$scenario"
done
grep -Fx exact-independent-rollback "$TEST_ROOT/exact/handoff.result" >/dev/null
grep -Fx normal-update-unmodified "$TEST_ROOT/normal-update/handoff.result" >/dev/null
printf 'recovery runtime shell contract: ok\n'

# Production fresh-update call uses a dynamic Bash FD, while the recovery ABI
# always uses FD9. Exercise actual inherited flock and inode identity in a child.
# Redirect only the fixed selected CLI path into this private fixture.
eval "$(extract_function prepare_independent_recovery_runtime | sed 's@/usr/libexec/celikpanel/recovery verify-material-support@"$TEST_ROOT/selected-recovery" verify-material-support@')"
cat > "$TEST_ROOT/selected-recovery" <<'SH'
#!/usr/bin/env bash
[[ $# == 3 && $1 == verify-material-support && $2 == --layout && $3 == snapshot-name-sha256-v1 ]] || exit 91
printf 'selected-material-support\n' >> "$FIXTURE_CALLS"
[[ ${FIXTURE_UNSUPPORTED:-0} == 0 ]]
SH
chmod 0755 "$TEST_ROOT/selected-recovery"
mkdir -p "$TEST_ROOT/fresh/recovery-runtime/bin"
cat > "$TEST_ROOT/fresh/recovery-runtime/bin/recovery" <<'PY'
#!/usr/bin/python3
import fcntl,os,sys
assert os.fstat(9).st_ino == os.stat(os.environ['FIXTURE_LOCK']).st_ino
assert any('FLOCK  ADVISORY  WRITE' in line for line in open('/proc/self/fdinfo/9'))
with open(os.environ['FIXTURE_LOCK'],'rb') as independent:
    try: fcntl.flock(independent,fcntl.LOCK_EX|fcntl.LOCK_NB)
    except BlockingIOError: pass
    else: raise AssertionError('native flock was released')
args=sys.argv[1:]
if args[0]=='prepare-runtime':
    assert args==['prepare-runtime','--source',os.environ['FIXTURE_KIT'],'--mode',os.environ['FIXTURE_MODE'],'--transaction-fd','9']
elif args[0]=='verify-compatibility':
    assert args==['verify-compatibility','--mode',os.environ['FIXTURE_MODE']]
else: raise AssertionError(args)
with open(os.environ['FIXTURE_CALLS'],'a') as f: f.write(args[0]+'\n')
if args[0]=='prepare-runtime' and os.environ.get('FIXTURE_REJECT_PREPARATION')=='1': sys.exit(78)
PY
chmod 0755 "$TEST_ROOT/fresh/recovery-runtime/bin/recovery"
(
    TRUSTED_RELEASE_ROOT=$TEST_ROOT/fresh
    export FIXTURE_LOCK=$TEST_ROOT/fresh.lock FIXTURE_KIT=$TRUSTED_RELEASE_ROOT/recovery-runtime FIXTURE_CALLS=$TEST_ROOT/fresh.calls
    : > "$FIXTURE_LOCK"
    exec {RELEASE_TRANSACTION_FD}<>"$FIXTURE_LOCK"
    [[ $RELEASE_TRANSACTION_FD != 9 ]] || fail 'fixture did not allocate a dynamic FD'
    flock -x "$RELEASE_TRANSACTION_FD"
    for FIXTURE_MODE in --normal --bootstrap-pre-ledger --bootstrap-schema17; do
        export FIXTURE_MODE
        BOOTSTRAP_PRE_LEDGER=0 BOOTSTRAP_SCHEMA17=0
        [[ $FIXTURE_MODE == --normal ]] || BOOTSTRAP_PRE_LEDGER=1
        [[ $FIXTURE_MODE != --bootstrap-schema17 ]] || BOOTSTRAP_SCHEMA17=1
        prepare_independent_recovery_runtime
    done
    [[ $(wc -l < "$FIXTURE_CALLS") -eq 9 ]] || fail 'preflight did not execute the expected preparation, compatibility and capability calls'
    # A rejected/incomplete promotion must not reach compatibility or the material
    # writer capability call. The actual selector journal has native Go tests.
    before_calls=$(wc -l < "$FIXTURE_CALLS")
    export FIXTURE_REJECT_PREPARATION=1
    status=0
    (prepare_independent_recovery_runtime) >"$TEST_ROOT/rejected-promotion.log" 2>&1 || status=$?
    [[ $status == 41 && $(wc -l < "$FIXTURE_CALLS") -eq $((before_calls + 1)) ]] || fail 'rejected preparation continued'
    [[ $(tail -n 1 "$FIXTURE_CALLS") == prepare-runtime ]] || fail 'unexpected call after rejected preparation'
    unset FIXTURE_REJECT_PREPARATION
    export FIXTURE_UNSUPPORTED=1
    status=0
    (prepare_independent_recovery_runtime) >"$TEST_ROOT/unsupported-kit.log" 2>&1 || status=$?
    [[ $status == 41 ]] || fail 'older selected kit was silently admitted'
    grep -F 'panel services have not been stopped' "$TEST_ROOT/unsupported-kit.log" >/dev/null
)
printf 'PASS: fresh updater preparation and compatibility retain FD9; rejected promotion stops admission\n'


# Real selector branches, with a private CLI boundary instead of the installed
# executable. The selected-kit and material parsers have separate root tests.
eval "$(sed -n '/^select_recovery_data() {$/,/^}$/p' "$ROOT/deploy/release-recovery-runner.sh")"
mkdir -m 0700 "$TEST_ROOT/selected-kit" "$TEST_ROOT/selected-kit/bin"
cat > "$TEST_ROOT/selected-kit/bin/recovery" <<'SH'
#!/usr/bin/env bash
[[ $# == 3 && $1 == material-root && $2 == --snapshot && $3 == "$FIXTURE_SNAPSHOT" ]] || exit 91
[[ $(stat -Lc '%d:%i' /proc/self/fd/9) == "$FIXTURE_LOCK_IDENTITY" ]] || exit 92
printf '%s' "$FIXTURE_MATERIAL_OUTPUT"
exit "$FIXTURE_MATERIAL_STATUS"
SH
chmod 0755 "$TEST_ROOT/selected-kit/bin/recovery"
selection_case() (
 set -euo pipefail
 local kind=$1
 export FIXTURE_SNAPSHOT=20260914T120000Z-from-unknown-to-$(printf 'a%.0s' {1..40})-$(printf 'b%.0s' {1..32})
 MARKER_SNAPSHOT=$FIXTURE_SNAPSHOT TARGET_COMMIT=$(printf 'a%.0s' {1..40})
 TRANSACTION_FD=9
 exec 9<>"$TEST_ROOT/selection.lock"
 flock -x 9
 export FIXTURE_LOCK_IDENTITY=$(stat -Lc '%d:%i' /proc/self/fd/9)
 export FIXTURE_MATERIAL_STATUS=0 FIXTURE_MATERIAL_OUTPUT=/var/lib/celikpanel-release-state/recovery-material/v1/$(printf 'c%.0s' {1..64})/data
 RECOVERY_CODE_ROOT=$TEST_ROOT/selected-kit ACTION=rollback
 find_exact_release() {
  [[ $1 == "$TARGET_COMMIT" && ( $kind == absent || $kind == legacy || $kind == update ) ]] || die 'unexpected retained-candidate lookup'
  RECOVERY_RELEASE=$TEST_ROOT/legacy-data
 }
 case $kind in
  present) ;;
  absent) FIXTURE_MATERIAL_STATUS=3 ;;
  invalid) FIXTURE_MATERIAL_STATUS=1 ;;
  unsupported) FIXTURE_MATERIAL_STATUS=2 ;;
  foreign-path) FIXTURE_MATERIAL_OUTPUT=/tmp/foreign/data ;;
  extra-output) FIXTURE_MATERIAL_OUTPUT+=$'\n/untrusted' ;;
  legacy) RECOVERY_CODE_ROOT= ;;
  update) ACTION=update ;;
 esac
 select_recovery_data
 case $kind in
  present) [[ $RECOVERY_RELEASE == "$FIXTURE_MATERIAL_OUTPUT" && $RECOVERY_MATERIAL_ROOT == "$RECOVERY_RELEASE" ]] ;;
  absent|legacy|update) [[ $RECOVERY_RELEASE == "$TEST_ROOT/legacy-data" && -z $RECOVERY_MATERIAL_ROOT ]] ;;
  *) fail "$kind unexpectedly continued" ;;
 esac
)
for kind in present absent invalid unsupported foreign-path extra-output legacy update; do
 status=0
 selection_case "$kind" >"$TEST_ROOT/selection-$kind.log" 2>&1 || status=$?
 expected=0
 case $kind in invalid|unsupported|foreign-path|extra-output) expected=41;; esac
 [[ $status == "$expected" ]] || { cat "$TEST_ROOT/selection-$kind.log" >&2; fail "selection $kind: $status"; }
 printf 'PASS: independent rollback data selection %s\n' "$kind"
done
python3 - "$ROOT" <<'PY'
from pathlib import Path
import sys
root=Path(sys.argv[1]);s=(root/'update.sh').read_text()
material=s.index('/usr/libexec/celikpanel/recovery prepare-recovery-material')
seal=s.rfind('panel_tls_snapshot_scheduler_matches_service_ledger',0,material)
assert seal>=0 and seal<material<s.index('if [[ $BOOTSTRAP_PRE_LEDGER -eq 1 ]]; then',material)
assert s.index('prepare_independent_recovery_runtime\n')<s.index('transaction_phase=quiesce-publishing')
r=(root/'rollback.sh').read_text()
assert '--candidate-root "$rollback_candidate_root"' in r
assert 'sudo /usr/libexec/celikpanel/recovery recover' in r
PY

# The public failure envelope must not call a possibly changed recovery launcher
# unchanged merely because application quiescence has not begun.
eval "$(extract_function report_update_failure)"
(
    mutation_started=0 transaction_started=0 quiesce_abort_failed=0
    transaction_completion_verified=0 scheduler_restore_verified=0
    recovery_runtime_preparation_attempted=1 recovery_runtime_preparation_verified=0
    update_failure_reason='runtime preparation is unconfirmed' update_failure_detail=
    report_update_failure 1 none 2>"$TEST_ROOT/promotion-failure-summary"
    grep -F 'code=recovery_runtime_preparation_unconfirmed state=recovery_required' "$TEST_ROOT/promotion-failure-summary" >/dev/null
    recovery_runtime_preparation_verified=1
    report_update_failure 1 none 2>"$TEST_ROOT/verified-preparation-summary"
    grep -F 'code=update_failed state=unchanged' "$TEST_ROOT/verified-preparation-summary" >/dev/null
)
printf 'PASS: unconfirmed recovery preparation is never reported as unchanged\n'
