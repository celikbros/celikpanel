#!/usr/bin/env bash
set -euo pipefail

candidate=${1:-deploy/recover-active-update-database.sh}
[[ -f "$candidate" ]] || {
    printf 'FAIL: missing recovery helper: %s\n' "$candidate" >&2
    exit 1
}
bash -n "$candidate"
if LC_ALL=C grep -q $'\r' "$candidate"; then
    printf 'FAIL: recovery helper contains CR bytes\n' >&2
    exit 1
fi

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

require_text() {
    grep -Fq -- "$1" "$candidate" || fail "$2"
}

require_count() {
    local needle=$1 expected=$2 message=$3 actual
    actual=$(grep -Fc -- "$needle" "$candidate" || true)
    [[ "$actual" == "$expected" ]] || fail "$message (found $actual, expected $expected)"
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
    [[ -n "$first" && -n "$second" && "$first" -lt "$second" ]] \
        || fail "$message"
}

require_text 'set -euo pipefail' "strict shell mode is missing"
require_text 'PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin' \
    "fixed privileged PATH is missing"
require_text 'SYSTEMCTL_BIN=/usr/bin/systemctl' \
    "exact systemctl path is not fixed"
require_text 'validate_exact_systemctl() {' \
    "exact systemctl validator is missing"
require_count 'validate_exact_systemctl' 2 \
    "exact systemctl validator must have one definition and one invocation"
require_text 'umask 077' "private recovery umask is missing"
require_text '[[ $EUID -eq 0 ]]' "root-only guard is missing"
require_text '--active-target=<40-lowercase-hex>' "active-target is not explicit"
require_text '--recovery-commit=<40-lowercase-hex>' "recovery-commit is not explicit"
require_text '--trusted-release=<absolute-immutable-release-root>' "trusted-release is not explicit"
require_text '[[ $# -eq 3 &&' "exact recovery argument count is missing"
require_text '"$ACTIVE_TARGET" =~ ^[0-9a-f]{40}$' \
    "active target is not exactly 40 lowercase hex"
require_text '"$RECOVERY_COMMIT" =~ ^[0-9a-f]{40}$' \
    "recovery commit is not exactly 40 lowercase hex"
require_text '[[ "${relative:0:12}" == "${RECOVERY_COMMIT:0:12}" ]]' \
    "trusted release basename is not bound to recovery-commit"
require_text '[[ "$commit" == "$RECOVERY_COMMIT" ]]' \
    "trusted release provenance is not bound to recovery-commit"
require_text 'tree=$(tr -d '\''[:space:]'\'' < "$canonical/release.tree")' \
    "trusted release tree provenance is not read"
require_text '[[ "$tree" =~ ^[0-9a-f]{40}$ || "$tree" =~ ^[0-9a-f]{64}$ ]]' \
    "trusted release tree is not exactly 40 or 64 lowercase hex"
require_text '"$marker_target" == "$ACTIVE_TARGET"' \
    "active marker is not bound independently to active-target"
require_text 'trusted release checksum verification failed' \
    "complete trusted-release checksum validation is missing"
require_text '| cmp -s - SHA256SUMS' "trusted manifest is not recomputed"
require_text 'sha256sum -c SHA256SUMS' "trusted manifest entries are not verified"
require_text 'recovery helper must execute from the verified trusted release' \
    "running helper is not pinned to the trusted release"
require_text 'source "$TRUSTED_RELEASE_ROOT/deploy/release-transaction-guard.sh"' \
    "transaction guard is not sourced from the trusted release"

