#!/usr/bin/env bash
# Exercise real updater error/EXIT functions without invoking an installed update.
set -euo pipefail
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
candidate="$repo_root/update.sh"
extract() {
    awk -v header="$1() {" '$0 == header { inside=1 } inside { print } inside && $0 == "}" { exit }' "$candidate"
}
for name in die run_update_idle_probe report_update_failure on_exit fail_before_active; do
    source <(extract "$name")
done
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

# Stand-ins only for host/recovery actions; the production error and cleanup
# decision path is executed unchanged. No systemctl, installer or host lock.
classify_durable_update_marker() { printf '%s\n' "$fixture_marker"; }
cleanup_incomplete() { echo 'fixture cleanup complete' >&2; }
restart_previous_services() { echo '!! Kurulu baytlar değişmeden önce güncelleme durdu.' >&2; }
stop_release_coordinators_fail_closed() { :; }
abort_quiesce_before_active() {
    transaction_started=0
    transaction_phase=quiesce-removed
    fixture_marker=none
}
release_release_mutation_lock() { :; }
busy_probe() {
    [[ "${CELIKPANEL_MUTATION_LOCK_FD:-}" == 19 ]] || return 2
    printf 'Service mutation idle check: service mutation state is not idle: the host package manager is active\n' >&2
    return 1
}
fixture() (
    set -euo pipefail
    mutation_started=0 transaction_started=1 quiesce_abort_failed=0
    transaction_phase=quiesce fixture_marker=quiesce
    transaction_completion_verified=0 scheduler_restore_verified=0
    update_failure_reason= update_failure_detail=
    trap on_exit EXIT
    printf '%05000d\n' 0
    if ! CELIKPANEL_MUTATION_LOCK_FD=19 run_update_idle_probe busy_probe; then
        fail_before_active 'agent/package state changed before freeze'
    fi
    fail 'busy package manager was admitted'
)
set +e
output=$(fixture 2>&1)
status=$?
set -e
[[ "$status" == 1 ]] || fail 'busy probe must fail'
last=${output##*$'\n'}
[[ "$last" == '!! CELIKPANEL_UPDATE_FAILURE code=package_manager_busy state=unchanged reason=the host package manager is active detail='* ]] || fail "cause/outcome lost: $last"
[[ "$last" == *'the host package manager is active'* ]] || fail 'underlying cause lost'
[[ ${#last} -lt 190 && "$last" != */* && "$last" != *\\* ]] || fail 'summary exceeds legacy panel contract'

# Recovery trouble must not receive the ordinary retry message.
for fixture_marker in active ambiguous; do
    output=$(mutation_started=0; transaction_started=1; quiesce_abort_failed=1; update_failure_reason='original failure'; update_failure_detail='idle: the host package manager is active'; report_update_failure 1 "$fixture_marker" 2>&1)
    [[ "$output" == *'state=recovery_required'* ]] || fail 'unsafe recovery classified unchanged'
done
output=$(mutation_started=1; transaction_started=0; quiesce_abort_failed=0; report_update_failure 1 none 2>&1)
[[ "$output" == *'state=recovery_required'* ]] || fail 'changed installation classified unchanged'
output=$(transaction_completion_verified=1; mutation_started=0; transaction_started=0; quiesce_abort_failed=0; report_update_failure 1 none 2>&1)
[[ "$output" == *'state=recovery_required'* ]] || fail 'completed installation classified unchanged'

# A successful later probe must clear stale failure details and never emit a
# failure summary. Long/multiline diagnostics remain one bounded final line.
update_failure_detail='idle: the host package manager is active'
run_update_idle_probe true >/dev/null
[[ -z "$update_failure_detail" ]] || fail 'stale diagnostic retained'
[[ -z "$(report_update_failure 0 none 2>&1)" ]] || fail 'success emits failure'
output=$(update_failure_reason=$(printf '%03000d' 0); update_failure_detail=$'first\nsecond\rthird'; report_update_failure 1 none 2>&1)
[[ "$output" != *$'\n'* && "$output" != *$'\r'* && ${#output} -lt 940 ]] || fail 'summary not bounded single line'
echo 'PASS: causal failure survives cleanup; legacy worker bound; busy/unsafe outcomes; no retry or host mutation'
