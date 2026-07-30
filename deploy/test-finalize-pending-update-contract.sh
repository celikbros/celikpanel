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
require_text 'finalizer requires completion.pending as the only durable phase' \
    "completion-only phase gate is missing"
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
require_text 'completion.pending disappeared during verified terminal removal; saved runtime state was left intact.' \
    "marker-absent partial-success handling is missing"

transaction_lock=$(line_of_last 'acquire_release_transaction_lock')
recovery_release=$(line_after "$transaction_lock" 'validate_immutable_release \')
target_release=$(line_after "$recovery_release" 'validate_immutable_release \')
guard_source=$(line_after "$target_release" 'source "$TRUSTED_RECOVERY_RELEASE/deploy/release-transaction-guard.sh"')
inherited_lock=$(line_after "$guard_source" 'release_txn_verify_inherited_lock')
marker_gate=$(line_after "$inherited_lock" 'finalizer requires completion.pending as the only durable phase')
pending_read=$(line_after "$marker_gate" 'release_txn_read_pending_fields')
pending_validate=$(line_after "$pending_read" 'release_txn_validate_pending_token \')
snapshot_validate=$(line_after "$pending_validate" 'validate_pending_update_snapshot "$pending_snapshot"')
artifact_validate=$(line_after "$snapshot_validate" 'verify_installed_release_artifacts')
enablement_validate=$(line_after "$artifact_validate" 'verify_saved_enablement')
stopped_validate=$(line_after "$enablement_validate" 'verify_both_units_stopped')
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
remove_pending=$(line_after "$completion_removing" 'release_txn_remove_completion_pending \')
terminal_unlock=$(line_after "$remove_pending" 'release_mutation_lock \')
marker_absent_branch=$(line_of_first 'if [[ "$completion_verified" -eq 1 &&')
marker_absent_unlock=$(line_after "$marker_absent_branch" 'release_mutation_lock')
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
    "completion-only gate must precede marker parsing"
assert_before "$pending_validate" "$snapshot_validate" \
    "exact pending token must be validated before snapshot trust"
assert_before "$snapshot_validate" "$artifact_validate" \
    "snapshot proof must precede installed artifact trust"
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
assert_before "$completion_removing" "$remove_pending" \
    "completion state must be set before durable marker removal"
assert_before "$remove_pending" "$terminal_unlock" \
    "completion marker must be removed while the mutation lock is held"
assert_before "$marker_absent_branch" "$marker_absent_unlock" \
    "marker-absent partial success must release the mutation lock"
assert_before "$marker_absent_unlock" "$marker_absent_return" \
    "marker-absent partial success must exit before fail-closed stopping"
assert_before "$marker_absent_return" "$fail_closed_stop" \
    "verified marker absence must not stop restored services"

printf 'pending-update finalizer contract: PASS\n'
