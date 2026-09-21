#!/usr/bin/env bash
set -Eeuo pipefail

fail() {
    printf 'release recovery contract: %s\n' "$*" >&2
    exit 1
}

expect_failure() {
    local label=$1
    shift
    if "$@" >"$TEST_ROOT/$label.stdout" 2>"$TEST_ROOT/$label.stderr"; then
        fail "$label unexpectedly succeeded"
    fi
}

extract_function_source() {
    local file=$1 function_name=$2
    sed -n "/^${function_name}() {$/,/^}$/p" "$file"
}

wait_for_file() {
    local path=$1 attempt
    for attempt in $(seq 1 200); do
        [[ -f $path ]] && return 0
        sleep 0.01
    done
    fail "timed out waiting for fixture boundary: $path"
}

kill_at_boundary() {
    local pid=$1 status
    kill -KILL "$pid"
    status=0
    wait "$pid" || status=$?
    [[ $status -eq 137 ]] || fail "fixture was not killed at the intended boundary: $status"
}

[[ $EUID -eq 0 ]] || fail 'run this contract as root'
command -v systemd-analyze >/dev/null || fail 'systemd-analyze is required'

REPO_ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
TEST_ROOT=$(mktemp -d /run/celikpanel-release-recovery-contract.XXXXXXXX)
trap 'rm -rf -- "$TEST_ROOT"' EXIT
chmod 0700 "$TEST_ROOT"

TARGET_VERSION=v9.9.51
TARGET_SEQUENCE=51
TARGET_COMMIT=1111111111111111111111111111111111111111
PREVIOUS_COMMIT=0000000000000000000000000000000000000000
SOURCE_ROOT=$TEST_ROOT/source
RUNNER=$TEST_ROOT/usr/libexec/celikpanel/release-recovery
SERVICE=$TEST_ROOT/etc/systemd/system/celikpanel-release-recovery.service
TIMER=$TEST_ROOT/etc/systemd/system/celikpanel-release-recovery.timer
START_GUARD=$TEST_ROOT/usr/libexec/celikpanel/release-transaction-start-guard
AGENT_DROPIN=$TEST_ROOT/etc/systemd/system/celikpanel-agent.service.d/10-release-transaction-guard.conf
PANEL_DROPIN=$TEST_ROOT/etc/systemd/system/celikpanel-panel.service.d/10-release-transaction-guard.conf
MANIFEST=$TEST_ROOT/var/lib/celikpanel-release-state/recovery-foundation.v1
SYSTEMCTL=$TEST_ROOT/usr/bin/systemctl
TRANSACTION_ROOT=$TEST_ROOT/var/lib/celikpanel-release-transaction

install -d -m 0700 "$SOURCE_ROOT"
install -d -m 0755 "$SOURCE_ROOT/deploy/systemd"
install -m 0755 "$REPO_ROOT/deploy/release-recovery-runner.sh" \
    "$SOURCE_ROOT/deploy/release-recovery-runner.sh"
install -m 0644 "$REPO_ROOT/deploy/release-recovery-foundation.sh" \
    "$SOURCE_ROOT/deploy/release-recovery-foundation.sh"
install -m 0644 "$REPO_ROOT/deploy/release-recovery-observation.sh" \
    "$SOURCE_ROOT/deploy/release-recovery-observation.sh"
# Only account lookup is a fixture seam; no host account/group is created.
# The actual observer parser, binding, lock and atomic publisher execute.
printf '\n_release_observation_gid() { printf "0\\n"; }\n' \
    >>"$SOURCE_ROOT/deploy/release-recovery-observation.sh"
install -m 0755 "$REPO_ROOT/deploy/release-transaction-start-guard.sh" \
    "$SOURCE_ROOT/deploy/release-transaction-start-guard.sh"
install -m 0755 "$REPO_ROOT/deploy/release-transaction-guard.sh" \
    "$SOURCE_ROOT/deploy/release-transaction-guard.sh"
install -m 0644 "$REPO_ROOT/deploy/release-recovery.protocol" \
    "$SOURCE_ROOT/deploy/release-recovery.protocol"
install -m 0644 "$REPO_ROOT/deploy/systemd/celikpanel-release-recovery.service" \
    "$SOURCE_ROOT/deploy/systemd/celikpanel-release-recovery.service"
install -m 0644 "$REPO_ROOT/deploy/systemd/celikpanel-release-recovery.timer" \
    "$SOURCE_ROOT/deploy/systemd/celikpanel-release-recovery.timer"
printf '%s\n' "$TARGET_COMMIT" >"$SOURCE_ROOT/release.commit"
cat >"$SOURCE_ROOT/deploy/release-sequence-policy" <<EOF
format=celikpanel-release-sequence-policy-v1
version=$TARGET_VERSION
current=$TARGET_SEQUENCE
previous=50
previous_version=v9.9.50
previous_commit=$PREVIOUS_COMMIT
EOF
chmod 0644 "$SOURCE_ROOT/release.commit" "$SOURCE_ROOT/deploy/release-sequence-policy"

grep -Fx 'ExecStart=/usr/libexec/celikpanel/release-recovery' \
    "$REPO_ROOT/deploy/systemd/celikpanel-release-recovery.service" >/dev/null
grep -Fx 'TimeoutStartSec=15min' \
    "$REPO_ROOT/deploy/systemd/celikpanel-release-recovery.service" >/dev/null
grep -Fx 'OnBootSec=5s' \
    "$REPO_ROOT/deploy/systemd/celikpanel-release-recovery.timer" >/dev/null
grep -Fx 'OnUnitInactiveSec=30s' \
    "$REPO_ROOT/deploy/systemd/celikpanel-release-recovery.timer" >/dev/null
grep -Fx 'Unit=celikpanel-release-recovery.service' \
    "$REPO_ROOT/deploy/systemd/celikpanel-release-recovery.timer" >/dev/null
ANALYZE_ROOT=$TEST_ROOT/systemd-analyze
install -d -m 0755 "$ANALYZE_ROOT/etc/systemd/system" \
    "$ANALYZE_ROOT/usr/libexec/celikpanel" "$ANALYZE_ROOT/usr/lib/systemd/system"
