#!/bin/bash
# Exercise the real pre-quiesce capture without systemd or live service changes.
# Gerçek quiesce-öncesi kaydı systemd veya canlı servis değişikliği olmadan sınar.
set -euo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
case_root=$(mktemp -d)
trap 'rm -rf -- "$case_root"' EXIT
extract_function() {
    awk -v header="$1() {" '
        $0 == header { inside=1 }
        inside { print }
        inside && $0 == "}" { exit }
    ' "$ROOT/update.sh"
}
for fn in service_state_is_active_like coordinator_cgroup_matches_pid \
    wait_for_quiesce_cgroup_capture capture_quiesce_coordinator_identity; do
    eval "$(extract_function "$fn")"
done

run_case() {
    local scenario=$1 expected_status=$2 expected_reads=$3 actual_status
    local result="$case_root/$scenario"
    mkdir "$result"
    printf '0\n' > "$result/reads"
    : > "$result/sleeps"
    set +e
    (
        set -euo pipefail
        update_failure_detail=
        die() { printf '%s detail=%s\n' "$*" "$update_failure_detail" >&2; exit 41; }
        service_cgroup_pids() {
            local count
            count=$(<"$result/reads")
            count=$((count + 1))
            printf '%s\n' "$count" > "$result/reads"
            case "$scenario" in
                unreadable) return 1 ;;
                empty) return 0 ;;
                inactive) return 0 ;;
                transient)
                    printf '4242\n'
                    [[ "$count" -gt 3 ]] || printf '4343\n'
                    ;;
                clean) printf '4242\n' ;;
                *) printf '4242\n4343\n' ;;
            esac
        }
        systemctl() {
            local count
            count=$(<"$result/reads")
            case "$*" in
                'show --property=ActiveState --value celikpanel-agent.service')
                    if [[ "$scenario" == inactive ]]; then printf 'inactive\n'
                    elif [[ "$scenario" == state-changed && "$count" -ge 2 ]]; then printf 'reloading\n'
                    else printf 'active\n'; fi
                    ;;
                'show --property=MainPID --value celikpanel-agent.service')
                    if [[ "$scenario" == inactive ]]; then printf '0\n'
                    elif [[ "$scenario" == pid-changed && "$count" -ge 2 ]]; then printf '4444\n'
                    else printf '4242\n'; fi
                    ;;
                *) printf 'unexpected systemctl mutation: %s\n' "$*" >&2; exit 99 ;;
            esac
        }
        coordinator_process_start_time() {
            local count
            count=$(<"$result/reads")
            if [[ "$scenario" == start-changed && "$count" -ge 2 ]]; then printf '200\n'
            else printf '100\n'; fi
        }
        awk() {
            local count
            count=$(<"$result/reads")
            if [[ "$scenario" == frozen && "$count" -ge 2 ]]; then printf 'T\n'
            elif [[ "$scenario" == traced && "$count" -ge 2 ]]; then printf 't\n'
            elif [[ "$scenario" == vanished && "$count" -ge 2 ]]; then return 1
            else printf 'S\n'; fi
        }
        sleep() { printf '%s\n' "$*" >> "$result/sleeps"; }
        if [[ "$scenario" == inactive ]]; then
            capture_quiesce_coordinator_identity celikpanel-agent.service inactive
        else
            capture_quiesce_coordinator_identity celikpanel-agent.service active
        fi
    ) > "$result/stdout" 2> "$result/stderr"
    actual_status=$?
    set -e
    [[ "$actual_status" == "$expected_status" ]] || {
        cat "$result/stderr" >&2
        echo "$scenario: status $actual_status, expected $expected_status" >&2; exit 1;
    }
    [[ "$(<"$result/reads")" == "$expected_reads" ]] || {
        echo "$scenario: unexpected observation count" >&2; exit 1;
    }
    if [[ "$expected_status" == 0 ]]; then
        if [[ "$scenario" == inactive ]]; then
            printf 'celikpanel-agent.service\tinactive\t0\t0\n' | cmp -s - "$result/stdout"
        else
            printf 'celikpanel-agent.service\tactive\t4242\t100\n' | cmp -s - "$result/stdout"
        fi
    else
        [[ ! -s "$result/stdout" ]] || { echo "$scenario published an identity" >&2; exit 1; }
    fi
    case "$scenario" in
        persistent)
            grep -Fq 'expected_pid=4242 observed_pids=4242,4343' "$result/stderr"
            [[ "$(wc -l < "$result/sleeps")" == 19 ]]
            ;;
        unreadable)
            grep -Fq 'observed_pids=unavailable' "$result/stderr"
            [[ ! -s "$result/sleeps" ]]
            ;;
        clean|inactive) [[ ! -s "$result/sleeps" ]] ;;
    esac
    printf 'PASS: %s\n' "$scenario"
}
run_case clean 0 1
run_case transient 0 4
run_case persistent 41 20
run_case unreadable 41 1
run_case empty 41 20
run_case pid-changed 41 2
run_case start-changed 41 2
run_case state-changed 41 2
run_case frozen 41 2
run_case traced 41 2
run_case vanished 41 2
run_case inactive 0 1

# Frozen and inactive verification still uses the strict matcher, without waits.
# Donmuş ve pasif doğrulama beklemeden katı eşleştirmeyi kullanmaya devam eder.
service_cgroup_pids() { printf '4242\n4343\n'; }
if coordinator_cgroup_matches_pid celikpanel-agent.service 4242; then exit 1; fi
if coordinator_cgroup_matches_pid celikpanel-agent.service 0; then exit 1; fi
printf 'PASS: strict matcher rejects extra and residual processes\n'
echo 'update quiesce capture: ok'
