#!/bin/bash
# Keep downloaded build toolchains compatible with the privileged updater:
# archive metadata cannot select owners, reused trees must be validated, and
# newly extracted trees must be sealed before any executable is trusted.
set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
INSTALL="$ROOT/install.sh"

die() {
    echo "install toolchain contract failed: $*" >&2
    exit 1
}

require_literal() {
    local literal=$1
    grep -Fq -- "$literal" "$INSTALL" || die "install.sh is missing: $literal"
}

reject_literal() {
    local literal=$1
    if grep -Fq -- "$literal" "$INSTALL"; then
        die "install.sh still contains forbidden text: $literal"
    fi
}

require_before() {
    local first=$1 second=$2 first_line second_line
    first_line=$(grep -Fn -- "$first" "$INSTALL" | head -n 1 | cut -d: -f1)
    second_line=$(grep -Fn -- "$second" "$INSTALL" | head -n 1 | cut -d: -f1)
    [[ -n "$first_line" && -n "$second_line" && "$first_line" -lt "$second_line" ]] ||
        die "install.sh sequence is invalid: $first must precede $second"
}

bash -n "$INSTALL"

require_literal 'validate_bootstrap_toolchain_tree() {'
require_literal 'seal_bootstrap_toolchain_tree() {'
require_literal '"$TOOLCHAIN/go"|"$TOOLCHAIN/node"|"$TOOLCHAIN"/.go-stage.*/go|"$TOOLCHAIN"/.node-stage.*) ;;'
require_literal '[[ "$target" == "$root"/* ]]'
require_literal '[[ "$link_value" != /* ]]'
require_literal '[ "$uid:$gid" = 0:0 ]'
require_literal '(( (8#$mode & 8#022) == 0 ))'
require_literal 'chown -R -h root:root -- "$root"'
require_literal 'chmod -R go-w -- "$root"'
require_literal 'GO_SHA256_AMD64=2852af0cb20a13139b3448992e69b868e50ed0f8a1e5940ee1de9e19a123b613'
require_literal 'GO_SHA256_ARM64=05de75d6994a2783699815ee553bd5a9327d8b79991de36e38b66862782f54ae'
require_literal 'NODE_SHA256_AMD64=55aa7153f9d88f28d765fcdad5ae6945b5c0f98a36881703817e4c450fa76742'
require_literal 'NODE_SHA256_ARM64=58c9520501f6ae2b52d5b210444e24b9d0c029a58c5011b797bc1fe7105886f6'
require_literal 'download_verified_toolchain_archive() {'
require_literal "curl --proto '=https' --tlsv1.2 --fail --location --retry 3"
require_literal 'actual_sha256=$(sha256sum -- "$archive"'
require_literal '[[ "$actual_sha256" != "$expected_sha256" ]]'
require_literal 'tar -xz --no-same-owner -C "$staging" --file "$archive"'
require_literal 'tar -xJ --no-same-owner -C "$staging" --strip-components=1 --file "$archive"'
require_literal 'mv -T --no-clobber -- "$staging/go" "$TOOLCHAIN/go"'
require_literal 'mv -T --no-clobber -- "$staging" "$TOOLCHAIN/node"'
require_literal 'validate_bootstrap_toolchain_tree "$TOOLCHAIN/go"'
require_literal 'validate_bootstrap_toolchain_tree "$TOOLCHAIN/node"'
require_literal 'validate_bootstrap_toolchain_tree "$staging/go"'
require_literal 'validate_bootstrap_toolchain_tree "$staging"'
require_literal 'seal_bootstrap_toolchain_tree "$TOOLCHAIN/go"'
require_literal 'seal_bootstrap_toolchain_tree "$TOOLCHAIN/node"'
reject_literal 'curl -fsSL "https://go.dev/dl/'
reject_literal 'curl -fsSL "https://nodejs.org/dist/'
require_literal 'npm ci --no-audit --no-fund'
reject_literal 'npm install --no-audit --no-fund'
require_before 'archive=$(download_verified_toolchain_archive "https://go.dev/' 'tar -xz --no-same-owner -C "$staging" --file "$archive"'
require_before 'archive=$(download_verified_toolchain_archive "https://nodejs.org/' 'tar -xJ --no-same-owner -C "$staging" --strip-components=1 --file "$archive"'

echo "install toolchain contract passed"
