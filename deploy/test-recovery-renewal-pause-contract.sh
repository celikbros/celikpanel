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
        OWNER_RETRY_SNAPSHOT=$owner RECOVERY_RETRY_SCHEDULED=0 RECOVERY_PAUSE_PENDING=0
        DISPATCH_AUTOMATIC_USED=${RUN_AUTOMATIC_USED:-3}
        after_failed_recovery_attempt
        printf 'scheduled=%s\n' "$RECOVERY_RETRY_SCHEDULED" >> "$work/trace"
        printf '%s\n' "$RECOVERY_PAUSE_PENDING" > "$work/pending"
    ) 2> "$work/stderr"
}
pending_line='The last admitted recovery attempt did not finish. The next run of the recovery timer records the pause and prints the one-time retry command for this operation.'

# Attempts left: never touch renewal; schedule the automatic retry hint.
for attempt in 1 2; do
    run_case update completion "$attempt" '' no ok
    [[ $(cat "$work/trace") == 'scheduled=1' ]] || fail "attempt $attempt changed renewal or lost the retry hint"
    grep -F "Automatic recovery attempt $attempt of 3 did not finish" "$work/stderr" >/dev/null || fail 'attempt guidance missing'
    [[ $(cat "$work/pending") == 0 ]] || fail "attempt $attempt claimed a pending pause"
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
        # upd4 F6: the last admitted attempt records that the pause follows.
        [[ $(cat "$work/pending") == 1 ]] || fail "forward $phase/$attempt did not mark the pending pause"
        grep -Fx "$pending_line" "$work/stderr" >/dev/null || fail 'pending pause not explained'
    done
done
# Candidate review N1: an owner retry admitted before the automatic budget was
# used up leaves the remaining automatic attempts to the timer: the normal
# retry hint, no pending pause and no renewal change.
for phase in completion active; do
    for used in 0 1 2; do
        RUN_AUTOMATIC_USED=$used run_case update "$phase" owner fixture-snapshot no ok
        [[ $(cat "$work/trace") == 'scheduled=1' && $(cat "$work/pending") == 0 ]] ||
            fail "early owner retry ($phase, $used used) paused or touched renewal: $(cat "$work/trace")"
        grep -F "did not finish; $((3 - used)) of 3 automatic attempts remain" "$work/stderr" >/dev/null ||
            fail 'early owner retry not explained'
        ! grep -Fx "$pending_line" "$work/stderr" >/dev/null || fail 'early owner retry announced a pause'
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
# Before the active marker the scheduler was never stopped: nothing to say
# about renewal; the pending pause is still recorded and explained.
run_case update quiesce 3 '' no ok
[[ $(cat "$work/trace") == 'scheduled=0' && $(cat "$work/pending") == 1 && $(grep -vxF "$pending_line" "$work/stderr" | grep -v '^İzin verilen son kurtarma denemesi' || true) == '' ]] ||
    fail 'quiesce phase reported or touched renewal'

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

# upd4 F6: the exit hook records pause_pending for the last admitted attempt and
# falls back to the plain failure when an older observer refuses the hint.
source <(extract "$runner" recovery_observation_exit)
declare -F recovery_observation_exit >/dev/null || fail 'runner observation exit hook is missing'
publish_case() {
    local scheduled=$1 pending=$2 refuse_hints=$3
    : > "$work/published"
    (
        RECOVERY_OBSERVATION_REQUEST=44444444444444444444444444444444
        RECOVERY_OBSERVATION_COMMIT=3333333333333333333333333333333333333333
        RECOVERY_RETRY_SCHEDULED=$scheduled RECOVERY_PAUSE_PENDING=$pending
        release_observation_publish() {
            printf '%s|%s\n' "$*" "${7:-}" >> "$work/published"
            [[ -z ${7:-} || $refuse_hints == no ]]
        }
        false || recovery_observation_exit
    ) 2>/dev/null || true
}
plain='44444444444444444444444444444444 3333333333333333333333333333333333333333 recovery_required none recovery_failed|'
publish_case 0 1 no
[[ $(cat "$work/published") == '44444444444444444444444444444444 3333333333333333333333333333333333333333 recovery_required none recovery_failed  pause_pending|pause_pending' ]] ||
    fail "pause_pending not published: $(cat "$work/published")"