install -m 0755 "$REPO_ROOT/deploy/release-recovery-runner.sh" \
    "$ANALYZE_ROOT/usr/libexec/celikpanel/release-recovery"
install -m 0644 "$REPO_ROOT/deploy/systemd/celikpanel-release-recovery.service" \
    "$ANALYZE_ROOT/etc/systemd/system/celikpanel-release-recovery.service"
install -m 0644 "$REPO_ROOT/deploy/systemd/celikpanel-release-recovery.timer" \
    "$ANALYZE_ROOT/etc/systemd/system/celikpanel-release-recovery.timer"
for target in sysinit.target basic.target timers.target multi-user.target shutdown.target; do
    printf '[Unit]\nDescription=contract stub %s\n' "$target" \
        >"$ANALYZE_ROOT/usr/lib/systemd/system/$target"
done
systemd-analyze verify --root="$ANALYZE_ROOT" \
    celikpanel-release-recovery.service celikpanel-release-recovery.timer \
    >"$TEST_ROOT/systemd-analyze.log" 2>&1 || {
        cat "$TEST_ROOT/systemd-analyze.log" >&2
        fail 'systemd-analyze rejected recovery units'
    }

install -d -m 0755 "$(dirname -- "$SYSTEMCTL")"
cat >"$SYSTEMCTL" <<EOF
#!/usr/bin/env bash
set -eu
root='$TEST_ROOT'
printf '%s\n' "\$*" >>"\$root/systemctl.trace"
case \$1 in
    is-system-running)
        if [[ -f "\$root/readiness-delay" ]]; then sleep 10; fi
        if [[ -f "\$root/readiness-state" ]]; then
            cat "\$root/readiness-state"
            exit "\$(cat "\$root/readiness-status")"
        fi
        printf '%s\n' running
        exit 0 ;;
    show)
        case \$2 in
            --property=LoadState) printf '%s\n' loaded ;;
            --property=FragmentPath)
                case \${4:-\${3:-}} in
                    *.service) printf '%s\n' "\$root/etc/systemd/system/celikpanel-release-recovery.service" ;;
                    *.timer) printf '%s\n' "\$root/etc/systemd/system/celikpanel-release-recovery.timer" ;;
                    *) exit 1 ;;
                esac ;;
            --property=NeedDaemonReload) printf '%s\n' no ;;
            --property=DropInPaths)
                case \${4:-\${3:-}} in
                    *.service) dropins=\$root/systemd-service-dropin-paths ;;
                    *.timer) dropins=\$root/systemd-timer-dropin-paths ;;
                    *) exit 1 ;;
                esac
                [[ ! -f "\$dropins" ]] || cat "\$dropins" ;;
            --property=ActiveState) printf '%s\n' active ;;
            *) exit 1 ;;
        esac ;;
    is-enabled) printf '%s\n' enabled ;;
    is-active) exit 0 ;;
    daemon-reload|enable|start) exit 0 ;;
    *) exit 1 ;;
esac
EOF
chmod 0755 "$SYSTEMCTL"

install -d -m 0755 "$TEST_ROOT/var" "$TEST_ROOT/var/lib"
source "$SOURCE_ROOT/deploy/release-recovery-foundation.sh"
(
    release_recovery_publish_intent "$SOURCE_ROOT" "$RUNNER" "$SERVICE" "$TIMER" \
        "$START_GUARD" "$AGENT_DROPIN" "$PANEL_DROPIN" "$MANIFEST" "$SYSTEMCTL"
    : >"$TEST_ROOT/foundation-intent.ready"
    while :; do sleep 1; done
) &
foundation_pid=$!
wait_for_file "$TEST_ROOT/foundation-intent.ready"
kill_at_boundary "$foundation_pid"
[[ -f ${MANIFEST}.intent ]] || fail 'foundation intent was not durably published'
# A retry may adopt only the old or candidate state bound by the exact durable
# intent. It must not require manual removal after the publishing process dies.
release_recovery_publish_intent "$SOURCE_ROOT" "$RUNNER" "$SERVICE" "$TIMER" \
    "$START_GUARD" "$AGENT_DROPIN" "$PANEL_DROPIN" "$MANIFEST" "$SYSTEMCTL"
install -D -m 0755 "$SOURCE_ROOT/deploy/release-recovery-runner.sh" "$RUNNER"
install -D -m 0644 "$SOURCE_ROOT/deploy/systemd/celikpanel-release-recovery.service" "$SERVICE"
install -D -m 0644 "$SOURCE_ROOT/deploy/systemd/celikpanel-release-recovery.timer" "$TIMER"
install -D -m 0755 "$SOURCE_ROOT/deploy/release-transaction-start-guard.sh" "$START_GUARD"
install -d -m 0755 "$(dirname -- "$AGENT_DROPIN")" "$(dirname -- "$PANEL_DROPIN")"
release_recovery_emit_dropin "$START_GUARD" >"$AGENT_DROPIN"
release_recovery_emit_dropin "$START_GUARD" >"$PANEL_DROPIN"
chmod 0644 "$AGENT_DROPIN" "$PANEL_DROPIN"
release_recovery_publish_manifest "$SOURCE_ROOT" "$RUNNER" "$SERVICE" "$TIMER" \
    "$START_GUARD" "$AGENT_DROPIN" "$PANEL_DROPIN" "$MANIFEST" "$SYSTEMCTL"
[[ ! -e ${MANIFEST}.intent ]] || fail 'committed foundation left its intent behind'
release_recovery_verify_foundation "$SOURCE_ROOT" "$RUNNER" "$SERVICE" "$TIMER" \
    "$START_GUARD" "$AGENT_DROPIN" "$PANEL_DROPIN" "$MANIFEST" "$SYSTEMCTL"

# Exercise both production commit callers, not only the post-publication
# verifier.  Effective service or timer overrides must abort immediately after
# daemon-reload and before any recovery enable/start/status action.
install -d -m 0755 "$TEST_ROOT/poison-path"
for poison_command in systemctl timeout; do
    cat >"$TEST_ROOT/poison-path/$poison_command" <<'POISON'