release_lock=$(line_of_last 'acquire_release_transaction_lock')
release_validation=$(line_of_last 'validate_trusted_release')
guard_source=$(line_of 'source "$TRUSTED_RELEASE_ROOT/deploy/release-transaction-guard.sh"')
guard_lock=$(line_after "$guard_source" 'release_txn_verify_inherited_lock "$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD"')
active_read=$(line_of 'release_txn_read_active_fields "$TRANSACTION_ROOT"')
stage_find=$(line_of 'stage_root=$(release_txn_find_update_snapshot_stage')
pre_guard_stopped=$(line_after "$stage_find" 'verify_both_units_stopped')
pre_guard_active=$(line_after "$pre_guard_stopped" 'active marker changed before recovery guard installation')
pre_guard_identity=$(line_after "$pre_guard_active" 'active marker identity changed before recovery guard installation')
pre_guard_digest=$(line_after "$pre_guard_identity" 'active marker bytes changed before recovery guard installation')
pre_guard_stage=$(line_after "$pre_guard_digest" 'snapshot stage changed before recovery guard installation')
preserve_first=$(line_after "$pre_guard_stage" 'install_and_verify_runtime_directory_preserve')
unit_guards_first=$(line_after "$preserve_first" 'release_txn_install_and_verify_unit_guards')
stale_first=$(line_after "$unit_guards_first" 'release_txn_clear_stale_start_authorization')
post_guard_stopped=$(line_after "$stale_first" 'verify_both_units_stopped')
post_guard_active=$(line_after "$post_guard_stopped" 'active marker changed after recovery guard installation')
post_guard_identity=$(line_after "$post_guard_active" 'active marker identity changed after recovery guard installation')
post_guard_digest=$(line_after "$post_guard_identity" 'active marker bytes changed after recovery guard installation')
post_guard_stage=$(line_after "$post_guard_digest" 'snapshot stage changed after recovery guard installation')
runtime_lock_dir=$(line_of_last 'prepare_runtime_mutation_lock_dir')
preserve_second=$(line_after "$runtime_lock_dir" 'install_and_verify_runtime_directory_preserve')
unit_guards_second=$(line_after "$preserve_second" 'release_txn_install_and_verify_unit_guards')
stale_second=$(line_after "$unit_guards_second" 'release_txn_clear_stale_start_authorization')
runtime_stopped=$(line_after "$stale_second" 'verify_both_units_stopped')
runtime_active=$(line_after "$runtime_lock_dir" 'active marker changed while preparing the ephemeral mutation lock')
runtime_identity=$(line_after "$runtime_active" 'active marker identity changed while preparing the ephemeral mutation lock')
runtime_digest=$(line_after "$runtime_identity" 'active marker bytes changed while preparing the ephemeral mutation lock')
runtime_stage=$(line_after "$runtime_digest" 'snapshot stage changed while preparing the ephemeral mutation lock')
mutation_lock=$(line_of_last 'acquire_mutation_lock')
mutation_lock_verify=$(line_after "$mutation_lock" 'verify_mutation_lock_held')
agent_idle=$(line_after "$mutation_lock" 'run_trusted_agent_idle_check')
idle_lock_verify=$(line_after "$agent_idle" 'verify_mutation_lock_held')
recovery_dir=$(line_after "$agent_idle" 'ensure_recovery_snapshot_directory')
rescue_pre_lock=$(line_after "$recovery_dir" 'verify_mutation_lock_held')
rescue_snapshot=$(line_after "$rescue_pre_lock" '--ensure-service-operation-rescue-snapshot "$rescue_destination"')
rescue_post_lock=$(line_after "$rescue_snapshot" 'verify_mutation_lock_held')
rescue_marker=$(line_after "$rescue_snapshot" 'active marker identity changed during rescue snapshot creation')
rescue_idle=$(line_after "$rescue_marker" 'run_trusted_agent_idle_check')
rescue_idle_lock=$(line_after "$rescue_idle" 'verify_mutation_lock_held')
rescue_verify=$(line_after "$rescue_marker" 'verify_root_snapshot_file "$rescue_destination"')
stage_absent=$(line_after "$rescue_verify" 'stage snapshot already exists; fail-closed with the rescue preserved')
stage_pre_lock=$(line_after "$stage_absent" 'verify_mutation_lock_held')
stage_snapshot=$(line_after "$stage_pre_lock" '--create-service-operation-snapshot "$snapshot_destination"')
stage_post_lock=$(line_after "$stage_snapshot" 'verify_mutation_lock_held')
post_marker=$(line_after "$stage_snapshot" 'active marker identity changed during database recovery')
final_idle=$(line_after "$post_marker" 'run_trusted_agent_idle_check')
final_idle_lock=$(line_after "$final_idle" 'verify_mutation_lock_held')
post_rescue=$(line_after "$post_marker" 'verify_root_snapshot_file "$rescue_destination"')
post_stage=$(line_after "$post_rescue" 'verify_root_snapshot_file "$snapshot_destination"')

