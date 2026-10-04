#!/usr/bin/env bash
# Candidate panel start boundaries in the real updater functions and main-flow
# blocks, with private process doubles. No systemd, installer, lock or host
# state is touched. Native acceptance (a candidate whose panel cannot start)
# is separate evidence and is not established by this contract.
# Gerçek güncelleyici işlevleri ve ana akış blokları özel süreç ikizleriyle
# sınanır; yerel kabul ayrı kanıttır.
set -euo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
UPDATE=$ROOT/update.sh
TEST_ROOT=$(mktemp -d)
trap 'rm -rf -- "$TEST_ROOT"' EXIT
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
extract_function() {
    awk -v header="$1() {" '$0 == header { inside=1 } inside { print } inside && $0 == "}" { exit }' "$UPDATE"
}
line_of() {
    local number
    number=$(grep -nF -- "$1" "$UPDATE" | sed -n "${2:-1}p" | cut -d: -f1)
    [[ -n $number ]] || fail "missing updater line: $1"
    printf '%s\n' "$number"
}

# --- Structure: the start check precedes completion.pending; the start wait
# replaces the single is-active check in both controlled-start paths.
check_call=$(grep -n '^    run_panel_startup_readiness_check$' "$UPDATE" | cut -d: -f1)
[[ $(grep -c '^    run_panel_startup_readiness_check$' "$UPDATE") == 1 ]] || fail 'start check must be called exactly once'
publication=$(grep -n '^    verify_database_publication_if_required "\$snapshot_name"$' "$UPDATE" | head -1 | cut -d: -f1)
completion=$(grep -n '^release_txn_mark_completion_pending \\$' "$UPDATE" | head -1 | cut -d: -f1)
[[ -n $check_call && -n $publication && -n $completion && $publication -lt $check_call && $check_call -lt $completion ]] \
    || fail "start check is not between database publication ($publication) and completion.pending ($completion): $check_call"
[[ $(sed -n "$((check_call + 1))p" "$UPDATE") == fi ]] || fail 'start check must stay inside the isolated database branch'
! grep -qF 'systemctl is-active --quiet celikpanel-panel.service || die "verified panel is not running"' "$UPDATE" \
    || fail 'single is-active check remains in the main flow'