#!/usr/bin/env bash
set -euo pipefail
: >"$POISON_PATH_SENTINEL"
exit 99
POISON
    chmod 0755 "$TEST_ROOT/poison-path/$poison_command"
done
export POISON_PATH_SENTINEL=$TEST_ROOT/poison-path-invoked

run_update_publisher_with_override() (
    set -Eeuo pipefail
    PATH=$TEST_ROOT/poison-path:$PATH
    export PATH
    eval "$(extract_function_source "$REPO_ROOT/update.sh" publish_release_recovery_foundation)"
    TRUSTED_RELEASE_ROOT=$SOURCE_ROOT
    RELEASE_RECOVERY_RUNNER=$RUNNER
    RELEASE_RECOVERY_UNIT=$SERVICE
    RELEASE_RECOVERY_TIMER=$TIMER
    RELEASE_TRANSACTION_HELPER=$START_GUARD
    RELEASE_RECOVERY_AGENT_DROPIN=$AGENT_DROPIN
    RELEASE_RECOVERY_PANEL_DROPIN=$PANEL_DROPIN
    RELEASE_RECOVERY_MANIFEST=$MANIFEST
    SYSTEMCTL_BIN=$SYSTEMCTL
    TIMEOUT_BIN=/usr/bin/false
    UNIT_DIR=$TEST_ROOT/etc/systemd/system
    preflight_release_recovery_foundation() { :; }
    validate_root_trusted_dir_chain() { :; }
    label_release_recovery_foundation() { :; }
    verify_release_recovery_foundation() { fail 'update publisher reached final verification'; }
    die() {
        printf 'update publisher rejected: %s\n' "$*" >&2
        exit 91
    }
    publish_release_recovery_foundation
)

run_install_committer_with_override() (
    set -Eeuo pipefail
    PATH=$TEST_ROOT/poison-path:$PATH
    export PATH
    eval "$(extract_function_source "$REPO_ROOT/install.sh" commit_fresh_release_recovery_foundation)"
    APPLY_ONLY=0
    INSTALL_RELEASE_TRANSACTION_FD=9
    RELEASE_TRANSACTION_ROOT=$TRANSACTION_ROOT
    SRC=$SOURCE_ROOT
    FIRST_INSTALL_TRUST_REQUESTED=0
    RELEASE_RECOVERY_RUNNER=$RUNNER
    RELEASE_RECOVERY_UNIT=$SERVICE
    RELEASE_RECOVERY_TIMER=$TIMER
    RELEASE_TRANSACTION_HELPER=$START_GUARD
    RELEASE_RECOVERY_AGENT_DROPIN=$AGENT_DROPIN
    RELEASE_RECOVERY_PANEL_DROPIN=$PANEL_DROPIN
    RELEASE_RECOVERY_MANIFEST=$MANIFEST
    SYSTEMCTL_BIN=$SYSTEMCTL
    TIMEOUT_BIN=/usr/bin/false
    UNIT_DIR=$TEST_ROOT/etc/systemd/system
    release_txn_verify_inherited_lock() { :; }
    release_recovery_source_identity() { :; }
    verify_reviewed_release_recovery_foundation() {
        fail 'install committer reached final verification'
    }
    restore_celikpanel_selinux_labels() { :; }
    die() {
        printf 'install committer rejected: %s\n' "$*" >&2
        exit 92
    }
    commit_fresh_release_recovery_foundation
)

assert_caller_override_rejected_before_execution() {
    local caller=$1 unit_kind=$2 fixture label=$caller-$unit_kind-override
    fixture=/etc/systemd/system/celikpanel-release-recovery.$unit_kind.d/99-reset.conf
    : >"$TEST_ROOT/systemctl.trace"
    printf '%s\n' "$fixture" >"$TEST_ROOT/systemd-$unit_kind-dropin-paths"
    case $caller in
        update) expect_failure "$label" run_update_publisher_with_override ;;
        install) expect_failure "$label" run_install_committer_with_override ;;
        *) fail "unknown recovery publication caller: $caller" ;;
    esac
    rm -f -- "$TEST_ROOT/systemd-$unit_kind-dropin-paths"
    grep -F 'unverified systemd drop-in' "$TEST_ROOT/$label.stderr" >/dev/null ||
        fail "$caller $unit_kind override did not fail at definition proof"
    grep -Fx 'daemon-reload' "$TEST_ROOT/systemctl.trace" >/dev/null ||
        fail "$caller $unit_kind override did not reach daemon-reload"
    grep -F "show --property=DropInPaths --value celikpanel-release-recovery.$unit_kind" \
        "$TEST_ROOT/systemctl.trace" >/dev/null ||
        fail "$caller $unit_kind override was not inspected"
    if grep -Eq '^(enable|start|is-enabled|is-active)([[:space:]]|$)' \
        "$TEST_ROOT/systemctl.trace"; then
        fail "$caller $unit_kind override reached a recovery lifecycle action"
    fi
}

for caller in update install; do
    for unit_kind in service timer; do
        assert_caller_override_rejected_before_execution "$caller" "$unit_kind"
    done
done
[[ ! -e $POISON_PATH_SENTINEL ]] ||
    fail 'publisher caller used PATH-shadow systemctl or timeout'

printf '%s\n' /etc/systemd/system/celikpanel-release-recovery.service.d/override.conf \
    >"$TEST_ROOT/systemd-service-dropin-paths"
expect_failure foundation-systemd-override release_recovery_verify_foundation \
    "$SOURCE_ROOT" "$RUNNER" "$SERVICE" "$TIMER" "$START_GUARD" \
    "$AGENT_DROPIN" "$PANEL_DROPIN" "$MANIFEST" "$SYSTEMCTL"
rm -f -- "$TEST_ROOT/systemd-service-dropin-paths"

