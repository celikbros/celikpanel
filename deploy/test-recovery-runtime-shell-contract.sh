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
[[ $# == 1 && $1 == --check-completed-update-database-wal-aware ]] || exit 91
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
eval "$(extract_function prepare_independent_recovery_runtime | sed 's@/usr/libexec/celikpanel/recovery@"$TEST_ROOT/selected-recovery"@g')"
cat > "$TEST_ROOT/selected-recovery" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
case ${1:-} in
 verify-material-support)
  [[ $# == 5 && $2 == --layout && $3 == snapshot-name-sha256-v1 &&
     $4 == --schema && $5 == celikpanel/recovery-material/v3 ]] || exit 91
  printf 'selected-material-support\n' >> "$FIXTURE_CALLS"
  [[ ${FIXTURE_UNSUPPORTED:-0} == 0 ]]
  ;;
 verify-database-support)
  [[ $# == 3 && $2 == --schema && $3 == celikpanel/database-migration-admission/v1 ]] || exit 92
  printf 'selected-database-support\n' >> "$FIXTURE_CALLS"
  [[ ${FIXTURE_DB_UNSUPPORTED:-0} == 0 ]]
  ;;
 probe-update-database)
  [[ $# == 1 && $FIXTURE_MODE == --normal ]] || exit 93
  [[ $(stat -Lc '%d:%i' /proc/self/fd/9) == $(stat -Lc '%d:%i' "$FIXTURE_LOCK") ]] || exit 94
  status=0; flock -n -E 75 "$FIXTURE_LOCK" true || status=$?
  [[ $status == 75 ]] || exit 95
  printf 'probe-update-database\n' >> "$FIXTURE_CALLS"
  [[ ${FIXTURE_DB_PROBE_REJECTED:-0} == 0 ]]
  ;;
 *) exit 96 ;;
esac
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
    cmp -s "$FIXTURE_CALLS" <(printf '%s\n' \
        prepare-runtime verify-compatibility selected-material-support selected-database-support probe-update-database \
        prepare-runtime verify-compatibility selected-material-support selected-database-support \
        prepare-runtime verify-compatibility selected-material-support selected-database-support) \
        || fail 'preflight order or normal-only database probe changed'
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
    [[ $(tail -n 1 "$FIXTURE_CALLS") == selected-material-support ]] || fail 'unsupported material continued to database admission'
    unset FIXTURE_UNSUPPORTED
    export FIXTURE_MODE=--normal FIXTURE_DB_UNSUPPORTED=1
    BOOTSTRAP_PRE_LEDGER=0 BOOTSTRAP_SCHEMA17=0
    status=0
    (prepare_independent_recovery_runtime) >"$TEST_ROOT/unsupported-db-kit.log" 2>&1 || status=$?
    [[ $status == 41 && $(tail -n 1 "$FIXTURE_CALLS") == selected-database-support ]] || fail 'unsupported database capability continued to metadata probe'
    grep -F 'panel services have not been stopped' "$TEST_ROOT/unsupported-db-kit.log" >/dev/null
    unset FIXTURE_DB_UNSUPPORTED
    export FIXTURE_DB_PROBE_REJECTED=1
    status=0
    (prepare_independent_recovery_runtime) >"$TEST_ROOT/rejected-db-probe.log" 2>&1 || status=$?
    [[ $status == 41 && $(tail -n 1 "$FIXTURE_CALLS") == probe-update-database ]] || fail 'rejected database metadata probe did not stop preflight'
)
printf 'PASS: fresh updater preparation and compatibility retain FD9; rejected promotion stops admission\n'


# Real selector branches, with a private CLI boundary instead of the installed
# executable. The selected-kit and material parsers have separate root tests.
eval "$(sed -n '/^select_recovery_data() {$/,/^}$/p' "$ROOT/deploy/release-recovery-runner.sh")"
mkdir -m 0700 "$TEST_ROOT/selected-kit" "$TEST_ROOT/selected-kit/bin"
cat > "$TEST_ROOT/selected-kit/bin/recovery" <<'SH'
#!/usr/bin/env bash
[[ $# == 3 && $1 == "$FIXTURE_MATERIAL_COMMAND" && $2 == --snapshot && $3 == "$FIXTURE_SNAPSHOT" ]] || exit 91
[[ $(stat -Lc '%d:%i' /proc/self/fd/9) == "$FIXTURE_LOCK_IDENTITY" ]] || exit 92
printf '%s' "$FIXTURE_MATERIAL_OUTPUT"
exit "$FIXTURE_MATERIAL_STATUS"
SH
chmod 0755 "$TEST_ROOT/selected-kit/bin/recovery"
selection_case() (
 set -euo pipefail
 local kind=$1 phase=$2
 export FIXTURE_SNAPSHOT=20260914T120000Z-from-unknown-to-$(printf 'a%.0s' {1..40})-$(printf 'b%.0s' {1..32})
 MARKER_SNAPSHOT=$FIXTURE_SNAPSHOT TARGET_COMMIT=$(printf 'a%.0s' {1..40})
 TRANSACTION_FD=9 TRANSACTION_PHASE=$phase
 exec 9<>"$TEST_ROOT/selection.lock"
 flock -x 9
 export FIXTURE_LOCK_IDENTITY=$(stat -Lc '%d:%i' /proc/self/fd/9)
 export FIXTURE_MATERIAL_STATUS=0 FIXTURE_MATERIAL_OUTPUT=/var/lib/celikpanel-release-state/recovery-material/v1/$(printf 'c%.0s' {1..64})/data
 export FIXTURE_MATERIAL_COMMAND=material-root
 RECOVERY_CODE_ROOT=$TEST_ROOT/selected-kit ACTION=rollback
 case $phase in
  completion|completion-scheduler|scheduler) ACTION=update; FIXTURE_MATERIAL_COMMAND=completion-material-root ;;
 esac
 find_exact_release() {
  [[ $1 == "$TARGET_COMMIT" && ( $kind == absent || $kind == legacy || $kind == update ||
      ( $kind == verified-v1 && $ACTION == update ) ) ]] || die 'unexpected retained-candidate lookup'
  RECOVERY_RELEASE=$TEST_ROOT/legacy-data
 }
 case $kind in
  present) ;;
  absent) FIXTURE_MATERIAL_STATUS=3; FIXTURE_MATERIAL_OUTPUT= ;;
  invalid) FIXTURE_MATERIAL_STATUS=1 ;;
  unsupported) FIXTURE_MATERIAL_STATUS=2 ;;
  verified-v1) FIXTURE_MATERIAL_STATUS=6; FIXTURE_MATERIAL_OUTPUT= ;;
  absent-with-output) FIXTURE_MATERIAL_STATUS=3 ;;
  v1-with-output) FIXTURE_MATERIAL_STATUS=6 ;;
  foreign-path) FIXTURE_MATERIAL_OUTPUT=/tmp/foreign/data ;;
  extra-output) FIXTURE_MATERIAL_OUTPUT+=$'\n/untrusted' ;;
  legacy) RECOVERY_CODE_ROOT= ;;
  update) ACTION=update; TRANSACTION_PHASE=active ;;
  *) exit 99 ;;
 esac
 select_recovery_data
 case $kind in
  present) [[ $RECOVERY_RELEASE == "$FIXTURE_MATERIAL_OUTPUT" && $RECOVERY_MATERIAL_ROOT == "$RECOVERY_RELEASE" ]] ;;
  absent|legacy|update|verified-v1) [[ $RECOVERY_RELEASE == "$TEST_ROOT/legacy-data" && -z $RECOVERY_MATERIAL_ROOT ]] ;;
  *) fail "$kind unexpectedly continued" ;;
 esac
)
for phase in active completion completion-scheduler scheduler; do
 for kind in present absent invalid unsupported verified-v1 absent-with-output v1-with-output foreign-path extra-output legacy update; do
  status=0
  selection_case "$kind" "$phase" >"$TEST_ROOT/selection-$phase-$kind.log" 2>&1 || status=$?
  expected=0
  case $kind in invalid|unsupported|absent-with-output|v1-with-output|foreign-path|extra-output) expected=41;; esac
  [[ $phase != active || $kind != verified-v1 ]] || expected=41
  [[ $status == "$expected" ]] || { cat "$TEST_ROOT/selection-$phase-$kind.log" >&2; fail "selection $phase/$kind: $status"; }
  printf 'PASS: independent recovery data selection %s/%s\n' "$phase" "$kind"
 done
