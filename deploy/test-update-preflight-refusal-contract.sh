#!/usr/bin/env bash
# upd4 F4/F5/O8: exercise the updater's real preflight, snapshot and renewal
# functions with stand-ins for host actions only (no systemctl, lock, panel,
# database or host state).
#  - The preliminary live panel probe is read once more after 2 s only when the
#    checker exits 75 (a concurrent write); a busy queue (exit 1) never is.
#  - A refusal before the freeze ends typed (update_preflight_refused, step and
#    reason class, the checker's line), state=unchanged, with the failure record.
#  - A failed panel database snapshot keeps the snapshot tool's line.
#  - The Certbot scheduler state before the update is recorded on/off.
# Güncelleyicinin gerçek ön denetim, anlık görüntü ve yenileme işlevlerini sına.
set -euo pipefail
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
candidate="$repo_root/update.sh"
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
extract() {
    awk -v header="$1() {" '$0 == header { inside=1 } inside { print } inside && $0 == "}" { exit }' "$candidate"
}
for name in die run_update_idle_probe run_update_live_panel_probe update_probe_diagnostic \
    fail_update_preflight fail_update_snapshot fail_before_active report_update_failure on_exit \
    publish_update_failure_observation publish_update_renewal_observation; do
    source <(extract "$name")
    declare -F "$name" >/dev/null || fail "updater function $name is missing"
done
eval "$(grep -E '^UPDATE_LIVE_PROBE_RETRY_DELAY=' "$candidate")"
[[ $UPDATE_LIVE_PROBE_RETRY_DELAY == 2 ]] || fail 'live probe pause is not 2 s'
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT

# Stand-ins for host actions only.
sleep() { echo "sleep $*" >> "$work/trace"; }
classify_durable_update_marker() { printf '%s\n' "$fixture_marker"; }
cleanup_incomplete() { :; }
restart_previous_services() { :; }
stop_release_coordinators_fail_closed() { :; }
release_release_mutation_lock() { :; }
abort_quiesce_before_active() {
    echo abort >> "$work/trace"
    [[ ${fixture_abort:-ok} == ok ]] || { quiesce_abort_failed=1; transaction_started=1; return 1; }
    transaction_started=0
    transaction_phase=quiesce-removed
    fixture_marker=none
}
release_observation_worker_request() { printf '%s\n' 44444444444444444444444444444444; }
release_observation_publish_failure() { printf 'sidecar %s %s %s\n' "$1" "$2" "$3" >> "$work/trace"; }
release_observation_publish_renewal() { printf 'renewal %s %s %s\n' "$1" "$2" "$3" >> "$work/trace"; }
# A stand-in checker: its exit codes and lines are read from a script file.
cat > "$work/checker" <<'CHECKER'
#!/usr/bin/env bash
count=$(( $(cat "$WORK/calls" 2>/dev/null || echo 0) + 1 ))
echo "$count" > "$WORK/calls"
read -r code line < <(sed -n "${count}p" "$WORK/plan")
[[ $CELIKPANEL_DATA_DIR == /fixture/data ]] || exit 92
[[ $code == 0 ]] && { echo '2026/10/01 00:00:00 WAL-aware service operation state is idle'; exit 0; }
printf '%s\n' '2026/10/01 00:00:00 Starting CelikPanel Backend...' "2026/10/01 00:00:00 $line" >&2
exit "$code"
CHECKER
chmod 0700 "$work/checker"
export WORK=$work
plan() { printf '%s\n' "$@" > "$work/plan"; rm -f "$work/calls"; : > "$work/trace"; }
changed='75 WAL-aware service operation idle check failed: service operations are not idle: SQLite sidecar -shm changed after pinning'
busy='1 WAL-aware service operation idle check failed: service operations are not idle: operation 0f0e is running'

