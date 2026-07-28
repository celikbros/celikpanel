#!/usr/bin/env bash
set -euo pipefail

candidate=${1:-/mnt/c/tmp/update-B67-quiesce-exit-hardened.sh}
[[ -f "$candidate" ]] || { echo "FAIL: missing candidate: $candidate" >&2; exit 1; }
bash -n "$candidate"
if LC_ALL=C grep -q $'\r' "$candidate"; then
    echo "FAIL: candidate contains CR bytes" >&2
    exit 1
fi

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

line_of() {
    local needle=$1
    grep -nF -- "$needle" "$candidate" | head -n1 | cut -d: -f1
}

line_of_last() {
    local needle=$1
    grep -nF -- "$needle" "$candidate" | tail -n1 | cut -d: -f1
}

line_after() {
    local start=$1 needle=$2
    awk -v start="$start" -v needle="$needle" \
        'NR > start && index($0, needle) { print NR; exit }' "$candidate"
}

assert_before() {
    local first=$1 second=$2 message=$3
    [[ -n "$first" && -n "$second" && "$first" -lt "$second" ]] || fail "$message"
}

flag_set=$(line_of 'printf -v "$flag_name" '\''%s'\'' 1')
sigstop=$(line_of 'if ! systemctl kill --kill-whom=all --signal=SIGSTOP "$unit"; then')
flag_reset=$(line_of 'printf -v "$flag_name" '\''%s'\'' 0')
assert_before "$flag_set" "$sigstop" "frozen flag must be published before SIGSTOP"
assert_before "$sigstop" "$flag_reset" "failed SIGSTOP must reset the frozen flag"

publish_phase=$(line_of 'transaction_phase=quiesce-publishing')
publish_failure=$(line_of '|| die "cannot publish quiesce update transaction marker"')
assert_before "$publish_phase" "$publish_failure" "local publishing phase must precede durable marker publication"

grep -Fq 'classify_durable_update_marker() {' "$candidate" \
    || fail "missing durable marker classifier"
grep -Fq 'marker_phase=$(classify_durable_update_marker)' "$candidate" \
    || fail "EXIT does not consult the durable marker classifier"
grep -Fq 'release_txn_validate_quiesce_token \' "$candidate" \
    || fail "quiesce classifier/abort lacks exact marker validation"
grep -Fq 'release_txn_validate_active_token \' "$candidate" \
    || fail "active classifier lacks exact marker validation"
grep -Fq 'release_txn_validate_pending_token \' "$candidate" \
    || fail "completion classifier lacks exact marker validation"

for helper in \
    capture_quiesce_coordinator_ledger \
    load_quiesce_coordinator_identities \
    verify_quiesce_coordinator_identity \
    verify_quiesce_coordinator_stopped; do
    grep -Fq "$helper() {" "$candidate" || fail "missing durable identity helper: $helper"
done
grep -Fq 'for unit in celikpanel-agent.service celikpanel-panel.service; do' "$candidate" \
    || fail "coordinator ledger is not emitted in canonical agent/panel order"
grep -Fq '"$size" -gt 0 && "$size" -le 1024' "$candidate" \
    || fail "coordinator ledger metadata bound is not guard-compatible"
grep -Fq '[[ "$state" == "$expected_state" ]]' "$candidate" \
    || fail "identity proof does not bind exact saved ActiveState"
grep -Fq '[[ "$main_pid" == "$expected_pid" ]]' "$candidate" \
    || fail "identity proof does not bind exact saved MainPID"
grep -Fq '[[ "$start_time" == "$expected_start" ]]' "$candidate" \
    || fail "identity proof does not bind exact saved /proc starttime"
grep -Fq 'coordinator_cgroup_matches_pid "$unit" "$main_pid"' "$candidate" \
    || fail "identity proof does not bind the exact one-PID cgroup"
grep -Fq '[[ "$expected_pid" == 0 && "$expected_start" == 0 && "$main_pid" == 0 ]]' "$candidate" \
    || fail "inactive identity is not canonical state/0/0"
grep -Fq 'transaction_phase=quiesce-failed' "$candidate" \
    || fail "quiesce abort has no fail-closed phase"
grep -Fq 'preserve_quiesce_recovery_marker' "$candidate" \
    || fail "quiesce abort failure does not preserve/recreate recovery marker"
grep -Fq 'stop_release_coordinators_fail_closed' "$candidate" \
    || fail "quiesce abort failure does not stop both coordinators"

capture_line=$(line_of 'capture_quiesce_coordinator_ledger "$tmp_snap/quiesce-coordinators.tsv"')
identity_load_line=$(line_after "$capture_line" 'load_quiesce_coordinator_identities "$tmp_snap/quiesce-coordinators.tsv"')
stage_validate_line=$(line_after "$identity_load_line" 'release_txn_validate_update_snapshot_stage "$SNAP_ROOT" "$snapshot_name" "$stage_root"')
stage_sync_line=$(line_after "$stage_validate_line" 'sync -f -- "$tmp_snap/service-states.tsv" "$tmp_snap/quiesce-coordinators.tsv"')
publishing_line=$(line_after "$stage_sync_line" 'transaction_phase=quiesce-publishing')
create_line=$(line_after "$publishing_line" 'release_txn_create_quiesce_marker \')
assert_before "$capture_line" "$identity_load_line" "captured coordinator identity must be loaded before publication"
assert_before "$identity_load_line" "$stage_validate_line" "identity ledger must precede stage validation"
assert_before "$stage_validate_line" "$stage_sync_line" "stage must validate before identity-ledger fsync"
assert_before "$stage_sync_line" "$publishing_line" "identity ledger must be durable before publication state"
assert_before "$publishing_line" "$create_line" "publishing state must precede quiesce marker creation"

assert_marker_call_shape() {
    local name=$1 kind=$2 expected_count=$3 line l1 l2 l3
    local validate1='"$RELEASE_TRANSACTION_ROOT" "$release_transaction_token" update "$snapshot_name" \'
    local mutate1='"$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \'
    local mutate2='"$release_transaction_token" update "$snapshot_name" \'
    local path_prefix='"$SNAP_ROOT" "$stage_root"'
    local -a lines=()
    mapfile -t lines < <(grep -nF -- "$name" "$candidate" | cut -d: -f1)
    [[ ${#lines[@]} -eq "$expected_count" ]] \
        || fail "$name call count changed: ${#lines[@]} != $expected_count"
    for line in "${lines[@]}"; do
        l1=$(sed -n "$((line + 1))p" "$candidate")
        l2=$(sed -n "$((line + 2))p" "$candidate")
        l3=$(sed -n "$((line + 3))p" "$candidate")
        l1=${l1#"${l1%%[![:space:]]*}"}
        l2=${l2#"${l2%%[![:space:]]*}"}
        l3=${l3#"${l3%%[![:space:]]*}"}
        if [[ "$kind" == validate ]]; then
            [[ "$l1" == "$validate1" && "$l2" == "$path_prefix"* ]] \
                || fail "$name does not use ROOT TOKEN OP SNAP SNAP_ROOT STAGE"
        else
            [[ "$l1" == "$mutate1" && "$l2" == "$mutate2" && "$l3" == "$path_prefix"* ]] \
                || fail "$name does not use ROOT FD TOKEN OP SNAP SNAP_ROOT STAGE"
        fi
    done
}

assert_marker_call_shape release_txn_validate_quiesce_token validate 5
assert_marker_call_shape release_txn_create_quiesce_marker mutate 2
assert_marker_call_shape release_txn_remove_quiesce_marker mutate 1
assert_marker_call_shape release_txn_promote_quiesce_to_active mutate 1

promotion_line=$(line_of_last 'release_txn_promote_quiesce_to_active \')
final_agent_freeze=$(awk -v end="$promotion_line" \
    'NR < end && index($0, "freeze_release_service_cgroup celikpanel-agent.service agent agent_frozen") { line=NR } END { print line+0 }' "$candidate")
panel_frozen_proof=$(awk -v start="$final_agent_freeze" -v end="$promotion_line" \
    'NR > start && NR < end && index($0, "verify_quiesce_coordinator_identity celikpanel-panel.service frozen") { print NR; exit }' "$candidate")
agent_frozen_proof=$(awk -v start="$final_agent_freeze" -v end="$promotion_line" \
    'NR > start && NR < end && index($0, "verify_quiesce_coordinator_identity celikpanel-agent.service frozen") { print NR; exit }' "$candidate")
final_quiesce_validate=$(awk -v start="$final_agent_freeze" -v end="$promotion_line" \
    'NR > start && NR < end && index($0, "release_txn_validate_quiesce_token") { print NR; exit }' "$candidate")
assert_before "$final_agent_freeze" "$panel_frozen_proof" "panel frozen proof must follow the final freeze"
assert_before "$panel_frozen_proof" "$agent_frozen_proof" "both frozen identity proofs must be ordered before promotion"
assert_before "$agent_frozen_proof" "$final_quiesce_validate" "frozen identities must be proven before marker validation"
assert_before "$final_quiesce_validate" "$promotion_line" "six-argument validation must precede seven-argument promotion"

grep -Fq 'recover_active_release_service() {' "$candidate" \
    || fail "active recovery helper is missing"
grep -Fq 'if verify_quiesce_coordinator_stopped "$unit"; then' "$candidate" \
    || fail "active recovery does not accept a proven already-stopped coordinator"
grep -Fq 'verify_quiesce_coordinator_identity "$unit" frozen' "$candidate" \
    || fail "active recovery does not require the exact captured frozen identity"
active_recovery_branch=$(line_of 'elif [[ "$transaction_phase" == active && $resume_active_update -eq 1 ]]; then')
active_recover_panel=$(line_after "$active_recovery_branch" 'recover_active_release_service celikpanel-panel.service panel panel_frozen')
active_recover_agent=$(line_after "$active_recover_panel" 'recover_active_release_service celikpanel-agent.service agent agent_frozen')
snapshot_publish=$(line_of 'mv -T --no-clobber -- "$tmp_snap" "$snap"')
assert_before "$active_recovery_branch" "$active_recover_panel" "active retry must classify panel without historical freeze"
assert_before "$active_recover_panel" "$active_recover_agent" "panel recovery must precede agent recovery"
assert_before "$active_recover_agent" "$snapshot_publish" "both coordinators must be recovered before snapshot publication"
active_retry_freezes=$(awk -v start="$active_recovery_branch" -v end="$active_recover_agent" \
    'NR > start && NR <= end && /freeze_release_service_cgroup/ { count++ } END { print count+0 }' "$candidate")
[[ "$active_retry_freezes" -eq 0 ]] || fail "active retry attempts to freeze a historical identity"

pending_start=$(line_of 'if [[ -e "$RELEASE_TRANSACTION_ROOT/completion.pending"')
pending_end=$(line_of 'resume_quiescing_update=0')
pending_prepare=$(awk -v s="$pending_start" -v e="$pending_end" \
    'NR >= s && NR < e && /prepare_runtime_mutation_lock_dir/ { print NR; exit }' "$candidate")
pending_first_idle=$(awk -v s="$pending_start" -v e="$pending_end" \
    'NR >= s && NR < e && /--check-service-mutation-idle/ { print NR; exit }' "$candidate")
assert_before "$pending_prepare" "$pending_first_idle" \
    "pending reboot path must prepare /run before its first mutation-lock use"

mapfile -t completion_removals < <(
    grep -nF 'release_txn_remove_completion_pending' "$candidate" | cut -d: -f1
)
[[ ${#completion_removals[@]} -eq 2 ]] \
    || fail "expected exactly pending and normal completion removals"
for removal in "${completion_removals[@]}"; do
    prior_auth=$(awk -v e="$removal" \
        'NR < e && /release_txn_remove_start_authorization/ { line=NR } END { print line+0 }' "$candidate")
    prior_unlock=$(awk -v e="$removal" \
        'NR < e && /release_release_mutation_lock/ { line=NR } END { print line+0 }' "$candidate")
    next_unlock=$(awk -v s="$removal" \
        'NR > s && /release_release_mutation_lock/ { print NR; exit }' "$candidate")
    locked_idle=$(awk -v s="$prior_auth" -v e="$removal" \
        'NR > s && NR < e && /--check-service-mutation-idle-under-external-lock/ { found=1 } END { print found+0 }' "$candidate")
    runtime_proof=$(awk -v s="$prior_auth" -v e="$removal" \
        'NR > s && NR < e && /verify_saved_runtime_states/ { found=1 } END { print found+0 }' "$candidate")
    enablement_proof=$(awk -v s="$prior_auth" -v e="$removal" \
        'NR > s && NR < e && /verify_saved_enablement/ { found=1 } END { print found+0 }' "$candidate")
    [[ "$prior_auth" -gt 0 && "$prior_unlock" -lt "$prior_auth" ]] \
        || fail "mutation lock is released between authorization removal and completion marker removal"
    [[ -n "$next_unlock" && "$next_unlock" -gt "$removal" ]] \
        || fail "completion marker must be removed before mutation lock release"
    [[ "$locked_idle" -eq 1 && "$runtime_proof" -eq 1 && "$enablement_proof" -eq 1 ]] \
        || fail "completion marker removal lacks lock-held runtime/enablement/idle proofs"
done

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

source <(extract_function service_state_is_active_like)
source <(extract_function coordinator_cgroup_matches_pid)
source <(extract_function verify_quiesce_coordinator_identity)
source <(extract_function verify_quiesce_coordinator_stopped)
source <(extract_function reject_extra_service_cgroup_processes)
source <(extract_function terminate_frozen_release_service)
source <(extract_function recover_active_release_service)
source <(extract_function resume_quiesced_release_services)
source <(extract_function verify_release_service_resumed)
source <(extract_function stop_release_coordinators_fail_closed)
source <(extract_function preserve_quiesce_recovery_marker)
source <(extract_function fail_closed_quiesce_abort)
source <(extract_function abort_quiesce_before_active)

declare -A saved_active_states=()
declare -A quiesce_active_states=()
declare -A quiesce_main_pids=()
declare -A quiesce_start_times=()
declare -A mock_state=()
declare -A mock_pid=()
declare -A mock_start=()
declare -A mock_process_state=()
declare -A mock_cgroup=()

mock_log="$tmp_root/mock.log"
RELEASE_TRANSACTION_ROOT="$tmp_root/transaction"
SNAP_ROOT="$tmp_root/snapshots"
stage_root="$SNAP_ROOT/.staging-update-test"
mkdir -m 0700 "$RELEASE_TRANSACTION_ROOT" "$SNAP_ROOT" "$stage_root"
RELEASE_TRANSACTION_FD=9
release_transaction_token=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
snapshot_name=20260728T000000Z-from-a-to-b-0123456789abcdef0123456789abcdef
preserve_staging=1
transaction_started=1
transaction_phase=quiesce
quiesce_abort_failed=0
panel_frozen=0
agent_frozen=0

die() {
    fail "$@"
}

reset_active_identities() {
    saved_active_states[celikpanel-panel.service]=active
    saved_active_states[celikpanel-agent.service]=active
    quiesce_active_states[celikpanel-panel.service]=active
    quiesce_active_states[celikpanel-agent.service]=active
    quiesce_main_pids[celikpanel-panel.service]=111
    quiesce_main_pids[celikpanel-agent.service]=222
    quiesce_start_times[celikpanel-panel.service]=1001
    quiesce_start_times[celikpanel-agent.service]=2002
    mock_state[celikpanel-panel.service]=active
    mock_state[celikpanel-agent.service]=active
    mock_pid[celikpanel-panel.service]=111
    mock_pid[celikpanel-agent.service]=222
    mock_start[celikpanel-panel.service]=1001
    mock_start[celikpanel-agent.service]=2002
    mock_process_state[celikpanel-panel.service]=T
    mock_process_state[celikpanel-agent.service]=T
    mock_cgroup[celikpanel-panel.service]=111
    mock_cgroup[celikpanel-agent.service]=222
}

reset_inactive_identities() {
    local unit
    for unit in celikpanel-panel.service celikpanel-agent.service; do
        saved_active_states[$unit]=inactive
        quiesce_active_states[$unit]=inactive
        quiesce_main_pids[$unit]=0
        quiesce_start_times[$unit]=0
        mock_state[$unit]=inactive
        mock_pid[$unit]=0
        mock_start[$unit]=0
        mock_process_state[$unit]=none
        mock_cgroup[$unit]=
    done
}

reset_abort_state() {
    : > "$mock_log"
    touch "$RELEASE_TRANSACTION_ROOT/quiesce.pending"
    preserve_staging=1
    transaction_started=1
    transaction_phase=quiesce
    quiesce_abort_failed=0
    panel_frozen=1
    agent_frozen=1
}

systemctl() {
    local unit
    printf 'systemctl %s\n' "$*" >> "$mock_log"
    case "$1" in
        show)
            unit=${!#}
            if [[ "$*" == *ActiveState* ]]; then
                printf '%s\n' "${mock_state[$unit]}"
            else
                printf '%s\n' "${mock_pid[$unit]}"
            fi
            ;;
        kill)
            unit=${!#}
            if [[ "$*" == *SIGCONT* ]] && service_state_is_active_like "${mock_state[$unit]}"; then
                mock_process_state[$unit]=S
            elif [[ "$*" == *SIGKILL* ]]; then
                mock_state[$unit]=inactive
                mock_pid[$unit]=0
                mock_start[$unit]=0
                mock_process_state[$unit]=none
                mock_cgroup[$unit]=
            fi
            ;;
        is-active)
            unit=${!#}
            service_state_is_active_like "${mock_state[$unit]}"
            ;;
    esac
}

awk() {
    local path=${!#} pid unit
    case "$path" in
        /proc/*/status)
            pid=${path#/proc/}
            pid=${pid%/status}
            for unit in celikpanel-panel.service celikpanel-agent.service; do
                if [[ "${mock_pid[$unit]}" == "$pid" ]]; then
                    printf '%s\n' "${mock_process_state[$unit]}"
                    return 0
                fi
            done
            return 1
            ;;
        *) command awk "$@" ;;
    esac
}

sleep() {
    :
}

coordinator_process_start_time() {
    local pid=$1 unit
    for unit in celikpanel-panel.service celikpanel-agent.service; do
        if [[ "${mock_pid[$unit]}" == "$pid" ]]; then
            printf '%s\n' "${mock_start[$unit]}"
            return 0
        fi
    done
    return 1
}

service_cgroup_pids() {
    local value=${mock_cgroup[$1]}
    [[ -z "$value" ]] || printf '%s\n' "$value"
}

assert_validate_quiesce_args() {
    [[ $# -eq 6 && "$1" == "$RELEASE_TRANSACTION_ROOT" &&
       "$2" == "$release_transaction_token" && "$3" == update &&
       "$4" == "$snapshot_name" && "$5" == "$SNAP_ROOT" && "$6" == "$stage_root" ]] \
        || fail "runtime quiesce validation arguments are not ROOT TOKEN OP SNAP SNAP_ROOT STAGE"
}

assert_mutate_quiesce_args() {
    [[ $# -eq 7 && "$1" == "$RELEASE_TRANSACTION_ROOT" && "$2" == "$RELEASE_TRANSACTION_FD" &&
       "$3" == "$release_transaction_token" && "$4" == update &&
       "$5" == "$snapshot_name" && "$6" == "$SNAP_ROOT" && "$7" == "$stage_root" ]] \
        || fail "runtime quiesce mutation arguments are not ROOT FD TOKEN OP SNAP SNAP_ROOT STAGE"
}

release_txn_validate_quiesce_token() {
    assert_validate_quiesce_args "$@"
    printf 'validate-quiesce\n' >> "$mock_log"
    [[ -f "$RELEASE_TRANSACTION_ROOT/quiesce.pending" ]]
}

release_txn_remove_quiesce_marker() {
    assert_mutate_quiesce_args "$@"
    printf 'remove-quiesce\n' >> "$mock_log"
    rm -f -- "$RELEASE_TRANSACTION_ROOT/quiesce.pending"
}

release_txn_create_quiesce_marker() {
    assert_mutate_quiesce_args "$@"
    printf 'create-quiesce\n' >> "$mock_log"
    touch "$RELEASE_TRANSACTION_ROOT/quiesce.pending"
}

release_release_mutation_lock() {
    printf 'unlock\n' >> "$mock_log"
}

assert_fail_closed_abort() {
    [[ -e "$RELEASE_TRANSACTION_ROOT/quiesce.pending" ]] \
        || fail "failed abort removed the recovery marker"
    [[ "$preserve_staging" -eq 1 && "$transaction_started" -eq 1 &&
       "$transaction_phase" == quiesce-failed && "$quiesce_abort_failed" -eq 1 ]] \
        || fail "failed abort did not preserve fail-closed local state"
    grep -Fq 'stop --no-block celikpanel-panel.service' "$mock_log" \
        || fail "failed abort did not stop panel"
    grep -Fq 'stop --no-block celikpanel-agent.service' "$mock_log" \
        || fail "failed abort did not stop agent"
    ! grep -Fq 'remove-quiesce' "$mock_log" \
        || fail "failed abort removed the exact recovery marker"
}

# Exact active identity proof covers saved state, PID, /proc starttime and sole cgroup.
# Tam aktif kimlik kanıtı; kaydedilmiş durumu, PID'yi, /proc başlangıç zamanını ve tek cgroup'u kapsar.
reset_active_identities
: > "$mock_log"
verify_quiesce_coordinator_identity celikpanel-panel.service frozen \
    || fail "exact saved active identity was rejected"
mock_state[celikpanel-panel.service]=reloading
if verify_quiesce_coordinator_identity celikpanel-panel.service frozen; then
    fail "replacement ActiveState passed exact identity proof"
fi
mock_state[celikpanel-panel.service]=active
mock_pid[celikpanel-panel.service]=333
if verify_quiesce_coordinator_identity celikpanel-panel.service frozen; then
    fail "replacement PID passed exact identity proof"
fi
mock_pid[celikpanel-panel.service]=111
mock_start[celikpanel-panel.service]=9999
if verify_quiesce_coordinator_identity celikpanel-panel.service frozen; then
    fail "replacement starttime passed exact identity proof"
fi
mock_start[celikpanel-panel.service]=1001
mock_cgroup[celikpanel-panel.service]=$'111\n333'
if verify_quiesce_coordinator_identity celikpanel-panel.service frozen; then
    fail "extra cgroup PID passed exact identity proof"
fi
mock_cgroup[celikpanel-panel.service]=111

# Active-like rows receive exactly one CONT each, only after exact identity proof.
# Aktif-benzeri satırların her biri, yalnız tam kimlik kanıtından sonra tek bir CONT alır.
: > "$mock_log"
mock_process_state[celikpanel-panel.service]=T
mock_process_state[celikpanel-agent.service]=T
resume_quiesced_release_services || fail "exact active identities could not be resumed"
[[ $(grep -cF 'SIGCONT celikpanel-panel.service' "$mock_log") -eq 1 ]] \
    || fail "panel did not receive exactly one CONT"
[[ $(grep -cF 'SIGCONT celikpanel-agent.service' "$mock_log") -eq 1 ]] \
    || fail "agent did not receive exactly one CONT"
[[ $(grep -cF 'SIGCONT' "$mock_log") -eq 2 ]] \
    || fail "CONT was sent outside the two captured active identities"

# Canonical inactive state/0/0 rows prove an empty cgroup and produce no signal.
# Kanonik pasif state/0/0 satırları boş cgroup'u kanıtlar ve hiçbir sinyal üretmez.
reset_inactive_identities
: > "$mock_log"
verify_quiesce_coordinator_identity celikpanel-panel.service unfrozen \
    || fail "canonical inactive state/0/0 identity was rejected"
resume_quiesced_release_services || fail "canonical inactive identities could not be resumed as no-ops"
! grep -Fq 'SIGCONT' "$mock_log" || fail "inactive state/0/0 identity received a signal command"
quiesce_main_pids[celikpanel-panel.service]=111
if verify_quiesce_coordinator_identity celikpanel-panel.service unfrozen; then
    fail "noncanonical inactive PID passed identity proof"
fi

# Successful abort resumes both exact active identities before marker removal.
# Başarılı iptal, marker kaldırılmadan önce iki tam aktif kimliği sürdürür.
reset_active_identities
reset_abort_state
abort_quiesce_before_active
[[ ! -e "$RELEASE_TRANSACTION_ROOT/quiesce.pending" ]] \
    || fail "successful abort left quiesce marker"
[[ "$transaction_started" -eq 0 && "$preserve_staging" -eq 0 && "$transaction_phase" == none ]] \
    || fail "successful abort did not reach terminal local state"
panel_cont=$(grep -nF 'SIGCONT celikpanel-panel.service' "$mock_log" | head -n1 | cut -d: -f1)
agent_cont=$(grep -nF 'SIGCONT celikpanel-agent.service' "$mock_log" | head -n1 | cut -d: -f1)
remove_line=$(grep -nF 'remove-quiesce' "$mock_log" | head -n1 | cut -d: -f1)
assert_before "$panel_cont" "$remove_line" "panel must be resumed before marker removal"
assert_before "$agent_cont" "$remove_line" "agent must be resumed before marker removal"

# A replacement PID fails before CONT, preserves/recreates the marker and stops both.
# Değiştirilmiş PID, CONT öncesinde başarısız olur; marker'ı korur/yeniden kurar ve ikisini de durdurur.
reset_active_identities
reset_abort_state
mock_pid[celikpanel-panel.service]=333
mock_start[celikpanel-panel.service]=3003
mock_cgroup[celikpanel-panel.service]=333
if abort_quiesce_before_active; then
    fail "replacement PID unexpectedly aborted quiesce successfully"
fi
assert_fail_closed_abort
! grep -Fq 'SIGCONT' "$mock_log" || fail "replacement PID was signalled before fail-closed stop"

# PID reuse with a different /proc starttime is equally fail-closed.
# Farklı /proc başlangıç zamanına sahip yeniden kullanılan PID de aynı şekilde güvenli kapalı kalır.
reset_active_identities
reset_abort_state
mock_start[celikpanel-panel.service]=9999
if abort_quiesce_before_active; then
    fail "replacement starttime unexpectedly aborted quiesce successfully"
fi
assert_fail_closed_abort
! grep -Fq 'SIGCONT' "$mock_log" || fail "replacement starttime was signalled before fail-closed stop"

# Simulate a crash after only the panel was terminated in the active phase.
# Active aşamada yalnız panel sonlandırıldıktan sonraki çökmeyi benzet.
reset_active_identities
: > "$mock_log"
touch "$RELEASE_TRANSACTION_ROOT/active"
rm -f -- "$RELEASE_TRANSACTION_ROOT/quiesce.pending"
panel_frozen=0
agent_frozen=0
recover_active_release_service celikpanel-panel.service panel panel_frozen
[[ "${mock_state[celikpanel-panel.service]}" == inactive &&
   "${mock_state[celikpanel-agent.service]}" == active ]] \
    || fail "first active-recovery termination did not leave the expected crash boundary"
[[ -e "$RELEASE_TRANSACTION_ROOT/active" && ! -e "$SNAP_ROOT/$snapshot_name" ]] \
    || fail "first-termination crash boundary lost the active marker or published a snapshot"
! grep -Fq 'SIGSTOP' "$mock_log" || fail "active recovery attempted to freeze a historical PID"
! grep -Fq 'SIGCONT' "$mock_log" || fail "active recovery attempted to resume a historical PID"

# A retry accepts the already-stopped panel, then terminates the exact frozen agent.
# Yeniden deneme durmuş paneli kabul eder, ardından tam askıdaki agent'ı sonlandırır.
: > "$mock_log"
panel_frozen=0
agent_frozen=0
recover_active_release_service celikpanel-panel.service panel panel_frozen
recover_active_release_service celikpanel-agent.service agent agent_frozen
[[ "${mock_state[celikpanel-panel.service]}" == inactive &&
   "${mock_state[celikpanel-agent.service]}" == inactive ]] \
    || fail "active retry did not prove both coordinators stopped"
! grep -Fq 'SIGKILL celikpanel-panel.service' "$mock_log" \
    || fail "already-stopped panel was signalled during active retry"
grep -Fq 'SIGKILL celikpanel-agent.service' "$mock_log" \
    || fail "exact frozen agent was not terminated during active retry"
[[ -e "$RELEASE_TRANSACTION_ROOT/active" && ! -e "$SNAP_ROOT/$snapshot_name" ]] \
    || fail "second-termination crash boundary lost the active marker or published a snapshot"

# A crash after both terminations is idempotent: the next retry emits no signal.
# İki sonlandırmadan sonraki çökme idempotenttir: sonraki deneme sinyal üretmez.
: > "$mock_log"
panel_frozen=0
agent_frozen=0
recover_active_release_service celikpanel-panel.service panel panel_frozen
recover_active_release_service celikpanel-agent.service agent agent_frozen
! grep -Fq 'systemctl kill' "$mock_log" \
    || fail "already-stopped coordinators were signalled after the second crash boundary"
[[ -e "$RELEASE_TRANSACTION_ROOT/active" && ! -e "$SNAP_ROOT/$snapshot_name" ]] \
    || fail "idempotent active retry changed the durable crash boundary"

echo "PASS: update quiesce/EXIT durable identity contract"
