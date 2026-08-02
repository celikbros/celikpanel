#!/usr/bin/env bash
set -euo pipefail

candidate=${1:-deploy/finalize-pending-update.sh}
[[ -f "$candidate" ]] || {
    printf 'FAIL: missing pending-update finalizer: %s\n' "$candidate" >&2
    exit 1
}
bash -n "$candidate"
if LC_ALL=C grep -q $'\r' "$candidate"; then
    printf 'FAIL: pending-update finalizer contains CR bytes\n' >&2
    exit 1
fi

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

require_text() {
    grep -Fq -- "$1" "$candidate" || fail "$2"
}

line_of_last() {
    local needle=$1
    grep -nF -- "$needle" "$candidate" | tail -n1 | cut -d: -f1
}

line_of_first() {
    local needle=$1
    grep -nF -- "$needle" "$candidate" | head -n1 | cut -d: -f1
}

line_after() {
    local start=$1 needle=$2
    awk -v start="$start" -v needle="$needle" \
        'NR > start && index($0, needle) { print NR; exit }' "$candidate"
}

assert_before() {
    local first=$1 second=$2 message=$3
    [[ -n "$first" && -n "$second" && "$first" -lt "$second" ]] \
        || fail "$message"
}

assert_no_text_between() {
    local start=$1 end=$2 needle=$3 message=$4
    if awk -v start="$start" -v end="$end" -v needle="$needle" \
        'NR > start && NR < end && index($0, needle) { found=1 }
         END { exit(found ? 0 : 1) }' "$candidate"; then
        fail "$message"
    fi
}

require_text 'set -euo pipefail' "strict shell mode is missing"
require_text 'PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin' \
    "fixed privileged PATH is missing"
require_text 'umask 077' "private umask is missing"
require_text '[[ $EUID -eq 0 ]]' "root-only guard is missing"
require_text '[[ $# -eq 4 &&' "exact argument count is missing"
require_text '"$PENDING_TARGET" =~ ^[0-9a-f]{40}$' \
    "pending target is not exactly 40 lowercase hex"
require_text '"$RECOVERY_COMMIT" =~ ^[0-9a-f]{40}$' \
    "recovery commit is not exactly 40 lowercase hex"
require_text '"$RECOVERY_COMMIT" != "$PENDING_TARGET"' \
    "recovery and pending-target commits are not required to be distinct"
require_text '"$TRUSTED_RECOVERY_RELEASE" != "$TRUSTED_TARGET_RELEASE"' \
    "recovery and target roots are not required to be distinct"
require_text '[[ "${relative:0:12}" == "${expected_commit:0:12}" ]]' \
    "release basename is not bound to its explicit commit"
require_text '[[ "$commit" == "$expected_commit" ]]' \
    "release provenance is not bound to its explicit commit"
require_text '| cmp -s - SHA256SUMS' \
    "release manifest closure is not recomputed"
require_text 'sha256sum -c SHA256SUMS' \
    "release manifest entries are not verified"
require_text 'pending-update finalizer must execute from the verified recovery release' \
    "running finalizer is not pinned to the recovery release"
require_text 'target web artifact is missing or unsafe' \
    "historical target web artifact is not required"
require_text 'finalizer requires completion.pending with at most its matching scheduler marker' \
    "completion plus optional scheduler phase gate is missing"
require_text 'scheduler-restore.pending does not exactly match completion.pending' \
    "optional scheduler marker is not bound exactly to completion.pending"
require_text 'finalizer requires completion.pending or an exact scheduler-only recovery marker' \
    "scheduler-only exact retry topology is missing"
require_text 'release_txn_read_scheduler_restore_fields' \
    "scheduler-only retry does not parse the exact scheduler marker"
require_text 'scheduler-only marker failed exact validation' \
    "scheduler-only retry does not reject a tampered marker"
require_text 'scheduler_recovery_verified=1' \
    "scheduler-only retry is not gated by complete recovery evidence"
require_text '"$target" == "$PENDING_TARGET"' \
    "snapshot identity is not bound to the explicit pending target"
