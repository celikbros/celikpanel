#!/bin/bash
# Keep every source-build path on one exact, reviewed Go toolchain. Automatic
# toolchain downloads are forbidden so a launcher cannot substitute unreviewed
# compiler bytes during a privileged build.
set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
GO_MOD="$ROOT/go.mod"
INSTALL="$ROOT/install.sh"
BOOTSTRAP="$ROOT/bootstrap-update.sh"
MAKEFILE="$ROOT/Makefile"
REBUILD="$ROOT/rebuild.sh"
CI="$ROOT/.github/workflows/ci.yml"
PORTABILITY="$ROOT/.github/workflows/portability.yml"
README_EN="$ROOT/README.md"
README_TR="$ROOT/README.tr.md"
OPERATIONS_EN="$ROOT/docs/OPERATIONS.md"
OPERATIONS_TR="$ROOT/docs/OPERATIONS.tr.md"
STORE_EN="$ROOT/docs/STORE.md"
STORE_TR="$ROOT/docs/STORE.tr.md"
BSD_PORT_EN="$ROOT/docs/BSD_PORT_MAP.md"
BSD_PORT_TR="$ROOT/docs/BSD_PORT_MAP.tr.md"
DECISIONS_EN="$ROOT/docs/DECISIONS.md"
DECISIONS_TR="$ROOT/docs/DECISIONS.tr.md"
DISTRO_SUPPORT_EN="$ROOT/docs/DISTRO-SUPPORT.md"
DISTRO_SUPPORT_TR="$ROOT/docs/DISTRO-SUPPORT.tr.md"
DISTRO_MATRIX_GO="$ROOT/internal/core/distro_matrix.go"
DISTRO_MATRIX_TEST_GO="$ROOT/internal/core/distro_matrix_test.go"

die() {
    echo "Go toolchain contract failed: $*" >&2
    exit 1
}

require_literal() {
    local file=$1 literal=$2
    grep -Fq -- "$literal" "$file" ||
        die "$(basename "$file") is missing: $literal"
}

reject_literal() {
    local file=$1 literal=$2
    ! grep -Fq -- "$literal" "$file" ||
        die "$(basename "$file") must not contain: $literal"
}

require_count() {
    local file=$1 literal=$2 expected=$3 actual
    actual=$(grep -F -c -- "$literal" "$file" || true)
    [[ "$actual" == "$expected" ]] ||
        die "$(basename "$file") count for '$literal' is $actual, want $expected"
}

require_sequence() {
    local file=$1 cursor=0 literal line
    shift
    for literal in "$@"; do
        line=$({ grep -Fn -- "$literal" "$file" || true; } |
            awk -F: -v cursor="$cursor" '$1 > cursor { print $1; exit }')
        [[ "$line" =~ ^[0-9]+$ ]] ||
            die "$(basename "$file") has no ordered marker after line $cursor: $literal"
        cursor=$line
    done
}

bash -n "$INSTALL" "$BOOTSTRAP" "$REBUILD"

require_count "$GO_MOD" 'go 1.26.5' 1
require_count "$GO_MOD" 'toolchain ' 0

require_literal "$INSTALL" 'GO_VERSION=1.26.5'
require_literal "$INSTALL" 'run_go_clean() {'
require_literal "$INSTALL" '"$TOOLCHAIN_ENV_BIN" -i'
require_literal "$INSTALL" 'GOENV=off'
require_literal "$INSTALL" 'GOWORK=off'
require_literal "$INSTALL" 'CGO_ENABLED=0'
require_literal "$INSTALL" 'run_go_clean "$candidate" env GOVERSION'
require_literal "$INSTALL" '[[ "$version" == "go$GO_VERSION" ]]'
require_literal "$INSTALL" '[[ "$reported_root" == "$canonical_root" ]]'
require_literal "$INSTALL" 'run_go_clean "$GO_BIN" build'
require_sequence "$INSTALL" \
    'seal_bootstrap_toolchain_tree "$staging/go"' \
    'go_toolchain_is_exact "$staging/go/bin/go" "$staging/go"' \
    'publish_bootstrap_go_candidate "$staging/go"' \
    'go_toolchain_is_exact "$TOOLCHAIN/go/bin/go" "$TOOLCHAIN/go"' \
    'run_go_clean "$GO_BIN" build'