probe_fixture() (
    set -euo pipefail
    mutation_started=0 transaction_started=1 quiesce_abort_failed=0
    transaction_phase=quiesce fixture_marker=quiesce
    transaction_completion_verified=0 scheduler_restore_verified=0
    update_failure_reason= update_failure_detail= update_failure_code= update_idle_probe_status=0
    snapshot_name=20261001T000000Z-from-unknown-to-3333333333333333333333333333333333333333-55555555555555555555555555555555
    trap on_exit EXIT
    if ! CELIKPANEL_DATA_DIR=/fixture/data \
        run_update_live_panel_probe "$work/checker" --check-service-operations-idle-wal-aware; then
        fail_update_preflight idle_probe "panel service operations are not idle; update refused"
    fi
    echo 'probe admitted' >&2
    trap - EXIT
)
calls() { cat "$work/calls"; }

# A concurrent write is read once more after 2 s; the second reading admits.
plan "$changed" '0'
output=$(probe_fixture 2>&1) || fail "re-read probe failed: $output"
[[ $(calls) == 2 && $(cat "$work/trace") == 'sleep 2' && $output == *'probe admitted'* ]] ||
    fail "concurrent write was not read exactly once more: calls=$(calls) trace=$(cat "$work/trace")"
[[ $output == *'reading it once more in 2 s'* && $output == *'2 sn sonra bir kez daha okunuyor'* ]] || fail 're-read is not explained'

