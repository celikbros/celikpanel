#!/usr/bin/env bash
# Exercise real updater error/EXIT functions without invoking an installed update.
set -euo pipefail
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
candidate="$repo_root/update.sh"
extract() {
    awk -v header="$1() {" '$0 == header { inside=1 } inside { print } inside && $0 == "}" { exit }' "$candidate"
}
for name in die run_update_idle_probe check_bind_update_compatibility preflight_bind_before_quiesce report_update_failure on_exit fail_before_active; do
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
# Exercise the actual early BIND shell probe and EXIT path. The target-agent
# stand-in checks the clean environment and mode; real host proofs run in Go.
# Gerçek erken BIND çağrısını ve EXIT yolunu sına; host kanıtları Go testindedir.
bind_fixture=$(mktemp -d)
trap 'rm -rf -- "$bind_fixture"' EXIT
cat > "$bind_fixture/fail-agent" <<'AGENT'
#!/usr/bin/env bash
set -euo pipefail
[[ $# == 1 && $CELIKPANEL_AGENT_STATE_DIR == /fixture/agent-private &&
   $CELIKPANEL_MUTATION_LOCK == /fixture/mutation.lock &&
   $CELIKPANEL_MUTATION_LOCK_FD == 19 && $HOME == /root && $LC_ALL == C &&
   -z ${CELIKPANEL_PREFLIGHT_UNTRUSTED:-} ]] || exit 92
case $1 in
    --check-bind-signed-update-compatible-under-external-lock|--check-pre-ledger-bind-signed-update-compatible-under-external-lock) ;;
    *) exit 93 ;;