done

# Reuse the real updater validators and real durable marker helpers. Only the
# selected recovery CLI is substituted; private path relocation retains each
# root-owner/mode/link check and the held native FD9 proof.
eval "$(extract_function validate_root_trusted_dir_chain)"
eval "$(extract_function validate_preflight_binary)"
eval "$(extract_function verify_independent_completion_material)"
eval "$(extract_function preflight_completion_material_admission | sed 's@reader=/usr/libexec/celikpanel/recovery@reader=$TEST_ROOT/completion-cli@')"
cat > "$TEST_ROOT/completion-cli" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[[ $# == 3 && $2 == --snapshot && $3 == "$FIXTURE_SNAPSHOT" ]] || exit 91
[[ $(stat -Lc '%d:%i' /proc/self/fd/9) == "$FIXTURE_LOCK_IDENTITY" ]] || exit 92
status=0
flock -n -E 75 "$FIXTURE_COMPLETION_LOCK" true || status=$?
[[ $status == 75 ]] || exit 93
printf '%s\n' "$1" >> "$FIXTURE_COMPLETION_CALLS"
case $1 in
 completion-material-root) printf '%s' "$FIXTURE_MATERIAL_OUTPUT"; exit "$FIXTURE_MATERIAL_STATUS" ;;
 verify-installed-completion)
  if [[ -n ${FIXTURE_EXPECTED_WEB_HASH:-} ]]; then
   [[ -f $FIXTURE_TRANSACTION_ROOT/completion.pending || -f $FIXTURE_TRANSACTION_ROOT/scheduler-restore.pending ]] || exit 95
   [[ $(sha256sum "$FIXTURE_WEB_FILE" | cut -d ' ' -f 1) == "$FIXTURE_EXPECTED_WEB_HASH" ]] || exit 96
  fi
  exit "${FIXTURE_PAYLOAD_STATUS:-0}" ;;
 database-policy)
  [[ -f $FIXTURE_TRANSACTION_ROOT/completion.pending || -f $FIXTURE_TRANSACTION_ROOT/scheduler-restore.pending ]] || exit 97
  case $FIXTURE_DATABASE_POLICY in
   required) printf 'required\n' ;;
   legacy) exit 6 ;;
   *) exit 98 ;;
  esac ;;
 verify-update-database)
  [[ $FIXTURE_DATABASE_POLICY == required ]] || exit 99
  [[ -f $FIXTURE_TRANSACTION_ROOT/completion.pending || -f $FIXTURE_TRANSACTION_ROOT/scheduler-restore.pending ]] || exit 100
  [[ $(sha256sum "$FIXTURE_DATABASE_FILE" | cut -d ' ' -f 1) == "$FIXTURE_EXPECTED_DATABASE_HASH" ]] || exit 101
  ;;
 *) exit 94 ;;