# Two concurrent writes: refused, typed, unchanged, recorded; no third reading.
plan "$changed" "$changed" '0'
status=0; output=$(probe_fixture 2>&1) || status=$?
last=${output##*$'\n'}
[[ $status == 1 && $(calls) == 2 && $output != *'probe admitted'* ]] || fail "second concurrent write: status=$status calls=$(calls)"
[[ $last == '!! CELIKPANEL_UPDATE_FAILURE code=update_preflight_refused state=unchanged reason=update preflight step=idle_probe class=concurrent_write: panel service operations are not idle; update refused detail=WAL-aware service operation idle check failed: service operations are not idle: SQLite sidecar -shm changed after pinning' ]] ||
    fail "typed concurrent-write refusal lost: $last"
[[ $(cat "$work/trace") == $'sleep 2\nabort\nsidecar 44444444444444444444444444444444 3333333333333333333333333333333333333333 update_preflight_refused' ]] ||
    fail "quiesce abort or failure record missing: $(cat "$work/trace")"
[[ $output == *'quiesce was safely aborted, rerun the exact trusted update'* ]] || fail 'die line lost the abort'

# A busy operation queue is never read again.
plan "$busy" '0'
status=0; output=$(probe_fixture 2>&1) || status=$?
last=${output##*$'\n'}
[[ $status == 1 && $(calls) == 1 && $(cat "$work/trace") != *sleep* ]] || fail "busy queue was read again: calls=$(calls)"
[[ $last == '!! CELIKPANEL_UPDATE_FAILURE code=update_preflight_refused state=unchanged reason=update preflight step=idle_probe class=operation_active: '*' detail=WAL-aware service operation idle check failed: service operations are not idle: operation 0f0e is running' ]] ||
    fail "busy queue refusal lost its class: $last"

# Any other refusal: not re-read, generic class.
plan '1 WAL-aware service operation idle check failed: service operations are not idle: panel database quick check returned "corrupt"' '0'
status=0; output=$(probe_fixture 2>&1) || status=$?
last=${output##*$'\n'}
[[ $status == 1 && $(calls) == 1 && $last == *'class=check_failed: '* ]] || fail "other refusal: calls=$(calls) $last"

# A failed quiesce abort is not unchanged: it is never typed or recorded.
plan "$busy"
status=0; output=$(fixture_abort=failed probe_fixture 2>&1) || status=$?
last=${output##*$'\n'}
[[ $last == '!! CELIKPANEL_UPDATE_FAILURE code=update_failed state=recovery_required '* && $(cat "$work/trace") != *sidecar* ]] ||
    fail "failed abort classified as a preflight stop: $last / $(cat "$work/trace")"

# Before any quiesce marker the refusal dies directly, typed and unchanged; a
# host package-manager refusal keeps its reviewed summary.
pre_quiesce_fixture() (
    set -euo pipefail
    mutation_started=0 transaction_started=0 quiesce_abort_failed=0
    transaction_phase=none fixture_marker=none
    transaction_completion_verified=0 scheduler_restore_verified=0
    update_failure_reason= update_failure_detail= update_failure_code= update_idle_probe_status=0 snapshot_name=
    trusted_release_commit=3333333333333333333333333333333333333333
    trap on_exit EXIT
    agent_probe() { printf '%s\n' "$1" >&2; return 1; }
    run_update_idle_probe agent_probe "$1" ||
        fail_update_preflight agent_idle "an existing server operation requires completion or exact recovery before update; coordinators remain available"
)
: > "$work/trace"
output=$(pre_quiesce_fixture 'Service mutation idle check: service mutation state is not idle: operation abc is running' 2>&1) || true
last=${output##*$'\n'}
[[ $last == '!! CELIKPANEL_UPDATE_FAILURE code=update_preflight_refused state=unchanged reason=update preflight step=agent_idle class=check_failed: an existing server operation requires completion or exact recovery before update; coordinators remain available detail=Service mutation idle check: service mutation state is not idle: operation abc is running' ]] ||
    fail "pre-quiesce refusal: $last"
[[ $(cat "$work/trace") == 'sidecar 44444444444444444444444444444444 3333333333333333333333333333333333333333 update_preflight_refused' ]] ||
    fail "pre-quiesce refusal not recorded: $(cat "$work/trace")"
: > "$work/trace"
output=$(pre_quiesce_fixture 'Service mutation idle check: service mutation state is not idle: the host package manager is active' 2>&1) || true
last=${output##*$'\n'}
[[ $last == '!! CELIKPANEL_UPDATE_FAILURE code=package_manager_busy state=unchanged reason=the host package manager is active detail=' ]] ||
    fail "package manager refusal lost its reviewed summary: $last"
grep -Fq update_preflight_refused "$work/trace" || fail 'package manager preflight stop was not recorded as terminal'

# The diagnostic line: first failure line, timestamp removed, printable, bounded.
[[ $(update_probe_diagnostic $'2026/10/01 00:49:31 Starting CelikPanel Backend...\n2026/10/01 00:49:31 Create service operation snapshot failed: process 4242 still uses the celikpanel UID\nsecond joined error') == 'Create service operation snapshot failed: process 4242 still uses the celikpanel UID' ]] ||
    fail 'snapshot diagnostic line not selected'
[[ $(update_probe_diagnostic $'first\n\x1b[1mlast line\r') == '[1mlast line' ]] || fail 'fallback diagnostic not the last printable line'
long=$(update_probe_diagnostic "x failed: $(printf '%0400d' 0)")
[[ ${#long} == 240 ]] || fail "diagnostic not bounded: ${#long}"
[[ -z $(update_probe_diagnostic '') ]] || fail 'empty output invented a diagnostic'

# F5: a failed snapshot keeps the tool's line in the failure line; the outcome
# (recovery required after the freeze) is unchanged.
snapshot_fixture() (
    set -euo pipefail
    mutation_started=0 transaction_started=1 quiesce_abort_failed=0
    transaction_phase=active fixture_marker=active
    transaction_completion_verified=0 scheduler_restore_verified=0
    update_failure_reason= update_failure_detail= update_failure_code= update_idle_probe_status=0
    trap on_exit EXIT
    snapshot_tool() { printf '%s\n' '2026/10/01 00:49:31 Starting CelikPanel Backend...' "2026/10/01 00:49:31 $1" >&2; return 1; }
    run_update_idle_probe snapshot_tool "$1" ||
        fail_update_snapshot "transaction-consistent panel database snapshot failed"
)
output=$(snapshot_fixture 'Create service operation snapshot failed: process 4242 still uses the celikpanel UID' 2>&1) || true
last=${output##*$'\n'}
[[ $last == '!! CELIKPANEL_UPDATE_FAILURE code=update_failed state=recovery_required reason=transaction-consistent panel database snapshot failed detail=Create service operation snapshot failed: process 4242 still uses the celikpanel UID' ]] ||
    fail "snapshot cause lost: $last"
[[ $output == *'!! transaction-consistent panel database snapshot failed: Create service operation snapshot failed: process 4242'* ]] ||
    fail 'snapshot cause not printed'
output=$(snapshot_fixture '' 2>&1) || true
last=${output##*$'\n'}
[[ $last == *'reason=transaction-consistent panel database snapshot failed detail=the snapshot tool recorded no diagnostic' ]] ||
    fail "empty snapshot diagnostic: $last"

# O8: the scheduler state before the update is recorded once as on or off.
renewal_case() {
    : > "$work/trace"
    printf 'celikpanel-agent.service\tenabled\tactive\ncelikpanel-panel.service\tenabled\tactive\ncelikpanel-firewall-restore.service\tenabled\tinactive\n' > "$work/ledger"
    printf '%s\n' "$@" >> "$work/ledger"
    ( snapshot_name=20261001T000000Z-from-unknown-to-3333333333333333333333333333333333333333-55555555555555555555555555555555
      publish_update_renewal_observation "$work/ledger" )
    cat "$work/trace"
}
[[ $(renewal_case $'certbot.timer\tnot-found\tinactive' $'certbot-renew.timer\tdisabled\tinactive') == 'renewal 44444444444444444444444444444444 3333333333333333333333333333333333333333 off' ]] ||
    fail 'renewal off not recorded'
[[ $(renewal_case $'certbot.timer\tenabled\tactive' $'certbot-renew.timer\tnot-found\tinactive') == *' on' ]] || fail 'renewal on not recorded'
[[ $(renewal_case $'certbot.timer\tdisabled\tactive' $'certbot-renew.timer\tnot-found\tinactive') == *' on' ]] || fail 'active timer not on'
: > "$work/trace"
( snapshot_name= ; publish_update_renewal_observation "$work/ledger" )
[[ ! -s $work/trace ]] || fail 'renewal recorded without the exact update identity'

# Wiring: exactly the two preliminary live probes may re-read; the frozen and
# stopped proofs never do; both snapshots keep their cause.
[[ $(grep -c 'run_update_live_panel_probe "$PREFLIGHT_PANEL"' "$candidate") == 2 ]] || fail 'live re-read is not limited to the two preliminary probes'
line_of() { grep -nF -- "$1" "$candidate" | head -1 | cut -d: -f1; }
live=$(line_of 'run_update_live_panel_probe "$PREFLIGHT_PANEL" --check-service-operations-idle-wal-aware; then')
freeze=$(line_of 'freeze_release_service_cgroup celikpanel-panel.service panel panel_frozen')
[[ -n $live && -n $freeze && $live -lt $freeze ]] || fail 'live re-read is not before the freeze'
awk -v from="$freeze" 'NR > from && /run_update_live_panel_probe/ { found=1 } END { exit found }' "$candidate" ||
    fail 'a proof after the freeze re-reads'
grep -Fq '|| fail_update_snapshot "transaction-consistent panel database snapshot failed"' "$candidate" || fail 'snapshot cause not wired'
grep -Fq '|| fail_update_snapshot "transaction-consistent durable recovery snapshot failed; recovery path was retained"' "$candidate" || fail 'rescue snapshot cause not wired'
for reason in 'final frozen panel idle proof failed' 'final frozen agent idle proof failed'; do
    grep -Fq "fail_before_active \"$reason\"" "$candidate" || fail "frozen proof changed: $reason"
done
grep -Fq 'publish_update_renewal_observation "$tmp_snap/service-states.tsv"' "$candidate" || fail 'renewal state not recorded at capture'

echo 'PASS: live probe re-read only for a concurrent write; typed unchanged preflight refusal; snapshot cause kept; renewal state recorded'