require_literal "$BOOTSTRAP" 'REQUIRED_GO_VERSION=go1.26.5'
require_literal "$BOOTSTRAP" 'PINNED_GO_ROOT="$TOOLCHAIN_ROOT/go"'
require_literal "$BOOTSTRAP" 'PINNED_GO_BIN="$PINNED_GO_ROOT/bin/go"'
require_literal "$BOOTSTRAP" 'GOENV=off'
require_literal "$BOOTSTRAP" 'GOWORK=off'
require_literal "$BOOTSTRAP" 'CGO_ENABLED=0'
require_literal "$BOOTSTRAP" 'validate_pinned_go_tree() {'
require_literal "$BOOTSTRAP" '[[ "$target" == "$root"/* ]]'
require_literal "$BOOTSTRAP" 'validate_pinned_go_toolchain() {'
require_sequence "$BOOTSTRAP" \
    'validate_pinned_go_tree "$PINNED_GO_ROOT"' \
    '[[ "$reported_root" == "$PINNED_GO_ROOT" ]]' \
    'validate_pinned_go_toolchain' \
    'go_bin="$PINNED_GO_BIN"' \
    'run_clean "$go_bin" build'

require_literal "$MAKEFILE" 'override REQUIRED_GO_VERSION := go1.26.5'
require_literal "$MAKEFILE" 'check-go:'
require_literal "$MAKEFILE" 'env -i HOME="$$HOME" PATH="$$PATH" LC_ALL=C GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 "$(GO)" env GOVERSION'
require_count "$MAKEFILE" 'env -i HOME="$$HOME" PATH="$$PATH" LC_ALL=C GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 "$(GO)" build' 3
require_literal "$MAKEFILE" 'test: check-go'
require_literal "$MAKEFILE" 'env -i HOME="$$HOME" PATH="$$PATH" LC_ALL=C GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 "$(GO)" test ./...'
require_literal "$MAKEFILE" 'vet: check-go'
require_literal "$MAKEFILE" 'env -i HOME="$$HOME" PATH="$$PATH" LC_ALL=C GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 "$(GO)" vet ./...'
require_literal "$MAKEFILE" 'panel: check-go'
require_literal "$MAKEFILE" 'agent: check-go'
require_literal "$MAKEFILE" 'schema17-bridge: check-go'
require_literal "$MAKEFILE" 'distro-matrix: check-go'
require_literal "$MAKEFILE" '"$(GO)" run ./tools/gen-distro-matrix'
require_literal "$MAKEFILE" 'freebsd-cross: check-go'
require_literal "$MAKEFILE" 'GOOS=freebsd GOARCH=amd64 "$(GO)" build ./cmd/panel ./cmd/agent'
require_literal "$MAKEFILE" 'GOOS=freebsd GOARCH=arm64 "$(GO)" build ./cmd/panel'

require_literal "$REBUILD" 'REQUIRED_GO_VERSION=go1.26.5'
require_literal "$REBUILD" 'run_go_clean() {'
require_literal "$REBUILD" 'actual_go_version=$(run_go_clean "$GO_BIN" env GOVERSION'
require_count "$REBUILD" 'run_go_clean "$GO_BIN" build' 2

require_literal "$CI" 'GOTOOLCHAIN: local'
require_count "$CI" 'actions/setup-go@v6' 4
require_count "$CI" "go-version: '1.26.5'" 4
require_count "$CI" 'test "$(go env GOVERSION)" = go1.26.5' 4
require_literal "$CI" 'needs: [go, panel-race, web, linux-arm64-compile, publisher-windows]'
require_literal "$CI" 'sudo env PATH=$PATH GOTOOLCHAIN=local bash deploy/test-signed-release-manifest-contract.sh'
reject_literal "$CI" 'windows-portability:'
reject_literal "$CI" 'freebsd-compile:'
reject_literal "$CI" 'darwin-compile:'
require_count "$CI" 'go test -race -count=1 -timeout=8m ./cmd/panel' 1
require_count "$CI" 'pattern:' 7
require_literal "$CI" "pattern: '^Test($|[A-C]|[^A-Z])'"
require_literal "$CI" "pattern: '^TestD'"
require_literal "$CI" "pattern: '^Test[E-G]'"
require_literal "$CI" "pattern: '^Test[H-M]'"
require_literal "$CI" "pattern: '^Test[N-R]'"
require_literal "$CI" "pattern: '^TestS'"
require_literal "$CI" "pattern: '^Test[T-Z]'"
require_literal "$CI" "-run '\${{ matrix.pattern }}'"
reject_literal "$CI" 'actions/setup-go@v5'
reject_literal "$CI" 'go-version-file:'

