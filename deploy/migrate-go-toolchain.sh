#!/bin/bash
# One-time migration of an existing CelikPanel build cache to exact Go 1.26.5.
# It changes no service, database, DNS record, or panel setting.
set -euo pipefail
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
umask 077

GO_VERSION=1.26.5
GO_SHA256_AMD64=5c2c3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393f053
GO_SHA256_ARM64=fe4789e92b1f33358680864bbe8704289e7bb5fc207d80623c308935bd696d49
PREFIX=/opt/celikpanel
TOOLCHAIN_ROOT="$PREFIX/.toolchain"
ACTIVE_ROOT="$TOOLCHAIN_ROOT/go"
ENV_BIN=/usr/bin/env
CHMOD_BIN=/usr/bin/chmod
CHOWN_BIN=/usr/bin/chown
CURL_BIN=/usr/bin/curl
DATE_BIN=/usr/bin/date
FIND_BIN=/usr/bin/find
MKTEMP_BIN=/usr/bin/mktemp
MV_BIN=/usr/bin/mv
READLINK_BIN=/usr/bin/readlink
RM_BIN=/usr/bin/rm
RMDIR_BIN=/usr/bin/rmdir
SHA256_BIN=/usr/bin/sha256sum
STAT_BIN=/usr/bin/stat
TAR_BIN=/usr/bin/tar
UNAME_BIN=/usr/bin/uname
download_path=
staging_root=
retired_root=
failed_root=
rollback_armed=0

die() { printf 'Go toolchain migration failed: %s\n' "$*" >&2; exit 1; }

cleanup() {
    local rc=$?
    trap - EXIT
    if (( rollback_armed == 1 )) &&
        [[ -d "$retired_root" && ! -L "$retired_root" ]]; then
        if [[ -e "$ACTIVE_ROOT" || -L "$ACTIVE_ROOT" ]]; then
            if [[ -e "$failed_root" || -L "$failed_root" ]] ||
                ! "$MV_BIN" -T --no-clobber -- "$ACTIVE_ROOT" "$failed_root"; then
                /usr/bin/printf 'URGENT: interrupted Go candidate could not be isolated; previous Go remains at %s\n' \
                    "$retired_root" >&2
                rc=1
            fi
        fi
        if [[ ! -e "$ACTIVE_ROOT" && ! -L "$ACTIVE_ROOT" ]] &&
            ! "$MV_BIN" -T --no-clobber -- "$retired_root" "$ACTIVE_ROOT"; then
            /usr/bin/printf 'URGENT: interrupted Go publication could not restore %s\n' \
                "$retired_root" >&2
            rc=1
        fi
    fi
    case "$download_path" in
        "$TOOLCHAIN_ROOT"/.go-migration-download.*)
            [[ -L "$download_path" ]] || "$RM_BIN" -f -- "$download_path"
            ;;
    esac
    case "$staging_root" in
        "$TOOLCHAIN_ROOT"/.go-migration-stage.*)
            [[ ! -d "$staging_root" || -L "$staging_root" ]] ||
                "$RM_BIN" -rf --one-file-system -- "$staging_root"
            ;;
    esac
    exit "$rc"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

run_clean_external() {
    "$ENV_BIN" -i HOME=/root PATH="$PATH" LC_ALL=C LANG=C "$@"
}

run_go_clean() {
    run_clean_external \
        GOTOOLCHAIN=local GOENV=off GOWORK=off GOPATH=/root/go \
        GOCACHE=/root/.cache/go-build CGO_ENABLED=0 "$@"
}