esac
if [[ ${0##*/} == fail-agent ]]; then
    echo 'BIND state and ownership receipts disagree' >&2
    exit 1
fi
printf 'verified:%s\n' "$1"
AGENT
cp "$bind_fixture/fail-agent" "$bind_fixture/ok-agent"
chmod 0700 "$bind_fixture/fail-agent" "$bind_fixture/ok-agent"
bind_preflight_fixture() (
    set -euo pipefail
    mutation_started=0 transaction_started=0 quiesce_abort_failed=0
    transaction_phase=none fixture_marker=none
    transaction_completion_verified=0 scheduler_restore_verified=0
    update_failure_reason= update_failure_detail=
    BOOTSTRAP_PRE_LEDGER=$1
    PREFLIGHT_AGENT="$bind_fixture/$2-agent"
    AGENT_STATE_DIR=/fixture/agent-private MUTATION_LOCK=/fixture/mutation.lock
    MUTATION_LOCK_FD=
    export CELIKPANEL_PREFLIGHT_UNTRUSTED=must-not-reach-agent
    prepare_runtime_mutation_lock_dir() { echo prepare >> "$bind_fixture/trace"; }
    acquire_release_mutation_lock() { MUTATION_LOCK_FD=19; echo acquire >> "$bind_fixture/trace"; }
    release_release_mutation_lock() { MUTATION_LOCK_FD=; echo release >> "$bind_fixture/trace"; }
    trap on_exit EXIT
    preflight_bind_before_quiesce
    echo 'preflight admitted' >> "$bind_fixture/trace"
)
for mode in 0 1; do
    : > "$bind_fixture/trace"
    status=0
    output=$(bind_preflight_fixture "$mode" fail 2>&1) || status=$?
    [[ $status == 1 ]] || fail "BIND preflight status=$status mode=$mode"
    [[ $(cat "$bind_fixture/trace") == $'prepare\nacquire\nrelease' ]] ||
        fail 'failed BIND preflight did not release its lock before refusal'
    last=${output##*$'\n'}
    [[ $last == *'state=unchanged'* &&
       $last == *'reason=managed BIND compatibility check failed before stopping panel services'* &&
       $last == *'BIND state and ownership receipts disagree'* ]] ||
        fail "BIND cause or unchanged outcome lost: $last"

    : > "$bind_fixture/trace"
    output=$(bind_preflight_fixture "$mode" ok 2>&1) || fail 'compatible BIND preflight rejected'
    [[ $(cat "$bind_fixture/trace") == $'prepare\nacquire\nrelease\npreflight admitted' ]] ||
        fail 'successful BIND preflight did not release its lock before proceeding'
    flag=--check-bind-signed-update-compatible-under-external-lock
    [[ $mode == 0 ]] || flag=--check-pre-ledger-bind-signed-update-compatible-under-external-lock
    [[ $output == *"verified:$flag"* && $output != *CELIKPANEL_UPDATE_FAILURE* ]] ||
        fail 'BIND preflight used wrong mode or reported a failure on success'
done

# F1 (upd3): the selected recovery runtime's read-only preflight fails before
# the EXIT trap exists. The typed step and the checker's first diagnostic line
# reach the one summary line older workers keep, the outcome is unchanged, and
# the request's failure sidecar names the terminal preflight stop.
for name in fail_recovery_runtime_preflight publish_update_failure_observation; do
    source <(extract "$name")
done
preflight_fixture() (
    set -euo pipefail
    mutation_started=0 transaction_started=0 quiesce_abort_failed=0
    transaction_completion_verified=0 scheduler_restore_verified=0
    recovery_runtime_preparation_attempted=1 recovery_runtime_preparation_verified=1
    firewall_runtime_preparation_attempted=1 firewall_runtime_preparation_verified=1
    update_failure_reason= update_failure_detail= update_failure_code= snapshot_name=
    trusted_release_commit=3333333333333333333333333333333333333333
    release_observation_worker_request() { printf '%s\n' 44444444444444444444444444444444; }
    release_observation_publish_failure() { printf 'sidecar %s %s %s\n' "$1" "$2" "$3" >> "$bind_fixture/sidecar"; }
    compat_probe() { printf '%s\n' "$1" >&2; return 3; }
    run_update_idle_probe compat_probe "$1" || fail_recovery_runtime_preflight "$2"
    echo 'preflight admitted' >&2
)
compat_line='The selected recovery runtime cannot verify this installation. The update has not stopped the panel. step=panel_database_check: Recovery database check: service operations are not idle: SQLite sidecar -wal changed after pinning'
: > "$bind_fixture/sidecar"
status=0
output=$(preflight_fixture "$compat_line" compatibility 2>&1) || status=$?
[[ $status == 1 && $output != *'preflight admitted'* ]] || fail "preflight failure did not stop the update: $status"
last=${output##*$'\n'}
[[ $last == '!! CELIKPANEL_UPDATE_FAILURE code=recovery_runtime_preflight_failed state=unchanged reason=recovery runtime preflight step=panel_database_check: Recovery database check: service operations are not idle: SQLite sidecar -wal changed after pinning detail=' ]] ||
    fail "typed preflight cause or unchanged outcome lost: $last"
[[ $(cat "$bind_fixture/sidecar") == 'sidecar 44444444444444444444444444444444 3333333333333333333333333333333333333333 recovery_runtime_preflight_failed' ]] ||
    fail 'preflight stop was not recorded for the exact request and target'
[[ $output == *'!! recovery runtime preflight step=panel_database_check: '*'nothing was changed'* ]] ||
    fail 'preflight die line does not say nothing was changed'

# A host package-manager refusal keeps its reviewed retry guidance.
: > "$bind_fixture/sidecar"
output=$(preflight_fixture 'The selected recovery runtime cannot verify this installation. The update has not stopped the panel. step=agent_ledger_check: Service mutation idle check: service mutation state is not idle: the host package manager is active' compatibility 2>&1) || true
last=${output##*$'\n'}
[[ $last == '!! CELIKPANEL_UPDATE_FAILURE code=package_manager_busy state=unchanged reason=the host package manager is active detail=' ]] ||
    fail "busy package manager in preflight lost its reviewed summary: $last"
grep -Fq 'recovery_runtime_preflight_failed' "$bind_fixture/sidecar" || fail 'busy preflight stop was not recorded as terminal'

# Steps without a typed checker line keep the updater's own step and the last
# printable diagnostic, bounded and without control bytes.
: > "$bind_fixture/sidecar"
output=$(preflight_fixture $'first line\n\x1b[1mRecovery material support could not be verified.' material_support 2>&1) || true
last=${output##*$'\n'}
[[ $last == '!! CELIKPANEL_UPDATE_FAILURE code=recovery_runtime_preflight_failed state=unchanged reason=recovery runtime preflight step=material_support: [1mRecovery material support could not be verified. detail=' ]] ||
    fail "untyped preflight step lost: $last"
output=$(preflight_fixture '' database_metadata 2>&1) || true
last=${output##*$'\n'}
[[ $last == *'reason=recovery runtime preflight step=database_metadata: no diagnostic was recorded detail=' ]] ||
    fail "empty preflight diagnostic not explained: $last"

# Unverified kit preparation is never reported as unchanged or as a terminal
# preflight stop, even with the preflight code set.
: > "$bind_fixture/sidecar"
output=$(update_failure_code=recovery_runtime_preflight_failed; recovery_runtime_preparation_attempted=1; recovery_runtime_preparation_verified=0; mutation_started=0; transaction_started=0; quiesce_abort_failed=0; transaction_completion_verified=0; scheduler_restore_verified=0; trusted_release_commit=3333333333333333333333333333333333333333; release_observation_worker_request() { echo 44444444444444444444444444444444; }; release_observation_publish_failure() { echo sidecar >> "$bind_fixture/sidecar"; }; report_update_failure 1 none 2>&1)
[[ $output == *'code=recovery_runtime_preparation_unconfirmed state=recovery_required'* && ! -s "$bind_fixture/sidecar" ]] ||
    fail "unverified kit preparation classified as a terminal preflight stop: $output"

echo 'PASS: causal failure survives cleanup; legacy worker bound; busy/unsafe outcomes; typed preflight stop; no retry or host mutation'