! grep -qF 'pending update panel is not active' "$UPDATE" || fail 'single is-active check remains in the pending path'
start=$(line_of 'systemctl start celikpanel-panel.service || die "verified panel could not be started"')
code_line=$(line_of '    update_failure_code=panel_start_unverified')
wait_line=$(line_of 'wait_for_stable_panel_start "$panel_startup_pins" \')
[[ $code_line -lt $start && $start -lt $wait_line && $completion -lt $start ]] || fail 'main-flow start wait order'
pending_start=$(line_of 'systemctl start celikpanel-panel.service \')
pending_wait=$(line_of 'wait_for_stable_panel_start "" \')
[[ $pending_start -lt $pending_wait ]] || fail 'pending-path start wait order'
[[ $(grep -c "Roll back if needed" "$UPDATE") == 1 ]] || fail 'rollback hint must exist only inside the guarded function'
final=$(line_of 'print_completed_update_snapshot_guidance "$snap" "$snapshot_name"')
[[ $final -gt $(line_of '==> Update complete / Güncelleme tamamlandı') ]] || fail 'final guidance order'

# The stability window must outlast one restart cycle of the shipped unit.
eval "$(grep -E '^PANEL_START_(STABLE_SAMPLES|WAIT_SECONDS|MAX_SAMPLES)=[0-9]+$' "$UPDATE")"
restart_sec=$(sed -n 's/^RestartSec=\([0-9][0-9]*\)$/\1/p' "$ROOT/deploy/systemd/celikpanel-panel.service")
[[ $restart_sec =~ ^[0-9]+$ ]] || fail 'panel unit RestartSec is not a plain number of seconds'
(( (PANEL_START_STABLE_SAMPLES - 1) * 5 > restart_sec * 10 )) || fail 'stability window does not exceed RestartSec'
(( PANEL_START_WAIT_SECONDS <= 60 && PANEL_START_MAX_SAMPLES <= 120 )) || fail 'start wait is not bounded as documented'

# --- Behaviour of the real functions.
for name in die report_update_failure publish_update_failure_observation on_exit \
    panel_startup_env_key panel_startup_environment run_panel_startup_readiness_check \
    panel_probe_target panel_http_probe wait_for_stable_panel_start \
    print_completed_update_snapshot_guidance; do
    extract_function "$name" > "$TEST_ROOT/$name.sh"
    [[ -s $TEST_ROOT/$name.sh ]] || fail "missing function $name"
    # shellcheck disable=SC1090
    source "$TEST_ROOT/$name.sh"
done
sed -n '/^# New normal updates remain active through isolated migration/,/^    || die "cannot mark update completion pending"$/p' "$UPDATE" > "$TEST_ROOT/active-tail.sh"
grep -q 'run_panel_startup_readiness_check' "$TEST_ROOT/active-tail.sh" || fail 'active tail extraction'
awk '/^if service_state_is_active_like "\$\{saved_active_states\[celikpanel-panel.service\]\}"; then$/ { inside=1 } inside { print } inside && $0 == "fi" { exit }' \
    "$UPDATE" > "$TEST_ROOT/panel-start.sh"
grep -q 'wait_for_stable_panel_start "\$panel_startup_pins"' "$TEST_ROOT/panel-start.sh" || fail 'panel start extraction'

FIXTURE_COMMIT=$(printf 'a%.0s' {1..40})
FIXTURE_REQUEST=$(printf 'd%.0s' {1..32})
PIN_A="sha256//$(printf 'A%.0s' {1..43})="
PIN_B="sha256//$(printf 'B%.0s' {1..43})="
mkdir -p "$TEST_ROOT/bin"
write_panel() {
    local body=$1
    cat > "$TEST_ROOT/bin/panel" <<SH
#!/usr/bin/env bash
[[ \$# == 1 && \$1 == --check-startup-readiness ]] || exit 90
env | grep '^CELIKPANEL_' | sort > '$TEST_ROOT/panel.env.seen'
$body
SH
    chmod 0755 "$TEST_ROOT/bin/panel"
}
common_doubles() {
    BIN_DIR=$TEST_ROOT/bin
    PANEL_ENV_FILE=$TEST_ROOT/absent-panel.env
    snapshot_name=20260930T000000Z-from-unknown-to-$FIXTURE_COMMIT-$(printf 'b%.0s' {1..32})
    RELEASE_TRANSACTION_ROOT=$TEST_ROOT/transaction RELEASE_TRANSACTION_FD=9 release_transaction_token=fixture-token
    mutation_started=1 transaction_started=1 quiesce_abort_failed=0 transaction_phase=none
    transaction_completion_verified=0 scheduler_restore_verified=0
    update_failure_reason= update_failure_detail= update_failure_code=
    panel_startup_listen= panel_startup_scheme= panel_startup_pins=
    sudo() {
        [[ $1 == -u && $2 == celikpanel && $3 == -- && $4 == env && $5 == -i ]] || exit 94
        shift 3
        "$@"
    }
    systemctl() {
        if [[ $* == 'show --property=Environment --value celikpanel-panel.service' ]]; then
            printf '%s\n' "${UNIT_ENVIRONMENT:-CELIKPANEL_DATA_DIR=/var/lib/celikpanel CELIKPANEL_WEB_DIR=/opt/celikpanel/web CELIKPANEL_LISTEN=:2083 CELIKPANEL_AGENT_SOCKET=/run/celikpanel/agent.sock CELIKPANEL_AGENT_TOKEN_FILE=/etc/celikpanel/agent.token CELIKPANEL_TLS=1 CELIKPANEL_PANEL_INSECURE_COOKIES_FLAG= CELIKPANEL_PANEL_DEMO_FLAG=}"
            return 0
        fi
        exit 93
    }
    run_panel_migrations_offline() { printf 'migrate\n' >> "$TRACE"; }
    verify_installed_release_artifacts() { printf 'verify-artifacts\n' >> "$TRACE"; }
    verify_database_publication_if_required() { printf 'verify-database\n' >> "$TRACE"; }
    release_txn_mark_completion_pending() {
        [[ $# == 5 && $4 == update && $5 == "$snapshot_name" ]] || exit 98
        printf 'completion-marker\n' >> "$TRACE"
    }
    classify_durable_update_marker() {
        if grep -qx 'completion-marker' "$TRACE"; then printf 'completion\n'; else printf 'active\n'; fi
    }
    stop_release_coordinators_fail_closed() { printf 'coordinators-stopped\n' >> "$TRACE"; }
    cleanup_incomplete() { :; }
    restart_previous_services() { printf 'restarted-old-services\n' >> "$TRACE"; }
    abort_quiesce_before_active() { return 1; }
    release_release_mutation_lock() { :; }
    release_txn_validate_pending_token() { return 1; }
    release_observation_worker_request() { printf '%s\n' "$FIXTURE_REQUEST"; }
    release_observation_publish_failure() { printf 'failure-sidecar:%s:%s:%s\n' "$@" >> "$TRACE"; }
    sleep() { :; }
}
run_active_tail() (
    set -euo pipefail
    TRACE=$TEST_ROOT/$1.trace
    : > "$TRACE"
    common_doubles
    isolated_database_work=/var/lib/celikpanel/.release-db-migrations/$(printf 'c%.0s' {1..64})/work
    trap on_exit EXIT
    # shellcheck disable=SC1091
    source "$TEST_ROOT/active-tail.sh"
    trap - EXIT
    printf 'pins=%s listen=%s scheme=%s code=%s\n' "$panel_startup_pins" "$panel_startup_listen" \
        "$panel_startup_scheme" "$update_failure_code" >> "$TRACE"
)

# 1. A failed start check is a failure in phase active: no completion marker,
#    coordinators stopped, typed summary last, sidecar requested.
write_panel "printf '%s\n' 'panel startup check failed: tls_pair_invalid: the panel TLS certificate and private key cannot be loaded as a matching pair' >&2; exit 1"
status=0
run_active_tail check-failed > "$TEST_ROOT/check-failed.log" 2>&1 || status=$?
[[ $status == 1 ]] || { cat "$TEST_ROOT/check-failed.log"; fail "failed start check exit $status"; }
trace=$(cat "$TEST_ROOT/check-failed.trace")
[[ $trace != *completion-marker* ]] || fail 'completion.pending published after a failed start check'
[[ $trace == *coordinators-stopped* && $trace != *restarted-old-services* ]] || fail 'active failure did not stop coordinators for rollback'
[[ $trace == *"verify-database"* ]] || fail 'database publication was not verified before the start check'
[[ $trace == *"failure-sidecar:$FIXTURE_REQUEST:$FIXTURE_COMMIT:candidate_panel_startup_check_failed"* ]] || fail 'typed sidecar not requested'
last=$(tail -n 1 "$TEST_ROOT/check-failed.log")
[[ $last == '!! CELIKPANEL_UPDATE_FAILURE code=candidate_panel_startup_check_failed state=recovery_required reason=new panel start check failed before completion: panel startup check failed: tls_pair_invalid: the panel TLS certificate and private key cannot be loaded as a matching pair detail=' ]] \
    || fail "typed summary: $last"
[[ ${#last} -lt 400 && $last != *$'\r'* ]] || fail 'summary not bounded'
seen=$(cat "$TEST_ROOT/panel.env.seen")
[[ $seen == $'CELIKPANEL_DATA_DIR=/var/lib/celikpanel\nCELIKPANEL_LISTEN=:2083\nCELIKPANEL_PANEL_DEMO_FLAG=\nCELIKPANEL_PANEL_INSECURE_COOKIES_FLAG=\nCELIKPANEL_TLS=1\nCELIKPANEL_WEB_DIR=/opt/celikpanel/web' ]] \
    || fail "panel check environment: $seen"

# 2. Unrecognized failure output is never forwarded; the code stays typed.
write_panel "printf 'secret=%s\n' hunter2 >&2; exit 3"
status=0
run_active_tail check-unrecognized > "$TEST_ROOT/check-unrecognized.log" 2>&1 || status=$?
last=$(tail -n 1 "$TEST_ROOT/check-unrecognized.log")
[[ $status == 1 && $last == *'code=candidate_panel_startup_check_failed '*'no recognized reason line'* && $last != *hunter2* ]] \
    || fail "unrecognized output: $last"

# 2b. N2: an environment entry the reader refuses (here a drop-in value with a
#     space, which systemd shows quoted) is not blamed on the candidate, whose
#     panel never ran: generic code, a plain detail, no typed sidecar; the
#     active phase still returns to the previous release.
write_panel "printf '%s\n' listen=:2083 scheme=https 'pin=$PIN_A' ready"
rm -f -- "$TEST_ROOT/panel.env.seen"
status=0
UNIT_ENVIRONMENT='CELIKPANEL_LISTEN=:2083 "CELIKPANEL_DATA_DIR=/srv/panel data"' \
    run_active_tail env-refused > "$TEST_ROOT/env-refused.log" 2>&1 || status=$?
trace=$(cat "$TEST_ROOT/env-refused.trace")
last=$(tail -n 1 "$TEST_ROOT/env-refused.log")
[[ $status == 1 && ! -e $TEST_ROOT/panel.env.seen && $trace != *completion-marker* &&
   $trace == *coordinators-stopped* && $trace != *failure-sidecar* ]] ||
    fail "environment refusal outcome: $status $trace"
[[ $last == '!! CELIKPANEL_UPDATE_FAILURE code=update_failed state=recovery_required reason=new panel start check could not read the panel unit environment or panel.env detail=the start check could not read the panel environment '*'the new panel itself was not checked' ]] ||
    fail "environment refusal blamed on the candidate: $last"

# 3. A passing check records pins and continues to completion.pending.
write_panel "printf '%s\n' listen=:2083 scheme=https 'pin=$PIN_A' 'pin=$PIN_B' would_create=sqlite-wal ready"
run_active_tail check-passed > "$TEST_ROOT/check-passed.log" 2>&1 || { cat "$TEST_ROOT/check-passed.log"; fail 'passing check refused'; }
mapfile -t passed < "$TEST_ROOT/check-passed.trace"
[[ ${passed[*]} == *"verify-database completion-marker"* ]] || fail "passing order: ${passed[*]}"
[[ ${passed[-1]} == "pins=$PIN_A;$PIN_B listen=:2083 scheme=https code=" ]] || fail "passing state: ${passed[-1]}"

# 4. Incomplete or inconsistent reports are refused before completion.
for body in "printf '%s\n' listen=:2083 scheme=https 'pin=$PIN_A'" \
    "printf '%s\n' listen=:9999 scheme=https 'pin=$PIN_A' ready" \
    "printf '%s\n' listen=:2083 scheme=https ready" \
    "printf '%s\n' listen=:2083 scheme=https pin=sha256//short ready"; do
    write_panel "$body"
    status=0
    run_active_tail check-report > "$TEST_ROOT/check-report.log" 2>&1 || status=$?
    [[ $status == 1 ]] && ! grep -qx completion-marker "$TEST_ROOT/check-report.trace" \
        && tail -n 1 "$TEST_ROOT/check-report.log" | grep -q 'code=candidate_panel_startup_check_failed' \
        || fail "unverifiable report admitted: $body"
done

# 5. panel.env overrides the unit and is read as strict data.
if [[ $EUID == 0 ]]; then
    env_file=$TEST_ROOT/panel.env
    printf '%s\n' '# owner settings' CELIKPANEL_LISTEN=127.0.0.1:8443 CELIKPANEL_TLS=1 \
        CELIKPANEL_PANEL_INSECURE_COOKIES_FLAG= CELIKPANEL_PANEL_DEMO_FLAG= > "$env_file"
    chmod 0600 "$env_file"
    (
        common_doubles
        PANEL_ENV_FILE=$env_file
        panel_startup_environment
        [[ $PANEL_STARTUP_LISTEN == 127.0.0.1:8443 && $PANEL_STARTUP_SCHEME == https ]]
    ) || fail 'panel.env override not applied'
    for bad in 'CELIKPANEL_UNKNOWN=1' 'CELIKPANEL_LISTEN=$(id)' 'no-assignment'; do
        printf '%s\n' "$bad" > "$env_file"
        ( common_doubles; PANEL_ENV_FILE=$env_file; panel_startup_environment ) && fail "panel.env accepted: $bad"
    done
    printf '%s\n' CELIKPANEL_LISTEN=:2083 > "$env_file"
    chmod 0644 "$env_file"
    ( common_doubles; PANEL_ENV_FILE=$env_file; panel_startup_environment ) && fail 'unsafe panel.env mode accepted'
else
    printf 'SKIP: panel.env ownership cases require root\n'
fi

# 6. Loopback target for every supported listen form.
for pair in ':2083=127.0.0.1:2083' '0.0.0.0:2083=127.0.0.1:2083' 'localhost:2083=127.0.0.1:2083' \
    '127.0.0.1:8443=127.0.0.1:8443' '[::]:2083=[::1]:2083' '[::1]:2083=[::1]:2083' '192.0.2.10:2083=192.0.2.10:2083' \
    'panel.example.test:2083=panel.example.test:2083' 'panel_host:2083=panel_host:2083' '0.0.0.0:02083=127.0.0.1:2083' ':02083=127.0.0.1:2083'; do
    [[ $(panel_probe_target "${pair%%=*}") == "${pair#*=}" ]] || fail "probe target ${pair%%=*}"
done
for bad in 2083 ':0' ':70000' 'host name:1'; do
    panel_probe_target "$bad" >/dev/null && fail "invalid listen accepted: $bad"
done

# 7. Stability wait: unchanged PID/restarts for the sample window and a coded
#    401 from the same process; bounded otherwise.
cat > "$TEST_ROOT/curl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$CURL_TRACE"
case "$CURL_MODE" in
    ok) printf '{"error":"authentication required","code":"AUTH_REQUIRED"}\n\n401' ;;
    ok-late) count=$(wc -l < "$CURL_TRACE"); if (( count < 4 )); then exit 7; fi; printf '{"code":"AUTH_REQUIRED"}\n\n401' ;;
    refused) exit 7 ;;
    wrong-code) printf '{"code":"PANEL_STARTING"}\n\n503' ;;
    foreign-401) printf 'Unauthorized\n\n401' ;;
esac
SH
chmod 0755 "$TEST_ROOT/curl"
export CURL_TRACE=$TEST_ROOT/curl.trace CURL_MODE
run_wait() (
    set -euo pipefail
    TRACE=$TEST_ROOT/wait.trace
    common_doubles
    CURL_BIN=$TEST_ROOT/curl
    PANEL_START_MAX_SAMPLES=40
    panel_startup_listen=${WAIT_LISTEN-:2083} panel_startup_scheme=${WAIT_SCHEME-https}
    sample_file=$TEST_ROOT/samples
    systemctl() {
        [[ $* == 'show --property=ActiveState --property=MainPID --property=NRestarts celikpanel-panel.service' ]] || {
            [[ $* == 'show --property=Environment --value celikpanel-panel.service' ]] || exit 93
            printf '%s\n' 'CELIKPANEL_LISTEN=:2083 CELIKPANEL_TLS=1'
            return 0
        }
        # Samples repeat cyclically, so a crash loop never becomes stable.
        local n lines state
        n=$(wc -l < "$TEST_ROOT/sample.count")
        lines=$(wc -l < "$sample_file")
        printf '.\n' >> "$TEST_ROOT/sample.count"
        state=$(sed -n "$(( n % lines + 1 ))p" "$sample_file")
        read -r a p r <<< "$state"
        printf 'ActiveState=%s\nMainPID=%s\nNRestarts=%s\n' "$a" "$p" "$r"
    }
    wait_for_stable_panel_start "$1"
)
wait_case() {
    local name=$1 want=$2 pins=$3 mode=$4
    shift 4
    printf '%s\n' "$@" > "$TEST_ROOT/samples"
    : > "$TEST_ROOT/sample.count"
    : > "$CURL_TRACE"
    CURL_MODE=$mode
    status=0
    run_wait "$pins" > "$TEST_ROOT/wait-$name.log" 2>&1 || status=$?
    [[ $status == "$want" ]] || { cat "$TEST_ROOT/wait-$name.log"; fail "wait $name returned $status, want $want"; }
}
wait_case stable 0 "$PIN_A;$PIN_B" ok 'active 4242 0'
[[ $(wc -l < "$TEST_ROOT/sample.count") -ge 11 ]] || fail 'stable wait returned before the restart window'
[[ $(wc -l < "$CURL_TRACE") == 1 ]] || fail 'probe repeated after an answer from the same process'
[[ $(cat "$CURL_TRACE") == *"--insecure --pinnedpubkey $PIN_A;$PIN_B "*"https://127.0.0.1:2083/api/v1/panel/availability" ]] \
    || fail "pinned probe arguments: $(cat "$CURL_TRACE")"
wait_case late-answer 0 "$PIN_A" ok-late 'active 4242 0'
wait_case crash-loop 1 "$PIN_A" ok 'active 10 0' 'active 10 0' 'activating 0 1' 'active 11 1' 'active 11 1' 'activating 0 2' 'active 12 2'
wait_case restart-counter 1 "$PIN_A" ok 'active 10 0' 'active 10 1' 'active 10 2' 'active 10 3' 'active 10 4' 'active 10 5' 'active 10 6' 'active 10 7' 'active 10 8' 'active 10 9' 'active 10 10' 'active 10 11' 'active 10 12' 'active 10 13'
wait_case failed-unit 1 "$PIN_A" ok 'failed 0 3'
wait_case refused 1 "$PIN_A" refused 'active 4242 0'
wait_case wrong-code 1 "$PIN_A" wrong-code 'active 4242 0'
wait_case foreign-401 1 "$PIN_A" foreign-401 'active 4242 0'
[[ $(wc -l < "$TEST_ROOT/sample.count") -le 40 ]] || fail 'wait exceeded its sample bound'
wait_case pending-path 0 "" ok 'active 77 0'
[[ $(cat "$CURL_TRACE") == *"--insecure --header"* && $(cat "$CURL_TRACE") != *pinnedpubkey* ]] || fail 'unpinned probe arguments'
WAIT_SCHEME=http wait_case plain-http 0 "" ok 'active 77 0'
[[ $(cat "$CURL_TRACE") != *--insecure* && $(cat "$CURL_TRACE") == *"http://127.0.0.1:2083/"* ]] || fail 'plain HTTP probe arguments'
WAIT_LISTEN= WAIT_SCHEME= wait_case derived-environment 0 "" ok 'active 77 0'

# 8. The main-flow start failure stays in completion with its typed code.
run_panel_start() (
    set -euo pipefail
    TRACE=$TEST_ROOT/start.trace
    : > "$TRACE"
    common_doubles
    printf 'completion-marker\n' >> "$TRACE"
    declare -A saved_active_states=([celikpanel-panel.service]=active)
    service_state_is_active_like() { [[ $1 == active ]]; }
    systemctl() { [[ $* == 'start celikpanel-panel.service' ]] || exit 93; printf 'started\n' >> "$TRACE"; }
    wait_for_stable_panel_start() { printf 'wait:%s\n' "$1" >> "$TRACE"; [[ $START_RESULT == ok ]]; }
    panel_startup_pins=$PIN_A
    trap on_exit EXIT
    # shellcheck disable=SC1091
    source "$TEST_ROOT/panel-start.sh"
    trap - EXIT
    printf 'code=%s\n' "$update_failure_code" >> "$TRACE"
)
START_RESULT=unstable
status=0
run_panel_start > "$TEST_ROOT/start.log" 2>&1 || status=$?
last=$(tail -n 1 "$TEST_ROOT/start.log")
[[ $status == 1 && $last == '!! CELIKPANEL_UPDATE_FAILURE code=panel_start_unverified state=recovery_required reason=the new panel did not stay running and answer on its own address after the update was applied detail=' ]] \
    || fail "start failure summary: $status $last"
grep -qx "wait:$PIN_A" "$TEST_ROOT/start.trace" || fail 'main-flow wait did not use the checked pins'
grep -qx "failure-sidecar:$FIXTURE_REQUEST:$FIXTURE_COMMIT:panel_start_unverified" "$TEST_ROOT/start.trace" || fail 'start sidecar not requested'
START_RESULT=ok
run_panel_start > "$TEST_ROOT/start.log" 2>&1 || fail 'stable start refused'
grep -qx 'code=' "$TEST_ROOT/start.trace" || fail 'typed code leaked past a verified start'

# 9. The final hint mirrors rollback.sh's material and identity refusals.
hint() (
    RELEASE_STATE_DIR=$TEST_ROOT/release-state TRUSTED_RELEASE_ROOT=/var/backups/celikpanel/releases/fixture
    print_completed_update_snapshot_guidance /var/backups/snap "$1"
)
name=20260930T000000Z-from-unknown-to-$FIXTURE_COMMIT-$(printf 'e%.0s' {1..32})
key=$(printf '%s' "$name" | sha256sum); key=${key%% *}
out=$(hint "$name")
[[ $out == *"rollback.sh' '/var/backups/snap'"* ]] || fail "legacy absent-material hint: $out"
mkdir -p "$TEST_ROOT/release-state/recovery-material/v1/$key"
out=$(hint "$name")
[[ $out != *rollback.sh* && $out == *'not supported in this release'* && $out == *'desteklenmiyor'* && $out == *'/var/backups/snap'* ]] \
    || fail "material-backed hint: $out"
out=$(hint not-a-canonical-snapshot)
[[ $out != *rollback.sh* ]] || fail 'non-canonical snapshot printed a rollback command'

# 10. F2: the post-start proof's exact curl is checked in the pre-change tool
#     preflight, like every other tool; a missing or non-executable one stops
#     the update before any change instead of after the switch.
extract_function preflight_staged_installer_runtime > "$TEST_ROOT/preflight.sh"
[[ -s $TEST_ROOT/preflight.sh ]] || fail 'missing function preflight_staged_installer_runtime'
# shellcheck disable=SC1091
source "$TEST_ROOT/preflight.sh"
preflight_case() (
    set -euo pipefail
    CURL_BIN=$1
    # Host identity and package tools are doubles; the fixture host may lack them.
    command() { [[ $1 == -v ]] && return 0; builtin command "$@"; }
    getent() { [[ $* == 'group celikpanel' ]]; }
    id() { [[ $* == celikpanel ]]; }
    preflight_staged_installer_runtime
    echo admitted
)
printf '#!/bin/sh\n' > "$TEST_ROOT/curl-not-executable"
chmod 0644 "$TEST_ROOT/curl-not-executable"
for missing in "$TEST_ROOT/absent-curl" "$TEST_ROOT/curl-not-executable"; do
    status=0
    out=$(preflight_case "$missing" 2>&1) || status=$?
    [[ $status == 1 && $out == "!! required update tool is missing: $missing; install it explicitly before retrying" ]] ||
        fail "missing curl admitted or untyped: $status $out"
done
[[ $(preflight_case "$TEST_ROOT/curl" 2>&1) == admitted ]] || fail 'present curl refused'
grep -Fxq 'CURL_BIN=/usr/bin/curl' "$UPDATE" || fail 'probe curl path changed'

printf 'PASS: candidate start check precedes completion and fails in active; start wait is bounded and typed; final hint matches rollback admission; curl checked before any change\n'