esac
SH
chmod 0755 "$TEST_ROOT/completion-cli"
completion_admission_case() (
 set -euo pipefail
 local kind=$1 phase=$2
 local case_root=$TEST_ROOT/admission-$phase-$kind
 mkdir -m 0700 "$case_root" "$case_root/transaction" "$case_root/bin"
 cp "$TEST_ROOT/completion-cli" "$case_root/bin/recovery"
 RELEASE_TRANSACTION_ROOT=$case_root/transaction RELEASE_TRANSACTION_FD=9
 export FIXTURE_COMPLETION_LOCK=$RELEASE_TRANSACTION_ROOT/transaction.lock
 exec 9<>"$FIXTURE_COMPLETION_LOCK"
 chmod 0600 "$FIXTURE_COMPLETION_LOCK"
 flock -x 9
 export FIXTURE_LOCK_IDENTITY=$(stat -Lc '%d:%i' /proc/self/fd/9)
 export FIXTURE_COMPLETION_CALLS=$case_root/calls
 export FIXTURE_SNAPSHOT=20260914T120000Z-from-unknown-to-$(printf 'a%.0s' {1..40})-$(printf 'b%.0s' {1..32})
 local token
 token=$(release_txn_generate_token)
 release_txn_create_active_marker "$RELEASE_TRANSACTION_ROOT" 9 "$token" update "$FIXTURE_SNAPSHOT"
 release_txn_mark_completion_pending "$RELEASE_TRANSACTION_ROOT" 9 "$token" update "$FIXTURE_SNAPSHOT"
 release_completion_present=1 release_scheduler_present=0
 if [[ $phase != completion ]]; then
  release_txn_mark_scheduler_restore_pending "$RELEASE_TRANSACTION_ROOT" 9 "$token" update "$FIXTURE_SNAPSHOT"
  release_scheduler_present=1
  if [[ $phase == scheduler ]]; then
   release_txn_remove_completion_pending "$RELEASE_TRANSACTION_ROOT" 9 "$token" update "$FIXTURE_SNAPSHOT"
   release_completion_present=0
  fi
 fi
 RECOVERY_EXPECTED_SNAPSHOT=$FIXTURE_SNAPSHOT CODE_ROOT=$case_root
 TRUSTED_RELEASE_ROOT=$TEST_ROOT/data RECOVERY_MATERIAL_ROOT=
 export FIXTURE_MATERIAL_STATUS=0 FIXTURE_MATERIAL_OUTPUT=$TRUSTED_RELEASE_ROOT
 case $kind in
  material) RECOVERY_MATERIAL_ROOT=$TRUSTED_RELEASE_ROOT ;;
  absent) FIXTURE_MATERIAL_STATUS=3; FIXTURE_MATERIAL_OUTPUT= ;;
  verified-v1) FIXTURE_MATERIAL_STATUS=6; FIXTURE_MATERIAL_OUTPUT= ;;
  known-v2-direct) ;;
  invalid-direct) FIXTURE_MATERIAL_STATUS=1 ;;
  absent-with-output) FIXTURE_MATERIAL_STATUS=3 ;;
  v1-with-output) FIXTURE_MATERIAL_STATUS=6 ;;
  changed-root) RECOVERY_MATERIAL_ROOT=$TRUSTED_RELEASE_ROOT; FIXTURE_MATERIAL_OUTPUT=$TEST_ROOT/other ;;
  changed-trusted-root) RECOVERY_MATERIAL_ROOT=$TRUSTED_RELEASE_ROOT; TRUSTED_RELEASE_ROOT=$TEST_ROOT/other ;;
  disappeared-material) RECOVERY_MATERIAL_ROOT=$TRUSTED_RELEASE_ROOT; FIXTURE_MATERIAL_STATUS=3; FIXTURE_MATERIAL_OUTPUT= ;;
  downgraded-material) RECOVERY_MATERIAL_ROOT=$TRUSTED_RELEASE_ROOT; FIXTURE_MATERIAL_STATUS=6; FIXTURE_MATERIAL_OUTPUT= ;;
  extra-output) RECOVERY_MATERIAL_ROOT=$TRUSTED_RELEASE_ROOT; FIXTURE_MATERIAL_OUTPUT+=$'\n/untrusted' ;;
  *) exit 99 ;;
 esac
 preflight_completion_material_admission
 [[ $(cat "$FIXTURE_COMPLETION_CALLS") == completion-material-root ]] || fail 'unexpected completion reader calls'
)
for phase in completion completion-scheduler scheduler; do
 for kind in material absent verified-v1 known-v2-direct invalid-direct absent-with-output v1-with-output changed-root changed-trusted-root disappeared-material downgraded-material extra-output; do
  status=0
  completion_admission_case "$kind" "$phase" >"$TEST_ROOT/admission-$phase-$kind.log" 2>&1 || status=$?
  expected=41
  case $kind in material|absent|verified-v1) expected=0;; esac
  [[ $status == "$expected" ]] || { cat "$TEST_ROOT/admission-$phase-$kind.log" >&2; fail "completion admission $phase/$kind: $status"; }
  printf 'PASS: updater completion admission %s/%s\n' "$phase" "$kind"
 done