assert_before "$release_lock" "$release_validation" \
    "persistent transaction lock must precede release-controlled validation"
assert_before "$release_validation" "$guard_source" \
    "complete release validation must precede sourcing its guard"
assert_before "$guard_source" "$guard_lock" \
    "trusted guard must verify the already-held release lock"
assert_before "$guard_lock" "$active_read" \
    "trusted inherited-lock proof must precede active marker inspection"
assert_before "$active_read" "$stage_find" \
    "exact active marker must be read before locating its stage"
assert_before "$stage_find" "$pre_guard_stopped" \
    "both coordinators must be proven stopped after locating the active stage"
assert_before "$pre_guard_stopped" "$pre_guard_active" \
    "active marker token must be reproven before any systemd mutation"
assert_before "$pre_guard_active" "$pre_guard_identity" \
    "active marker identity must be reproven before any systemd mutation"
assert_before "$pre_guard_identity" "$pre_guard_digest" \
    "active marker digest must follow its identity proof"
assert_before "$pre_guard_digest" "$pre_guard_stage" \
    "active stage must be reproven before any systemd mutation"
assert_before "$pre_guard_stage" "$preserve_first" \
    "runtime preservation must happen only after exact active recovery proof"
assert_before "$preserve_first" "$unit_guards_first" \
    "runtime preservation must precede installing start guards"
assert_before "$unit_guards_first" "$stale_first" \
    "start guards must be proven before clearing stale authorization"
assert_before "$stale_first" "$post_guard_stopped" \
    "coordinators must be reproven stopped after systemd guard mutation"
assert_before "$post_guard_stopped" "$post_guard_active" \
    "active token must be reproven after systemd guard mutation"
assert_before "$post_guard_active" "$post_guard_identity" \
    "active marker identity must follow its token reproof"
assert_before "$post_guard_identity" "$post_guard_digest" \
    "active marker digest must follow its identity reproof"
assert_before "$post_guard_digest" "$post_guard_stage" \
    "active stage must be reproven after systemd guard mutation"
assert_before "$post_guard_stage" "$runtime_lock_dir" \
    "runtime-lock recreation must follow all post-guard proofs"
assert_before "$runtime_lock_dir" "$preserve_second" \
    "runtime preservation must be reproven after runtime-lock recreation"
assert_before "$preserve_second" "$unit_guards_second" \
    "start guards must be reproven after runtime-lock recreation"
assert_before "$unit_guards_second" "$stale_second" \
    "stale authorization must be cleared again after runtime-lock recreation"
assert_before "$stale_second" "$runtime_stopped" \
    "coordinators must be reproven stopped after repeated guards"
assert_before "$runtime_stopped" "$runtime_active" \
    "active marker must be reproven after runtime-lock recreation"
assert_before "$runtime_active" "$runtime_identity" \
    "active marker identity must be reproven after its token"
assert_before "$runtime_identity" "$runtime_digest" \
    "active marker bytes must be reproven after its identity"
assert_before "$runtime_digest" "$runtime_stage" \
    "active snapshot stage must be reproven after runtime-lock recreation"
assert_before "$runtime_stage" "$mutation_lock" \
    "runtime-lock recreation proofs must precede mutation-lock acquisition"
assert_before "$mutation_lock" "$mutation_lock_verify" \
    "new mutation lock pathname and descriptor must be proven immediately"
assert_before "$mutation_lock_verify" "$agent_idle" \
    "mutation flock must be held before the trusted agent idle proof"
assert_before "$agent_idle" "$idle_lock_verify" \
    "mutation lock identity must survive the trusted agent idle proof"
