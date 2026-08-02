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
require_literal 'GO_VERSION=1.26.5'
require_literal 'GO_SHA256_AMD64=5c2c3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393f053'
require_literal 'GO_SHA256_ARM64=fe4789e92b1f33358680864bbe8704289e7bb5fc207d80623c308935bd696d49'
require_literal 'NODE_SHA256_AMD64=55aa7153f9d88f28d765fcdad5ae6945b5c0f98a36881703817e4c450fa76742'
require_literal 'NODE_SHA256_ARM64=58c9520501f6ae2b52d5b210444e24b9d0c029a58c5011b797bc1fe7105886f6'
require_literal 'go_toolchain_version() {'
require_literal 'run_external_clean() {'
require_literal 'run_go_clean() {'
require_literal 'run_node_clean() {'
require_literal '"$TOOLCHAIN_ENV_BIN" -i'
require_literal 'TOOLCHAIN_ENV_BIN=/usr/bin/env'
require_literal 'TOOLCHAIN_CURL_BIN=/usr/bin/curl'
require_literal 'TOOLCHAIN_INSTALL_BIN=/usr/bin/install'
require_literal 'TOOLCHAIN_MV_BIN=/usr/bin/mv'
require_literal 'TOOLCHAIN_READLINK_BIN=/usr/bin/readlink'
require_literal 'TOOLCHAIN_SHA256_BIN=/usr/bin/sha256sum'
require_literal 'TOOLCHAIN_STAT_BIN=/usr/bin/stat'
require_literal 'TOOLCHAIN_TAR_BIN=/usr/bin/tar'
require_literal 'validate_bootstrap_trusted_directory() {'
require_literal '[[ "$uid:$gid" == 0:0 ]]'
require_literal 'validate_bootstrap_trusted_directory /'
require_literal 'validate_bootstrap_trusted_directory /usr'
require_literal 'validate_bootstrap_trusted_directory /usr/bin'
require_literal 'validate_bootstrap_trusted_directory /opt'
require_literal 'validate_bootstrap_trusted_directory "$PREFIX"'
require_literal 'validate_bootstrap_trusted_directory "$TOOLCHAIN"'
require_literal '"$TOOLCHAIN_INSTALL_BIN" -d -m 0755 -o root -g root -- /opt'
require_literal '"$TOOLCHAIN_INSTALL_BIN" -d -m 0755 -o root -g root -- "$PREFIX"'
require_literal '"$TOOLCHAIN_INSTALL_BIN" -d -m 0755 -o root -g root -- "$TOOLCHAIN"'
require_literal 'PATH=/usr/sbin:/usr/bin:/sbin:/bin'
require_literal 'PATH="$node_bin_dir:/usr/sbin:/usr/bin:/sbin:/bin"'
require_literal 'GOENV=off'
require_literal 'GOWORK=off'
require_literal 'CGO_ENABLED=0'
require_literal '"$@"'
require_literal 'run_go_clean "$candidate" env GOVERSION'
require_literal '[[ "$version" == "go$GO_VERSION" ]]'
require_literal '[[ "$reported_root" == "$canonical_root" ]]'
require_literal 'download_verified_toolchain_archive() {'
require_literal "run_external_clean \"\$TOOLCHAIN_CURL_BIN\" --disable --proto '=https' --tlsv1.2"
require_literal 'read -r actual_sha256 _ <<<"$(run_external_clean "$TOOLCHAIN_SHA256_BIN" -- "$archive")"'
require_literal '[[ "$actual_sha256" != "$expected_sha256" ]]'
require_literal 'run_external_clean "$TOOLCHAIN_TAR_BIN" -xz --no-same-owner'
require_literal 'run_external_clean "$TOOLCHAIN_TAR_BIN" -xJ --no-same-owner'
require_literal '"$TOOLCHAIN_MV_BIN" -T --no-clobber -- "$staging/go" "$TOOLCHAIN/go"'
require_literal 'rollback_bootstrap_go_publication() {'
require_literal 'publish_bootstrap_go_candidate() ('
require_literal 'trap finish_go_publication EXIT'
require_literal "trap 'exit 129' HUP"
require_literal "trap 'exit 130' INT"
require_literal "trap 'exit 143' TERM"
require_literal 'rollback_armed=1'
require_literal '"$TOOLCHAIN_MV_BIN" -T --no-clobber -- "$TOOLCHAIN/go" "$retired_root"'
require_literal '"$TOOLCHAIN_MV_BIN" -T --no-clobber -- "$retired_root" "$active_root"'
require_literal 'mv -T --no-clobber -- "$staging" "$TOOLCHAIN/node"'
require_literal 'validate_bootstrap_toolchain_tree "$TOOLCHAIN/go"'
require_literal 'validate_bootstrap_toolchain_tree "$TOOLCHAIN/node"'
require_literal 'seal_bootstrap_toolchain_tree "$staging/go"'
require_literal 'go_toolchain_is_exact "$staging/go/bin/go" "$staging/go"'
require_literal 'validate_bootstrap_toolchain_tree "$staging"'
require_literal 'seal_bootstrap_toolchain_tree "$TOOLCHAIN/go"'
require_literal 'seal_bootstrap_toolchain_tree "$TOOLCHAIN/node"'
require_literal 'go_toolchain_is_exact "$TOOLCHAIN/go/bin/go" "$TOOLCHAIN/go"'
require_literal 'run_go_clean "$GO_BIN" build'
require_literal 'run_node_clean "$NODE_BIN" "$NODE_BIN/npm" ci'
require_literal 'PATH Go ignored for privileged build'
reject_literal '        $@'
reject_literal 'curl -fsSL "https://go.dev/dl/'
reject_literal 'curl -fsSL "https://nodejs.org/dist/'
reject_literal 'command -v go >/dev/null && { echo go; return; }'
reject_literal 'rm -rf -- "$TOOLCHAIN/go"'
reject_literal 'npm install --no-audit --no-fund'
require_before 'ensure_bootstrap_toolchain_root' 'case "$root" in'
require_before 'validate_bootstrap_trusted_directory /usr/bin' '"$TOOLCHAIN_INSTALL_BIN" -d -m 0755 -o root -g root -- /opt'
require_before 'validate_bootstrap_trusted_directory /opt' '"$TOOLCHAIN_INSTALL_BIN" -d -m 0755 -o root -g root -- "$PREFIX"'
require_before 'validate_bootstrap_trusted_directory "$PREFIX"' '"$TOOLCHAIN_INSTALL_BIN" -d -m 0755 -o root -g root -- "$TOOLCHAIN"'
require_before 'archive=$(download_verified_toolchain_archive "https://go.dev/' 'run_external_clean "$TOOLCHAIN_TAR_BIN" -xz --no-same-owner'
require_before 'go_toolchain_is_exact "$staging/go/bin/go" "$staging/go"' 'publish_bootstrap_go_candidate "$staging/go"'
require_before 'trap finish_go_publication EXIT' 'rollback_armed=1'
require_before "trap 'exit 129' HUP" 'rollback_armed=1'
require_before "trap 'exit 130' INT" 'rollback_armed=1'
require_before "trap 'exit 143' TERM" 'rollback_armed=1'
require_before 'rollback_armed=1' '"$TOOLCHAIN_MV_BIN" -T --no-clobber -- "$TOOLCHAIN/go" "$retired_root"'
require_before 'archive=$(download_verified_toolchain_archive "https://nodejs.org/' 'run_external_clean "$TOOLCHAIN_TAR_BIN" -xJ --no-same-owner'

