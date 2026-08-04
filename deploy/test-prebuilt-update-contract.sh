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
require_literal 'for required_command in bash chmod chown cmp dirname env find grep mv od readlink'
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

require_before 'validate_release_tree "$SOURCE_ROOT" 0' 'chown -R root:root -- "$SOURCE_ROOT"'
require_before 'validate_release_tree "$SOURCE_ROOT" 0' 'mv -T --no-clobber -- "$SOURCE_ROOT" "$FINAL_RELEASE"'
require_before 'validate_release_tree "$FINAL_RELEASE" 1' 'bash "$FINAL_RELEASE/update.sh" --normal'

require_literal 'cp install.sh bootstrap-update.sh bootstrap-prebuilt-update.sh update.sh rollback.sh' "$makefile"
require_literal 'deploy/write-release-manifest.sh' "$makefile"
require_literal 'find "$root" -xdev -type f -links +1' "$repo_root/deploy/write-release-manifest.sh"
require_literal 'release.commit' "$makefile"
require_literal 'release.tree' "$makefile"

printf 'prebuilt update contract passed\n'