done
# Run the extracted complete trusted-root validator as an actual updater entry.
# Relocate only the fixed kit/material namespace into the private fixture; real
# entry path, manifest digest, ownership, links and checker metadata still apply.
entry_root=$TEST_ROOT/completion-entry
mkdir -m 0700 "$entry_root" "$entry_root/kits" "$entry_root/stage" "$entry_root/stage/bin" "$entry_root/material"
entry_material=$entry_root/material/$(printf 'c%.0s' {1..64})/data
mkdir -p "$entry_material"
printf '%s\n' "$(printf 'a%.0s' {1..40})" > "$entry_material/release.commit"
printf '%s\n' "$(printf 'd%.0s' {1..40})" > "$entry_material/release.tree"
cp "$TEST_ROOT/completion-cli" "$entry_root/stage/bin/recovery"
for checker in panel-checker agent-checker schema17-bridge; do
 printf '#!/bin/sh\nexit 92\n' > "$entry_root/stage/bin/$checker"
 chmod 0755 "$entry_root/stage/bin/$checker"
done
{
 printf '#!/usr/bin/env bash\nset -euo pipefail\n'
 declare -f die validate_root_trusted_dir_chain validate_preflight_binary verify_independent_completion_material
 extract_function validate_recovery_code_root | sed "s@/usr/libexec/celikpanel/recovery-runtimes/v1/@$entry_root/kits/@"
 extract_function validate_trusted_release | sed "s@/var/lib/celikpanel-release-state/recovery-material/v1/@$entry_root/material/@"
 cat <<'SH'
RECOVERY_RUNTIME_ROOT=$(dirname -- "$0")
RECOVER_EXISTING_TRANSACTION=1 RECOVERY_EXPECTED_OPERATION=update RECOVERY_EXPECTED_PHASE=$2
RECOVERY_EXPECTED_SNAPSHOT=$FIXTURE_SNAPSHOT RELEASE_TRANSACTION_FD=9
TRUSTED_RELEASE_ROOT=$FIXTURE_MATERIAL_OUTPUT RECOVERY_MATERIAL_ROOT=$TRUSTED_RELEASE_ROOT
case $1 in
 exact) ;;
 wrong-operation) RECOVERY_EXPECTED_OPERATION=rollback ;;
 wrong-phase) RECOVERY_EXPECTED_PHASE=active ;;
 not-recovery) RECOVER_EXISTING_TRANSACTION=0 ;;
 foreign-root) RECOVERY_MATERIAL_ROOT=/var/lib/foreign/data ;;
 changed-trusted-root) TRUSTED_RELEASE_ROOT=${TRUSTED_RELEASE_ROOT%/data} ;;
 invalid-material) export FIXTURE_MATERIAL_STATUS=1 ;;
 entry-changed) printf '\n# changed\n' >> "$0" ;;
 unsafe-checker) chmod 0775 "$RECOVERY_RUNTIME_ROOT/bin/schema17-bridge" ;;
 *) exit 99 ;;
esac
validate_trusted_release
[[ $trusted_release_commit == $(printf 'a%.0s' {1..40}) &&
   $trusted_release_tree == $(printf 'd%.0s' {1..40}) &&
   $PREFLIGHT_PANEL == "$RECOVERY_RUNTIME_ROOT/bin/panel-checker" &&
   $PREFLIGHT_AGENT == "$RECOVERY_RUNTIME_ROOT/bin/agent-checker" &&
   $SCHEMA17_BRIDGE == "$RECOVERY_RUNTIME_ROOT/bin/schema17-bridge" ]]
[[ ! -e $TRUSTED_RELEASE_ROOT/bin && ! -e $TRUSTED_RELEASE_ROOT/install.sh &&
   ! -e $TRUSTED_RELEASE_ROOT/SHA256SUMS ]]