# Exercise the actual wrapper definitions. Static literal checks alone can miss
# an unquoted $@ elsewhere in the installer, which would split -ldflags values.
wrapper_defs=$(
    sed -n \
        -e '/^run_external_clean() {$/,/^}$/p' \
        -e '/^run_go_clean() {$/,/^}$/p' \
        -e '/^run_node_clean() {$/,/^}$/p' \
        "$INSTALL"
)

assert_wrapper_preserves_arguments() {
    local wrapper_kind=$1 actual expected
    expected=$'<-ldflags>\n<-s -w main.version=go1.26.5>\n<argument with spaces>'
    actual=$(
        WRAPPER_DEFS="$wrapper_defs" WRAPPER_KIND="$wrapper_kind" /bin/bash <<'BASH'
set -euo pipefail
TOOLCHAIN_ENV_BIN=/usr/bin/env
eval "$WRAPPER_DEFS"
case "$WRAPPER_KIND" in
    go)
        run_go_clean /bin/bash -c 'printf "<%s>\n" "$@"' \
            _ -ldflags '-s -w main.version=go1.26.5' 'argument with spaces'
        ;;
    node)
        run_node_clean /usr/bin /bin/bash -c 'printf "<%s>\n" "$@"' \
            _ -ldflags '-s -w main.version=go1.26.5' 'argument with spaces'
        ;;
    *)
        exit 2
        ;;