install -d -m 0700 "$TRANSACTION_ROOT"
: >"$TRANSACTION_ROOT/transaction.lock"
chmod 0600 "$TRANSACTION_ROOT/transaction.lock"
run_final_proof() {
    CELIKPANEL_RELEASE_RECOVERY_TESTING=1 \
    CELIKPANEL_RELEASE_RECOVERY_TEST_ROOT="$TEST_ROOT" \
        /bin/bash "$RUNNER" --verify-final-state \
        --expected-version "$TARGET_VERSION" \
        --expected-commit "$TARGET_COMMIT" \
        --expected-sequence "$TARGET_SEQUENCE"
}
run_final_proof
printf '%s\n' /etc/systemd/system/celikpanel-release-recovery.timer.d/override.conf \
    >"$TEST_ROOT/systemd-timer-dropin-paths"
expect_failure final-proof-systemd-override run_final_proof
rm -f -- "$TEST_ROOT/systemd-timer-dropin-paths"
expect_failure wrong-sequence env CELIKPANEL_RELEASE_RECOVERY_TESTING=1 \
    CELIKPANEL_RELEASE_RECOVERY_TEST_ROOT="$TEST_ROOT" /bin/bash "$RUNNER" \
    --verify-final-state --expected-version "$TARGET_VERSION" \
    --expected-commit "$TARGET_COMMIT" --expected-sequence 55
install -m 0600 "$MANIFEST" "${MANIFEST}.intent"
expect_failure pending-intent run_final_proof
rm -f -- "${MANIFEST}.intent"
: >"$TRANSACTION_ROOT/unexpected"
chmod 0600 "$TRANSACTION_ROOT/unexpected"
expect_failure unexpected-marker run_final_proof
rm -f -- "$TRANSACTION_ROOT/unexpected"
run_final_proof

# Exercise the real immutable-release selector and post-child reproof.  The
# retained payload is deliberately tiny, but has the exact production shape
# and checksum/provenance rules.
RELEASES_ROOT=$TEST_ROOT/var/backups/celikpanel/releases
SNAPSHOT_ROOT=$TEST_ROOT/var/backups/celikpanel/update-snapshots
SNAPSHOT=20260828T120000Z-from-unknown-to-$TARGET_COMMIT-0123456789abcdef0123456789abcdef
TREE=2222222222222222222222222222222222222222
install -d -m 0755 "$TEST_ROOT/var/backups" "$TEST_ROOT/var/backups/celikpanel"
install -d -m 0700 "$RELEASES_ROOT" "$SNAPSHOT_ROOT" "$SNAPSHOT_ROOT/$SNAPSHOT"
printf '%s\t%s\t%s\n' \
    celikpanel-agent.service enabled active \
    celikpanel-panel.service enabled active \
    celikpanel-firewall-restore.service disabled inactive \
    >"$SNAPSHOT_ROOT/$SNAPSHOT/service-states.tsv"
chmod 0600 "$SNAPSHOT_ROOT/$SNAPSHOT/service-states.tsv"

write_active_marker() {
    rm -f -- "$TRANSACTION_ROOT/quiesce.pending" "$TRANSACTION_ROOT/active" \
        "$TRANSACTION_ROOT/completion.pending" "$TRANSACTION_ROOT/scheduler-restore.pending"
    printf '%s\n' version=1 \
        token=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
        operation=update "snapshot=$SNAPSHOT" >"$TRANSACTION_ROOT/active"
    chmod 0600 "$TRANSACTION_ROOT/active"
}

run_recovery() {
    local -a owner_args=()
    if [[ ${CONTRACT_OWNER_RETRY:-0} == 1 ]]; then owner_args=(--owner-retry --snapshot "$SNAPSHOT"); fi
    CELIKPANEL_RELEASE_RECOVERY_TESTING=1 \
    CELIKPANEL_RELEASE_RECOVERY_TEST_ROOT="$TEST_ROOT" /bin/bash "$RUNNER" "${owner_args[@]}" "$@"
}

refresh_release_checksums() {
    local release=$1
    (
        cd "$release"
        LC_ALL=C find . -xdev -type f ! -path './SHA256SUMS' -print0 \
            | LC_ALL=C sort -z | xargs -0 sha256sum >SHA256SUMS
    )
    chmod 0644 "$release/SHA256SUMS"
}

make_retained_release() {
    local release=$1
    install -d -m 0700 "$release"
    install -d -m 0755 "$release/bin" "$release/deploy/systemd"
    install -m 0755 "$REPO_ROOT/deploy/release-transaction-guard.sh" \
        "$release/deploy/release-transaction-guard.sh"
    install -m 0755 "$REPO_ROOT/deploy/release-transaction-start-guard.sh" \
        "$release/deploy/release-transaction-start-guard.sh"
    install -m 0755 "$REPO_ROOT/deploy/panel-tls-snapshot.sh" \
        "$release/deploy/panel-tls-snapshot.sh"
    install -m 0755 "$REPO_ROOT/deploy/release-recovery-runner.sh" \
        "$release/deploy/release-recovery-runner.sh"
    install -m 0644 "$REPO_ROOT/deploy/release-recovery-foundation.sh" \
        "$release/deploy/release-recovery-foundation.sh"
    install -m 0644 "$SOURCE_ROOT/deploy/release-recovery-observation.sh" \
        "$release/deploy/release-recovery-observation.sh"
    install -m 0644 "$REPO_ROOT/deploy/release-recovery.protocol" \
        "$release/deploy/release-recovery.protocol"
    install -m 0644 "$REPO_ROOT/deploy/systemd/celikpanel-release-recovery.service" \
        "$release/deploy/systemd/celikpanel-release-recovery.service"
    install -m 0644 "$REPO_ROOT/deploy/systemd/celikpanel-release-recovery.timer" \
        "$release/deploy/systemd/celikpanel-release-recovery.timer"
    install -m 0644 "$SOURCE_ROOT/deploy/release-sequence-policy" \
        "$release/deploy/release-sequence-policy"
    printf '%s\n' 1 >"$release/release.version"
    printf '%s\n' "$TARGET_COMMIT" >"$release/release.commit"
    printf '%s\n' "$TREE" >"$release/release.tree"
    cat >"$release/rollback.sh" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
root=${CELIKPANEL_RELEASE_RECOVERY_TEST_ROOT:?}
printf "dispatch\n" >>"$root/child-dispatches"
case $(cat "$root/child-mode") in
    success) rm -f -- "$root/var/lib/celikpanel-release-transaction/active" ;;
    leave-active) : ;;
    fail) exit 23 ;;
    kill-runner) kill -KILL "$PPID"; exit 23 ;;
    replace-lock-success)
        rm -f -- "$root/var/lib/celikpanel-release-transaction/transaction.lock"
        : >"$root/var/lib/celikpanel-release-transaction/transaction.lock"
        chmod 0600 "$root/var/lib/celikpanel-release-transaction/transaction.lock"
        rm -f -- "$root/var/lib/celikpanel-release-transaction/active"
        ;;
    replace-lock-fail)
        rm -f -- "$root/var/lib/celikpanel-release-transaction/transaction.lock"
        : >"$root/var/lib/celikpanel-release-transaction/transaction.lock"
        chmod 0600 "$root/var/lib/celikpanel-release-transaction/transaction.lock"
        exit 23
        ;;
    *) exit 64 ;;