SH
} > "$entry_root/stage/update.sh"
chmod 0755 "$entry_root/stage/update.sh"
entry_sha=$(sha256sum "$entry_root/stage/update.sh"); entry_sha=${entry_sha%% *}
printf '%s  update.sh\n' "$entry_sha" > "$entry_root/stage/runtime.manifest"
chmod 0600 "$entry_root/stage/runtime.manifest"
kit_sha=$(sha256sum "$entry_root/stage/runtime.manifest"); kit_sha=${kit_sha%% *}
mv "$entry_root/stage" "$entry_root/kits/$kit_sha"
(
 export FIXTURE_COMPLETION_LOCK=$entry_root/transaction.lock
 exec 9<>"$FIXTURE_COMPLETION_LOCK"
 flock -x 9
 export FIXTURE_LOCK_IDENTITY=$(stat -Lc '%d:%i' /proc/self/fd/9)
 export FIXTURE_SNAPSHOT=20260914T120000Z-from-unknown-to-$(printf 'a%.0s' {1..40})-$(printf 'b%.0s' {1..32})
 export FIXTURE_COMPLETION_CALLS=$entry_root/calls FIXTURE_MATERIAL_STATUS=0 FIXTURE_MATERIAL_OUTPUT=$entry_material
 for phase in completion completion-scheduler scheduler; do
  for kind in exact wrong-operation wrong-phase not-recovery foreign-root changed-trusted-root invalid-material; do
   status=0
   "$entry_root/kits/$kit_sha/update.sh" "$kind" "$phase" >"$entry_root/$phase-$kind.log" 2>&1 || status=$?
   expected=41; [[ $kind != exact ]] || expected=0
   [[ $status == "$expected" ]] || { cat "$entry_root/$phase-$kind.log" >&2; fail "completion entry $phase/$kind: $status"; }
   printf 'PASS: material updater entry %s/%s\n' "$phase" "$kind"
  done
 done
 # Metadata and entry tampering are terminal in this fixture and happen last.
 for kind in unsafe-checker entry-changed; do
  status=0
  "$entry_root/kits/$kit_sha/update.sh" "$kind" completion >"$entry_root/$kind.log" 2>&1 || status=$?
  [[ $status == 41 ]] || { cat "$entry_root/$kind.log" >&2; fail "completion entry $kind: $status"; }
  printf 'PASS: material updater entry %s\n' "$kind"
 done
)

# Exercise the whole installed-artifact validator. Foundation files/manifests
# use their real producers and validators; only recovery/systemctl CLIs are
# boundary stubs. No candidate bin/web payload exists in the material case.
eval "$(extract_function verify_installed_release_artifacts)"
eval "$(extract_function verify_release_recovery_foundation)"
source "$ROOT/deploy/release-recovery-foundation.sh"
mkdir -m 0700 "$TEST_ROOT/fixture-tools"
cat > "$TEST_ROOT/fixture-tools/systemctl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
 'is-enabled celikpanel-release-recovery.service'|'is-enabled celikpanel-release-recovery.timer') printf 'enabled\n' ;;
 'is-active --quiet celikpanel-release-recovery.timer') exit 0 ;;
 'show --property=LoadState --value celikpanel-release-recovery.service'|'show --property=LoadState --value celikpanel-release-recovery.timer') printf 'loaded\n' ;;
 'show --property=FragmentPath --value celikpanel-release-recovery.service') printf '%s\n' "$FIXTURE_FOUNDATION_SERVICE" ;;
 'show --property=FragmentPath --value celikpanel-release-recovery.timer') printf '%s\n' "$FIXTURE_FOUNDATION_TIMER" ;;
 'show --property=NeedDaemonReload --value celikpanel-release-recovery.service'|'show --property=NeedDaemonReload --value celikpanel-release-recovery.timer') printf 'no\n' ;;
 'show --property=DropInPaths --value celikpanel-release-recovery.service'|'show --property=DropInPaths --value celikpanel-release-recovery.timer') ;;
 *) exit 91 ;;