esac
BASH
    )
    [[ "$actual" == "$expected" ]] ||
        die "$wrapper_kind wrapper did not preserve command arguments"
}

assert_wrapper_preserves_arguments go
assert_wrapper_preserves_arguments node

TOOLCHAIN_ENV_BIN=/usr/bin/env
eval "$wrapper_defs"
export TAR_OPTIONS=--help
export CURL_HOME=/definitely/untrusted
clean_env=$(TOOLCHAIN_ENV_BIN=/usr/bin/env run_external_clean /usr/bin/env)
[[ "$clean_env" != *TAR_OPTIONS* && "$clean_env" != *CURL_HOME* ]] ||
    die 'external wrapper leaked caller-controlled environment'
unset TAR_OPTIONS CURL_HOME

trusted_dir_def=$(
    sed -n '/^validate_bootstrap_trusted_directory() {$/,/^}$/p' "$INSTALL"
)
[[ -n "$trusted_dir_def" ]] ||
    die 'trusted-directory validator definition could not be extracted'

ancestor_tmp=$(mktemp -d)
ancestor_cleanup() {
    rm -rf -- "$ancestor_tmp"
}
trap ancestor_cleanup EXIT

TOOLCHAIN_READLINK_BIN=/usr/bin/readlink
TOOLCHAIN_STAT_BIN=/usr/bin/stat
eval "$trusted_dir_def"

if [[ "$(uname -s)" == Linux ]]; then
    for directory in / /usr /usr/bin /opt; do
        validate_bootstrap_trusted_directory "$directory" ||
            die "stock Linux trust root was rejected: $directory"
    done
fi

fake_stat="$ancestor_tmp/fake-stat"
cat > "$fake_stat" <<'BASH'
#!/bin/bash
printf '%s\n' "${FAKE_STAT_RESULT:?}"
BASH
chmod 0700 "$fake_stat"
TOOLCHAIN_STAT_BIN="$fake_stat"

run_trusted_directory_check() (
    export FAKE_STAT_RESULT=$1
    validate_bootstrap_trusted_directory "$2"
)

run_trusted_directory_check '0 0 755' "$ancestor_tmp" ||
    die 'root-owned non-writable trusted directory was rejected'
if run_trusted_directory_check '1000 0 755' "$ancestor_tmp" >/dev/null 2>&1; then
    die 'non-root trusted-directory owner was accepted'
fi
if run_trusted_directory_check '0 1000 755' "$ancestor_tmp" >/dev/null 2>&1; then
    die 'non-root trusted-directory group was accepted'
fi
if run_trusted_directory_check '0 0 775' "$ancestor_tmp" >/dev/null 2>&1; then
    die 'group-writable trusted directory was accepted'
fi
if run_trusted_directory_check '0 0 755' "$ancestor_tmp/." >/dev/null 2>&1; then
    die 'non-canonical trusted-directory path was accepted'
fi

ancestor_cleanup
trap - EXIT

rollback_def=$(
    sed -n '/^rollback_bootstrap_go_publication() {$/,/^}$/p' "$INSTALL"
)
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
TOOLCHAIN="$tmp/toolchain"
TOOLCHAIN_MV_BIN=/usr/bin/mv
mkdir -p "$TOOLCHAIN"
c() { :; }
eval "$rollback_def"
active="$TOOLCHAIN/go"
retired="$TOOLCHAIN/.go-retired.test"
failed="$TOOLCHAIN/.go-failed.test"
mkdir "$retired"
printf 'old\n' > "$retired/old-marker"
rollback_bootstrap_go_publication "$active" "$retired" "$failed"
[[ -f "$active/old-marker" ]] || die 'pre-publication rollback did not restore old Go'
mv "$active" "$retired"
mkdir "$active"
printf 'candidate\n' > "$active/candidate-marker"
rollback_bootstrap_go_publication "$active" "$retired" "$failed"
[[ -f "$active/old-marker" && -f "$failed/candidate-marker" ]] ||
    die 'post-publication rollback did not isolate candidate and restore old Go'

echo "install toolchain contract passed"
