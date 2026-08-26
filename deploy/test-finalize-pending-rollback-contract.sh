#!/usr/bin/env bash
set -euo pipefail

candidate=deploy/finalize-pending-rollback.sh
[[ $# -eq 0 ]] || candidate=$1
[[ -f "$candidate" ]] || {
    printf 'FAIL: missing pending-rollback finalizer: %s\n' "$candidate" >&2
    exit 1
}
bash -n "$candidate"
if LC_ALL=C grep -q $'\r' "$candidate"; then
    printf 'FAIL: pending-rollback finalizer contains CR bytes\n' >&2
    exit 1
fi

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

require_text() {
    grep -Fq -- "$1" "$candidate" || fail "$2"
}

reject_text() {
    if grep -Fq -- "$1" "$candidate"; then
        fail "$2"
    fi
}

line_of_last() {
    grep -nF -- "$1" "$candidate" | tail -n1 | cut -d: -f1
}

line_after() {
    local start=$1 needle=$2
    awk -v start="$start" -v needle="$needle" \
        'NR > start && index($0, needle) { print NR; exit }' "$candidate"
}

assert_before() {
    local first=$1 second=$2 message=$3
    [[ -n "$first" && -n "$second" && "$first" -lt "$second" ]] ||
        fail "$message"
}

require_text 'set -euo pipefail' "strict shell mode is missing"
require_text '[[ $EUID -eq 0 ]]' "root-only boundary is missing"
require_text '[[ $# -eq 4 &&' "exact recovery argument count is missing"
require_text '--trusted-transaction-release=' "separate transaction-release CLI is missing"
require_text '"$RECOVERY_COMMIT" != "$PENDING_TARGET"' "recovery and transaction commits need not be distinct"
require_text '"$TRUSTED_RECOVERY_RELEASE" != "$TRUSTED_TRANSACTION_RELEASE"' "recovery and transaction roots need not be distinct"
require_text 'find "$canonical" -type f -links +1' "immutable releases do not reject hard-linked files"
require_text "stat -Lc '%u %g %a %h'" "immutable release entries are not proved root:root with link metadata"
require_text '| cmp -s - SHA256SUMS' "release manifest closure is not recomputed"
require_text 'sha256sum -c SHA256SUMS' "release checksums are not verified"
require_text 'deploy/finalize-pending-rollback.sh' "running helper is not pinned to the verified recovery release"
require_text 'transaction "$TRUSTED_TRANSACTION_RELEASE" "$PENDING_TARGET"' "historical transaction release is not independently verified"
require_text 'TRANSACTION_RELEASE_TREE=$tree' "transaction tree provenance is not retained"
require_text '"$TRANSACTION_RELEASE_TREE"' "snapshot tree is not bound to the transaction release"

require_text "printf 'normal\\n' | cmp -s -" "normal-v6 transition marker is not exact"
require_text 'pending-rollback finalizer accepts only an exact normal v6 snapshot' "non-normal snapshots are not explicitly refused"
require_text 'normal rollback snapshot contains bootstrap transition payloads' "normal snapshot does not reject pre-ledger/schema17 payloads"
require_text 'agent-state/service-mutations.json' "normal snapshot durable ledger is not required"
require_text 'cmp -s "$pending_snapshot_path/bin/panel" "$BIN_DIR/panel"' "installed panel is not bound to snapshot bytes"
require_text 'cmp -s "$pending_snapshot_path/bin/agent" "$BIN_DIR/agent"' "installed agent is not bound to snapshot bytes"
require_text '<(cd "$pending_snapshot_path/web"' "installed web tree is not bound to snapshot bytes"
require_text 'cmp -s "$pending_snapshot_path/units/$unit" "$UNIT_DIR/$unit"' "installed systemd units are not bound to snapshot bytes"
require_text 'cmp -s "$pending_snapshot_path/libexec/get.sh" "$RELEASE_UPDATER"' "installed updater is not bound to snapshot bytes"
require_text 'cmp -s "$pending_snapshot_path/agent-state/service-mutations.json" "$AGENT_LEDGER"' "installed normal ledger is not bound to snapshot bytes"
require_text '"$TRUSTED_TRANSACTION_RELEASE/bin/panel"' "WAL-aware checker is not pinned to the transaction release"
require_text '"$TRUSTED_TRANSACTION_RELEASE/bin/agent"' "mutation checker is not pinned to the transaction release"
reject_text 'run_panel_migrations_offline' "rollback completion recovery must never run update migrations"
reject_text '--migrate-only' "rollback completion recovery contains a migration path"
reject_text '"$pending_token" update "$pending_snapshot"' "a durable marker operation is still incorrectly labelled update"

require_text 'panel_tls_restore_snapshot \' "idempotent rollback TLS restoration is missing"
require_text 'panel_tls_secure_restore_parent "$PANEL_TLS_DIR" \' "TLS data parent is not secured"
require_text 'panel_tls_restore_service_parent "$PANEL_TLS_DIR" \' "TLS data parent ownership is not restored"
require_text 'open_mutation_lock handoff' "same-inode nonblocking agent-start handoff is missing"
require_text 'another mutation entered during the controlled agent-start handoff' "handoff contention is not fail-closed"
require_text 'stop_coordinators_trap_safe' "failure cleanup has no bounded coordinator stop proof"
require_text 'coordinators_stopped_trap_safe' "failure cleanup does not recursively prove stopped coordinators"
require_text 'coordinator stopped state could not be proved' "failure cleanup can still overclaim a stopped state"
require_text 'durable marker state requires operator verification' "unproved marker state is still overclaimed"
require_text 'completion.pending was preserved and both coordinators were proved stopped' "proved fail-closed result is missing"
require_text 'systemctl kill --kill-whom=all --signal=SIGKILL "$unit"' "failed graceful stop has no cgroup termination fallback"
require_text 'job=$(systemctl show --property=Job --value "$unit"' "stopped proof ignores queued restart jobs"
require_text 'authorization_closed=0' "failure cleanup does not fail closed when start authorization removal fails"

main_start=$(line_of_last 'prepare_runtime_mutation_lock_dir')
idle_unlocked=$(line_after "$main_start" 'run_target_agent_idle_unlocked')
lock_immediate=$(line_after "$idle_unlocked" 'open_mutation_lock immediate')
socket_cleanup=$(line_after "$lock_immediate" 'remove_stale_agent_socket_under_lock')
idle_locked=$(line_after "$socket_cleanup" 'run_target_agent_idle_locked')
tls_secure=$(line_after "$idle_locked" 'panel_tls_secure_restore_parent "$PANEL_TLS_DIR" \')
tls_restore=$(line_after "$idle_locked" 'panel_tls_restore_snapshot \')
tls_owner_restore=$(line_after "$tls_restore" 'panel_tls_restore_service_parent "$PANEL_TLS_DIR" \')
authorization=$(line_after "$tls_restore" 'release_txn_create_start_authorization \')
handoff_unlock=$(line_after "$authorization" 'release_mutation_lock \')
agent_start=$(line_after "$handoff_unlock" 'wait_for_agent_ready')
handoff_relock=$(line_after "$agent_start" 'open_mutation_lock handoff')
panel_start=$(line_after "$handoff_relock" 'wait_for_panel_ready')
panel_stop=$(line_after "$panel_start" 'systemctl stop celikpanel-panel.service \')
final_wal=$(line_after "$panel_stop" 'run_target_panel_wal_idle')
panel_restart=$(line_after "$final_wal" 'wait_for_panel_ready')
remove_auth=$(line_after "$panel_restart" 'release_txn_remove_start_authorization \')
mark_scheduler=$(line_after "$remove_auth" 'release_txn_mark_scheduler_restore_pending \')
remove_completion=$(line_after "$mark_scheduler" 'release_txn_remove_completion_pending \')
terminal_unlock=$(line_after "$remove_completion" 'release_mutation_lock \')
restore_scheduler=$(line_after "$terminal_unlock" 'panel_tls_restore_certbot_scheduler')

assert_before "$idle_unlocked" "$lock_immediate" "unlocked idle proof must precede immediate mutation-lock ownership"
assert_before "$lock_immediate" "$socket_cleanup" "mutation lock must protect stale socket cleanup"
assert_before "$socket_cleanup" "$idle_locked" "locked idle proof must follow stale socket cleanup"
assert_before "$idle_locked" "$tls_secure" "locked idle proof must precede TLS parent transition"
assert_before "$tls_secure" "$tls_restore" "TLS data parent must be secured before TLS rollback restoration"
assert_before "$tls_restore" "$tls_owner_restore" "TLS data parent ownership must be restored after TLS publication"
assert_before "$idle_locked" "$tls_restore" "locked idle proof must precede TLS rollback restoration"
assert_before "$tls_owner_restore" "$authorization" "TLS parent ownership must be restored before controlled-start authorization"
assert_before "$authorization" "$handoff_unlock" "start authorization must exist before the narrow agent lock handoff"
assert_before "$handoff_unlock" "$agent_start" "mutation lock must be released before restored agent startup"
assert_before "$agent_start" "$handoff_relock" "same-inode lock must be reacquired after the agent publishes its socket"
assert_before "$handoff_relock" "$panel_start" "panel start must occur only after mutation-lock reacquisition"
assert_before "$panel_start" "$panel_stop" "controlled panel execution must precede its final WAL-aware stop"
assert_before "$panel_stop" "$final_wal" "panel must be stopped before the final WAL-aware proof"
assert_before "$final_wal" "$panel_restart" "final WAL-aware proof must precede saved panel-state restoration"
assert_before "$panel_restart" "$remove_auth" "saved runtime state must be restored before start authorization removal"
assert_before "$remove_auth" "$mark_scheduler" "start authorization must be removed before terminal marker transition"
assert_before "$mark_scheduler" "$remove_completion" "scheduler obligation must be durable before completion marker removal"
assert_before "$remove_completion" "$terminal_unlock" "completion marker must be removed before releasing the mutation lock"
assert_before "$terminal_unlock" "$restore_scheduler" "scheduler restoration must happen only after terminal mutation-lock release"

printf 'PASS: pending rollback finalizer contract\n'
