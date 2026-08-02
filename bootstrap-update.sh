#!/bin/bash
# Prepare one immutable, root-owned release for a normal update, the one-time
# exact pre-ledger transition, or the reviewed exact-schema-17 bridge, then
# hand it to update.sh.
# Normal güncelleme veya tek seferlik tam pre-ledger geçişi için değişmez,
# root sahipli bir sürüm hazırlar ve update.sh'ye verir.
set -euo pipefail

RELEASES_ROOT=/var/backups/celikpanel/releases
TOOLCHAIN_ROOT=/opt/celikpanel/.toolchain

die() {
    echo "!! $*" >&2
    exit 1
}

# Every privileged directory in the release path must be canonical, root-owned
# and unavailable for group/other writes.
# Sürüm yolundaki her ayrıcalıklı dizin kanonik, root sahipli ve grup/diğer
# yazmalarına kapalı olmalıdır.
validate_root_trusted_dir_chain() {
    local path=$1 canonical current owner mode permissions
    [[ "$path" == /* ]] || die "trusted directory path must be absolute: $path"
    canonical=$(readlink -e -- "$path") || die "trusted directory is unavailable: $path"
    [[ "$canonical" == "$path" ]] || die "trusted directory contains a symlink or alias: $path"
    current=$path
    while true; do
        [[ -d "$current" && ! -L "$current" ]] || die "unsafe trusted directory: $current"
        read -r owner mode < <(stat -Lc '%u %a' -- "$current") \
            || die "cannot inspect trusted directory: $current"
        [[ "$owner" == 0 ]] || die "trusted directory must be owned by root: $current"
        permissions=$((8#$mode))
        (( (permissions & 0022) == 0 )) \
            || die "trusted directory must not be group/other writable: $current"
        [[ "$current" == / ]] && break
        current=$(dirname "$current")
    done
}

# Git objects and privileged entrypoints are inputs to commands run as root.
# Require every entry to be root-owned, non-writable by group/other, and never
# a symlink or special filesystem object.
# Git nesneleri ve ayrıcalıklı giriş noktaları root olarak çalışan komutların
# girdileridir. Her girdinin root sahipli, grup/diğer yazmasına kapalı ve asla
# sembolik bağ ya da özel dosya olmadığını zorunlu tut.
validate_root_owned_regular_tree() {
    local root=$1 entry owner mode permissions
    [[ -d "$root" && ! -L "$root" ]] || die "trusted input tree is unsafe: $root"
    if find "$root" -type l -print -quit | grep -q .; then
        die "trusted input tree contains a symbolic link: $root"
    fi
    if find "$root" ! -type d ! -type f -print -quit | grep -q .; then
        die "trusted input tree contains a special filesystem object: $root"
    fi
    while IFS= read -r -d '' entry; do
        read -r owner mode < <(stat -Lc '%u %a' -- "$entry") \
            || die "cannot inspect trusted input entry: $entry"
        [[ "$owner" == 0 ]] || die "trusted input entry must be owned by root: $entry"
        permissions=$((8#$mode))
        (( (permissions & 0022) == 0 )) \
            || die "trusted input entry must not be group/other writable: $entry"
    done < <(find "$root" -print0)
}

# Privileged child tools receive a minimal environment so Git, Go and Node
# behavior cannot be changed through caller-controlled configuration variables.
# Ayrıcalıklı alt araçlar en küçük ortamı alır; böylece Git, Go ve Node davranışı
# çağıranın denetlediği yapılandırma değişkenleriyle değiştirilemez.
run_clean() {
    env -i HOME=/root PATH="$PATH" LC_ALL=C "$@"
}

# Create only the documented root-owned release hierarchy, refusing aliases or
# unsafe pre-existing path components.
# Yalnızca belgelenmiş root sahipli sürüm hiyerarşisini oluştur; takma yolları
# veya önceden var olan güvensiz yol bileşenlerini reddet.
prepare_release_root() {
    local directory desired_mode
    validate_root_trusted_dir_chain /var
    for directory in /var/backups /var/backups/celikpanel "$RELEASES_ROOT"; do
        desired_mode=0700
        [[ "$directory" != /var/backups ]] || desired_mode=0755
        if [[ -e "$directory" || -L "$directory" ]]; then
            [[ -d "$directory" && ! -L "$directory" ]] \
                || die "unsafe release directory: $directory"
        else
            install -d -m "$desired_mode" -o root -g root -- "$directory" \
                || die "cannot create release directory: $directory"
        fi
        validate_root_trusted_dir_chain "$directory"
    done
}

# Build tools run as root, so resolve them only through canonical files under
# trusted directory chains.
# Derleme araçları root olarak çalışır; bu yüzden onları yalnızca güvenilir
# dizin zincirlerindeki kanonik dosyalar üzerinden çöz.
trusted_command() {
    local name=$1 candidate canonical owner mode permissions
    candidate=$(command -v -- "$name" 2>/dev/null || true)
    if [[ -z "$candidate" ]]; then
        case "$name" in
            go) candidate="$TOOLCHAIN_ROOT/go/bin/go" ;;
            node) candidate="$TOOLCHAIN_ROOT/node/bin/node" ;;
            npm) candidate="$TOOLCHAIN_ROOT/node/bin/npm" ;;
        esac
    fi
    [[ -n "$candidate" && "$candidate" == /* ]] || die "trusted build tool is unavailable: $name"
    validate_root_trusted_dir_chain "$(dirname "$candidate")"
    canonical=$(readlink -e -- "$candidate") || die "cannot resolve build tool: $candidate"
    validate_root_trusted_dir_chain "$(dirname "$canonical")"
    [[ -f "$canonical" && -x "$canonical" ]] || die "build tool is not executable: $canonical"
    read -r owner mode < <(stat -Lc '%u %a' -- "$canonical") || die "cannot inspect build tool: $canonical"
    [[ "$owner" == 0 ]] || die "build tool must be owned by root: $canonical"
    permissions=$((8#$mode))
    (( (permissions & 0022) == 0 )) || die "build tool must not be group/other writable: $canonical"
    printf '%s\n' "$candidate"
}

# The release contains only directories and regular files from the reviewed
# commit, all root-owned and never group/other writable.
# Sürüm yalnızca incelenmiş commit'ten gelen dizinleri ve normal dosyaları
# içerir; hepsi root sahipli ve grup/diğer yazmasına kapalıdır.
validate_release_tree() {
    local root=$1 entry owner mode permissions
    validate_root_trusted_dir_chain "$root"
    read -r owner mode < <(stat -Lc '%u %a' -- "$root") || die "cannot inspect release root"
    [[ "$owner" == 0 && "$mode" == 700 ]] || die "release root must be root-owned mode 0700"
    if find "$root" -type l -print -quit | grep -q .; then
        die "release contains a symbolic link"
    fi
    if find "$root" ! -type d ! -type f -print -quit | grep -q .; then
        die "release contains a special filesystem object"
    fi
    while IFS= read -r -d '' entry; do
        read -r owner mode < <(stat -Lc '%u %a' -- "$entry") || die "cannot inspect release entry: $entry"
        [[ "$owner" == 0 ]] || die "release entry must be owned by root: $entry"
        permissions=$((8#$mode))
        (( (permissions & 0022) == 0 )) || die "release entry must not be group/other writable: $entry"
    done < <(find "$root" -mindepth 1 -print0)
    [[ -x "$root/bin/panel" && -f "$root/bin/panel" ]] || die "staged panel binary is missing"
    [[ -x "$root/bin/agent" && -f "$root/bin/agent" ]] || die "staged agent binary is missing"
    [[ -x "$root/bin/schema17-bridge" && -f "$root/bin/schema17-bridge" ]] \
        || die "staged schema-17 bridge binary is missing"
    [[ -x "$root/install.sh" && -f "$root/install.sh" ]] || die "staged installer is missing"
    [[ -x "$root/update.sh" && -f "$root/update.sh" ]] || die "staged updater is missing"
    [[ -x "$root/rollback.sh" && -f "$root/rollback.sh" ]] || die "staged rollback is missing"
    [[ -f "$root/web/dist/index.html" ]] || die "staged web build is missing"
    [[ -f "$root/SHA256SUMS" && ! -L "$root/SHA256SUMS" ]] || die "release checksum manifest is missing"
    (
        cd "$root"
        LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 \
            | LC_ALL=C sort -z \
            | xargs -0 sha256sum \
            | cmp -s - SHA256SUMS
        sha256sum -c SHA256SUMS >/dev/null
    ) || die "release checksum manifest does not exactly match the staged release"
}

# Make a verified release tree durable: regular files first, then directories deepest-first.
# Doğrulanmış sürüm ağacını kalıcılaştır: önce normal dosyaları, ardından dizinleri en derinden başlayarak eşitle.
sync_release_tree_durably() {
    local root=$1
    validate_release_tree "$root"
    find "$root" -type f -exec sync -f -- {} \; || die "release files could not be made durable: $root"
    find "$root" -depth -type d -exec sync -f -- {} \; || die "release directories could not be made durable: $root"
    sync -f -- "$root" "$RELEASES_ROOT" || die "release staging hierarchy could not be made durable: $root"
    validate_release_tree "$root"
}

[[ $EUID -eq 0 ]] || die "Run as root / root olarak çalıştırın: sudo ./bootstrap-update.sh [--normal|--bootstrap-pre-ledger|--bootstrap-schema17]"
[[ $# -eq 1 ]] || die "usage: bootstrap-update.sh --normal|--bootstrap-pre-ledger|--bootstrap-schema17"
case "$1" in
    --normal|--bootstrap-pre-ledger|--bootstrap-schema17) UPDATE_MODE=$1 ;;
    *) die "unknown release mode / bilinmeyen sürüm modu: $1" ;;
esac

# Ignore a caller-controlled PATH before privileged build tools are resolved.
# Prefer canonical bin directories before compatibility sbin/bin aliases.
# Arch links /usr/sbin, /sbin and /bin back to /usr/bin; resolving a common
# tool through an alias would correctly fail the trust-chain check even though
# the canonical /usr/bin entry is available.
# Ayrıcalıklı derleme araçları çözülmeden önce çağıranın denetlediği PATH'i yok
# say. Arch uyumluluk bağlantılarından önce kanonik bin dizinlerini tercih et.
PATH=/usr/local/bin:/usr/bin:/usr/local/sbin:/usr/sbin:/bin:/sbin
export PATH
umask 077

source_root=$(cd -- "$(dirname -- "$(readlink -f -- "$0")")" && pwd)
validate_root_trusted_dir_chain "$source_root"
[[ -d "$source_root/.git" && ! -L "$source_root/.git" ]] \
    || die "bootstrap preparation requires a real Git directory"
validate_root_trusted_dir_chain "$source_root/.git"
validate_root_owned_regular_tree "$source_root/.git"
running_script=$(readlink -e -- "$0") || die "cannot resolve bootstrap entrypoint"
[[ "$running_script" == "$source_root/bootstrap-update.sh" && -f "$running_script" && ! -L "$running_script" ]] \
    || die "bootstrap entrypoint is not the trusted checkout script"
read -r script_owner script_mode < <(stat -Lc '%u %a' -- "$running_script") \
    || die "cannot inspect bootstrap entrypoint"
script_permissions=$((8#$script_mode))
[[ "$script_owner" == 0 ]] || die "bootstrap entrypoint must be owned by root"
(( (script_permissions & 0022) == 0 )) \
    || die "bootstrap entrypoint must not be group/other writable"
cd "$source_root"
git_bin=$(trusted_command git)
tar_bin=$(trusted_command tar)
[[ -z "$(run_clean "$git_bin" status --porcelain=v1 --untracked-files=all)" ]] \
    || die "Git checkout is not clean; commit or remove every pending file first"

release_commit=$(run_clean "$git_bin" rev-parse --verify HEAD) || die "cannot resolve release commit"
release_tree=$(run_clean "$git_bin" rev-parse --verify 'HEAD^{tree}') || die "cannot resolve release tree"
release_short=$(run_clean "$git_bin" rev-parse --short=12 HEAD) || die "cannot resolve short release commit"
[[ "$release_commit" =~ ^[0-9a-f]{40,64}$ ]] || die "unexpected release commit format"
[[ "$release_tree" =~ ^[0-9a-f]{40,64}$ ]] || die "unexpected release tree format"
[[ "$release_short" =~ ^[0-9a-f]{12}$ ]] || die "unexpected short release commit format"
release_version=$release_short

prepare_release_root
stamp=$(date -u +%Y%m%dT%H%M%SZ)
release_nonce=$(od -An -N12 -tx1 /dev/urandom | tr -d '[:space:]')
[[ "$release_nonce" =~ ^[0-9a-f]{24}$ ]] || die "cannot create release nonce"
release_root="$RELEASES_ROOT/$release_short-$release_nonce"
incomplete_root="$RELEASES_ROOT/.$release_short-$release_nonce.incomplete.$$"
[[ ! -e "$release_root" && ! -L "$release_root" ]] || die "release path already exists: $release_root"
[[ ! -e "$incomplete_root" && ! -L "$incomplete_root" ]] || die "incomplete release path already exists"
mkdir -m 0700 -- "$incomplete_root"

cleanup_incomplete() {
    if [[ -n "${incomplete_root:-}" && -d "$incomplete_root" && ! -L "$incomplete_root" ]]; then
        rm -rf -- "$incomplete_root"
    fi
}
trap cleanup_incomplete EXIT

echo "==> Staging reviewed Git archive / İncelenmiş Git arşivi hazırlanıyor"
run_clean "$git_bin" archive --format=tar HEAD \
    | run_clean "$tar_bin" -xf - -C "$incomplete_root"
if find "$incomplete_root" -type l -print -quit | grep -q .; then
    die "reviewed archive contains a symbolic link"
fi
if find "$incomplete_root" ! -type d ! -type f -print -quit | grep -q .; then
    die "reviewed archive contains a special filesystem object"
fi

go_bin=$(trusted_command go)
node_bin=$(trusted_command node)
npm_bin=$(trusted_command npm)
mkdir -m 0755 -- "$incomplete_root/bin"
version_flags="-X main.buildVersion=$release_version -X main.buildCommit=$release_commit"

echo "==> Building matching panel and agent / Eşleşen panel ve agent derleniyor"
(
    cd "$incomplete_root"
    run_clean "$go_bin" build -trimpath -buildvcs=false -ldflags "-s -w $version_flags" -o bin/panel ./cmd/panel
    run_clean "$go_bin" build -trimpath -buildvcs=false -ldflags "-s -w $version_flags" -o bin/agent ./cmd/agent
    run_clean "$go_bin" build -trimpath -buildvcs=false -ldflags "-s -w" -o bin/schema17-bridge ./deploy/schema17bridge
)

echo "==> Building matching web artifact / Eşleşen web ürünü derleniyor"
(
    cd "$incomplete_root/web"
    env -i HOME=/root LC_ALL=C PATH="$(dirname "$node_bin"):$PATH" \
        "$npm_bin" ci --ignore-scripts --no-audit --no-fund
    env -i HOME=/root LC_ALL=C PATH="$(dirname "$node_bin"):$PATH" \
        "$npm_bin" run build
)
[[ "$incomplete_root/web/node_modules" == "$incomplete_root/"* ]] || die "unsafe node_modules cleanup path"
rm -rf -- "$incomplete_root/web/node_modules"

printf '%s\n' 1 > "$incomplete_root/release.version"
printf '%s\n' "$release_commit" > "$incomplete_root/release.commit"
printf '%s\n' "$release_tree" > "$incomplete_root/release.tree"
printf '%s\n' "$stamp" > "$incomplete_root/release.created-at-utc"

# The top-level 0700 directory makes the release root-only; inner read modes
# remain suitable for install.sh to copy web assets for the panel user.
# En üstteki 0700 dizin sürümü yalnızca root'a açar; iç okuma kipleri
# install.sh'nin web ürünlerini panel kullanıcısı için kopyalamasına uygun kalır.
chown -R root:root -- "$incomplete_root"
find "$incomplete_root" -type d -exec chmod 0755 {} +
find "$incomplete_root" -type f -exec chmod 0644 {} +
chmod 0700 "$incomplete_root"
chmod 0755 \
    "$incomplete_root/bin/panel" \
    "$incomplete_root/bin/agent" \
    "$incomplete_root/bin/schema17-bridge" \
    "$incomplete_root/install.sh" \
    "$incomplete_root/update.sh" \
    "$incomplete_root/rollback.sh" \
    "$incomplete_root/bootstrap-update.sh"
(
    cd "$incomplete_root"
    LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 \
        | LC_ALL=C sort -z \
        | xargs -0 sha256sum > SHA256SUMS
    chmod 0644 SHA256SUMS
)
validate_release_tree "$incomplete_root"

# Persist every staged byte and directory entry before publishing the release name.
# Sürüm adını yayımlamadan önce hazırlanmış bütün baytları ve dizin girdilerini kalıcılaştır.
sync_release_tree_durably "$incomplete_root"

mv -T --no-clobber -- "$incomplete_root" "$release_root" \
    || die "release publish failed / sürüm yayımlanamadı"
[[ ! -e "$incomplete_root" && -d "$release_root" ]] \
    || die "release publish collision / sürüm yayımlama çakışması"
sync -f -- "$release_root" "$RELEASES_ROOT" \
    || die "published release could not be made durable"
incomplete_root=""
validate_release_tree "$release_root"
echo "==> Verified immutable release / Doğrulanmış değişmez sürüm: $release_root"

# update.sh revalidates this exact release and its binaries before it stops a
# service; neither mode can pull or select another source afterward.
# update.sh bir servisi durdurmadan önce bu tam sürümü ve binary'lerini yeniden
# doğrular; hiçbir mod sonradan başka kaynak çekemez veya seçemez.
env -i HOME=/root PATH="$PATH" LC_ALL=C \
    CELIKPANEL_TRUSTED_RELEASE_ROOT="$release_root" \
    CELIKPANEL_PREFLIGHT_PANEL="$release_root/bin/panel" \
    CELIKPANEL_PREFLIGHT_AGENT="$release_root/bin/agent" \
    /bin/bash "$release_root/update.sh" "$UPDATE_MODE"

trap - EXIT
