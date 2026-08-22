#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
update=$repo_root/update.sh
rollback=$repo_root/rollback.sh
installer=$repo_root/install.sh
finalizer=$repo_root/deploy/finalize-pending-update.sh

fail() {
    printf 'release updater rollback contract failed: %s\n' "$1" >&2
    exit 1
}

line_of() {
    local file=$1 literal=$2
    grep -Fn -- "$literal" "$file" | head -n 1 | cut -d: -f1
}

for script in "$update" "$rollback" "$installer" "$finalizer"; do
    bash -n "$script" || fail "shell syntax is invalid: $script"
done

grep -Fq 'SNAPSHOT_VERSION=6' "$update" \
    || fail 'update does not create snapshot schema v6'
grep -Fq 'SUPPORTED_SNAPSHOT_VERSION=6' "$rollback" \
    || fail 'rollback does not require snapshot schema v6'
grep -Fq 'SNAPSHOT_VERSION=6' "$finalizer" \
    || fail 'pending-update finalizer does not require snapshot schema v6'
grep -Fq 'RELEASE_UPDATER=/usr/libexec/celikpanel/get.sh' "$update" \
    || fail 'update does not pin the installed updater path'
grep -Fq 'RELEASE_UPDATER=/usr/libexec/celikpanel/get.sh' "$rollback" \
    || fail 'rollback does not pin the installed updater path'
grep -Fq 'install_reviewed_release_updater' "$installer" \
    || fail 'installer does not publish the reviewed updater'
grep -Fq 'rollback intentionally retains them for alpha35 compatibility' "$update" \
    || fail 'update does not declare monotonic BIND root hardening rollback compatibility'
grep -Fq -- '--prepare-bind-generation-root-under-external-lock' "$update" \
    || fail 'signed updater does not prepare an exact managed legacy BIND root'
if grep -Fq 'dpkg-statoverride --remove' "$rollback"; then
    fail 'rollback removes monotonic BIND root hardening'
fi

capture_line=$(line_of "$update" 'printf '\''present\n'\'' > "$tmp_snap/release-updater.state"')
apply_line=$(line_of "$update" '/bin/bash "$TRUSTED_RELEASE_ROOT/install.sh"')
publish_line=$(line_of "$installer" 'mv -T -- "$tmp" "$RELEASE_UPDATER"')
completion_line=$(line_of "$update" 'release_txn_mark_completion_pending \')
[[ -n "$capture_line" && -n "$apply_line" && -n "$publish_line" && -n "$completion_line" ]] \
    || fail 'update snapshot/publication order markers are missing'
proof_line_update=$(awk -v start="$apply_line" \
    'NR > start && /verify_installed_release_artifacts/ { print NR; exit }' "$update")
[[ "$capture_line" -lt "$apply_line" && "$apply_line" -lt "$proof_line_update" &&
   "$proof_line_update" -lt "$completion_line" ]] \
    || fail 'updater is not captured before apply and proved before completion'
grep -Fq 'cp -a "$RELEASE_UPDATER" "$tmp_snap/libexec/get.sh"' "$update" \
    || fail 'present updater bytes are not captured in the snapshot'
grep -Fq "printf 'absent\\n'" "$update" || fail 'absent updater state is not captured'

state_check_line=$(line_of "$rollback" 'release updater presence marker is missing or unsafe')
mutation_line=$(line_of "$rollback" 'rollback_mutation_started=1')
restore_line=$(line_of "$rollback" 'mv -T -- "$updater_tmp" "$RELEASE_UPDATER"')
proof_line=$(line_of "$rollback" 'restored release updater bytes differ from snapshot')
[[ -n "$state_check_line" && -n "$mutation_line" && -n "$restore_line" && -n "$proof_line" ]] \
    || fail 'rollback updater state/restore/proof markers are missing'
[[ "$state_check_line" -lt "$mutation_line" && "$mutation_line" -lt "$restore_line" &&
   "$restore_line" -lt "$proof_line" ]] \
    || fail 'rollback updater verification and restore order is unsafe'
grep -Fq 'rm -f -- "$RELEASE_UPDATER"' "$rollback" \
    || fail 'rollback cannot restore an absent updater state'
grep -Fq '[[ "$(stat -Lc '\''%u:%g:%a:%h'\'' -- "$RELEASE_UPDATER")" == 0:0:755:1 ]]' "$rollback" \
    || fail 'rollback does not prove restored updater metadata'

printf 'release updater rollback contract passed\n'