test -f "$PORTABILITY"
require_literal "$PORTABILITY" 'workflow_dispatch:'
reject_literal "$PORTABILITY" 'schedule:'
reject_literal "$PORTABILITY" 'pull_request:'
reject_literal "$PORTABILITY" 'push:'
require_count "$PORTABILITY" 'actions/setup-go@v6' 3
require_count "$PORTABILITY" "go-version: '1.26.5'" 3
require_count "$PORTABILITY" 'test "$(go env GOVERSION)" = go1.26.5' 2
require_literal "$PORTABILITY" "if ((go env GOVERSION) -ne 'go1.26.5')"
require_literal "$PORTABILITY" 'windows-portability:'
require_literal "$PORTABILITY" 'freebsd-compile:'
require_literal "$PORTABILITY" 'darwin-compile:'
reject_literal "$PORTABILITY" 'go-version-file:'

# Prove that every test the exact Go toolchain currently discovers belongs to
# exactly one panel race shard. Keep explicit boundary sentinels too: a valid
# test may be named exactly `Test`, so current source names alone are not a
# sufficient proof that the empty suffix remains covered.
panel_shard_membership_count() {
    local test_name=$1 count=0
    [[ "$test_name" =~ ^Test($|[A-C]|[^A-Z]) ]] && ((count += 1))
    [[ "$test_name" =~ ^TestD ]] && ((count += 1))
    [[ "$test_name" =~ ^Test[E-G] ]] && ((count += 1))
    [[ "$test_name" =~ ^Test[H-M] ]] && ((count += 1))
    [[ "$test_name" =~ ^Test[N-R] ]] && ((count += 1))
    [[ "$test_name" =~ ^TestS ]] && ((count += 1))
    [[ "$test_name" =~ ^Test[T-Z] ]] && ((count += 1))
    printf '%s\n' "$count"
}

for boundary_name in Test TestA TestC TestD TestE TestG TestH TestM TestN TestR TestS TestT TestZ Test_ Test9 Testa; do
    [[ "$(panel_shard_membership_count "$boundary_name")" == 1 ]] ||
        die "panel race shard boundary is not disjoint and exhaustive: $boundary_name"
done

# Keep Go configuration fail-closed while retaining the small set of platform
# directory variables Windows Go needs to locate its module and build caches.
# They are runtime locations, not compiler-selection or build-policy inputs.
contract_go_env=(
    HOME="$HOME"
    PATH="$PATH"
    LC_ALL=C
    GOTOOLCHAIN=local
    GOENV=off
    GOWORK=off
    CGO_ENABLED=0
)
for platform_var in USERPROFILE LOCALAPPDATA SYSTEMROOT TEMP TMP; do
    if [[ -n ${!platform_var:-} ]]; then
        contract_go_env+=("$platform_var=${!platform_var}")
    fi
done

actual_go_version=$(env -i "${contract_go_env[@]}" \
    go env GOVERSION) || die "cannot inspect the Go compiler used for test discovery"
[[ "$actual_go_version" == go1.26.5 ]] ||
    die "test discovery requires go1.26.5, got $actual_go_version"

panel_test_output=$(
    cd -- "$ROOT"
    env -i "${contract_go_env[@]}" \
        go test -list '^Test' ./cmd/panel
) || die "cannot discover cmd/panel tests with the exact Go toolchain"

