#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
LIBRARY=$ROOT/deploy/release-unit-transition.sh
bash -n "$LIBRARY" "$0"
fail() { printf 'FAIL: %s\n' "$1" >&2; exit 1; }
expect_failure() {
    local message=$1
    shift
    if "$@"; then fail "$message"; fi
}
if [[ $EUID -ne 0 ]]; then
    printf 'SKIP: unit transition fixture requires root-owned file metadata\n'
    exit 0
fi
TRUSTED_RELEASE_ROOT=$ROOT
source "$ROOT/deploy/release-transaction-guard.sh"
source "$LIBRARY"
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
transaction_root=$tmp/transaction
snapshot_units=$tmp/snapshot-units
candidate_units=$tmp/candidate-units
systemd_root=$tmp/systemd
mkdir -m 0700 -- "$transaction_root" "$snapshot_units"
mkdir -m 0755 -- "$candidate_units" "$systemd_root"
: > "$transaction_root/transaction.lock"
chmod 0600 -- "$transaction_root/transaction.lock"
exec {lock_fd}<>"$transaction_root/transaction.lock"
flock -n -x "$lock_fd" || fail 'cannot hold transaction lock'
root_identity=$(release_txn_systemd_unit_root_identity "$systemd_root")
units=(celikpanel-agent.service celikpanel-panel.service celikpanel-firewall-restore.service)
for unit in "${units[@]}"; do
    printf '[Unit]\nDescription=old %s\n' "$unit" > "$snapshot_units/$unit"
    printf '[Unit]\nDescription=candidate %s\n' "$unit" > "$candidate_units/$unit"
    chmod 0644 -- "$snapshot_units/$unit" "$candidate_units/$unit"
done
# The target release's systemd source directory legitimately has other files.
printf 'unrelated recovery source\n' > "$candidate_units/celikpanel-release-recovery.service"
printf 'owner workload\n' > "$systemd_root/owner-workload.service"
chmod 0644 -- "$systemd_root/owner-workload.service"
owner_initial=$(sha256sum "$systemd_root/owner-workload.service")
args=("$transaction_root" "$lock_fd" "$snapshot_units" "$candidate_units" "$systemd_root" present "$root_identity")

reset_old() {
    for unit in "${units[@]}"; do
        rm -f -- "$systemd_root/$unit"
        cp -- "$snapshot_units/$unit" "$systemd_root/$unit"
        chown root:root -- "$systemd_root/$unit"
        chmod 0644 -- "$systemd_root/$unit"
    done
}
assert_tree() {
    local expected=$1
    for unit in "${units[@]}"; do
        cmp -s -- "$expected/$unit" "$systemd_root/$unit" || fail "wrong $unit contents"
        [[ $(stat -Lc '%u:%g:%a:%h' "$systemd_root/$unit") == 0:0:644:1 ]] || fail 'wrong published metadata'
    done
    [[ $(sha256sum "$systemd_root/owner-workload.service") == "$owner_initial" ]] || fail 'unrelated owner unit changed'
}
assert_no_stage() {
    [[ -z $(find "$systemd_root" -maxdepth 1 -name '.celikpanel-unit.*' -print -quit) ]] || fail 'failed call leaked its stage'
}

reset_old
release_unit_validate_transition "${args[@]}"
release_unit_publish_transition "${args[@]}"
assert_tree "$candidate_units"
unchanged_identity=$(stat -Lc '%d:%i:%Y:%Z' "$systemd_root/celikpanel-agent.service")
release_unit_publish_transition "${args[@]}"
[[ $(stat -Lc '%d:%i:%Y:%Z' "$systemd_root/celikpanel-agent.service") == "$unchanged_identity" ]] || fail 'candidate retry replaced unchanged file'
release_unit_restore_transition "${args[@]}"
assert_tree "$snapshot_units"
unchanged_identity=$(stat -Lc '%d:%i:%Y:%Z' "$systemd_root/celikpanel-agent.service")
release_unit_restore_transition "${args[@]}"
[[ $(stat -Lc '%d:%i:%Y:%Z' "$systemd_root/celikpanel-agent.service") == "$unchanged_identity" ]] || fail 'restore retry replaced unchanged file'

