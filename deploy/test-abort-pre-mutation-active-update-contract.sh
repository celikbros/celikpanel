#!/usr/bin/env bash
set -euo pipefail

candidate=${1:-deploy/abort-pre-mutation-active-update.sh}
guard=${2:-deploy/release-transaction-guard.sh}
[[ -f "$candidate" ]] || {
    printf 'FAIL: missing pre-mutation abort helper: %s\n' "$candidate" >&2
    exit 1
}
[[ -f "$guard" ]] || {
    printf 'FAIL: missing release transaction guard: %s\n' "$guard" >&2
    exit 1
}
bash -n "$candidate"
bash -n "$guard"
if LC_ALL=C grep -q $'\r' "$candidate" || LC_ALL=C grep -q $'\r' "$guard"; then
    printf 'FAIL: pre-mutation recovery scripts contain CR bytes\n' >&2
    exit 1
fi

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

require_text() {
    grep -Fq -- "$1" "$candidate" || fail "$2"
}

require_guard_text() {
    grep -Fq -- "$1" "$guard" || fail "$2"
}

reject_text() {
    if grep -Fq -- "$1" "$candidate"; then
        fail "$2"
    fi
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
require_text 'umask 077' "private umask is missing"
require_text '[[ $# -eq 10 &&' "exact ten-argument gate is missing"
require_text '"$ACTIVE_TARGET" =~ ^[0-9a-f]{40}$' \
    "active target is not exactly 40 lowercase hex"
require_text '"$ACTIVE_TOKEN" =~ ^[0-9a-f]{64}$' \
    "active token is not exactly 64 lowercase hex"
require_text '"$PREVIOUS_RELEASE_COMMIT" =~ ^[0-9a-f]{40}$' \
    "previous release commit is not explicit and canonical"
require_text '"$PREVIOUS_AGENT_SHA256" =~ ^[0-9a-f]{64}$' \
    "previous agent digest is not explicit and canonical"
require_text '"$PREVIOUS_PANEL_SHA256" =~ ^[0-9a-f]{64}$' \
    "previous panel digest is not explicit and canonical"
require_text '"$PREVIOUS_WEB_SHA256" =~ ^[0-9a-f]{64}$' \
    "previous web digest is not explicit and canonical"
require_text '"$RECOVERY_COMMIT" =~ ^[0-9a-f]{40}$' \
    "recovery commit is not explicit and canonical"
require_text '[[ $EUID -eq 0 ]]' "root-only guard is missing"

require_text '[[ "$relative" =~ ^[0-9a-f]{12}-[0-9a-f]{24}$ &&' \
    "immutable release basename is not canonical"
require_text '"${relative:0:12}" == "${RECOVERY_COMMIT:0:12}"' \
    "immutable release basename is not commit-bound"
require_text '| cmp -s - SHA256SUMS' \
    "immutable release manifest closure is not recomputed"
require_text 'sha256sum -c SHA256SUMS' \
    "immutable release manifest entries are not verified"
require_text '"$commit" == "$RECOVERY_COMMIT"' \
    "immutable release provenance is not recovery-commit-bound"
require_text 'pre-mutation recovery helper must execute from the verified trusted release' \
    "running helper is not pinned to the immutable release"
require_text 'source "$TRUSTED_RELEASE_ROOT/deploy/release-transaction-guard.sh"' \
    "transaction guard is not sourced from the immutable release"

require_text '[[ "$entries" == $'\''SHA256SUMS\nagent\npanel\nweb.tar.gz'\'' ]]' \
    "previous release exact direct-file allowlist is missing"
require_text 'previous release checksum manifest differs from the explicit reviewed hashes' \
    "previous release manifest is not bound to explicit digests"
require_text 'cmp -s "$canonical/$file" "$BIN_DIR/$file"' \
    "installed binaries are not byte-compared with the previous release"
require_text 'installed web tree structure differs from the previous release' \
    "installed web structure is not compared with the previous release"
require_text 'installed web bytes differ from the previous release' \
    "installed web bytes are not compared with the previous release"