require_text '"$TRUSTED_TARGET_RELEASE/bin/panel"' \
    "offline checks are not pinned to the historical target panel"
require_text '"$TRUSTED_TARGET_RELEASE/bin/agent"' \
    "offline checks are not pinned to the historical target agent"
require_text 'RuntimeDirectoryPreserve=yes' \
    "runtime mutation-lock preservation is missing"
require_text 'open_mutation_lock handoff' \
    "controlled same-inode handoff reacquire is missing"
require_text 'flock -n -x "$MUTATION_LOCK_FD"' \
    "nonblocking mutation-lock acquisition is missing"
require_text 'another mutation entered during the controlled agent-start handoff' \
    "busy handoff rejection is missing"
if grep -Eq -- 'flock[[:space:]]+-w' "$candidate"; then
    fail "finalizer must not wait for a mutation lock during handoff"
fi
require_text 'mutation lock inode changed across the controlled agent start' \
    "mutation-lock inode continuity proof is missing"
require_text 'remove_stale_agent_socket_under_lock()' \
    "locked stale-agent socket cleanup is missing"
require_text 'stale agent socket path is not an exact non-symlink socket' \
    "stale socket type/path proof is missing"
require_text 'rm -- "$AGENT_SOCKET"' \
    "exact stale socket removal is missing"
require_text '[[ ! -e "$AGENT_SOCKET" && ! -L "$AGENT_SOCKET" ]]' \
    "fresh agent socket precondition is missing"
require_text '[[ -S "$AGENT_SOCKET" && ! -L "$AGENT_SOCKET" ]]' \
    "fresh non-symlink agent socket proof is missing"
require_text 'completion.pending was preserved and both coordinators were stopped' \
    "fail-closed completion preservation is missing"
require_text 'completion_verified=0' \
    "terminal completion verification state is missing"
require_text 'completion_removing=0' \
    "terminal completion removal state is missing"
require_text 'scheduler_restore_completed=0' \
    "post-restore marker-removal uncertainty state is missing"
require_text 'Runtime completion is durable; exact Certbot scheduler restoration remains safely retryable.' \
    "exact scheduler-marker partial-success handling is missing"
require_text 'durable marker removal is uncertain. Runtime was left intact and finalization did not claim success.' \
    "restore-success marker-removal uncertainty is not handled safely"
require_text 'release_txn_mark_scheduler_restore_pending \' \
    "terminal scheduler obligation is not durably published"
require_text 'release_txn_remove_scheduler_restore_pending \' \
    "completed scheduler obligation is not durably removed"