# Any per-file old/candidate mixture can be published or restored.
cp -- "$candidate_units/celikpanel-panel.service" "$systemd_root/celikpanel-panel.service"
release_unit_validate_transition "${args[@]}"
release_unit_restore_transition "${args[@]}"
assert_tree "$snapshot_units"
cp -- "$candidate_units/celikpanel-agent.service" "$systemd_root/celikpanel-agent.service"
release_unit_publish_transition "${args[@]}"
assert_tree "$candidate_units"

reset_old
printf 'owner edit\n' > "$systemd_root/celikpanel-agent.service"
expect_failure 'owner edit admitted' release_unit_validate_transition "${args[@]}"
expect_failure 'restore overwrote owner edit' release_unit_restore_transition "${args[@]}"
grep -Fx 'owner edit' "$systemd_root/celikpanel-agent.service" >/dev/null || fail 'owner edit lost'
reset_old
rm -- "$systemd_root/celikpanel-agent.service"
expect_failure 'missing coordinator admitted' release_unit_publish_transition "${args[@]}"
[[ ! -e $systemd_root/celikpanel-agent.service ]] || fail 'missing coordinator recreated without evidence'
reset_old
rm -- "$systemd_root/celikpanel-agent.service"
ln -s "$snapshot_units/celikpanel-agent.service" "$systemd_root/celikpanel-agent.service"
expect_failure 'symlink admitted' release_unit_validate_transition "${args[@]}"
reset_old
ln -- "$systemd_root/celikpanel-agent.service" "$tmp/unit-alias"
expect_failure 'hard link admitted' release_unit_validate_transition "${args[@]}"
rm -- "$tmp/unit-alias"
chmod 0664 -- "$systemd_root/celikpanel-agent.service"
expect_failure 'unsafe mode admitted' release_unit_validate_transition "${args[@]}"
chmod 0644 -- "$systemd_root/celikpanel-agent.service"
chown 1:1 -- "$systemd_root/celikpanel-agent.service"
expect_failure 'owner change admitted' release_unit_validate_transition "${args[@]}"
chown root:root -- "$systemd_root/celikpanel-agent.service"
chmod 0600 -- "$candidate_units/celikpanel-agent.service"
expect_failure 'unsafe candidate source admitted' release_unit_validate_transition "${args[@]}"
chmod 0644 -- "$candidate_units/celikpanel-agent.service"
printf 'extra\n' > "$snapshot_units/extra"
expect_failure 'noncanonical snapshot set admitted' release_unit_validate_transition "${args[@]}"
rm -- "$snapshot_units/extra"
bad_args=("${args[@]}")
bad_args[1]=999
expect_failure 'closed inherited lock admitted' release_unit_validate_transition "${bad_args[@]}"
bad_args=("${args[@]}")
bad_args[6]=0:0
expect_failure 'changed unit parent identity admitted' release_unit_validate_transition "${bad_args[@]}"