assert_before "$agent_idle" "$recovery_dir" \
    "idle proof must precede creation of the exact rescue directory"
assert_before "$recovery_dir" "$rescue_pre_lock" \
    "mutation lock identity must be checked immediately before rescue publication"
assert_before "$rescue_pre_lock" "$rescue_snapshot" \
    "private recovery directory must exist before rescue publication"
assert_before "$rescue_snapshot" "$rescue_post_lock" \
    "mutation lock identity must be checked immediately after rescue publication"
assert_before "$rescue_post_lock" "$rescue_marker" \
    "active marker must be reproven after rescue publication"
assert_before "$rescue_marker" "$rescue_idle" \
    "trusted agent idle state must be reproven after rescue publication"
assert_before "$rescue_idle" "$rescue_idle_lock" \
    "mutation lock identity must survive the post-rescue agent idle proof"
assert_before "$rescue_idle_lock" "$rescue_verify" \
    "rescue metadata verification must follow marker identity proof"
assert_before "$rescue_verify" "$stage_absent" \
    "durable rescue must be valid before an existing stage fails closed"
assert_before "$stage_absent" "$stage_pre_lock" \
    "mutation lock identity must be checked immediately before stage publication"
assert_before "$stage_pre_lock" "$stage_snapshot" \
    "stage absence must be proven only after rescue is durable"
assert_before "$stage_snapshot" "$stage_post_lock" \
    "mutation lock identity must be checked immediately after stage publication"
assert_before "$stage_post_lock" "$post_marker" \
    "active marker must be reproven after stage snapshot creation"
assert_before "$post_marker" "$final_idle" \
    "trusted agent idle state must be reproven after stage snapshot creation"
assert_before "$final_idle" "$final_idle_lock" \
    "mutation lock identity must survive the final agent idle proof"
assert_before "$final_idle_lock" "$post_rescue" \
    "rescue must be revalidated after canonical normalization"
assert_before "$post_rescue" "$post_stage" \
    "stage snapshot must be checked after the rescue survives"

require_text 'RECOVERY_SNAPSHOT_ROOT=/var/backups/celikpanel/recovery-snapshots' \
    "rescue is not outside the active update stage"
require_text 'recovery snapshot root must be root:root mode 0700' \
    "private rescue root metadata contract is missing"
require_text 'exact recovery snapshot directory must be root:root mode 0700' \
    "exact rescue directory metadata contract is missing"
require_text 'sync -f -- "$rescue_destination" "$recovery_snapshot_dir"' \
    "rescue file and directory durability proof is missing"
require_text '--ensure-service-operation-rescue-snapshot "$rescue_destination"' \
    "trusted panel rescue operation is missing"
require_text 'stage snapshot already exists; fail-closed with the rescue preserved' \
    "existing stage does not fail closed after preserving rescue"
require_text 'Durable rescue: $rescue_destination' \
    "operator handoff omits the durable rescue path"

require_text 'prepare_runtime_mutation_lock_dir() {' \
    "ephemeral mutation-lock directory preparation is missing"
require_text 'RELEASE_TRANSACTION_RUNTIME_ROOT=/run/celikpanel-release-transaction' \
    "release transaction runtime root is not fixed"
require_text 'RELEASE_TRANSACTION_HELPER=/usr/libexec/celikpanel/release-transaction-start-guard' \
    "release transaction start helper is not fixed"
require_text 'UNIT_DIR=/etc/systemd/system' \
    "systemd unit directory is not fixed"
require_text 'awk bash basename chmod chown cmp cp dirname find flock getent grep id install mkdir' \
    "privileged recovery tools are not included in the fixed-PATH prerequisite check"
require_text 'mktemp mv readlink rm sha256sum sort stat sync systemctl tr xargs' \
    "transaction-guard tools are not included in the fixed-PATH prerequisite check"
require_text 'trusted release transaction start guard is missing or unsafe' \
    "trusted start-guard source is not validated explicitly"