esac
EOF
    for executable in install.sh update.sh bin/panel bin/agent; do
        printf '#!/usr/bin/env bash\nexit 0\n' >"$release/$executable"
    done
    chmod 0755 "$release/rollback.sh" "$release/install.sh" "$release/update.sh" \
        "$release/bin/panel" "$release/bin/agent"
    chmod 0644 "$release/release.version" "$release/release.commit" "$release/release.tree"
    refresh_release_checksums "$release"
}

write_active_marker
expect_failure zero-release run_recovery
RELEASE_ONE=$RELEASES_ROOT/${TARGET_COMMIT:0:12}-aaaaaaaaaaaaaaaaaaaaaaaa
make_retained_release "$RELEASE_ONE"
# Execute the whole production runner with its existing child/systemd fixtures.
# This is observer glue coverage, not evidence of a native VM rollback body.
TRUSTED_RELEASE_ROOT=$SOURCE_ROOT
source "$SOURCE_ROOT/deploy/release-recovery-observation.sh"
RELEASE_OBSERVATION_ROOT=$TEST_ROOT/var/lib/celikpanel-recovery-observations
RELEASE_OBSERVATION_BINDINGS=$TEST_ROOT/var/lib/celikpanel-release-state/recovery-observation-bindings
OBSERVATION_TEST_REQUEST=77777777777777777777777777777777
release_observation_publish "$OBSERVATION_TEST_REQUEST" "$TARGET_COMMIT" running none update_running
(
    exec 9<>"$TRANSACTION_ROOT/transaction.lock"
    flock -x 9
    source "$SOURCE_ROOT/deploy/release-transaction-guard.sh"
    release_observation_worker_request() { printf '%s\n' 77777777777777777777777777777777; }
    release_observation_bind_update "$TRANSACTION_ROOT" 9 \
        aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
        "$SNAPSHOT" "$TARGET_COMMIT"
)
observation_binding_before=$(sha256sum "$RELEASE_OBSERVATION_BINDINGS/$SNAPSHOT.binding")
printf 'starting\n' >"$TEST_ROOT/readiness-state"
printf '1\n' >"$TEST_ROOT/readiness-status"
run_recovery >"$TEST_ROOT/first-boot-wait.log" 2>&1 || fail 'first boot transition did not defer'
_release_observation_read "$OBSERVATION_TEST_REQUEST" 0
[[ $OBSERVATION_PHASE == recovering && $OBSERVATION_PROOF == none &&
   $OBSERVATION_PREVIOUS == none ]] || fail 'first boot wait invented a recovery failure'
[[ ! -e "$TEST_ROOT/child-dispatches" && -f "$TRANSACTION_ROOT/active" ]] || fail 'first boot wait dispatched or completed recovery'
rm -f -- "$TEST_ROOT/readiness-state" "$TEST_ROOT/readiness-status"
printf '%s\n' fail >"$TEST_ROOT/child-mode"
write_active_marker
expect_failure observed-child-failure run_recovery
_release_observation_read "$OBSERVATION_TEST_REQUEST" 0
[[ $OBSERVATION_PHASE == recovery_required && $OBSERVATION_PROOF == none &&
   $OBSERVATION_REASON == recovery_failed ]] || fail 'runner failure did not publish exact recovery observation'
# A boot transition is a pending prerequisite, not a failed rollback. Exercise
# the real runner while the fixture child would succeed if wrongly dispatched.
printf '%s\n' success >"$TEST_ROOT/child-mode"
transition_marker_before=$(sha256sum "$TRANSACTION_ROOT/active")
transition_dispatch_before=$(sha256sum "$TEST_ROOT/child-dispatches")
for readiness in initializing starting stopping; do
    printf '%s\n' "$readiness" >"$TEST_ROOT/readiness-state"
    printf '1\n' >"$TEST_ROOT/readiness-status"
    run_recovery >"$TEST_ROOT/deferred-$readiness.log" 2>&1 || fail "$readiness did not defer"
    grep -F 'native recovery timer will retry this same operation' "$TEST_ROOT/deferred-$readiness.log" >/dev/null || fail 'deferred recovery has no resumption guidance'
    [[ $(sha256sum "$TRANSACTION_ROOT/active") == "$transition_marker_before" ]] || fail 'deferral changed the transaction'
    [[ $(sha256sum "$TEST_ROOT/child-dispatches") == "$transition_dispatch_before" ]] || fail 'deferral dispatched recovery'
    [[ $(sha256sum "$RELEASE_OBSERVATION_BINDINGS/$SNAPSHOT.binding") == "$observation_binding_before" ]] || fail 'deferral changed the exact request binding'
    _release_observation_read "$OBSERVATION_TEST_REQUEST" 0
    [[ $OBSERVATION_PHASE == recovering && $OBSERVATION_PROOF == none &&
       $OBSERVATION_REASON == recovery_running && $OBSERVATION_PREVIOUS == recovery_failed ]] || fail 'deferral lost prior failure or claimed terminal proof'
    wait_path=$RELEASE_OBSERVATION_ROOT/$OBSERVATION_TEST_REQUEST.wait
    grep -Fx "waiting_for=$readiness" "$wait_path" >/dev/null || fail 'wrong typed wait reason'
    grep -Fx "observation_identity=$(TZ=UTC0 stat -Lc '%d:%i:%s:%y:%z' "$RELEASE_OBSERVATION_ROOT/$OBSERVATION_TEST_REQUEST.status")" "$wait_path" >/dev/null || fail 'wait is not bound to exact status file'
    (exec 9<>"$TRANSACTION_ROOT/transaction.lock"; flock -xn 9) || fail 'deferred invocation retained the transaction lock'
    observation_before=$(sha256sum "$RELEASE_OBSERVATION_ROOT/$OBSERVATION_TEST_REQUEST.status")
    expect_failure "final-proof-$readiness" run_final_proof
    grep -F 'final-state proof requires no pending transaction markers' "$TEST_ROOT/final-proof-$readiness.stderr" >/dev/null || fail 'final proof accepted deferral as completion'
    [[ $(sha256sum "$RELEASE_OBSERVATION_ROOT/$OBSERVATION_TEST_REQUEST.status") == "$observation_before" ]] || fail 'final proof changed observation'
    [[ $(sha256sum "$TEST_ROOT/child-dispatches") == "$transition_dispatch_before" ]] || fail 'final proof dispatched recovery'
