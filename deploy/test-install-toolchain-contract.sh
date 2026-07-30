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

bash -n "$INSTALL"

require_literal 'validate_bootstrap_toolchain_tree() {'
require_literal 'seal_bootstrap_toolchain_tree() {'
require_literal '"$TOOLCHAIN/go"|"$TOOLCHAIN/node") ;;'
require_literal '[[ "$target" == "$root"/* ]]'
require_literal '[ "$uid" = 0 ]'
require_literal '(( (8#$mode & 8#022) == 0 ))'
require_literal 'chown -R -h root:root -- "$root"'
require_literal 'chmod -R go-w -- "$root"'
require_literal 'tar -xz --no-same-owner -C "$TOOLCHAIN"'
require_literal 'tar -xJ --no-same-owner -C "$TOOLCHAIN/node" --strip-components=1'
require_literal 'validate_bootstrap_toolchain_tree "$TOOLCHAIN/go"'
require_literal 'validate_bootstrap_toolchain_tree "$TOOLCHAIN/node"'
require_literal 'seal_bootstrap_toolchain_tree "$TOOLCHAIN/go"'
require_literal 'seal_bootstrap_toolchain_tree "$TOOLCHAIN/node"'

echo "install toolchain contract passed"