esac
SH
chmod 0755 "$TEST_ROOT/fixture-tools/systemctl"
payload_case() (
 set -euo pipefail
 local kind=$1 case_root=$TEST_ROOT/payload-$1 source target mode
 mkdir -p "$case_root/data/deploy/systemd" "$case_root/data/libexec" "$case_root/kit/bin" "$case_root/installed/bin" "$case_root/installed/web" "$case_root/installed/usr/libexec/celikpanel" "$case_root/installed/units" "$case_root/installed/state"
 TRUSTED_RELEASE_ROOT=$case_root/data RECOVERY_MATERIAL_ROOT=$TRUSTED_RELEASE_ROOT CODE_ROOT=$case_root/kit
 cp "$TEST_ROOT/completion-cli" "$CODE_ROOT/bin/recovery"
 printf '%s\n' "$(printf 'a%.0s' {1..40})" > "$TRUSTED_RELEASE_ROOT/release.commit"
 for source in deploy/release-sequence-policy deploy/release-recovery.protocol deploy/release-recovery-runner.sh deploy/release-transaction-start-guard.sh deploy/systemd/celikpanel-release-recovery.service deploy/systemd/celikpanel-release-recovery.timer; do
  cp "$ROOT/$source" "$TRUSTED_RELEASE_ROOT/$source"
 done
 printf 'fixture updater comparison bytes\n' > "$TRUSTED_RELEASE_ROOT/libexec/get.sh"
 BIN_DIR=$case_root/installed/bin WEB_DIR=$case_root/installed/web
 for target in "$BIN_DIR/panel" "$BIN_DIR/agent"; do
  printf '#!/bin/sh\nexit 92\n' > "$target"
  chmod 0755 "$target"
 done
 printf 'fixture panel assets\n' > "$WEB_DIR/index.html"
 RELEASE_UPDATER=$case_root/installed/get.sh
 RELEASE_RECOVERY_RUNNER=$case_root/installed/recovery-runner
 RELEASE_RECOVERY_UNIT=$case_root/installed/units/celikpanel-release-recovery.service
 RELEASE_RECOVERY_TIMER=$case_root/installed/units/celikpanel-release-recovery.timer
 RELEASE_TRANSACTION_HELPER=$case_root/installed/usr/libexec/celikpanel/release-transaction-start-guard
 RELEASE_RECOVERY_AGENT_DROPIN=$case_root/installed/units/agent-dropin.conf
 RELEASE_RECOVERY_PANEL_DROPIN=$case_root/installed/units/panel-dropin.conf
 RELEASE_RECOVERY_MANIFEST=$case_root/installed/state/foundation.manifest
 SYSTEMCTL_BIN=$TEST_ROOT/fixture-tools/systemctl
 export PATH=$TEST_ROOT/fixture-tools:$PATH FIXTURE_FOUNDATION_SERVICE=$RELEASE_RECOVERY_UNIT FIXTURE_FOUNDATION_TIMER=$RELEASE_RECOVERY_TIMER
 while IFS='|' read -r source target mode; do
  install -m "$mode" "$TRUSTED_RELEASE_ROOT/$source" "$target"
 done <<EOF
libexec/get.sh|$RELEASE_UPDATER|0755
deploy/release-recovery-runner.sh|$RELEASE_RECOVERY_RUNNER|0755
deploy/systemd/celikpanel-release-recovery.service|$RELEASE_RECOVERY_UNIT|0644
deploy/systemd/celikpanel-release-recovery.timer|$RELEASE_RECOVERY_TIMER|0644
deploy/release-transaction-start-guard.sh|$RELEASE_TRANSACTION_HELPER|0755
EOF
 release_recovery_emit_dropin "$RELEASE_TRANSACTION_HELPER" > "$RELEASE_RECOVERY_AGENT_DROPIN"
 cp "$RELEASE_RECOVERY_AGENT_DROPIN" "$RELEASE_RECOVERY_PANEL_DROPIN"
 chmod 0644 "$RELEASE_RECOVERY_AGENT_DROPIN" "$RELEASE_RECOVERY_PANEL_DROPIN"
 release_recovery_emit_candidate_manifest "$TRUSTED_RELEASE_ROOT" "$RELEASE_TRANSACTION_HELPER" > "$RELEASE_RECOVERY_MANIFEST"
 chmod 0600 "$RELEASE_RECOVERY_MANIFEST"
 export FIXTURE_COMPLETION_LOCK=$case_root/transaction.lock
 exec 9<>"$FIXTURE_COMPLETION_LOCK"
 flock -x 9
 RELEASE_TRANSACTION_FD=9
 export FIXTURE_LOCK_IDENTITY=$(stat -Lc '%d:%i' /proc/self/fd/9)
 export FIXTURE_SNAPSHOT=20260914T120000Z-from-unknown-to-$(printf 'a%.0s' {1..40})-$(printf 'b%.0s' {1..32})
 RECOVERY_EXPECTED_SNAPSHOT=$FIXTURE_SNAPSHOT
 export FIXTURE_COMPLETION_CALLS=$case_root/calls FIXTURE_MATERIAL_STATUS=0 FIXTURE_MATERIAL_OUTPUT=$TRUSTED_RELEASE_ROOT FIXTURE_PAYLOAD_STATUS=0
 case $kind in
  flow-*) ;;
  exact) ;;
  payload-unproven) FIXTURE_PAYLOAD_STATUS=1 ;;
  material-changed) FIXTURE_MATERIAL_STATUS=1 ;;
  data-changed) printf 'changed\n' >> "$TRUSTED_RELEASE_ROOT/libexec/get.sh" ;;
  unsafe-panel) chmod 0775 "$BIN_DIR/panel" ;;
  foundation-changed) printf 'changed\n' >> "$RELEASE_RECOVERY_RUNNER" ;;
  legacy|legacy-panel-changed|legacy-web-changed)
   RECOVERY_MATERIAL_ROOT=
   mkdir -p "$TRUSTED_RELEASE_ROOT/bin" "$TRUSTED_RELEASE_ROOT/web/dist"
   cp "$BIN_DIR/panel" "$BIN_DIR/agent" "$TRUSTED_RELEASE_ROOT/bin/"
   cp "$WEB_DIR/index.html" "$TRUSTED_RELEASE_ROOT/web/dist/"
   [[ $kind != legacy-panel-changed ]] || printf 'changed\n' >> "$BIN_DIR/panel"
   [[ $kind != legacy-web-changed ]] || printf 'changed\n' >> "$WEB_DIR/index.html"
   ;;
  *) exit 99 ;;
 esac
 verify_installed_release_artifacts
 if [[ $kind == flow-* ]]; then terminal_flow_case "${kind#flow-}"; exit; fi
 if [[ $kind == legacy ]]; then
  [[ ! -e $FIXTURE_COMPLETION_CALLS ]] || fail 'legacy payload unexpectedly invoked material CLI'
 else
  [[ ! -e $TRUSTED_RELEASE_ROOT/bin && ! -e $TRUSTED_RELEASE_ROOT/web ]]
  cmp -s "$FIXTURE_COMPLETION_CALLS" <(printf 'completion-material-root\nverify-installed-completion\n') || fail 'payload validation did not use exact independent proofs'
 fi
)
for kind in exact payload-unproven material-changed data-changed unsafe-panel foundation-changed legacy legacy-panel-changed legacy-web-changed; do
 status=0
 payload_case "$kind" >"$TEST_ROOT/payload-$kind.log" 2>&1 || status=$?
 expected=41; [[ $kind != exact && $kind != legacy ]] || expected=0
 [[ $status == "$expected" ]] || { cat "$TEST_ROOT/payload-$kind.log" >&2; fail "completion payload $kind: $status"; }
 printf 'PASS: independent completion payload %s\n' "$kind"
