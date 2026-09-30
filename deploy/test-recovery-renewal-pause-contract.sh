#!/usr/bin/env bash
# F2 (upd3): Certbot renewal must not stay stopped while a forward completion
# waits for its owner. Exercise the runner's real post-failure functions with
# stand-ins for the Certbot helpers only; no systemctl, lock or host state.
# Yenileme, ileri tamamlama sahibini beklerken durdurulmuş kalmamalıdır.
set -euo pipefail
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
runner=$repo_root/deploy/release-recovery-runner.sh
updater=$repo_root/update.sh
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
extract() {
    awk -v header="$2() {" '$0 == header { inside=1 } inside { print } inside && $0 == "}" { exit }' "$1"
}
for name in restore_renewal_after_final_attempt after_failed_recovery_attempt; do
    source <(extract "$runner" "$name")
    declare -F "$name" >/dev/null || fail "runner function $name is missing"
done
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
RECOVERY_SNAPSHOT_DIR=/fixture/snapshot
panel_tls_certbot_scheduler_matches_snapshot() { echo "match $1" >> "$work/trace"; [[ $(cat "$work/matches") == yes ]]; }
panel_tls_restore_certbot_scheduler() { echo "restore $1" >> "$work/trace"; [[ $(cat "$work/restore") == ok ]]; }

run_case() {
    local operation=$1 phase=$2 attempt=$3 owner=$4 matches=$5 restore=$6
    : > "$work/trace"
    printf '%s\n' "$matches" > "$work/matches"
    printf '%s\n' "$restore" > "$work/restore"
    (
        MARKER_OPERATION=$operation TRANSACTION_PHASE=$phase DISPATCH_ATTEMPT=$attempt
        OWNER_RETRY_SNAPSHOT=$owner RECOVERY_RETRY_SCHEDULED=0
        after_failed_recovery_attempt
        printf 'scheduled=%s\n' "$RECOVERY_RETRY_SCHEDULED" >> "$work/trace"
    ) 2> "$work/stderr"
}

# Attempts left: never touch renewal; schedule the automatic retry hint.
for attempt in 1 2; do
    run_case update completion "$attempt" '' no ok
    [[ $(cat "$work/trace") == 'scheduled=1' ]] || fail "attempt $attempt changed renewal or lost the retry hint"
    grep -F "Automatic recovery attempt $attempt of 3 did not finish" "$work/stderr" >/dev/null || fail 'attempt guidance missing'
done
# An owner retry never claims another automatic attempt.
run_case update completion 2 fixture-snapshot no ok
[[ $(cat "$work/trace") == 'scheduled=0' ]] || fail 'owner retry scheduled an automatic attempt'

# Last attempt of a forward completion: restore exactly once, from the snapshot.
for phase in completion completion-scheduler scheduler; do
    for attempt in 3 owner; do
        run_case update "$phase" "$attempt" '' no ok
        [[ $(cat "$work/trace") == $'match /fixture/snapshot/panel-tls\nrestore /fixture/snapshot/panel-tls\nscheduled=0' ]] ||
            fail "forward $phase/$attempt did not restore renewal: $(cat "$work/trace")"
        grep -F 'was returned to its state from before the update' "$work/stderr" >/dev/null || fail 'restore not explained'
    done
done
# Already in its recorded state (for example a timer that was disabled before
# the update): nothing is changed.
run_case update completion 3 '' yes ok
[[ $(cat "$work/trace") == $'match /fixture/snapshot/panel-tls\nscheduled=0' ]] || fail 'matching scheduler was changed'
grep -F 'is already in its state from before the update' "$work/stderr" >/dev/null || fail 'matching state not explained'
# A refused restore (for example a later owner change) is reported, not forced.
run_case update completion 3 '' no refused
[[ $(cat "$work/trace") == $'match /fixture/snapshot/panel-tls\nrestore /fixture/snapshot/panel-tls\nscheduled=0' ]] || fail 'refused restore retried'
grep -F 'could not be returned to its state from before the update' "$work/stderr" >/dev/null || fail 'refused restore not explained'

# Rollback directions restore the panel certificate files from the snapshot;
# renewal stays paused and the journal says why.
for pair in update:active rollback:active rollback:completion rollback:completion-scheduler rollback:scheduler; do
    run_case "${pair%%:*}" "${pair#*:}" 3 '' no ok
    [[ $(cat "$work/trace") == $'match /fixture/snapshot/panel-tls\nscheduled=0' ]] || fail "$pair touched renewal"
    grep -F 'stays paused until this operation is retried and finishes' "$work/stderr" >/dev/null || fail "$pair pause not explained"
    run_case "${pair%%:*}" "${pair#*:}" owner fixture-snapshot yes ok
    grep -F 'is already in its state from before the update' "$work/stderr" >/dev/null || fail "$pair unpaused renewal misreported"
done
# Before the active marker the scheduler was never stopped: nothing to say.
run_case update quiesce 3 '' no ok
[[ $(cat "$work/trace") == 'scheduled=0' && ! -s $work/stderr ]] || fail 'quiesce phase reported or touched renewal'

# Historical recovery code without the helpers never guesses.
unset -f panel_tls_certbot_scheduler_matches_snapshot
run_case update completion 3 '' no ok
[[ $(cat "$work/trace") == 'scheduled=0' ]] || fail 'restored without the reviewed helper'
grep -F 'this recovery code cannot restore it' "$work/stderr" >/dev/null || fail 'missing helper not explained'

# Wiring: the runner restores after the child failed and before releasing the
# lock; the forward retry pauses renewal again before it stops or starts any
# coordinator.
awk '/^if \[\[ \$child_status -ne 0 \]\]; then$/ { getline a; getline b; print a; print b; exit }' "$runner" > "$work/branch"
[[ $(cat "$work/branch") == $'    after_failed_recovery_attempt\n    release_transaction_lock' ]] || fail 'post-failure hook is not bound before the lock release'
grep -Fx 'systemctl() { "$SYSTEMCTL_BIN" "$@"; }' "$runner" >/dev/null || fail 'Certbot helpers are not bound to the exact systemctl'
line_of() { grep -nF -- "$2" "$1" | head -1 | cut -d: -f1; }
validate=$(line_of "$updater" 'validate_pending_update_snapshot "$pending_snapshot"')
repause=$(awk -v from="$validate" 'NR > from && index($0, "panel_tls_quiesce_certbot_scheduler \"$pending_snapshot_path/panel-tls\"") { print NR; exit }' "$updater")
first_stop=$(awk -v from="$validate" 'NR > from && /systemctl stop celikpanel-panel.service/ { print NR; exit }' "$updater")
[[ -n $repause && $((repause - validate)) -le 8 ]] || fail 'renewal is not paused again directly after the pending snapshot is validated'
[[ -n $validate && -n $repause && -n $first_stop && $validate -lt $repause && $repause -lt $first_stop ]] ||
    fail 'forward retry does not pause renewal again before stopping coordinators'

echo 'PASS: renewal restored once after a failed final forward attempt, kept paused for rollbacks, re-paused by the retry'