done
# Optional guidance cannot block deferral or turn it into a failed rollback.
wait_path=$RELEASE_OBSERVATION_ROOT/$OBSERVATION_TEST_REQUEST.wait
chmod 0660 "$wait_path"
run_recovery >"$TEST_ROOT/unsafe-wait.log" 2>&1 || fail 'unsafe hint blocked known transition'
grep -F 'recovery waiting observation is unavailable' "$TEST_ROOT/unsafe-wait.log" >/dev/null || fail 'unsafe hint was silently accepted'
_release_observation_read "$OBSERVATION_TEST_REQUEST" 0
[[ $OBSERVATION_PHASE == recovering && $OBSERVATION_PROOF == none && $OBSERVATION_PREVIOUS == recovery_failed ]] || fail 'optional hint changed verified base'
[[ $(sha256sum "$TRANSACTION_ROOT/active") == "$transition_marker_before" && $(sha256sum "$TEST_ROOT/child-dispatches") == "$transition_dispatch_before" ]] || fail 'optional hint caused mutation'
(exec 9<>"$TRANSACTION_ROOT/transaction.lock"; flock -xn 9) || fail 'optional hint retained transaction lock'
chmod 0640 "$wait_path"
# A surprising status, malformed response or bounded probe failure is not a
# known transition and must never reach the mutation child.
for readiness in maintenance offline unknown empty multiline running-error starting-error timeout; do
    printf '1\n' >"$TEST_ROOT/readiness-status"
    case $readiness in
        empty) : >"$TEST_ROOT/readiness-state" ;;
        multiline) printf 'starting\nrunning\n' >"$TEST_ROOT/readiness-state" ;;
        running-error) printf 'running\n' >"$TEST_ROOT/readiness-state" ;;
        starting-error) printf 'starting\n' >"$TEST_ROOT/readiness-state"; printf '2\n' >"$TEST_ROOT/readiness-status" ;;
        timeout) printf 'running\n' >"$TEST_ROOT/readiness-state"; printf '0\n' >"$TEST_ROOT/readiness-status"; : >"$TEST_ROOT/readiness-delay" ;;
        *) printf '%s\n' "$readiness" >"$TEST_ROOT/readiness-state" ;;
    esac
    expect_failure "readiness-$readiness" run_recovery
    grep -F 'Cannot verify operating system readiness' "$TEST_ROOT/readiness-$readiness.stderr" >/dev/null || fail 'readiness rejection was not explained'
    [[ $(sha256sum "$TRANSACTION_ROOT/active") == "$transition_marker_before" ]] || fail 'unknown readiness changed the transaction'
    [[ $(sha256sum "$TEST_ROOT/child-dispatches") == "$transition_dispatch_before" ]] || fail 'unknown readiness dispatched recovery'
    rm -f -- "$TEST_ROOT/readiness-delay"
done
rm -f -- "$TEST_ROOT/readiness-state" "$TEST_ROOT/readiness-status"
# Even a ready host must not use the read-only final-proof command to repair
# an active transaction; only the subsequent recovery invocation may dispatch.
expect_failure final-proof-ready-active run_final_proof
[[ $(sha256sum "$TEST_ROOT/child-dispatches") == "$transition_dispatch_before" ]] || fail 'ready final proof dispatched recovery'
# The first child failure consumed one durable slot; waiting and final proof
# did not. Two more attempts reach the child, the fourth only reports guidance.
budget=$TEST_ROOT/var/lib/celikpanel-release-state/recovery-dispatch/v1/$SNAPSHOT
[[ -f $budget/1 && ! -e $budget/2 ]] || fail 'wait or proof consumed dispatch budget'
printf '%s\n' fail >"$TEST_ROOT/child-mode"
for attempt in 2 3; do
    if [[ $attempt == 3 ]]; then printf 'kill-runner\n' >"$TEST_ROOT/child-mode"; fi
    expect_failure "budget-failure-$attempt" run_recovery
    [[ -f $budget/$attempt ]] || fail 'dispatch had no durable receipt'
done
budget_marker=$(sha256sum "$TRANSACTION_ROOT/active")
budget_calls=$(sha256sum "$TEST_ROOT/child-dispatches")
budget_receipts=$(sha256sum "$budget/1" "$budget/2" "$budget/3")
for attempt in 4 5; do
    run_recovery >"$TEST_ROOT/budget-paused-$attempt.log" 2>&1
    grep -F 'Automatic recovery paused after three admitted attempts' "$TEST_ROOT/budget-paused-$attempt.log" >/dev/null || fail 'budget not explained'
    [[ $(sha256sum "$TEST_ROOT/child-dispatches") == "$budget_calls" ]] || fail 'exhausted budget dispatched'
    [[ $(sha256sum "$TRANSACTION_ROOT/active") == "$budget_marker" ]] || fail 'exhausted budget changed marker'
    _release_observation_read "$OBSERVATION_TEST_REQUEST" 0
    [[ $OBSERVATION_PHASE == recovery_required && $OBSERVATION_REASON == recovery_incomplete &&
       $OBSERVATION_PREVIOUS == recovery_failed && $OBSERVATION_PROOF == none ]] || fail 'pause lost failure or claimed proof'
    (exec 9<>"$TRANSACTION_ROOT/transaction.lock"; flock -xn 9) || fail 'paused budget retained lock'