require_text '[[ "$entries" == $'\''assets\nindex.html\nvite.svg'\'' ]]' \
    "reviewed historical web top-level allowlist is missing"
require_text '[[ "$(find "$verification_tmp" -xdev -type d | wc -l)" -eq 2 &&' \
    "reviewed historical web archive exact counts are missing"

require_text 'celikpanel-agent.service\tenabled\tactive' \
    "exact incident agent service state is missing"
require_text 'celikpanel-panel.service\tenabled\tactive' \
    "exact incident panel service state is missing"
require_text 'celikpanel-firewall-restore.service\tnot-found\tinactive' \
    "exact incident firewall service state is missing"
require_text 'celikpanel-agent.service\tactive\t748468\t121952692' \
    "exact incident agent coordinator identity is missing"
require_text 'celikpanel-panel.service\tactive\t748470\t121952692' \
    "exact incident panel coordinator identity is missing"
require_text 'captured pre-mutation coordinator process still exists' \
    "captured coordinator PID/start-time reuse proof is missing"
require_text 'snapshot storage contains another incomplete release stage' \
    "single incomplete-stage proof is missing"
require_text 'final snapshot exists; pre-mutation abort is forbidden' \
    "final-snapshot absence proof is missing"
require_text 'pre-ledger stage contains payload beyond the exact reviewed allowlist' \
    "exact pre-ledger payload allowlist is missing"
require_text 'snapshot transition is not the exact pre-ledger state' \
    "exact pre-ledger transition proof is missing"

require_text 'RuntimeDirectoryPreserve=yes' \
    "runtime mutation-lock preservation is missing"
require_text 'release_txn_install_and_verify_unit_guards \' \
    "persistent systemd start guards are missing"
require_text 'flock -n -x "$MUTATION_LOCK_FD"' \
    "nonblocking mutation-lock acquisition is missing"
require_text 'remove_stale_agent_socket_under_lock()' \
    "locked stale-agent socket cleanup is missing"
require_text 'verify_running_unit_executable()' \
    "running executable identity proof is missing"
require_text '"/proc/$main_pid/exe"' \
    "running executable must be proven from procfs"
require_text 'verify_both_units_stopped' \
    "recursive stopped-coordinator proof is missing"
require_text 'active marker identity or bytes changed during recovery' \
    "repeated active inode/digest proof is missing"
require_text 'both coordinators were stopped and the stage was left for inspection' \
    "post-marker-removal failure is not fail-closed"
require_text 'active marker and stage were preserved and both coordinators remain stopped' \
    "pre-marker-removal failure is not fail-closed"

reject_text '"$TRUSTED_RELEASE_ROOT/update.sh"' \
    "incident abort helper must not invoke the updater"
reject_text '"$TRUSTED_RELEASE_ROOT/rollback.sh"' \
    "incident abort helper must not invoke rollback"
reject_text 'create-service-operation-snapshot' \
    "incident abort helper must not mutate the service-operation database"
reject_text 'panel.db' "incident abort helper must not access the panel database"

transaction_lock=$(line_of_last 'acquire_release_transaction_lock')
release_validation=$(line_after "$transaction_lock" 'validate_trusted_recovery_release')
guard_source=$(line_after "$release_validation" \
    'source "$TRUSTED_RELEASE_ROOT/deploy/release-transaction-guard.sh"')
inherited_lock=$(line_after "$guard_source" 'release_txn_verify_inherited_lock')
active_capture=$(line_after "$inherited_lock" 'capture_active_marker_evidence')
stage_proof=$(line_after "$active_capture" 'verify_exact_preledger_stage')
saved_enablement=$(line_after "$stage_proof" 'verify_saved_enablement')
stopped_proof=$(line_after "$saved_enablement" 'verify_both_units_stopped')
exit_trap=$(line_after "$stopped_proof" 'trap recovery_exit EXIT')
artifact_proof=$(line_after "$exit_trap" \
    'validate_previous_release_and_installed_artifacts')
