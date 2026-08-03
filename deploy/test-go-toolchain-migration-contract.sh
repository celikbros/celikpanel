#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
MIGRATOR="$ROOT/deploy/migrate-go-toolchain.sh"

fail() {
    printf 'go toolchain migration contract failed: %s\n' "$*" >&2
    exit 1
}

require_literal() {
    grep -Fq -- "$1" "$MIGRATOR" || fail "missing literal: $1"
}

require_absent() {
    if grep -Fq -- "$1" "$MIGRATOR"; then
        fail "forbidden literal present: $1"
    fi
}

require_order() {
    local first=$1 second=$2 first_line second_line
    first_line=$(grep -nF -- "$first" "$MIGRATOR" | head -n1 | cut -d: -f1)
    second_line=$(grep -nF -- "$second" "$MIGRATOR" | head -n1 | cut -d: -f1)
    [[ -n "$first_line" && -n "$second_line" && "$first_line" -lt "$second_line" ]] ||
        fail "invalid order: $first must precede $second"
}

extract_function() {
    local name=$1
    awk -v start="${name}() {" '
        $0 == start { capture = 1 }
        capture { print }
        capture && /^}$/ { exit }
    ' "$MIGRATOR"
}

bash -n "$MIGRATOR"

require_literal 'GO_VERSION=1.26.5'
require_literal 'GO_SHA256_AMD64=5c2c3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393f053'
require_literal 'GO_SHA256_ARM64=fe4789e92b1f33358680864bbe8704289e7bb5fc207d80623c308935bd696d49'
require_literal 'PATH=/usr/sbin:/usr/bin:/sbin:/bin'
require_absent '/usr/local/bin'
require_literal '[[ $EUID -eq 0 ]] || die "run as root"'
require_literal '[[ $# -eq 0 ]] || die "this migrator accepts no arguments"'
require_literal 'ENV_BIN=/usr/bin/env'
require_literal 'CURL_BIN=/usr/bin/curl'
require_literal 'TAR_BIN=/usr/bin/tar'
require_literal 'run_clean_external() {'
require_literal '"$ENV_BIN" -i HOME=/root PATH="$PATH" LC_ALL=C LANG=C "$@"'
require_literal '[[ "$(run_clean_external "$UNAME_BIN" -s)" == Linux ]] || die "this migrator supports Linux only"'
require_literal 'validate_parent /'
require_literal 'validate_parent /usr'
require_literal 'validate_parent /usr/bin'
require_literal 'validate_parent /opt'
require_literal "run_clean_external \"\$CURL_BIN\" --disable --proto '=https' --tlsv1.2 --fail --location --retry 3"
require_literal '[[ "$actual" == "$expected" ]] || die "Go archive SHA-256 mismatch"'
require_literal 'run_clean_external "$TAR_BIN" --extract --gzip --no-same-owner --no-same-permissions'
require_literal 'GOTOOLCHAIN=local GOENV=off GOWORK=off'
require_literal '[[ "$reported_root" == "$canonical_root" ]]'
require_literal '"$MV_BIN" -T --no-clobber -- "$retired_root" "$ACTIVE_ROOT"'
require_literal 'rollback_armed=1'
require_literal 'trap cleanup EXIT'
require_literal "trap 'exit 129' HUP"
require_literal "trap 'exit 130' INT"
require_literal "trap 'exit 143' TERM"
require_absent 'systemctl'
require_absent '/etc/'

require_order '[[ "$actual" == "$expected" ]]' 'run_clean_external "$TAR_BIN" --extract --gzip'
require_order 'go_tree_is_exact "$staging_root/go"' 'rollback_armed=1'
require_order 'rollback_armed=1' '"$MV_BIN" -T --no-clobber -- "$ACTIVE_ROOT" "$retired_root"'
require_order '"$MV_BIN" -T --no-clobber -- "$ACTIVE_ROOT" "$retired_root"' '"$MV_BIN" -T --no-clobber -- "$staging_root/go" "$ACTIVE_ROOT"'

eval "$(extract_function version_is_older)"
version_is_older go1.25.0 || fail 'go1.25.0 was not accepted as older'
version_is_older go1.26.4 || fail 'go1.26.4 was not accepted as older'
if version_is_older go1.26.5; then fail 'exact version was treated as older'; fi
if version_is_older go1.27.0; then fail 'newer version was treated as older'; fi
if version_is_older invalid; then fail 'invalid version was treated as older'; fi