done
# Wrong snapshot, broken receipts and metadata cannot authorize an owner retry.
expect_failure foreign-owner-retry run_recovery --owner-retry --snapshot "${SNAPSHOT%?}e"
chmod 0644 "$budget/2"
expect_failure unsafe-budget run_recovery
chmod 0600 "$budget/2"
mv "$budget/2" "$budget/saved-2"
expect_failure missing-budget-slot run_recovery
mv "$budget/saved-2" "$budget/2"
cp -p "$budget/2" "$budget/saved-2"
for kind in symlink fifo hardlink truncated; do
    rm -- "$budget/2"
    case $kind in
        symlink) ln -s saved-2 "$budget/2" ;;
        fifo) mkfifo -m 0600 "$budget/2" ;;
        hardlink) ln "$budget/saved-2" "$budget/2" ;;
        truncated) printf 'schema=celikpanel-recovery-dispatch/v1' >"$budget/2"; chmod 0600 "$budget/2" ;;
    esac
    expect_failure "budget-$kind" run_recovery --owner-retry --snapshot "$SNAPSHOT"
done
rm -- "$budget/2"
mv "$budget/saved-2" "$budget/2"
[[ $(sha256sum "$TEST_ROOT/child-dispatches") == "$budget_calls" ]] || fail 'invalid retry reached child'
# Explicit native owner retry is one dispatch, without replenishing auto slots.
CONTRACT_OWNER_RETRY=1
printf '%s\n' fail >"$TEST_ROOT/child-mode"
expect_failure owner-retry-failed run_recovery
[[ $(sha256sum "$budget/1" "$budget/2" "$budget/3") == "$budget_receipts" ]] || fail 'owner retry reset auto budget'
CONTRACT_OWNER_RETRY=0
owner_calls=$(sha256sum "$TEST_ROOT/child-dispatches")
run_recovery >"$TEST_ROOT/budget-after-owner.log" 2>&1
[[ $(sha256sum "$TEST_ROOT/child-dispatches") == "$owner_calls" ]] || fail 'owner retry replenished budget'
# Later unrelated child/final-proof cases explicitly authorize one owner attempt.
CONTRACT_OWNER_RETRY=1
printf '%s\n' success >"$TEST_ROOT/child-mode"
printf 'PASS: durable three-attempt budget, interrupted runner, preserved failure and explicit owner retry\n'
# Ready dispatch resumes the same marker and request without a new mutation.
run_recovery
_release_observation_read "$OBSERVATION_TEST_REQUEST" 0
[[ $OBSERVATION_PHASE == recovered && $OBSERVATION_PROOF == rollback_verified &&
   $OBSERVATION_PREVIOUS == recovery_failed ]] || fail 'runner terminal proof or prior failure was lost'
[[ $(sha256sum "$RELEASE_OBSERVATION_BINDINGS/$SNAPSHOT.binding") == "$observation_binding_before" ]] || fail 'runner altered immutable request binding'

printf '%s\n' leave-active >"$TEST_ROOT/child-mode"
write_active_marker
expect_failure child-exit0-marker-remains run_recovery
grep -F 'release recovery child returned success while a verified marker remains' \
    "$TEST_ROOT/child-exit0-marker-remains.stderr" >/dev/null ||
    fail 'successful child bypassed final marker reproof'

for readiness_status in 0 1; do
    printf 'degraded\n' >"$TEST_ROOT/readiness-state"
    printf '%s\n' "$readiness_status" >"$TEST_ROOT/readiness-status"
    write_active_marker
    expect_failure "degraded-$readiness_status" run_recovery
    grep -F 'release recovery child returned success while a verified marker remains' \
        "$TEST_ROOT/degraded-$readiness_status.stderr" >/dev/null || fail 'degraded readiness did not reach child reproof'
done
rm -f -- "$TEST_ROOT/readiness-state" "$TEST_ROOT/readiness-status"

printf '%s\n' fail >"$TEST_ROOT/child-mode"
write_active_marker
expect_failure child-nonzero run_recovery
grep -F 'release recovery child failed with status 23' \
    "$TEST_ROOT/child-nonzero.stderr" >/dev/null ||
    fail 'nonzero child was not reported after reproof'

RELEASE_TWO=$RELEASES_ROOT/${TARGET_COMMIT:0:12}-bbbbbbbbbbbbbbbbbbbbbbbb
cp -a -- "$RELEASE_ONE" "$RELEASE_TWO"
chmod 0700 "$RELEASE_TWO"
printf '%s\n' success >"$TEST_ROOT/child-mode"
write_active_marker
run_recovery

RELEASE_THREE=$RELEASES_ROOT/${TARGET_COMMIT:0:12}-cccccccccccccccccccccccc
cp -a -- "$RELEASE_ONE" "$RELEASE_THREE"
chmod 0700 "$RELEASE_THREE"
printf '%s\n' conflict >"$RELEASE_THREE/conflict.fixture"
chmod 0644 "$RELEASE_THREE/conflict.fixture"
refresh_release_checksums "$RELEASE_THREE"
write_active_marker
expect_failure conflicting-releases run_recovery
grep -F 'retained releases for the target have conflicting payloads' \
    "$TEST_ROOT/conflicting-releases.stderr" >/dev/null ||
    fail 'conflicting retained releases were not identified'
rm -rf -- "$RELEASE_THREE"

printf '%s\n' replace-lock-success >"$TEST_ROOT/child-mode"
write_active_marker
expect_failure child-exit0-reproof run_recovery
grep -F 'transaction lock path identity changed during recovery' \
    "$TEST_ROOT/child-exit0-reproof.stderr" >/dev/null ||
    fail 'successful child bypassed transaction lock identity reproof'
