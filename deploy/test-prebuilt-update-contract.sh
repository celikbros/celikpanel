#!/usr/bin/env bash
# Keep the public prebuilt update entry point inside the same immutable,
# transaction-safe boundary as the reviewed source-build updater.
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
bootstrap="$repo_root/bootstrap-prebuilt-update.sh"
makefile="$repo_root/Makefile"

fail() {
  printf 'prebuilt update contract failed: %s\n' "$1" >&2
  exit 1
}

require_literal() {
  local literal=$1 file=${2:-$bootstrap}
  grep -Fq -- "$literal" "$file" || fail "missing: $literal"
}

require_before() {
  local first=$1 second=$2 first_line second_line
  first_line=$(grep -Fn -- "$first" "$bootstrap" | head -n 1 | cut -d: -f1)
  second_line=$(grep -Fn -- "$second" "$bootstrap" | head -n 1 | cut -d: -f1)
  [[ -n "$first_line" && -n "$second_line" && "$first_line" -lt "$second_line" ]] \
    || fail "$first must precede $second"
}

[[ -f "$bootstrap" && ! -L "$bootstrap" ]] || fail "bootstrap is missing or unsafe"
bash -n "$bootstrap"

require_literal 'RELEASES_ROOT=/var/backups/celikpanel/releases'
require_literal 'for required_command in awk bash chmod chown cmp cut dirname env find flock getent grep id mv od'
require_literal 'prebuilt source is outside the fixed download staging boundary'
require_literal 'find "$root" -xdev -type l'
require_literal 'find "$root" -xdev -type f -links +1'
require_literal 'cmp -s - SHA256SUMS'
require_literal 'sha256sum -c SHA256SUMS'
require_literal 'release.version release.commit release.tree'
require_literal 'published release name is invalid'
require_literal 'mv -T --no-clobber -- "$SOURCE_ROOT" "$FINAL_RELEASE"'
require_literal 'CELIKPANEL_TRUSTED_RELEASE_ROOT="$FINAL_RELEASE"'
require_literal 'bash "$FINAL_RELEASE/update.sh" --normal'
require_literal 'release_txn_read_active_fields "$TRANSACTION_ROOT"'
require_literal 'release_txn_parse_update_snapshot_name "$active_snapshot"'
require_literal 'known_failed_target=8bbbac8b628fae4fca0e127e52c1c7835f56f8b8'
require_literal 'local TRUSTED_RELEASE_ROOT=$FINAL_RELEASE'
require_literal 'find "$RELEASES_ROOT" -xdev -mindepth 1 -maxdepth 1 -type d'
require_literal 'validate_release_tree "$known_release" 1'
require_literal 'the immutable alpha.4 release needed for recovery is unavailable'
require_literal 'release_txn_verify_inherited_lock "$TRANSACTION_ROOT" "$transaction_fd"'
require_literal 'find_known_alpha4_tls_failure_stage'
require_literal 'release_txn_validate_service_states "$child/service-states.tsv"'
require_literal 'captured alpha.4 coordinator process still exists'
require_literal 'validate_exact_alpha2_installed_artifacts'
require_literal 'validate_known_alpha4_tls_capture'
require_literal 'verify_active_marker_unchanged'
require_literal 'remove_known_alpha4_pre_snapshot_payload'
require_literal 'systemctl start "${saved_units[0]}"'
require_literal 'systemctl start "${saved_units[1]}"'
require_literal 'release_txn_remove_pre_mutation_active_marker'
require_literal 'release_txn_cleanup_unmarked_update_snapshot_stage'
require_literal 'panel_tls_normalize_legacy_self_signed "$PANEL_TLS_DIR" "$panel_uid" "$panel_gid"'

require_before 'validate_release_tree "$SOURCE_ROOT" 0' 'chown -R root:root -- "$SOURCE_ROOT"'
require_before 'validate_release_tree "$SOURCE_ROOT" 0' 'mv -T --no-clobber -- "$SOURCE_ROOT" "$FINAL_RELEASE"'
require_before 'validate_release_tree "$FINAL_RELEASE" 1' 'bash "$FINAL_RELEASE/update.sh" --normal'
require_before 'abort_known_older_pre_mutation_update' 'bash "$FINAL_RELEASE/update.sh" --normal'
require_before 'validate_release_tree "$known_release" 1' 'release_txn_verify_inherited_lock "$TRANSACTION_ROOT" "$transaction_fd"'
require_before 'panel_tls_normalize_legacy_self_signed "$PANEL_TLS_DIR" "$panel_uid" "$panel_gid"' 'release_txn_remove_pre_mutation_active_marker'
require_before 'release_txn_remove_pre_mutation_active_marker' 'systemctl start "${saved_units[0]}"'
require_before 'systemctl start "${saved_units[0]}"' 'systemctl start "${saved_units[1]}"'
require_before 'release_txn_remove_pre_mutation_active_marker' 'release_txn_cleanup_unmarked_update_snapshot_stage'

require_literal 'cp install.sh bootstrap-update.sh bootstrap-prebuilt-update.sh update.sh rollback.sh' "$makefile"
require_literal 'deploy/write-release-manifest.sh' "$makefile"
require_literal 'find "$root" -xdev -type f -links +1' "$repo_root/deploy/write-release-manifest.sh"
require_literal 'release.commit' "$makefile"
require_literal 'release.tree' "$makefile"

printf 'prebuilt update contract passed\n'