require_text 'install_and_verify_runtime_directory_preserve() {' \
    "runtime-directory preservation publisher is missing"
require_text 'target=$directory/09-runtime-directory-preserve.conf' \
    "runtime-directory preservation is not a separate lower-priority drop-in"
require_text 'tmp=$(mktemp -p "$directory" '\''.09-runtime-directory-preserve.conf.tmp.XXXXXXXXXX'\'')' \
    "runtime-directory preservation is not staged in the destination directory"
require_text 'printf '\''[Service]\nRuntimeDirectoryPreserve=yes\n'\'' > "$tmp"' \
    "runtime-directory preservation bytes are not exact"
require_text 'chown root:root -- "$tmp"' \
    "runtime-directory preservation ownership is not normalized before publication"
require_text 'chmod 0644 -- "$tmp"' \
    "runtime-directory preservation mode is not normalized before publication"
require_text 'sync -f -- "$tmp"' \
    "runtime-directory preservation file is not durable before publication"
require_text 'mv -T -- "$tmp" "$target"' \
    "runtime-directory preservation is not atomically published"
require_text 'sync -f -- "$directory"' \
    "runtime-directory preservation directory is not durable after publication"
require_text 'installed runtime-directory preserve drop-in metadata is unsafe' \
    "published runtime-directory preservation metadata is not revalidated"
require_text 'cmp -s "$target" <(printf '\''[Service]\nRuntimeDirectoryPreserve=yes\n'\'')' \
    "published runtime-directory preservation bytes are not revalidated"
require_text '"$SYSTEMCTL_BIN" daemon-reload' \
    "runtime-directory preservation reload is not bound to the exact systemctl binary"
require_text '"$SYSTEMCTL_BIN" show --property=DropInPaths --value celikpanel-agent.service' \
    "loaded runtime-directory preservation path is not proven"
require_text '"$SYSTEMCTL_BIN" show --property=RuntimeDirectoryPreserve --value celikpanel-agent.service' \
    "systemd manager runtime-directory preservation value is not proven"
require_text '[[ "$manager_value" == yes ]]' \
    "systemd manager must report RuntimeDirectoryPreserve=yes"
require_count 'install_and_verify_runtime_directory_preserve' 3 \
    "runtime-directory preservation must have one definition and two runtime proofs"
require_text 'release_txn_install_and_verify_unit_guards \' \
    "systemd transaction start guards are not installed and verified"
require_count 'release_txn_install_and_verify_unit_guards' 2 \
    "systemd transaction start guards must be installed and reproven"
require_text '"$UNIT_DIR" "$RELEASE_TRANSACTION_HELPER" "$RELEASE_TRANSACTION_FD"' \
    "transaction start-guard installation is not bound to fixed paths and inherited flock"
require_text 'release transaction service guards could not be installed and verified' \
    "transaction start-guard installation does not fail closed"
require_text 'release_txn_clear_stale_start_authorization \' \
    "stale release start authorization is not cleared"
require_count 'release_txn_clear_stale_start_authorization' 2 \
    "stale start authorization must be cleared before and after runtime recreation"
require_text '"$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_RUNTIME_ROOT" "$RELEASE_TRANSACTION_FD"' \
    "stale authorization cleanup is not bound to the persistent transaction flock"
require_text 'stale release start authorization could not be cleared' \
    "stale authorization cleanup does not fail closed"
require_text '[[ "$lock_dir" == /run/celikpanel ]]' \
    "runtime recreation is not pinned to the fixed mutation-lock directory"
require_text '[[ "$parent" == /run && -d "$parent" && ! -L "$parent" ]]' \
    "runtime recreation does not validate the fixed parent"
require_text 'validate_root_trusted_dir_chain "$parent"' \
    "runtime parent trusted-chain validation is missing"
require_text '[[ -d "$lock_dir" && ! -L "$lock_dir" ]]' \
    "an existing runtime lock directory is not rejected when unsafe"
require_text 'existing mutation lock directory must be root:celikpanel mode 0750' \
    "an existing runtime lock directory is not required to have exact metadata"
