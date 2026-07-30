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
mutation_lock=$(line_of_last 'acquire_mutation_lock')
agent_idle=$(line_after "$mutation_lock" 'run_trusted_agent_idle_check')
recovery_dir=$(line_after "$agent_idle" 'ensure_recovery_snapshot_directory')
rescue_snapshot=$(line_after "$recovery_dir" '--ensure-service-operation-rescue-snapshot "$rescue_destination"')
rescue_marker=$(line_after "$rescue_snapshot" 'active marker identity changed during rescue snapshot creation')
rescue_verify=$(line_after "$rescue_marker" 'verify_root_snapshot_file "$rescue_destination"')
stage_absent=$(line_after "$rescue_verify" 'stage snapshot already exists; fail-closed with the rescue preserved')
stage_snapshot=$(line_after "$stage_absent" '--create-service-operation-snapshot "$snapshot_destination"')
post_marker=$(line_after "$stage_snapshot" 'active marker identity changed during database recovery')
post_rescue=$(line_after "$post_marker" 'verify_root_snapshot_file "$rescue_destination"')
post_stage=$(line_after "$post_rescue" 'verify_root_snapshot_file "$snapshot_destination"')

assert_before "$release_lock" "$release_validation" \
    "persistent transaction lock must precede release-controlled validation"
assert_before "$release_validation" "$guard_source" \
    "complete release validation must precede sourcing its guard"
assert_before "$guard_source" "$guard_lock" \
    "trusted guard must verify the already-held release lock"
assert_before "$guard_lock" "$active_read" \
    "release lock must be verified before reading the active marker"
assert_before "$active_read" "$stage_find" \
    "exact active marker must be read before locating its stage"
assert_before "$stage_find" "$mutation_lock" \
    "canonical active stage must be located before mutation-lock acquisition"
assert_before "$mutation_lock" "$agent_idle" \
    "mutation flock must be held before the trusted agent idle proof"
assert_before "$agent_idle" "$recovery_dir" \
    "idle proof must precede creation of the exact rescue directory"
assert_before "$recovery_dir" "$rescue_snapshot" \
    "private recovery directory must exist before rescue publication"
assert_before "$rescue_snapshot" "$rescue_marker" \
    "active marker must be reproven after rescue publication"
assert_before "$rescue_marker" "$rescue_verify" \
    "rescue metadata verification must follow marker identity proof"
assert_before "$rescue_verify" "$stage_absent" \
    "durable rescue must be valid before an existing stage fails closed"
assert_before "$stage_absent" "$stage_snapshot" \
    "stage absence must be proven only after rescue is durable"
assert_before "$stage_snapshot" "$post_marker" \
    "active marker must be reproven after stage snapshot creation"
assert_before "$post_marker" "$post_rescue" \
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

require_text 'verify_unit_recursively_stopped() {' "recursive stopped-unit verifier is missing"
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
if grep -Eq '^[[:space:]]*systemctl[[:space:]]+(start|stop|restart|try-restart|reload|enable|disable|mask|unmask)' "$candidate"; then
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