done
# Execute the actual updater tails, including their real EXIT traps and marker
# producers/removers. Snapshot/Go proofs and native service boundaries are stubbed;
# the whole installed-artifact/foundation validator above remains real. A stubbed
# immutable web digest models the Go After proof, not a second shell implementation.
python3 - "$ROOT/update.sh" "$TEST_ROOT" <<'PY'
from pathlib import Path
import sys, textwrap
s=Path(sys.argv[1]).read_text(); out=Path(sys.argv[2])
start=s.index('    if service_state_is_active_like "${saved_active_states[celikpanel-panel.service]}"; then', s.index('cannot authorize pending update controlled starts'))
end=s.index('\nfi\nresume_quiescing_update=', start)
trap_start=s.index('    pending_finalization_succeeded=0', s.index('# A durable completion marker'))
trap_end=s.index("    IFS=$'\\t' read -r pending_token", trap_start)
(out/'completion-tail.sh').write_text(textwrap.dedent(s[trap_start:trap_end])+s[start:end])
start=s.index('scheduler_recovery_succeeded=0')
end=s.index('\n# A durable completion marker', start)
(out/'scheduler-tail.sh').write_text(s[start:end])
PY
eval "$(extract_function service_state_is_active_like)"
eval "$(extract_function verify_saved_enablement)"
eval "$(extract_function verify_saved_runtime_states)"
# Empty on the pre-fix source: the actual extracted tail then exposes the bug.
eval "$(extract_function verify_independent_completion_terminal)"
# Keep the real policy parser and publication gate. Only their fixed CLI path
# is relocated, so terminal tests cannot silently bypass required v3 DB proof.
eval "$(extract_function read_database_migration_policy)"
eval "$(extract_function verify_database_publication_if_required)"
eval "$(extract_function run_database_recovery_command | sed 's@/usr/libexec/celikpanel/recovery@"$TEST_ROOT/completion-cli"@')"
terminal_flow_case() {
 local flow=$1 unit
 RELEASE_TRANSACTION_ROOT=$case_root/transaction
 mkdir -m 0700 "$RELEASE_TRANSACTION_ROOT"
 export FIXTURE_COMPLETION_LOCK=$RELEASE_TRANSACTION_ROOT/transaction.lock
 exec 9<>"$FIXTURE_COMPLETION_LOCK"
 chmod 0600 "$FIXTURE_COMPLETION_LOCK"
 flock -x 9
 export FIXTURE_LOCK_IDENTITY=$(stat -Lc '%d:%i' /proc/self/fd/9)
 pending_token=$(release_txn_generate_token)
 pending_snapshot=$FIXTURE_SNAPSHOT
 release_txn_create_active_marker "$RELEASE_TRANSACTION_ROOT" 9 "$pending_token" update "$pending_snapshot"
 release_txn_mark_completion_pending "$RELEASE_TRANSACTION_ROOT" 9 "$pending_token" update "$pending_snapshot"
 pending_snapshot_path=$case_root/snapshot
 RELEASE_TRANSACTION_RUNTIME_ROOT=$case_root/runtime
 mkdir -m 0700 "$pending_snapshot_path" "$RELEASE_TRANSACTION_RUNTIME_ROOT"
 release_completion_present=1 release_scheduler_present=0
 if [[ $flow == scheduler-* ]]; then
  release_txn_mark_scheduler_restore_pending "$RELEASE_TRANSACTION_ROOT" 9 "$pending_token" update "$pending_snapshot"
  release_txn_remove_completion_pending "$RELEASE_TRANSACTION_ROOT" 9 "$pending_token" update "$pending_snapshot"
  release_completion_present=0 release_scheduler_present=1
 fi
 export FIXTURE_TRANSACTION_ROOT=$RELEASE_TRANSACTION_ROOT FIXTURE_WEB_FILE=$WEB_DIR/index.html
 export FIXTURE_EXPECTED_WEB_HASH=$(sha256sum "$FIXTURE_WEB_FILE" | cut -d ' ' -f 1)
 declare -A saved_enabled_states=() saved_active_states=()
 for unit in celikpanel-agent.service celikpanel-panel.service celikpanel-firewall-restore.service; do
  saved_enabled_states[$unit]=enabled
  saved_active_states[$unit]=active
 done
 RECOVERY_AGENT_CHECKER=/usr/bin/true RECOVERY_PANEL_CHECKER=/usr/bin/true
 AGENT_STATE_DIR=$case_root MUTATION_LOCK=$case_root/mutation.lock MUTATION_LOCK_FD=
 PANEL_DB=$case_root/db
 printf 'verified canonical database\n' > "$PANEL_DB"
 export FIXTURE_DATABASE_FILE=$PANEL_DB FIXTURE_DATABASE_POLICY=required
 export FIXTURE_EXPECTED_DATABASE_HASH=$(sha256sum "$PANEL_DB" | cut -d ' ' -f 1)
 [[ $flow != *-legacy-clean ]] || FIXTURE_DATABASE_POLICY=legacy
 printf 'active\n' > "$case_root/current-state"
 printf 'enabled\n' > "$case_root/current-enablement"
 systemctl() {
  case "$*" in
   'start celikpanel-panel.service')
    printf '%s\n' "$*" >> "$case_root/starts"
    [[ $flow != completion-panel-edit ]] || printf 'owner edit\n' >> "$FIXTURE_WEB_FILE"
    [[ $flow != completion-db-edit ]] || printf 'owner database edit\n' >> "$FIXTURE_DATABASE_FILE"
    ;;
   'is-active --quiet celikpanel-agent.service'|'is-active --quiet celikpanel-panel.service'|'is-active --quiet celikpanel-firewall-restore.service')
    [[ $(cat "$case_root/current-state") == active ]] ;;
   'is-enabled celikpanel-agent.service'|'is-enabled celikpanel-panel.service'|'is-enabled celikpanel-firewall-restore.service')
    cat "$case_root/current-enablement" ;;
   *) "$SYSTEMCTL_BIN" "$@" ;;
  esac
 }
 release_txn_remove_start_authorization() {
  printf 'remove-start-authorization\n' >> "$case_root/start-auth"
  [[ $flow != completion-authorization-edit ]] || printf 'owner edit\n' >> "$FIXTURE_WEB_FILE"
 }
 validate_pending_update_snapshot() { [[ $1 == "$pending_snapshot" ]] || die 'wrong snapshot'; }
 panel_tls_quiesce_certbot_scheduler() { printf 'quiesce\n' >> "$case_root/scheduler"; }
 panel_tls_restore_certbot_scheduler() {
  printf 'restore\n' >> "$case_root/scheduler"
  case $flow in
   completion-scheduler-edit|scheduler-edit) printf 'owner edit\n' >> "$FIXTURE_WEB_FILE" ;;
   scheduler-db-edit) printf 'owner database edit\n' >> "$FIXTURE_DATABASE_FILE" ;;
   scheduler-service-edit) printf 'inactive\n' > "$case_root/current-state" ;;
   scheduler-enablement-edit) printf 'disabled\n' > "$case_root/current-enablement" ;;
  esac
 }
 stop_release_coordinators_fail_closed() { printf 'stop\n' >> "$case_root/stops"; }
 if [[ $flow == scheduler-* ]]; then
  source "$TEST_ROOT/scheduler-tail.sh"
 else
  source "$TEST_ROOT/completion-tail.sh"
 fi
}
for flow in completion-panel-edit completion-authorization-edit completion-scheduler-edit scheduler-edit scheduler-service-edit scheduler-enablement-edit completion-db-edit scheduler-db-edit completion-clean scheduler-clean completion-legacy-clean scheduler-legacy-clean; do
 status=0
 payload_case "flow-$flow" >"$TEST_ROOT/flow-$flow.log" 2>&1 || status=$?
 expected=41; [[ $flow != *-clean ]] || expected=0
 [[ $status == "$expected" ]] || { cat "$TEST_ROOT/flow-$flow.log" >&2; fail "terminal flow $flow: got $status, want $expected"; }
 case_root=$TEST_ROOT/payload-flow-$flow
 if [[ $expected == 41 ]]; then
  [[ -f $case_root/transaction/completion.pending || -f $case_root/transaction/scheduler-restore.pending ]] || fail 'failed terminal proof removed its last marker'
  ! grep -q '^==> Previous' "$TEST_ROOT/flow-$flow.log" || fail 'failed proof emitted success'
  if [[ $flow == completion-panel-edit || $flow == completion-authorization-edit || $flow == completion-scheduler-edit || $flow == scheduler-edit ]]; then
   grep -qx 'owner edit' "$case_root/installed/web/index.html" || fail 'owner bytes were overwritten'
  fi
  if [[ $flow != completion-panel-edit && $flow != completion-authorization-edit && $flow != completion-db-edit ]]; then
   [[ ! -e $case_root/stops ]] || fail 'late scheduler proof failure stopped or restarted saved runtime'
  fi
 else
  [[ ! -e $case_root/transaction/completion.pending && ! -e $case_root/transaction/scheduler-restore.pending ]] || fail 'successful exact terminal proof did not finish its markers'
 fi
 if [[ $flow == completion-* ]]; then
  [[ $(wc -l < "$case_root/starts") == 1 ]] || fail 'completion restarted panel more than once'
 else
  [[ ! -e $case_root/starts ]] || fail 'scheduler-only recovery started a coordinator'
 fi
 # The selected CLI records artifact and DB proof ordering. Legacy6 must
 # suppress only DB publication verification, never artifact checks.
 python3 - "$case_root/calls" "$flow" "$expected" <<'PYFLOW'
from pathlib import Path
import sys
calls=Path(sys.argv[1]).read_text().splitlines();flow=sys.argv[2]
legacy='-legacy-clean' in flow
policies=[i for i,v in enumerate(calls) if v=='database-policy']
for i in policies:
    assert i>0 and calls[i-1]=='verify-installed-completion', (flow,calls)
    if not legacy:
        assert i+1<len(calls) and calls[i+1]=='verify-update-database', (flow,calls)
if legacy:
    assert 'verify-update-database' not in calls, (flow,calls)
if sys.argv[3]=='0' or flow.endswith('-db-edit'):
    assert policies, (flow,'required terminal database policy was skipped')
PYFLOW
 if [[ $flow == *-db-edit ]]; then
  grep -qx 'owner database edit' "$case_root/db" || fail 'owner database bytes were overwritten'
 fi
 printf 'PASS: extracted terminal flow %s\n' "$flow"
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