require_text 'install -d -m 0750 -o root -g celikpanel -- "$lock_dir"' \
    "runtime lock directory is not recreated with exact metadata"
require_text 'entries=("$lock_dir"/*)' \
    "runtime lock directory contents are not enumerated fail-closed"
require_text 'mutation lock directory contains unexpected entries' \
    "runtime lock directory does not reject multiple entries"
require_text '[[ "$entry" == "$MUTATION_LOCK" ]]' \
    "runtime lock directory permits an unexpected entry"
require_text 'existing mutation lock must be empty root:celikpanel mode 0600 with one link' \
    "existing mutation lock lacks exact metadata validation"
require_text '(umask 077; set -o noclobber; : > "$MUTATION_LOCK")' \
    "missing mutation lock is not created exclusively"
require_text 'chown root:celikpanel -- "$MUTATION_LOCK"' \
    "new mutation lock ownership is not normalized"
require_text 'chmod 0600 -- "$MUTATION_LOCK"' \
    "new mutation lock permissions are not normalized"
require_text 'mutation lock must be empty root:celikpanel mode 0600 with one link' \
    "created or existing mutation lock lacks strict metadata validation"
require_text 'verify_mutation_lock_held() {' \
    "held mutation-lock verifier is missing"
require_text '[[ -e "/proc/$BASHPID/fd/$MUTATION_LOCK_FD" ]]' \
    "held mutation-lock descriptor presence is not proven"
require_text 'path_identity=$(stat -Lc '\''%d:%i'\'' -- "$MUTATION_LOCK")' \
    "held mutation-lock pathname identity is not captured"
require_text 'fd_identity=$(stat -Lc '\''%d:%i'\'' -- "/proc/$BASHPID/fd/$MUTATION_LOCK_FD")' \
    "held mutation-lock descriptor identity is not captured"
require_text 'mutation lock pathname no longer names the held lock inode' \
    "held mutation-lock pathname and descriptor are not compared"
require_count 'verify_mutation_lock_held' 9 \
    "mutation-lock identity must be defined once and reproven eight times"
require_text 'held mutation lock directory must contain exactly the fixed lock' \
    "held mutation-lock verification does not reject missing or extra directory entries"
require_text 'held mutation lock directory contains an unexpected entry' \
    "held mutation-lock verification does not bind the sole entry to the fixed pathname"
require_text 'A coordinator that held the old /run/celikpanel lock inode before it stopped' \
    "deleted-old-lock-inode hazard is not documented at the authority transition"

require_text 'verify_unit_recursively_stopped() {' "recursive stopped-unit verifier is missing"
require_text 'systemctl show --property=Job --value "$unit"' \
    "stopped-unit verifier does not reject queued systemd jobs"
require_text '[[ -z "$job" ]]' \
    "stopped-unit verifier does not require an empty queued-job field"
require_text 'inactive|failed)' "stopped-unit verifier does not require inactive/failed"
require_text '[[ "$main_pid" == 0 ]]' "stopped-unit verifier does not require MainPID=0"
require_text 'find "$cgroup_root" -type f -name cgroup.procs -print0' \
    "nested cgroup.procs files are not inspected recursively"