[[ $EUID -eq 0 ]] || fail 'dynamic filesystem cases must run through sudo/root'
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
TOOLCHAIN_ROOT="$tmp/toolchain"
ACTIVE_ROOT="$TOOLCHAIN_ROOT/go"
ENV_BIN=/usr/bin/env
FIND_BIN=/usr/bin/find
MV_BIN=/usr/bin/mv
READLINK_BIN=/usr/bin/readlink
RM_BIN=/usr/bin/rm
STAT_BIN=/usr/bin/stat
TAR_BIN=/usr/bin/tar
mkdir -p "$ACTIVE_ROOT/bin"
chmod 0700 "$tmp" "$TOOLCHAIN_ROOT" "$ACTIVE_ROOT" "$ACTIVE_ROOT/bin"
printf '#!/bin/sh\nexit 0\n' > "$ACTIVE_ROOT/bin/go"
chmod 0700 "$ACTIVE_ROOT/bin/go"
die() { printf '%s\n' "$*" >&2; exit 1; }
eval "$(extract_function run_clean_external)"
eval "$(extract_function validate_parent)"
eval "$(extract_function validate_tree)"
validate_parent "$tmp"
validate_tree "$ACTIVE_ROOT"

chmod 0770 "$tmp"
if (validate_parent "$tmp") >/dev/null 2>&1; then
    fail 'group-writable trusted ancestor was accepted'
fi
chmod 0700 "$tmp"
validate_parent "$tmp"

touch "$ACTIVE_ROOT/group-writable"
chmod 0660 "$ACTIVE_ROOT/group-writable"
if (validate_tree "$ACTIVE_ROOT") >/dev/null 2>&1; then
    fail 'group-writable file was accepted'
fi
rm -f -- "$ACTIVE_ROOT/group-writable"

ln -s /etc/passwd "$ACTIVE_ROOT/escape"
if (validate_tree "$ACTIVE_ROOT") >/dev/null 2>&1; then
    fail 'escaping symlink was accepted'
fi
rm -f -- "$ACTIVE_ROOT/escape"

mkfifo "$ACTIVE_ROOT/special"
if (validate_tree "$ACTIVE_ROOT") >/dev/null 2>&1; then
    fail 'special filesystem object was accepted'
fi
rm -f -- "$ACTIVE_ROOT/special"

archive="$tmp/clean-env.tar"
printf 'clean\n' > "$tmp/clean-marker"
/usr/bin/tar -cf "$archive" -C "$tmp" clean-marker
export TAR_OPTIONS=--help
export CURL_HOME="$tmp/untrusted-curl-home"
poisoned() { printf 'poisoned\n' > "$tmp/exported-function-ran"; }
export -f poisoned
clean_env=$(run_clean_external /usr/bin/env)
[[ "$clean_env" != *TAR_OPTIONS* && "$clean_env" != *CURL_HOME* && "$clean_env" != *BASH_FUNC* ]] ||
    fail 'clean command wrapper leaked caller-controlled environment'
listed=$(run_clean_external "$TAR_BIN" -tf "$archive")
[[ "$listed" == clean-marker ]] || fail 'clean tar invocation inherited TAR_OPTIONS'
if run_clean_external /usr/bin/bash -c 'declare -F poisoned >/dev/null'; then
    fail 'clean command wrapper imported an exported shell function'
fi
unset TAR_OPTIONS CURL_HOME
unset -f poisoned

eval "$(extract_function cleanup)"
rm -rf -- "$ACTIVE_ROOT"
retired_root="$TOOLCHAIN_ROOT/.go-retired.test"
mkdir -m 0700 "$retired_root"
printf 'old\n' > "$retired_root/old-marker"
failed_root="$TOOLCHAIN_ROOT/.go-failed.test"
download_path=
staging_root=
rollback_armed=1
(cleanup)
[[ -f "$ACTIVE_ROOT/old-marker" && ! -e "$retired_root" ]] ||
    fail 'interrupted publication did not restore the previous active tree'

mv "$ACTIVE_ROOT" "$retired_root"
mkdir -m 0700 "$ACTIVE_ROOT"
printf 'candidate\n' > "$ACTIVE_ROOT/candidate-marker"
rollback_armed=1
(cleanup)
[[ -f "$ACTIVE_ROOT/old-marker" && -f "$failed_root/candidate-marker" && ! -e "$retired_root" ]] ||
    fail 'interrupted post-publication validation did not isolate the candidate and restore the previous tree'

printf 'Go toolchain migration contract passed\n'
