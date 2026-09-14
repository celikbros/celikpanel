#!/usr/bin/env bash
# Local native filesystem/marker tests; no installed service is operated.
set -euo pipefail
[[ $EUID == 0 ]] || { echo 'SKIP: reboot runtime proof requires root'; exit 0; }
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
TEST_ROOT=$(mktemp -d /run/celikpanel-reboot-runtime-test.XXXXXXXX)
trap 'rm -rf -- "$TEST_ROOT"' EXIT
chmod 0700 "$TEST_ROOT"
fail() { echo "FAIL: $*" >&2; exit 1; }
die() { echo "$*" >&2; exit 41; }
CODE_ROOT=$ROOT
source "$ROOT/deploy/release-transaction-guard.sh"
eval "$(sed -n '/^validate_root_trusted_dir_chain() {$/,/^}$/p' "$ROOT/rollback.sh")"
eval "$(sed -n '/^prepare_missing_stopped_runtime_directory() {$/,/^}$/p' "$ROOT/rollback.sh")"
eval "$(sed -n '/^stop_new_agent_and_hold_mutation_lock() {$/,/^}$/p' "$ROOT/rollback.sh")"
directory_source=$(sed -n '/^prepare_runtime_mutation_lock_dir() {$/,/^}$/p' "$ROOT/rollback.sh")

# Only the literal fixed path is relocated to this disposable fixture. Native
# stat/chown/chmod/mkdir/rename and native transaction markers remain real.
getent() {
    if [[ $* == 'group celikpanel' ]]; then printf '%s\n' 'celikpanel:x:0:'; else command getent "$@"; fi
}
systemctl() {
    [[ $1 == show && $3 == --value ]] || fail 'unexpected systemd action in read-only admission'
    case $2 in
        --property=ActiveState) printf '%s\n' "${fixture_state:-inactive}" ;;
        --property=MainPID) printf '%s\n' "${fixture_pid:-0}" ;;
        --property=ControlPID) printf '%s\n' 0 ;;
        --property=Job) printf '%s\n' "${fixture_job:-}" ;;
        *) fail 'unexpected property' ;;
    esac
}
reject_extra_service_cgroup_processes() { [[ $2 == 0 && ${fixture_extra_pid:-0} == 0 ]] || die 'cgroup not empty'; }

make_case() {
    case_root=$TEST_ROOT/$1
    mkdir -m 0700 "$case_root" "$case_root/transaction" "$case_root/snapshot"
    MUTATION_LOCK=$case_root/runtime/service-mutation.lock
    RUNTIME_DIR=$case_root/runtime
    eval "${directory_source//\/run\/celikpanel/$RUNTIME_DIR}"
    RELEASE_TRANSACTION_ROOT=$case_root/transaction
    RELEASE_TRANSACTION_FD=9
    exec 9<>"$RELEASE_TRANSACTION_ROOT/transaction.lock"
    chmod 0600 "$RELEASE_TRANSACTION_ROOT/transaction.lock"
    flock -x 9
    rollback_transaction_token=$(release_txn_generate_token)
    snapshot_name=20260914T120000Z-from-unknown-to-$(printf 'a%.0s' {1..40})-$(printf 'b%.0s' {1..32})
    snap=$case_root/snapshot
    rollback_verified_snapshot=$snap
    rollback_transaction_started=1
    rollback_pending_resume=0
    release_txn_create_active_marker "$RELEASE_TRANSACTION_ROOT" 9 "$rollback_transaction_token" rollback "$snapshot_name"
    # Explicit owner state: disabled before the update but active at capture.
    # Reboot therefore cannot rely on systemd starting Agent to recreate /run.
    printf '%s\n' $'celikpanel-agent.service\tdisabled\tactive' $'celikpanel-panel.service\tdisabled\tactive' > "$snap/service-states.tsv"
    fixture_state=inactive fixture_pid=0 fixture_job= fixture_extra_pid=0
}