require_text '| while IFS= read -r -d '\'''''\'' procs_file; do' \
    "recursive cgroup traversal is not a pipefail-protected pipeline"
if grep -Fq -- 'done < <(find "$cgroup_root" -type f -name cgroup.procs -print0)' "$candidate"; then
    fail "recursive cgroup traversal masks find failures through process substitution"
fi
require_text 'verify_unit_recursively_stopped celikpanel-agent.service' \
    "agent stopped proof is missing"
require_text 'verify_unit_recursively_stopped celikpanel-panel.service' \
    "panel stopped proof is missing"
require_text '--check-service-mutation-idle-under-external-lock' \
    "trusted agent does not use the external mutation-lock proof"
require_text 'CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD"' \
    "mutation-lock descriptor is not passed to the trusted agent"

require_text '--snapshot-schema=normal' "snapshot operations are not pinned to normal schema"
require_text '--release-transaction-fd="$RELEASE_TRANSACTION_FD"' \
    "snapshot operations do not receive the inherited release lock"
require_text '--release-transaction-token="$transaction_token"' \
    "snapshot operations are not bound to the exact active token"
require_text '--release-transaction-operation=update' \
    "snapshot operations are not bound to update"
require_text '--release-transaction-snapshot="$snapshot_name"' \
    "snapshot operations are not bound to the exact active snapshot"
require_text 'recovery snapshot must be root:root mode 0600 with one link' \
    "snapshot metadata postcondition is missing"
require_text 'canonical panel database was not normalized to celikpanel-owned mode 0600 with one link' \
    "canonical database metadata postcondition is missing"
require_text '"$mode" == 600 || "$mode" == 640 || "$mode" == 644' \
    "canonical source does not accept exactly the supported private/legacy modes"
require_text 'require_no_sqlite_sidecars "$rescue_destination"' \
    "rescue sidecars are not rejected"
require_text 'require_no_sqlite_sidecars "$snapshot_destination"' \
    "stage snapshot sidecars are not rejected"
require_text 'require_no_sqlite_sidecars "$PANEL_DB"' \
    "canonical database sidecars are not rejected"
require_text 'recovery unexpectedly published a final snapshot' \
    "final snapshot absence is not reproven"
require_text 'Both CelikPanel coordinators remain stopped.' \
    "success output does not state the stopped-service invariant"
require_text 'Do not run the recovery release updater.' \
    "success output does not prohibit the recovery updater"
require_text 'with its original immutable release path and original --normal invocation.' \
    "handoff does not require the original active-target updater invocation"

if grep -Fq -- '$TRUSTED_RELEASE_ROOT/update.sh' "$candidate"; then
    fail "recovery helper emits or invokes the recovery release updater"
fi
if grep -Eq '^[[:space:]]*(rm|rmdir|unlink|mv|cp)[[:space:]]' "$candidate"; then
    fail "recovery helper contains a destructive or untrusted snapshot-copy command"
fi
forbidden_systemctl_mutation='^[[:space:]]*(systemctl|"\$SYSTEMCTL_BIN")[[:space:]]+(start|stop|restart|try-restart|reload|enable|disable|mask|unmask)([[:space:]]|$)'
for command_prefix in systemctl '"$SYSTEMCTL_BIN"'; do
    for action in start stop restart try-restart reload enable disable mask unmask; do
        printf '%s %s celikpanel-agent.service\n' "$command_prefix" "$action" \
            | grep -Eq "$forbidden_systemctl_mutation" \
            || fail "systemctl mutation guard misses $command_prefix $action"
    done
    if printf '%s daemon-reload\n' "$command_prefix" \
        | grep -Eq "$forbidden_systemctl_mutation"; then
        fail "systemctl mutation guard rejects permitted $command_prefix daemon-reload"
    fi
done
if grep -Eq "$forbidden_systemctl_mutation" "$candidate"; then
    fail "recovery helper can mutate service state or authorization"
fi
if grep -Eq '^[[:space:]]*(kill|pkill|killall)[[:space:]]' "$candidate"; then
    fail "recovery helper can signal a process"
fi
if grep -Fq -- 'EXPECTED_TARGET' "$candidate"; then
    fail "obsolete combined target/recovery provenance remains"
fi
for forbidden in \
    release_txn_create_quiesce_marker \
    release_txn_promote_quiesce_to_active \
    release_txn_remove_quiesce_marker \
    release_txn_create_active_marker \
    release_txn_mark_completion_pending \
    release_txn_remove_completion_pending \
    release_txn_takeover_active_for_rollback \
    release_txn_reset_update_snapshot_stage \
    release_txn_cleanup_unmarked_update_snapshot_stage; do
    if grep -Fq -- "$forbidden" "$candidate"; then
        fail "recovery helper may mutate transaction state via $forbidden"
    fi
done

printf 'PASS: active-update database recovery static contract\n'