: >"$TRANSACTION_ROOT/transaction.lock"
chmod 0600 "$TRANSACTION_ROOT/transaction.lock"
printf '%s\n' replace-lock-fail >"$TEST_ROOT/child-mode"
write_active_marker
expect_failure child-nonzero-reproof run_recovery
grep -F 'transaction lock path identity changed during recovery' \
    "$TEST_ROOT/child-nonzero-reproof.stderr" >/dev/null ||
    fail 'nonzero child bypassed transaction lock identity reproof'
: >"$TRANSACTION_ROOT/transaction.lock"
chmod 0600 "$TRANSACTION_ROOT/transaction.lock"
rm -f -- "$TRANSACTION_ROOT/active"

# Marker staging and start authorization also have explicit kill/retry
# boundaries. A pre-rename marker crash is cleaned outside the strict root; a
# dead authorization holder is quarantined by the next exact lock owner.
FI_PARENT=$TEST_ROOT/fi
FI_ROOT=$FI_PARENT/release-transaction
FI_RUNTIME_PARENT=$FI_PARENT/run
FI_RUNTIME=$FI_RUNTIME_PARENT/release-transaction
FI_TOKEN=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
FI_SNAPSHOT=fixture-snapshot
install -d -m 0755 "$FI_PARENT" "$FI_RUNTIME_PARENT"
install -d -m 0700 "$FI_ROOT"
: >"$FI_ROOT/transaction.lock"
chmod 0600 "$FI_ROOT/transaction.lock"
(
    exec 9<>"$FI_ROOT/transaction.lock"
    flock -x 9
    TRUSTED_RELEASE_ROOT=$SOURCE_ROOT
    source "$SOURCE_ROOT/deploy/release-transaction-guard.sh"
    _release_txn_stage_marker "$FI_ROOT" 9 active "$FI_TOKEN" update "$FI_SNAPSHOT"
    : >"$TEST_ROOT/marker-stage.ready"
    while :; do sleep 1; done
) &
marker_pid=$!
wait_for_file "$TEST_ROOT/marker-stage.ready"
kill_at_boundary "$marker_pid"
[[ ! -e $FI_ROOT/active ]] || fail 'pre-rename marker kill published an active marker'
find "${FI_ROOT}.marker-staging" -mindepth 1 -maxdepth 1 -type f -print -quit |
    grep -q . || fail 'marker kill fixture did not retain its staged boundary'
(
    exec 9<>"$FI_ROOT/transaction.lock"
    flock -x 9
    TRUSTED_RELEASE_ROOT=$SOURCE_ROOT
    source "$SOURCE_ROOT/deploy/release-transaction-guard.sh"
    release_txn_create_active_marker "$FI_ROOT" 9 "$FI_TOKEN" update "$FI_SNAPSHOT"
)
[[ -f $FI_ROOT/active ]] || fail 'marker retry did not publish the canonical active marker'
[[ $(find "$FI_ROOT" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort |
    tr '\n' ' ') == 'active transaction.lock ' ]] ||
    fail 'marker retry polluted the strict transaction root'
[[ -z $(find "${FI_ROOT}.marker-staging" -mindepth 1 -maxdepth 1 -print -quit) ]] ||
    fail 'marker retry left staging artifacts'

(
    exec 9<>"$FI_ROOT/transaction.lock"
    flock -x 9
    TRUSTED_RELEASE_ROOT=$SOURCE_ROOT
    source "$SOURCE_ROOT/deploy/release-transaction-guard.sh"
    release_txn_create_start_authorization \
        "$FI_ROOT" "$FI_RUNTIME" 9 "$FI_TOKEN" update "$FI_SNAPSHOT"
    : >"$TEST_ROOT/authorization.ready"
    while :; do sleep 1; done
) &
authorization_pid=$!
wait_for_file "$TEST_ROOT/authorization.ready"
kill_at_boundary "$authorization_pid"
[[ -d $FI_RUNTIME/start.authorization ]] ||
    fail 'authorization kill fixture missed the durable publication boundary'
(
    exec 9<>"$FI_ROOT/transaction.lock"
    flock -x 9
    TRUSTED_RELEASE_ROOT=$SOURCE_ROOT
    source "$SOURCE_ROOT/deploy/release-transaction-guard.sh"
    release_txn_clear_stale_start_authorization "$FI_ROOT" "$FI_RUNTIME" 9
)
[[ ! -e $FI_RUNTIME/start.authorization ]] ||
    fail 'next lock owner did not clear stale start authorization'
if [[ -d ${FI_RUNTIME}.authorization-discard ]]; then
    [[ -z $(find "${FI_RUNTIME}.authorization-discard" -mindepth 1 -maxdepth 1 -print -quit) ]] ||
        fail 'authorization retry left an unclassified discard artifact'
fi
(
    exec 9<>"$FI_ROOT/transaction.lock"
    flock -x 9
    TRUSTED_RELEASE_ROOT=$SOURCE_ROOT
    source "$SOURCE_ROOT/deploy/release-transaction-guard.sh"
    release_txn_create_start_authorization \
        "$FI_ROOT" "$FI_RUNTIME" 9 "$FI_TOKEN" update "$FI_SNAPSHOT"
    release_txn_remove_start_authorization \
        "$FI_ROOT" "$FI_RUNTIME" 9 "$FI_TOKEN" update "$FI_SNAPSHOT"
)
[[ ! -e $FI_RUNTIME/start.authorization && ! -L $FI_RUNTIME/start.authorization ]] ||
    fail 'fresh authorization removal left a live authorization'
[[ -z $(find "$FI_RUNTIME" -mindepth 1 -maxdepth 1 \
    -name '.start.authorization.tmp.*' -print -quit) ]] ||
    fail 'fresh authorization lifecycle left a staging artifact'
if [[ -d ${FI_RUNTIME}.authorization-discard ]]; then
    [[ -z $(find "${FI_RUNTIME}.authorization-discard" -mindepth 1 -maxdepth 1 -print -quit) ]] ||
        fail 'fresh authorization removal left a discard artifact'
fi

bash "$REPO_ROOT/deploy/test-release-recovery-rollback-handoff.sh"
printf 'release recovery unit/foundation/final-proof contract: PASS\n'