mapfile -t discovered_panel_tests < <(
    printf '%s\n' "$panel_test_output" | grep -E '^Test([^[:space:]]*)$' || true
)
((${#discovered_panel_tests[@]} > 0)) ||
    die "exact Go test discovery returned no cmd/panel tests"
for test_name in "${discovered_panel_tests[@]}"; do
    [[ "$(panel_shard_membership_count "$test_name")" == 1 ]] ||
        die "discovered panel test is not in exactly one race shard: $test_name"
done

require_literal "$README_EN" 'Requirements: exactly Go 1.26.5'
require_literal "$README_EN" 'make panel agent'
reject_literal "$README_EN" 'GOTOOLCHAIN=local go build'
require_literal "$README_TR" 'Gereksinimler: tam Go 1.26.5'
require_literal "$README_TR" 'make panel agent'
reject_literal "$README_TR" 'GOTOOLCHAIN=local go build'

for release_doc in "$OPERATIONS_EN" "$OPERATIONS_TR" "$STORE_EN" "$STORE_TR"; do
    reject_literal "$release_doc" 'go test'
    reject_literal "$release_doc" 'go vet'
done
require_literal "$OPERATIONS_EN" 'make test vet web'
require_literal "$OPERATIONS_TR" 'make test vet web'
require_literal "$STORE_EN" '`make test vet web`'
require_literal "$STORE_TR" '`make test vet web`'

for user_doc in \
    "$README_EN" "$README_TR" \
    "$OPERATIONS_EN" "$OPERATIONS_TR" \
    "$STORE_EN" "$STORE_TR" \
    "$BSD_PORT_EN" "$BSD_PORT_TR" \
    "$DECISIONS_EN" "$DECISIONS_TR" \
    "$DISTRO_SUPPORT_EN" "$DISTRO_SUPPORT_TR"; do
    reject_literal "$user_doc" 'go build'
    reject_literal "$user_doc" 'go test'
    reject_literal "$user_doc" 'go vet'
    reject_literal "$user_doc" 'go run'
done
require_literal "$BSD_PORT_EN" 'make freebsd-cross'
require_literal "$BSD_PORT_TR" 'make freebsd-cross'
require_literal "$DISTRO_SUPPORT_EN" 'Regenerate: make distro-matrix'
require_literal "$DISTRO_SUPPORT_TR" 'Üretmek için: make distro-matrix'
require_literal "$DISTRO_MATRIX_GO" 'Regenerate: make distro-matrix'
require_literal "$DISTRO_MATRIX_GO" 'Üretmek için: make distro-matrix'
require_literal "$DISTRO_MATRIX_TEST_GO" 'make distro-matrix'
reject_literal "$DISTRO_MATRIX_GO" 'go run'
reject_literal "$DISTRO_MATRIX_TEST_GO" 'go run'

# Exercise the Make gate without a network or a real compiler. Both fakes insist
# that the caller sets GOTOOLCHAIN=local; the old compiler must fail closed and
# the exact compiler must pass.
if command -v make >/dev/null; then
    contract_tmp=$(mktemp -d)
    trap 'rm -rf -- "$contract_tmp"' EXIT

    old_go="$contract_tmp/go-old"
    exact_go="$contract_tmp/go-exact"
    printf '%s\n' \
        '#!/bin/sh' \
        '[ "${GOTOOLCHAIN:-}" = local ] || exit 41' \
        '[ -z "${GOFLAGS+x}" ] || exit 43' \
        '[ -z "${GOROOT+x}" ] || exit 44' \
        '[ -z "${CC+x}" ] || exit 45' \
        '[ "$1 $2" = "env GOVERSION" ] || exit 42' \
        'printf "%s\n" go1.25.0' > "$old_go"
    printf '%s\n' \
        '#!/bin/sh' \
        '[ "${GOTOOLCHAIN:-}" = local ] || exit 41' \
        '[ -z "${GOFLAGS+x}" ] || exit 43' \
        '[ -z "${GOROOT+x}" ] || exit 44' \
        '[ -z "${CC+x}" ] || exit 45' \
        '[ "$1 $2" = "env GOVERSION" ] || exit 42' \
        'printf "%s\n" go1.26.5' > "$exact_go"
    chmod 0700 "$old_go" "$exact_go"

    if GOFLAGS='-toolexec=attacker' GOROOT=/attacker CC=/attacker/cc \
        make -s -f "$MAKEFILE" check-go GO="$old_go" \
            REQUIRED_GO_VERSION=go1.25.0 >/dev/null 2>&1; then
        die "Make accepted an old Go compiler"
    fi
    GOFLAGS='-toolexec=attacker' GOROOT=/attacker CC=/attacker/cc \
        make -s -f "$MAKEFILE" check-go GO="$exact_go" >/dev/null ||
        die "Make rejected the exact Go compiler"

    rm -rf -- "$contract_tmp"
    trap - EXIT
fi

echo "Go toolchain contract passed"