unit_guards=$(line_after "$artifact_proof" \
    'release_txn_install_and_verify_unit_guards \')
active_reproof=$(line_after "$unit_guards" 'verify_active_evidence')
mutation_lock=$(line_after "$active_reproof" 'open_mutation_lock')
locked_active_reproof=$(line_after "$mutation_lock" 'verify_active_evidence')
removal_begun=$(line_after "$locked_active_reproof" 'marker_removal_begun=1')
marker_remove=$(line_after "$removal_begun" \
    'release_txn_remove_pre_mutation_active_marker \')
markerless_prestart=$(line_after "$marker_remove" \
    'verify_markerless_pre_start_evidence')
agent_start=$(line_after "$markerless_prestart" 'wait_for_agent_ready')
panel_start=$(line_after "$agent_start" 'wait_for_panel_ready')
restored_proof=$(line_after "$panel_start" \
    'verify_markerless_restored_evidence')
stage_cleanup=$(line_after "$restored_proof" \
    'release_txn_cleanup_unmarked_update_snapshot_stage \')
success=$(line_after "$stage_cleanup" 'recovery_succeeded=1')

assert_before "$transaction_lock" "$release_validation" \
    "release lock must precede immutable release validation"
assert_before "$release_validation" "$guard_source" \
    "immutable release must be verified before recovery code is sourced"
assert_before "$guard_source" "$inherited_lock" \
    "sourced guard must verify the inherited release lock"
assert_before "$active_capture" "$stage_proof" \
    "exact active marker must be captured before stage trust"
assert_before "$stopped_proof" "$exit_trap" \
    "initial read-only stopped proof must precede the mutating failure trap"
assert_before "$exit_trap" "$artifact_proof" \
    "fail-closed trap must precede installed-artifact proof temp mutation"
assert_before "$artifact_proof" "$unit_guards" \
    "previous installed bytes must be proved before systemd guard mutation"
assert_before "$unit_guards" "$active_reproof" \
    "active evidence must be reproved after persistent guard installation"
assert_before "$active_reproof" "$mutation_lock" \
    "active evidence must be proved before mutation-lock acquisition"
assert_before "$mutation_lock" "$locked_active_reproof" \
    "active evidence must be reproved while the mutation lock is held"
assert_before "$locked_active_reproof" "$removal_begun" \
    "locked active proof must precede the terminal-removal state"
assert_before "$removal_begun" "$marker_remove" \
    "terminal-removal state must precede durable marker removal"
assert_before "$marker_remove" "$markerless_prestart" \
    "markerless stopped evidence must be proved immediately after removal"
assert_before "$markerless_prestart" "$agent_start" \
    "active marker must be durably absent before the agent starts"
assert_before "$agent_start" "$panel_start" \
    "agent readiness must precede panel start"
assert_before "$panel_start" "$restored_proof" \
    "restored runtime and byte proof must follow both starts"
assert_before "$restored_proof" "$stage_cleanup" \
    "evidence stage cleanup must be the last recovery mutation"
assert_before "$stage_cleanup" "$success" \
    "success must not be published before guarded stage cleanup"

require_guard_text 'release_txn_remove_pre_mutation_active_marker()' \
    "terminal pre-mutation active-marker primitive is missing"
require_guard_text 'pre-mutation active abort is defined only for update' \
    "terminal primitive does not reject non-update operations"
require_guard_text 'release_txn_validate_active_token "$root" "$token" "$operation" "$snapshot"' \
    "terminal primitive is not bound to the exact active token"
require_guard_text 'found=$(release_txn_find_update_snapshot_stage "$snapshot_root" "$snapshot")' \
    "terminal primitive does not resolve the canonical stage"
require_guard_text '[[ "$found" == "$stage" ]]' \
    "terminal primitive is not bound to the explicitly reviewed stage"
require_guard_text 'sync -f -- "$root"' \
    "terminal primitive does not durably sync marker removal"
require_guard_text 'pre-mutation active transaction marker still exists' \
    "terminal primitive does not prove marker absence before returning"

printf 'PASS: pre-mutation active update abort contract\n'
