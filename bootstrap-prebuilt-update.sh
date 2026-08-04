#!/bin/bash
# Publish a verified prebuilt release archive into the fixed root-only release
# store, then enter the existing transactional updater. No compiler, package
# manager, or mutable Git checkout is used on the target server.
set -euo pipefail

PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
umask 077

RELEASES_ROOT=/var/backups/celikpanel/releases
SOURCE_ROOT=$(cd -- "$(dirname -- "$(readlink -f -- "$0")")" && pwd)

die() {
    printf '!! %s\n' "$*" >&2
    exit 1
}

[[ $EUID -eq 0 ]] || die "Run as root / root olarak çalıştırın"
[[ $# -eq 1 && $1 == --normal ]] \
    || die "usage: bootstrap-prebuilt-update.sh --normal"

for required_command in bash chmod chown cmp dirname env find grep mv od readlink \
    sha256sum sort stat sync tr xargs; do
    command -v "$required_command" >/dev/null 2>&1 \
        || die "$required_command is required / $required_command gereklidir"
done

validate_root_trusted_dir_chain() {
    local path=$1 canonical current owner group mode permissions
    [[ "$path" == /* ]] || die "trusted directory path must be absolute: $path"
    canonical=$(readlink -e -- "$path") || die "trusted directory is unavailable: $path"
    [[ "$canonical" == "$path" ]] || die "trusted directory contains a symlink or alias: $path"
    current=$path
    while true; do
        [[ -d "$current" && ! -L "$current" ]] || die "unsafe trusted directory: $current"
        read -r owner group mode < <(stat -Lc '%u %g %a' -- "$current") \
            || die "cannot inspect trusted directory: $current"
        permissions=$((8#$mode))
        [[ "$owner" == 0 && "$group" == 0 ]] \
            || die "trusted directory must be owned by root: $current"
        (( (permissions & 0022) == 0 )) \
            || die "trusted directory must not be group/other writable: $current"
        [[ "$current" == / ]] && break
        current=$(dirname -- "$current")
    done
}

validate_release_tree() {
    local root=$1 require_direct=${2:-0} canonical relative entry owner group mode links permissions
    local version commit tree expected actual
    canonical=$(readlink -e -- "$root") || die "release tree is unavailable: $root"
    [[ "$canonical" == "$root" ]] || die "release tree contains a symlink or alias: $root"
    validate_root_trusted_dir_chain "$root"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$root") \
        || die "cannot inspect release root"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 700 ]] \
        || die "release root must be root:root mode 0700"

    if [[ "$require_direct" == 1 ]]; then
        [[ "$root" == "$RELEASES_ROOT/"* ]] || die "release is outside release storage"
        relative=${root#"$RELEASES_ROOT/"}
        [[ "$relative" != */* && "$relative" =~ ^[0-9a-f]{12}-[0-9a-f]{24}$ ]] \
            || die "published release name is invalid: $relative"
    fi

    if find "$root" -xdev -type l -print -quit | grep -q .; then
        die "release contains a symbolic link"
    fi
    if find "$root" -xdev ! -type d ! -type f -print -quit | grep -q .; then
        die "release contains a special filesystem object"
    fi
    if find "$root" -xdev -type f -links +1 -print -quit | grep -q .; then
        die "release contains a hard-linked file"
    fi
    while IFS= read -r -d '' entry; do
        read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$entry") \
            || die "cannot inspect release entry: $entry"
        permissions=$((8#$mode))
        [[ "$owner" == 0 && "$group" == 0 ]] \
            || die "release entry must be owned by root: $entry"
        (( (permissions & 0022) == 0 )) \
            || die "release entry must not be group/other writable: $entry"
        [[ ! -f "$entry" || "$links" == 1 ]] \
            || die "release file must have exactly one link: $entry"
    done < <(find "$root" -xdev -mindepth 1 -print0)

    [[ ! -e "$root/.git" && ! -L "$root/.git" ]] \
        || die "release must not contain a mutable Git checkout"
    [[ -f "$root/SHA256SUMS" && ! -L "$root/SHA256SUMS" ]] \
        || die "release checksum manifest is missing"
    (
        cd "$root"
        LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 \
            | LC_ALL=C sort -z \
            | xargs -0 sha256sum \
            | cmp -s - SHA256SUMS
        sha256sum -c SHA256SUMS >/dev/null
    ) || die "release checksum verification failed"

    for expected in release.version release.commit release.tree bin/panel bin/agent \
        bin/schema17-bridge install.sh update.sh rollback.sh \
        deploy/panel-tls-snapshot.sh web/dist/index.html; do
        [[ -f "$root/$expected" && ! -L "$root/$expected" ]] \
            || die "required release file is missing: $expected"
    done
    version=$(tr -d '[:space:]' < "$root/release.version")
    commit=$(tr -d '[:space:]' < "$root/release.commit")
    tree=$(tr -d '[:space:]' < "$root/release.tree")
    [[ "$version" == 1 ]] || die "unsupported release manifest version: $version"
    [[ "$commit" =~ ^[0-9a-f]{40,64}$ ]] || die "invalid release commit"
    [[ "$tree" =~ ^[0-9a-f]{40,64}$ ]] || die "invalid release tree"
    [[ -x "$root/bin/panel" && -x "$root/bin/agent" && -x "$root/bin/schema17-bridge" ]] \
        || die "release binaries are not executable"
    [[ -x "$root/install.sh" && -x "$root/update.sh" && -x "$root/rollback.sh" ]] \
        || die "release entry scripts are not executable"
    VALIDATED_RELEASE_COMMIT=$commit
}

sync_release_tree_durably() {
    local root=$1 entry
    while IFS= read -r -d '' entry; do
        sync -f -- "$entry" || die "cannot make release file durable: $entry"
    done < <(find "$root" -xdev -type f -print0)
    while IFS= read -r -d '' entry; do
        sync -f -- "$entry" || die "cannot make release directory durable: $entry"
    done < <(find "$root" -xdev -type d -depth -print0)
}

validate_root_trusted_dir_chain "$RELEASES_ROOT"
case "$SOURCE_ROOT" in
    "$RELEASES_ROOT"/.download.*/*/celikpanel-v*) ;;
    *) die "prebuilt source is outside the fixed download staging boundary: $SOURCE_ROOT" ;;
esac
validate_release_tree "$SOURCE_ROOT" 0

# Archive extraction deliberately does not preserve publisher-side ownership.
# Normalize only the already validated, root-only, symlink-free staging tree.
chown -R root:root -- "$SOURCE_ROOT"
find "$SOURCE_ROOT" -xdev -type d -exec chmod 0755 -- {} +
find "$SOURCE_ROOT" -xdev -type f -exec chmod 0644 -- {} +
chmod 0700 -- "$SOURCE_ROOT"
chmod 0755 -- "$SOURCE_ROOT/bin/panel" "$SOURCE_ROOT/bin/agent" \
    "$SOURCE_ROOT/bin/schema17-bridge" "$SOURCE_ROOT/install.sh" \
    "$SOURCE_ROOT/update.sh" "$SOURCE_ROOT/rollback.sh" \
    "$SOURCE_ROOT/bootstrap-prebuilt-update.sh"

validate_release_tree "$SOURCE_ROOT" 0
sync_release_tree_durably "$SOURCE_ROOT"

nonce=$(od -An -N12 -tx1 /dev/urandom | tr -d ' \n')
[[ "$nonce" =~ ^[0-9a-f]{24}$ ]] || die "could not create a safe release nonce"
release_name=${VALIDATED_RELEASE_COMMIT:0:12}-$nonce
FINAL_RELEASE=$RELEASES_ROOT/$release_name
[[ ! -e "$FINAL_RELEASE" && ! -L "$FINAL_RELEASE" ]] \
    || die "release destination already exists"
mv -T --no-clobber -- "$SOURCE_ROOT" "$FINAL_RELEASE"
sync -f -- "$RELEASES_ROOT" || die "cannot make release publication durable"
validate_release_tree "$FINAL_RELEASE" 1

printf '==> Verified prebuilt release / Doğrulanmış hazır sürüm: %s\n' "$FINAL_RELEASE"
env -i PATH="$PATH" HOME=/root LC_ALL=C \
    CELIKPANEL_TRUSTED_RELEASE_ROOT="$FINAL_RELEASE" \
    CELIKPANEL_PREFLIGHT_PANEL="$FINAL_RELEASE/bin/panel" \
    CELIKPANEL_PREFLIGHT_AGENT="$FINAL_RELEASE/bin/agent" \
    bash "$FINAL_RELEASE/update.sh" --normal