publish_case 1 0 no
[[ $(cat "$work/published") == *'|retry_scheduled' ]] || fail 'retry_scheduled hint lost'
publish_case 0 0 no
[[ $(cat "$work/published") == "$plain" ]] || fail "plain failure changed: $(cat "$work/published")"
# An older observer library refuses the new hint: the failure is still recorded.
publish_case 0 1 yes
[[ $(cat "$work/published") == *$'|pause_pending\n'"$plain" ]] || fail "refused hint lost the failure: $(cat "$work/published")"
grep -Fq 'recovery_required none recovery_failed "" pause_pending' "$runner" || fail 'runner does not publish pause_pending'

# Candidate review F3: a child that fails and leaves no marker ended the
# operation itself. A quiesce recovery does this by design (abort, exit 1).
# Neither the retry hint, the pending pause nor a renewal restore follows; the
# aborted update is recorded as failed (it ended before any change); any other
# phase leaves the exit hook's plain recovery failure.
source <(extract "$runner" end_after_failed_child_without_marker)
declare -F end_after_failed_child_without_marker >/dev/null || fail 'runner no-marker end is missing'
no_marker_case() {
    local operation=$1 phase=$2 bound=$3
    : > "$work/published"
    (
        MARKER_OPERATION=$operation TRANSACTION_PHASE=$phase child_status=1
        RECOVERY_OBSERVATION_REQUEST= RECOVERY_OBSERVATION_COMMIT=
        if [[ $bound == yes ]]; then
            RECOVERY_OBSERVATION_REQUEST=44444444444444444444444444444444
            RECOVERY_OBSERVATION_COMMIT=3333333333333333333333333333333333333333
        fi
        RECOVERY_RETRY_SCHEDULED=0 RECOVERY_PAUSE_PENDING=0
        release_observation_publish() { printf '%s|%s\n' "$*" "${7:-}" >> "$work/published"; }
        release_transaction_lock() { printf 'lock-released\n' >> "$work/published"; }
        after_failed_recovery_attempt() { printf 'retry-handling\n' >> "$work/published"; }
        restore_renewal_after_final_attempt() { printf 'renewal\n' >> "$work/published"; }
        die() { printf '!! %s\n' "$*" >&2; exit 1; }
        trap 'printf "exit-hook scheduled=%s pending=%s\n" "$RECOVERY_RETRY_SCHEDULED" "$RECOVERY_PAUSE_PENDING" >> "$work/published"' EXIT
        end_after_failed_child_without_marker
        printf 'continued\n' >> "$work/published"
    ) 2> "$work/stderr" && fail 'no-marker end returned success'
    return 0
}
no_marker_case update quiesce yes
[[ $(cat "$work/published") == $'44444444444444444444444444444444 3333333333333333333333333333333333333333 failed none update_failed|\nlock-released' ]] ||
    fail "aborted quiesce not recorded as an update that ended before any change: $(cat "$work/published")"
grep -F 'stopped before the installed release or its data changed' "$work/stderr" >/dev/null &&
    grep -F 'kurulu sürüm veya verileri değişmeden durduruldu' "$work/stderr" >/dev/null &&
    grep -F 'left no pending transaction' "$work/stderr" >/dev/null || fail 'aborted quiesce not explained'
no_marker_case update quiesce no
[[ $(cat "$work/published") == 'lock-released' ]] || fail "unbound quiesce end published: $(cat "$work/published")"
for pair in update:active update:completion rollback:active; do
    no_marker_case "${pair%%:*}" "${pair#*:}" yes
    [[ $(cat "$work/published") == $'lock-released\nexit-hook scheduled=0 pending=0' ]] ||
        fail "$pair no-marker end published a hint, restored renewal or skipped the plain failure: $(cat "$work/published")"
    grep -F 'Its result is not verified' "$work/stderr" >/dev/null || fail "$pair no-marker end not explained"
done
# Wiring: the no-marker end runs right after the child's reproof and before the
# retry handling, which it never reaches.
awk '/^if \[\[ \$child_status -ne 0 \]\] && ! markers_still_present; then$/ { print; getline; print; getline; print; getline; print; exit }' "$runner" > "$work/branch"
[[ $(cat "$work/branch") == $'if [[ $child_status -ne 0 ]] && ! markers_still_present; then\n    end_after_failed_child_without_marker\nfi\nif [[ $child_status -ne 0 ]]; then' ]] ||
    fail "no-marker end is not wired before the retry handling: $(cat "$work/branch")"

echo 'PASS: renewal restored once after a failed final forward attempt, kept paused for rollbacks, re-paused by the retry; pause_pending recorded until the pause; early owner retry keeps the normal retry; a failed child without a marker ends without retry hints'