# Wrappers inject failures at actual filesystem boundaries; production file
# validators, lock proof, staging, rename and recovery functions remain real.
injection=none
rename_count=0
sync() {
    command sync "$@" || return 1
    if [[ $injection == late-owner && $# -eq 3 && $1 == -f && $2 == -- &&
          $3 == "$systemd_root/.celikpanel-unit."* ]]; then
        printf 'late owner edit\n' > "$systemd_root/celikpanel-agent.service"
        injection=none
    fi
}
mv() {
    if [[ $# -eq 4 && $1 == -T && $2 == -- && $3 == "$systemd_root/.celikpanel-unit."* ]]; then
        local destination=$4
        [[ -f $destination ]] || fail 'replacement exposed an absent coordinator'
        cmp -s "$destination" "$snapshot_units/${destination##*/}" ||
            cmp -s "$destination" "$candidate_units/${destination##*/}" || fail 'replacement observed partial live bytes'
        rename_count=$((rename_count + 1))
        if [[ $injection == fail-panel && $destination == "$systemd_root/celikpanel-panel.service" ]]; then
            return 1
        fi
    fi
    command mv "$@" || return 1
    if [[ ${destination:-} == "$systemd_root/celikpanel-firewall-restore.service" ]]; then
        if [[ $injection == revert-published-agent ]]; then
            cp -- "$snapshot_units/celikpanel-agent.service" "$systemd_root/celikpanel-agent.service"
            injection=none
        elif [[ $injection == revert-restored-agent ]]; then
            cp -- "$candidate_units/celikpanel-agent.service" "$systemd_root/celikpanel-agent.service"
            injection=none
        fi
    fi
}

reset_old
injection=late-owner
expect_failure 'late owner edit overwritten after stage fsync' release_unit_publish_transition "${args[@]}"
grep -Fx 'late owner edit' "$systemd_root/celikpanel-agent.service" >/dev/null || fail 'late owner edit lost'
cmp -s "$snapshot_units/celikpanel-panel.service" "$systemd_root/celikpanel-panel.service" || fail 'preflight rejection changed next unit'
assert_no_stage
reset_old
injection=fail-panel
expect_failure 'rename failure did not propagate' release_unit_publish_transition "${args[@]}"
cmp -s "$candidate_units/celikpanel-agent.service" "$systemd_root/celikpanel-agent.service" || fail 'first atomic publication missing'
cmp -s "$snapshot_units/celikpanel-panel.service" "$systemd_root/celikpanel-panel.service" || fail 'failed replacement removed old file'
assert_no_stage
injection=none
release_unit_restore_transition "${args[@]}"
assert_tree "$snapshot_units"
release_unit_publish_transition "${args[@]}"
injection=fail-panel
expect_failure 'interrupted restoration did not propagate' release_unit_restore_transition "${args[@]}"
cmp -s "$snapshot_units/celikpanel-agent.service" "$systemd_root/celikpanel-agent.service" || fail 'first restored unit missing'
cmp -s "$candidate_units/celikpanel-panel.service" "$systemd_root/celikpanel-panel.service" || fail 'interrupted restore removed candidate unit'
assert_no_stage
injection=none
release_unit_restore_transition "${args[@]}"
assert_tree "$snapshot_units"
[[ $rename_count -gt 0 ]] || fail 'atomic rename boundary was not exercised'
reset_old
injection=revert-published-agent
expect_failure 'publication reported complete for an allowed but old unit' release_unit_publish_transition "${args[@]}"
release_unit_validate_transition "${args[@]}"
injection=none
release_unit_publish_transition "${args[@]}"
injection=revert-restored-agent
expect_failure 'restoration reported complete for an allowed but candidate unit' release_unit_restore_transition "${args[@]}"
release_unit_validate_transition "${args[@]}"
injection=none
release_unit_restore_transition "${args[@]}"
assert_tree "$snapshot_units"
unset -f mv sync

# Redirect only the fixed selected reader into this private fixture. All unit
# metadata, byte-state, inherited flock and atomic replacement checks are real.
# The reader itself has root-owned artifact/metadata/read-only Go tests.
eval "$(declare -f _release_unit_verify_firewall_helper | sed 's@/usr/libexec/celikpanel/recovery@"$tmp/selected-recovery"@g')"
cat > "$tmp/selected-recovery" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[[ $# == 3 && $1 == verify-firewall-unit && $2 == --unit ]] || exit 91
printf '%s\n' "$3" >> "$FIXTURE_PROOF_CALLS"
[[ $3 != "${FIXTURE_REJECT_UNIT:-}" ]]
SH
chmod 0755 "$tmp/selected-recovery"
export FIXTURE_PROOF_CALLS=$tmp/helper-proof.calls
: > "$FIXTURE_PROOF_CALLS"
firewall=celikpanel-firewall-restore.service
cp -- "$snapshot_units/$firewall" "$tmp/legacy-firewall"
printf 'ExecStart=/usr/libexec/celikpanel/firewall/candidate/restore --restore\n' >> "$candidate_units/$firewall"
reset_old
export FIXTURE_REJECT_UNIT=$candidate_units/$firewall
expect_failure 'missing candidate helper permitted publication' release_unit_publish_transition "${args[@]}"
assert_tree "$snapshot_units"
[[ $(wc -l < "$FIXTURE_PROOF_CALLS") == 1 ]] || fail 'helper refusal was not before unit writes'
unset FIXTURE_REJECT_UNIT
release_unit_publish_transition "${args[@]}"
assert_tree "$candidate_units"
# Corruption after publication must prevent an idempotent success, but must not
# make the unusable candidate helper a prerequisite for the valid old rollback.
export FIXTURE_REJECT_UNIT=$candidate_units/$firewall
expect_failure 'unchanged candidate unit hid damaged helper' release_unit_publish_transition "${args[@]}"
proof_count=$(wc -l < "$FIXTURE_PROOF_CALLS")
release_unit_restore_transition "${args[@]}"
assert_tree "$snapshot_units"
[[ $(wc -l < "$FIXTURE_PROOF_CALLS") == "$proof_count" ]] || fail 'legacy rollback required damaged candidate helper'
unset FIXTURE_REJECT_UNIT
# A retained independent old destination must itself be proved before any unit
# is restored. Candidate health does not prove old generation health.
printf 'ExecStart=/usr/libexec/celikpanel/firewall/old/restore --restore\n' >> "$snapshot_units/$firewall"
reset_old
release_unit_publish_transition "${args[@]}"
export FIXTURE_REJECT_UNIT=$snapshot_units/$firewall
expect_failure 'damaged old helper permitted restore' release_unit_restore_transition "${args[@]}"
assert_tree "$candidate_units"
unset FIXTURE_REJECT_UNIT
release_unit_restore_transition "${args[@]}"
assert_tree "$snapshot_units"
# A read error is unknown, never evidence of a legacy unit.
expect_failure 'unreadable source was treated as legacy' _release_unit_verify_firewall_helper "$tmp/missing-unit"
cp -- "$tmp/legacy-firewall" "$snapshot_units/$firewall"
reset_old

# A firewall unit absent in the old snapshot may be added by publication, or
# already absent on an interrupted restoration retry. No other absence is valid.
rm -- "$snapshot_units/celikpanel-firewall-restore.service"
rm -- "$systemd_root/celikpanel-firewall-restore.service"
args[5]=absent
release_unit_validate_transition "${args[@]}"
release_unit_publish_transition "${args[@]}"
assert_tree "$candidate_units"
release_unit_restore_transition "${args[@]}"
[[ ! -e $systemd_root/celikpanel-firewall-restore.service ]] || fail 'old firewall absence not restored'
release_unit_restore_transition "${args[@]}"
release_unit_validate_transition "${args[@]}"
printf 'owner firewall unit\n' > "$systemd_root/celikpanel-firewall-restore.service"
chmod 0644 -- "$systemd_root/celikpanel-firewall-restore.service"
expect_failure 'owner firewall unit removed' release_unit_restore_transition "${args[@]}"
grep -Fx 'owner firewall unit' "$systemd_root/celikpanel-firewall-restore.service" >/dev/null || fail 'owner firewall unit lost'
[[ $(sha256sum "$systemd_root/owner-workload.service") == "$owner_initial" ]] || fail 'unrelated owner unit changed'
assert_no_stage
printf 'PASS: verified old/candidate unit transitions, atomic publication/restoration and owner-change refusal\n'