transaction_lock=$(line_of_last 'acquire_release_transaction_lock')
recovery_release=$(line_after "$transaction_lock" 'validate_immutable_release \')
target_release=$(line_after "$recovery_release" 'validate_immutable_release \')
guard_source=$(line_after "$target_release" 'source "$TRUSTED_RECOVERY_RELEASE/deploy/release-transaction-guard.sh"')
inherited_lock=$(line_after "$guard_source" 'release_txn_verify_inherited_lock')
marker_gate=$(line_after "$inherited_lock" 'finalizer requires completion.pending with at most its matching scheduler marker')
pending_read=$(line_after "$marker_gate" 'release_txn_read_pending_fields')
pending_validate=$(line_after "$pending_read" 'release_txn_validate_pending_token \')
optional_scheduler_validate=$(line_after "$pending_validate" 'release_txn_validate_scheduler_restore_token \')
scheduler_only_read=$(line_after "$optional_scheduler_validate" 'release_txn_read_scheduler_restore_fields')
scheduler_only_trap=$(line_after "$scheduler_only_read" 'trap finalization_exit EXIT')
scheduler_only_validate=$(line_after "$scheduler_only_trap" 'release_txn_validate_scheduler_restore_token \')
snapshot_validate=$(line_after "$scheduler_only_validate" 'validate_pending_update_snapshot "$pending_snapshot"')
artifact_validate=$(line_after "$snapshot_validate" 'verify_installed_release_artifacts')
enablement_validate=$(line_after "$artifact_validate" 'verify_saved_enablement')
scheduler_only_branch=$(line_after "$enablement_validate" 'if [[ "$scheduler_only_resume" -eq 1 ]]; then')
scheduler_runtime_proof=$(line_after "$scheduler_only_branch" 'verify_saved_runtime_states')
scheduler_recovery_proof=$(line_after "$scheduler_runtime_proof" 'scheduler_recovery_verified=1')
scheduler_revalidate=$(line_after "$scheduler_recovery_proof" 'release_txn_validate_scheduler_restore_token \')
scheduler_quiesce=$(line_after "$scheduler_revalidate" 'panel_tls_quiesce_certbot_scheduler')
scheduler_post_quiesce_validate=$(line_after "$scheduler_quiesce" 'release_txn_validate_scheduler_restore_token \')
scheduler_restore=$(line_after "$scheduler_post_quiesce_validate" 'panel_tls_restore_certbot_scheduler')
scheduler_restore_completed=$(line_after "$scheduler_restore" 'scheduler_restore_completed=1')
scheduler_remove=$(line_after "$scheduler_restore_completed" 'release_txn_remove_scheduler_restore_pending \')
scheduler_success=$(line_after "$scheduler_remove" 'finalization_succeeded=1')
scheduler_exit=$(line_after "$scheduler_success" 'exit 0')
late_branch=$(line_after "$scheduler_exit" 'if [[ "$completion_present" -eq 1 && "$scheduler_present" -eq 1 ]]; then')
late_trap=$(line_after "$late_branch" 'trap finalization_exit EXIT')
stopped_validate=$(line_after "$late_trap" 'verify_both_units_stopped')
exit_trap=$(line_after "$stopped_validate" 'trap finalization_exit EXIT')
preserve_runtime=$(line_after "$exit_trap" 'install_and_verify_runtime_directory_preserve')
unit_guards=$(line_after "$preserve_runtime" 'release_txn_install_and_verify_unit_guards \')
stale_auth=$(line_after "$unit_guards" 'release_txn_clear_stale_start_authorization \')
post_guard_stopped=$(line_after "$stale_auth" 'verify_both_units_stopped')
post_guard_evidence=$(line_after "$post_guard_stopped" 'verify_pending_evidence')
runtime_dir=$(line_after "$post_guard_evidence" 'prepare_runtime_mutation_lock_dir')
unlocked_idle=$(line_after "$runtime_dir" 'run_target_agent_idle_unlocked')
first_lock=$(line_after "$unlocked_idle" 'open_mutation_lock immediate')
stale_socket_cleanup=$(line_after "$first_lock" 'remove_stale_agent_socket_under_lock')
locked_idle=$(line_after "$stale_socket_cleanup" 'run_target_agent_idle_locked')
wal_idle=$(line_after "$locked_idle" 'run_target_panel_wal_idle')
migration=$(line_after "$wal_idle" 'run_panel_migrations_offline')
pre_auth_evidence=$(line_after "$migration" 'verify_pending_evidence')
authorization=$(line_after "$pre_auth_evidence" 'release_txn_create_start_authorization \')
saved_agent_branch=$(line_after "$authorization" 'if service_state_is_active_like "${saved_active_states[celikpanel-agent.service]}"; then')
handoff_unlock=$(line_after "$saved_agent_branch" 'release_mutation_lock \')
agent_start=$(line_after "$handoff_unlock" 'wait_for_agent_ready')
handoff_relock=$(line_after "$agent_start" 'open_mutation_lock handoff')
inactive_else=$(line_after "$handoff_relock" 'else')
inactive_end=$(line_after "$inactive_else" 'fi')
common_lock_verify=$(line_after "$inactive_end" 'verify_mutation_lock_held')
post_handoff_idle=$(line_after "$common_lock_verify" 'run_target_agent_idle_locked')
post_handoff_evidence=$(line_after "$post_handoff_idle" 'verify_pending_evidence')
panel_start=$(line_after "$post_handoff_evidence" 'wait_for_panel_ready')
runtime_proof=$(line_after "$panel_start" 'verify_saved_runtime_states')
post_start_idle=$(line_after "$runtime_proof" 'run_target_agent_idle_locked')
post_start_evidence=$(line_after "$post_start_idle" 'verify_pending_evidence')
remove_auth=$(line_after "$post_start_evidence" 'release_txn_remove_start_authorization \')
terminal_runtime=$(line_after "$remove_auth" 'verify_saved_runtime_states')
terminal_idle=$(line_after "$terminal_runtime" 'run_target_agent_idle_locked')
terminal_evidence=$(line_after "$terminal_idle" 'verify_pending_evidence')
completion_verified=$(line_after "$terminal_evidence" 'completion_verified=1')
completion_removing=$(line_after "$completion_verified" 'completion_removing=1')
mark_scheduler=$(line_after "$completion_removing" 'release_txn_mark_scheduler_restore_pending \')
remove_pending=$(line_after "$mark_scheduler" 'release_txn_remove_completion_pending \')
terminal_unlock=$(line_after "$remove_pending" 'release_mutation_lock \')
terminal_quiesce=$(line_after "$terminal_unlock" 'panel_tls_quiesce_certbot_scheduler')
terminal_scheduler_validate=$(line_after "$terminal_quiesce" 'release_txn_validate_scheduler_restore_token \')
terminal_scheduler_restore=$(line_after "$terminal_scheduler_validate" 'panel_tls_restore_certbot_scheduler')
terminal_restore_completed=$(line_after "$terminal_scheduler_restore" 'scheduler_restore_completed=1')
remove_scheduler=$(line_after "$terminal_restore_completed" 'release_txn_remove_scheduler_restore_pending \')
terminal_success=$(line_after "$remove_scheduler" 'finalization_succeeded=1')
restore_absent_branch=$(line_of_first 'if [[ "$scheduler_restore_completed" -eq 1 &&')
restore_absent_unlock=$(line_after "$restore_absent_branch" 'release_mutation_lock')
restore_absent_message=$(line_after "$restore_absent_unlock" 'durable marker removal is uncertain.')
restore_absent_return=$(line_after "$restore_absent_message" 'return "$status"')
marker_absent_branch=$(line_of_first 'if [[ ( "$completion_verified" -eq 1 &&')
marker_retry_validate=$(line_after "$marker_absent_branch" 'release_txn_validate_scheduler_restore_token \')
marker_absent_unlock=$(line_after "$marker_retry_validate" 'release_mutation_lock')
marker_absent_return=$(line_after "$marker_absent_unlock" 'return "$status"')
fail_closed_stop=$(line_after "$marker_absent_return" 'systemctl stop celikpanel-panel.service')

assert_before "$transaction_lock" "$recovery_release" \
    "persistent transaction lock must precede release validation"
assert_before "$recovery_release" "$target_release" \
    "recovery and target releases are not independently validated"
assert_before "$target_release" "$guard_source" \
    "both releases must be validated before recovery code is sourced"
assert_before "$guard_source" "$inherited_lock" \
    "sourced guard must verify the inherited transaction lock"
assert_before "$marker_gate" "$pending_read" \
    "completion plus optional scheduler gate must precede marker parsing"
assert_before "$pending_validate" "$snapshot_validate" \
    "exact pending token must be validated before snapshot trust"
assert_before "$pending_validate" "$optional_scheduler_validate" \
    "completion token must be validated before its optional scheduler token"
assert_before "$optional_scheduler_validate" "$snapshot_validate" \
    "optional scheduler token must be validated before snapshot trust"
assert_before "$scheduler_only_read" "$scheduler_only_trap" \
    "scheduler-only identity must be parsed before its fail-closed trap"
assert_before "$scheduler_only_trap" "$scheduler_only_validate" \
    "scheduler-only fail-closed trap must precede exact marker validation"
assert_before "$scheduler_only_validate" "$snapshot_validate" \
    "scheduler-only marker must be exact before snapshot trust"
assert_before "$snapshot_validate" "$artifact_validate" \
    "snapshot proof must precede installed artifact trust"
assert_before "$scheduler_only_branch" "$scheduler_runtime_proof" \
    "scheduler-only retry must enter its dedicated runtime-proof branch"
assert_before "$scheduler_runtime_proof" "$scheduler_recovery_proof" \
    "saved runtime state must be proved before recovery is retry-safe"
assert_before "$scheduler_recovery_proof" "$scheduler_revalidate" \
    "complete recovery evidence must precede scheduler marker revalidation"
assert_before "$scheduler_revalidate" "$scheduler_quiesce" \
    "exact scheduler marker must be revalidated before re-quiescing"
assert_before "$scheduler_quiesce" "$scheduler_post_quiesce_validate" \
    "scheduler marker must be revalidated after re-quiescing"
assert_before "$scheduler_post_quiesce_validate" "$scheduler_restore" \
    "post-quiesce marker proof must precede scheduler restoration"
assert_before "$scheduler_restore" "$scheduler_restore_completed" \
    "scheduler restore completion must be recorded immediately after restoration"
assert_before "$scheduler_restore_completed" "$scheduler_remove" \
    "scheduler state must be restored before exact marker removal"
assert_before "$scheduler_remove" "$scheduler_success" \
    "scheduler-only success must follow exact marker removal"
assert_before "$scheduler_success" "$scheduler_exit" \
    "scheduler-only recovery must exit before full finalization mutation"
assert_before "$late_branch" "$late_trap" \
    "completion+scheduler late topology must install its fail-closed trap"
assert_before "$late_trap" "$stopped_validate" \
    "late completion+scheduler topology must not bypass the stopped-unit gate"
assert_before "$stopped_validate" "$exit_trap" \
    "initial read-only proofs must precede the mutating failure trap"
assert_before "$exit_trap" "$preserve_runtime" \
    "fail-closed trap must precede system mutation"
assert_before "$unit_guards" "$stale_auth" \
    "unit guards must be installed before stale authorization cleanup"
assert_before "$post_guard_stopped" "$post_guard_evidence" \
    "stopped state must be reproved after guard installation"
assert_before "$runtime_dir" "$unlocked_idle" \
    "trusted runtime lock directory must precede idle checks"
assert_before "$unlocked_idle" "$first_lock" \
    "unlocked durable idle proof must precede mutation locking"
assert_before "$first_lock" "$stale_socket_cleanup" \
    "stale socket cleanup must follow exact lock acquisition"
assert_before "$stale_socket_cleanup" "$locked_idle" \
    "stale socket cleanup must precede locked idle proof"
assert_before "$wal_idle" "$migration" \
    "WAL-aware idle proof must precede offline migration"
assert_before "$pre_auth_evidence" "$authorization" \
    "marker/artifact evidence must be rechecked before authorization"
assert_before "$authorization" "$saved_agent_branch" \
    "saved agent state must select the lock handoff branch"
assert_before "$authorization" "$handoff_unlock" \
    "start authorization must precede the mutation-lock handoff"
assert_before "$handoff_unlock" "$agent_start" \
    "agent must not start while the finalizer owns its mutation lock"
assert_before "$agent_start" "$handoff_relock" \
    "agent readiness must precede nonblocking lock reacquire"
assert_before "$handoff_relock" "$inactive_else" \
    "unlock/start/reacquire must remain inside the saved-active branch"
assert_before "$inactive_end" "$common_lock_verify" \
    "both saved-agent branches must join at the common locked proof"
assert_no_text_between "$inactive_else" "$common_lock_verify" 'release_mutation_lock' \
    "saved-inactive branch must retain the mutation lock continuously"
assert_before "$common_lock_verify" "$post_handoff_idle" \
    "common held-lock proof must precede post-start idle proof"
assert_before "$post_handoff_evidence" "$panel_start" \
    "marker/artifact evidence must be rechecked before panel start"
assert_before "$panel_start" "$runtime_proof" \
    "panel start must be followed by saved runtime-state verification"
assert_before "$post_start_evidence" "$remove_auth" \
    "authorization must remain until post-start evidence succeeds"
assert_before "$remove_auth" "$terminal_runtime" \
    "runtime state must be rechecked after authorization removal"
assert_before "$terminal_evidence" "$completion_verified" \
    "terminal evidence must precede completion verification state"
assert_before "$completion_verified" "$completion_removing" \
    "verified completion must precede removal state"
assert_before "$completion_removing" "$mark_scheduler" \
    "completion state must be set before publishing scheduler recovery"
assert_before "$mark_scheduler" "$remove_pending" \
    "scheduler recovery must be durable before completion removal"
assert_before "$remove_pending" "$terminal_unlock" \
    "completion marker must be removed while the mutation lock is held"
assert_before "$terminal_unlock" "$terminal_quiesce" \
    "mutation lock must be released before scheduler re-quiesce"
assert_before "$terminal_quiesce" "$terminal_scheduler_validate" \
    "terminal scheduler marker must be revalidated after re-quiesce"
assert_before "$terminal_scheduler_validate" "$terminal_scheduler_restore" \
    "terminal marker proof must precede scheduler restoration"
assert_before "$terminal_scheduler_restore" "$terminal_restore_completed" \
    "terminal scheduler restore completion must be recorded immediately"
assert_before "$terminal_restore_completed" "$remove_scheduler" \
    "terminal scheduler state must be restored before marker removal"
assert_before "$remove_scheduler" "$terminal_success" \
    "final success must follow exact scheduler marker removal"
assert_before "$restore_absent_branch" "$restore_absent_unlock" \
    "restore-completed markerless partial success must release any mutation lock"
assert_before "$restore_absent_unlock" "$restore_absent_message" \
    "markerless post-restore state must be reported explicitly"
assert_before "$restore_absent_message" "$restore_absent_return" \
    "markerless post-restore partial success must return before fail-closed stopping"
assert_before "$restore_absent_return" "$marker_absent_branch" \
    "post-restore markerless handling must precede exact-marker retry handling"
assert_before "$marker_absent_branch" "$marker_retry_validate" \
    "partial success must prove the exact scheduler retry marker"
assert_before "$marker_retry_validate" "$marker_absent_unlock" \
    "exact scheduler proof must precede mutation-lock release"
assert_before "$marker_absent_unlock" "$marker_absent_return" \
    "exact scheduler partial success must exit before fail-closed stopping"
assert_before "$marker_absent_return" "$fail_closed_stop" \
    "verified exact scheduler retry state must not stop restored services"

extract_function() {
    local name=$1
    awk -v signature="$name() {" '
        $0 == signature { inside=1 }
        inside { print }
        inside && $0 == "}" { exit }
    ' "$candidate"
}

tmp_root=$(mktemp -d)
case "$tmp_root" in
    /tmp/*) ;;
    *) fail "unsafe temporary root: $tmp_root" ;;
esac
trap 'rm -rf -- "$tmp_root"' EXIT

source <(extract_function finalization_exit)

TRANSACTION_ROOT="$tmp_root/transaction"
TRANSACTION_RUNTIME_ROOT="$tmp_root/runtime"
mkdir -m 0700 "$TRANSACTION_ROOT" "$TRANSACTION_RUNTIME_ROOT"
RELEASE_TRANSACTION_FD=9
pending_token=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
pending_snapshot=20260801T000000Z-from-a-to-b-0123456789abcdef0123456789abcdef
mock_log="$tmp_root/finalizer.log"
mock_scheduler_marker_valid=1

assert_scheduler_marker_args() {
    [[ $# -eq 4 && "$1" == "$TRANSACTION_ROOT" &&
       "$2" == "$pending_token" && "$3" == update &&
       "$4" == "$pending_snapshot" ]] \
        || fail "scheduler validation arguments are not ROOT TOKEN OP SNAP"
}

release_txn_validate_scheduler_restore_token() {
    assert_scheduler_marker_args "$@"
    printf 'validate-scheduler\n' >> "$mock_log"
    [[ "$mock_scheduler_marker_valid" -eq 1 &&
       -f "$TRANSACTION_ROOT/scheduler-restore.pending" ]]
}

release_txn_remove_start_authorization() {
    printf 'remove-start-authorization\n' >> "$mock_log"
}

release_mutation_lock() {
    printf 'unlock\n' >> "$mock_log"
}

systemctl() {
    printf 'systemctl %s\n' "$*" >> "$mock_log"
}

reset_finalizer_exit_state() {
    : > "$mock_log"
    rm -f -- \
        "$TRANSACTION_ROOT/completion.pending" \
        "$TRANSACTION_ROOT/scheduler-restore.pending"
    finalization_succeeded=0
    scheduler_restore_completed=0
    completion_verified=0
    completion_removing=0
    scheduler_only_resume=0
    scheduler_recovery_verified=0
    start_authorization_created=0
    mock_scheduler_marker_valid=1
}

run_finalizer_failure() {
    local status
    set +e
    ( set +e; false; finalization_exit )
    status=$?
    set -e
    [[ "$status" -ne 0 ]] \
        || fail "failed pending finalization unexpectedly returned success"
}

assert_finalizer_runtime_preserved() {
    ! grep -Fq 'systemctl stop celikpanel-panel.service' "$mock_log" \
        || fail "$1 stopped panel despite exact terminal evidence"
    ! grep -Fq 'systemctl stop celikpanel-agent.service' "$mock_log" \
        || fail "$1 stopped agent despite exact terminal evidence"
    grep -Fq 'unlock' "$mock_log" \
        || fail "$1 did not release the mutation lock"
}

assert_finalizer_runtime_stopped() {
    grep -Fq 'systemctl stop celikpanel-panel.service' "$mock_log" \
        || fail "$1 did not stop panel"
    grep -Fq 'systemctl stop celikpanel-agent.service' "$mock_log" \
        || fail "$1 did not stop agent"
}

# Scheduler restoration completed and its marker unlink became visible before
# the parent-directory fsync failed. Runtime is complete and must stay up.
reset_finalizer_exit_state
scheduler_restore_completed=1
run_finalizer_failure
assert_finalizer_runtime_preserved "visible scheduler unlink boundary"

# Completion unlink became visible only after the exact scheduler obligation
# was published. Preserve runtime and the retry marker.
reset_finalizer_exit_state
touch "$TRANSACTION_ROOT/scheduler-restore.pending"
completion_verified=1
completion_removing=1
run_finalizer_failure
assert_finalizer_runtime_preserved "visible completion unlink boundary"
[[ -e "$TRANSACTION_ROOT/scheduler-restore.pending" ]] \
    || fail "visible completion unlink boundary removed scheduler evidence"

# A dedicated scheduler-only retry is safe only after full recovery proof.
reset_finalizer_exit_state
touch "$TRANSACTION_ROOT/scheduler-restore.pending"
scheduler_only_resume=1
scheduler_recovery_verified=1
run_finalizer_failure
assert_finalizer_runtime_preserved "scheduler-only verified retry"

# Before completion unlink, completion+scheduler remains a full finalization
# retry topology. The failing invocation itself must fail closed.
reset_finalizer_exit_state
touch \
    "$TRANSACTION_ROOT/completion.pending" \
    "$TRANSACTION_ROOT/scheduler-restore.pending"
completion_verified=1
completion_removing=1
run_finalizer_failure
assert_finalizer_runtime_stopped "completion plus scheduler pre-unlink boundary"

# Missing, incomplete, or tampered scheduler proof never preserves runtime.
reset_finalizer_exit_state
completion_verified=1
completion_removing=1
run_finalizer_failure
assert_finalizer_runtime_stopped "completion without scheduler evidence"

reset_finalizer_exit_state
touch "$TRANSACTION_ROOT/scheduler-restore.pending"
scheduler_only_resume=1
scheduler_recovery_verified=1
mock_scheduler_marker_valid=0
run_finalizer_failure
assert_finalizer_runtime_stopped "invalid scheduler-only marker"
[[ -e "$TRANSACTION_ROOT/scheduler-restore.pending" ]] \
    || fail "invalid scheduler-only marker was removed"

printf 'pending-update finalizer contract: PASS\n'