validate_parent() {
    local root=$1 canonical uid gid mode
    [[ -d "$root" && ! -L "$root" ]] || die "trusted parent is not a real directory: $root"
    canonical=$("$READLINK_BIN" -e -- "$root") || die "trusted parent is unavailable: $root"
    [[ "$canonical" == "$root" ]] || die "trusted parent is not canonical: $root"
    read -r uid gid mode < <("$STAT_BIN" -c '%u %g %a' -- "$root") || die "cannot inspect $root"
    [[ "$uid:$gid" == 0:0 ]] || die "trusted parent is not root:root: $root"
    (( (8#$mode & 8#022) == 0 )) || die "trusted parent is group/other writable: $root"
}

validate_tree() {
    local root=$1 canonical entry uid gid mode link_value target
    case "$root" in
        "$ACTIVE_ROOT"|"$TOOLCHAIN_ROOT"/.go-migration-stage.*/go) ;;
        *) die "unexpected Go tree path: $root" ;;
    esac
    [[ -d "$root" && ! -L "$root" ]] || die "Go root is not a real directory: $root"
    canonical=$("$READLINK_BIN" -e -- "$root") || die "Go root is unavailable: $root"
    [[ "$canonical" == "$root" ]] || die "Go root is not canonical: $root"
    while IFS= read -r -d '' entry; do
        read -r uid gid mode < <("$STAT_BIN" -c '%u %g %a' -- "$entry") || die "cannot inspect $entry"
        [[ "$uid:$gid" == 0:0 ]] || die "Go entry is not root:root: $entry"
        if [[ -L "$entry" ]]; then
            link_value=$("$READLINK_BIN" -- "$entry") || die "cannot inspect symlink: $entry"
            [[ "$link_value" != /* ]] || die "absolute Go symlink is forbidden: $entry"
            target=$("$READLINK_BIN" -f -- "$entry") || die "broken Go symlink: $entry"
            [[ "$target" == "$root"/* ]] || die "Go symlink escapes its root: $entry"
            continue
        fi
        [[ -d "$entry" || -f "$entry" ]] || die "special object in Go tree: $entry"
        (( (8#$mode & 8#022) == 0 )) || die "Go entry is group/other writable: $entry"
    done < <("$FIND_BIN" "$root" -xdev -print0)
}

go_version() { run_go_clean "$1" env GOVERSION 2>/dev/null; }

go_tree_is_exact() {
    local root=$1 candidate version reported_root canonical_root canonical_candidate
    candidate="$root/bin/go"
    canonical_root=$("$READLINK_BIN" -e -- "$root") || return 1
    canonical_candidate=$("$READLINK_BIN" -e -- "$candidate") || return 1
    [[ "$canonical_candidate" == "$canonical_root/bin/go" ]] || return 1
    version=$(go_version "$candidate") || return 1
    [[ "$version" == "go$GO_VERSION" ]] || return 1
    reported_root=$(run_go_clean "$candidate" env GOROOT 2>/dev/null) || return 1
    [[ "$reported_root" == "$canonical_root" ]]
}

version_is_older() {
    local version=$1 major minor patch
    [[ "$version" =~ ^go([0-9]+)\.([0-9]+)(\.([0-9]+))?$ ]] || return 1
    major=${BASH_REMATCH[1]}; minor=${BASH_REMATCH[2]}; patch=${BASH_REMATCH[4]:-0}
    (( major < 1 )) && return 0
    (( major > 1 )) && return 1
    (( minor < 26 )) && return 0
    (( minor > 26 )) && return 1
    (( patch < 5 ))
}

[[ $EUID -eq 0 ]] || die "run as root"
[[ $# -eq 0 ]] || die "this migrator accepts no arguments"
for command_path in \
    "$ENV_BIN" "$CHMOD_BIN" "$CHOWN_BIN" "$CURL_BIN" "$DATE_BIN" \
    "$FIND_BIN" "$MKTEMP_BIN" "$MV_BIN" "$READLINK_BIN" "$RM_BIN" \
    "$RMDIR_BIN" "$SHA256_BIN" "$STAT_BIN" "$TAR_BIN" "$UNAME_BIN"; do
    [[ -f "$command_path" && -x "$command_path" ]] ||
        die "required trusted command is unavailable: $command_path"
done
[[ "$(run_clean_external "$UNAME_BIN" -s)" == Linux ]] || die "this migrator supports Linux only"

validate_parent /
validate_parent /usr
validate_parent /usr/bin
validate_parent /opt
validate_parent "$PREFIX"
[[ -e "$TOOLCHAIN_ROOT" || -L "$TOOLCHAIN_ROOT" ]] || die "existing toolchain parent is missing"
validate_parent "$TOOLCHAIN_ROOT"

[[ -e "$ACTIVE_ROOT" || -L "$ACTIVE_ROOT" ]] || die "existing Go toolchain is missing"
validate_tree "$ACTIVE_ROOT"
[[ -x "$ACTIVE_ROOT/bin/go" ]] || die "existing Go command is not executable"
if go_tree_is_exact "$ACTIVE_ROOT"; then
    printf 'Go toolchain is already exact go%s and sealed; no toolchain changes made.\n' "$GO_VERSION"
    exit 0
fi
current_version=$(go_version "$ACTIVE_ROOT/bin/go") || die "existing Go version is unreadable"
version_is_older "$current_version" || die "refusing to replace non-older Go: $current_version"

case "$(run_clean_external "$UNAME_BIN" -m)" in
    x86_64) architecture=amd64; expected=$GO_SHA256_AMD64 ;;
    aarch64) architecture=arm64; expected=$GO_SHA256_ARM64 ;;
    *) die "unsupported Linux architecture: $(run_clean_external "$UNAME_BIN" -m)" ;;
esac
download_path=$(run_clean_external "$MKTEMP_BIN" "$TOOLCHAIN_ROOT/.go-migration-download.XXXXXXXX") || die "cannot create download"
"$CHMOD_BIN" 0600 -- "$download_path"; "$CHOWN_BIN" root:root -- "$download_path"
run_clean_external "$CURL_BIN" --disable --proto '=https' --tlsv1.2 --fail --location --retry 3 --show-error --silent \
    --output "$download_path" "https://go.dev/dl/go${GO_VERSION}.linux-${architecture}.tar.gz" || die "download failed"
read -r actual _ <<<"$(run_clean_external "$SHA256_BIN" -- "$download_path")" || die "hashing failed"
[[ "$actual" == "$expected" ]] || die "Go archive SHA-256 mismatch"

staging_root=$(run_clean_external "$MKTEMP_BIN" -d "$TOOLCHAIN_ROOT/.go-migration-stage.XXXXXXXX") || die "cannot create staging"
"$CHMOD_BIN" 0700 -- "$staging_root"; "$CHOWN_BIN" root:root -- "$staging_root"
run_clean_external "$TAR_BIN" --extract --gzip --no-same-owner --no-same-permissions \
    --directory "$staging_root" --file "$download_path" || die "extraction failed"
"$RM_BIN" -f -- "$download_path"; download_path=
[[ -d "$staging_root/go" && ! -L "$staging_root/go" ]] || die "unexpected archive layout"
mapfile -t unexpected_entries < <("$FIND_BIN" "$staging_root" -mindepth 1 -maxdepth 1 ! -name go -print -quit)
(( ${#unexpected_entries[@]} == 0 )) || die "unexpected top-level entry"
validate_tree "$staging_root/go"
[[ -x "$staging_root/go/bin/go" ]] || die "candidate Go is not executable"
go_tree_is_exact "$staging_root/go" || die "candidate is not exact go$GO_VERSION with matching GOROOT"

retired_root="$TOOLCHAIN_ROOT/.go-retired.$(run_clean_external "$DATE_BIN" -u +%Y%m%dT%H%M%SZ).$$"
failed_root="$TOOLCHAIN_ROOT/.go-failed.$(run_clean_external "$DATE_BIN" -u +%Y%m%dT%H%M%SZ).$$"
[[ ! -e "$retired_root" && ! -L "$retired_root" ]] || die "retired path collision"
[[ ! -e "$failed_root" && ! -L "$failed_root" ]] || die "failed path collision"
# Arm rollback before the first rename. The cleanup path derives publication
# state from the filesystem, so TERM/HUP/INT in either rename window restores
# the previous tree and retains an interrupted candidate for review.
rollback_armed=1
"$MV_BIN" -T --no-clobber -- "$ACTIVE_ROOT" "$retired_root" || {
    rollback_armed=0
    die "could not retire active Go"
}
"$MV_BIN" -T --no-clobber -- "$staging_root/go" "$ACTIVE_ROOT" ||
    die "publication failed; cleanup will restore the previous Go"
"$RMDIR_BIN" -- "$staging_root"; staging_root=
if ! (validate_tree "$ACTIVE_ROOT" && go_tree_is_exact "$ACTIVE_ROOT"); then
    die "published Go failed validation; cleanup will restore the previous Go"
fi
rollback_armed=0
printf 'Migrated %s to go%s; previous tree retained at %s\n' "$current_version" "$GO_VERSION" "$retired_root"
printf 'No CelikPanel service, database, DNS record, or panel setting was changed.\n'