for phase in active pending; do
    make_case "missing-$phase"
    if [[ $phase == pending ]]; then
        release_txn_mark_completion_pending "$RELEASE_TRANSACTION_ROOT" 9 "$rollback_transaction_token" rollback "$snapshot_name"
        rollback_pending_resume=1
    fi
    PREFLIGHT_AGENT=$case_root/first-probe
    cat > "$PREFLIGHT_AGENT" <<'PROBE'
#!/usr/bin/env bash
[[ $(stat -Lc '%u:%g:%a' -- "$(dirname "$CELIKPANEL_MUTATION_LOCK")") == 0:0:750 ]] || exit 74
exit 73
PROBE
    chmod 0755 "$PREFLIGHT_AGENT"
    AGENT_STATE_DIR=$case_root/state
    status=0
    (stop_new_agent_and_hold_mutation_lock --check-service-mutation-idle --check-service-mutation-idle-under-external-lock first-probe-stopped later) >"$case_root/log" 2>&1 || status=$?
    [[ $status == 41 ]] || fail "unexpected stopped fixture status $status"
    grep -Fq first-probe-stopped "$case_root/log" || fail 'did not reach first checker after preparation'
    [[ $(stat -Lc '%u:%g:%a' -- "$RUNTIME_DIR") == 0:0:750 ]] || fail 'runtime directory was not prepared before first check'
    [[ ! -e $MUTATION_LOCK ]] || fail 'directory preparation created a mutation lock'
    before=$(stat -Lc '%d:%i:%u:%g:%a:%y:%z' -- "$RUNTIME_DIR")
    prepare_runtime_mutation_lock_dir
    [[ $(stat -Lc '%d:%i:%u:%g:%a:%y:%z' -- "$RUNTIME_DIR") == "$before" ]] || fail 'existing canonical runtime metadata changed'
done

for scenario in active-coordinator live-pid queued-job extra-process wrong-token missing-snapshot-admission; do
    make_case "$scenario"
    case $scenario in
        active-coordinator) fixture_state=active ;;
        live-pid) fixture_pid=123 ;;
        queued-job) fixture_job='42 /org/freedesktop/systemd1/job/42' ;;
        extra-process) fixture_extra_pid=123 ;;
        wrong-token) rollback_transaction_token=$(printf 'c%.0s' {1..64}) ;;
        missing-snapshot-admission) rollback_verified_snapshot= ;;
    esac
    if (prepare_missing_stopped_runtime_directory) >"$case_root/log" 2>&1; then fail "$scenario admitted directory creation"; fi
    [[ ! -e $RUNTIME_DIR ]] || fail "$scenario created runtime state"
done

for scenario in owner-mode owner-group symlink; do
    make_case "$scenario"
    mkdir -m 0750 "$case_root/owner"
    case $scenario in
        owner-mode) mkdir -m 0700 "$RUNTIME_DIR" ;;
        owner-group) mkdir -m 0750 "$RUNTIME_DIR"; chown 0:1 "$RUNTIME_DIR" ;;
        symlink) ln -s "$case_root/owner" "$RUNTIME_DIR" ;;
    esac
    before=$(stat -c '%d:%i:%u:%g:%a:%h:%s:%y:%z' -- "$RUNTIME_DIR")
    if (prepare_runtime_mutation_lock_dir) >"$case_root/log" 2>&1; then fail "$scenario was normalized"; fi
    [[ $(stat -c '%d:%i:%u:%g:%a:%h:%s:%y:%z' -- "$RUNTIME_DIR") == "$before" ]] || fail "$scenario metadata changed"
done

make_case owner-publication-race
mv() {
    mkdir -m 0700 "$RUNTIME_DIR"
    printf '%s\n' owner > "$RUNTIME_DIR/owner-record"
    command mv "$@"
}
if (prepare_runtime_mutation_lock_dir) >"$case_root/log" 2>&1; then fail 'concurrent owner directory was adopted'; fi
[[ $(cat "$RUNTIME_DIR/owner-record") == owner && $(stat -Lc '%a' "$RUNTIME_DIR") == 700 ]] || fail 'owner publication was overwritten'
unset -f mv
printf '%s\n' 'PASS: reboot runtime creation precedes first probe, requires exact admitted stopped state, and preserves owner paths'
