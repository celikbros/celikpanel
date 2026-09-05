#!/bin/bash
# R-054. A prerequisite step that upgrades packages can replace the running
# kernel and take its module tree with it, after which the host can load no
# kernel module at all and the firewall and the VPN cannot work. The installer
# must notice, must say so as the last thing the operator reads, and must not
# accuse a machine that simply keeps no kernel modules.
#
# R-054. Paketleri yukselten bir on gereksinim adimi calisan cekirdegi
# degistirebilir; kurulum bunu fark etmeli ve operatorun okudugu son sey olarak
# soylemelidir.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL="$ROOT/install.sh"

die() {
    printf 'install kernel reboot contract failed: %s\n' "$*" >&2
    exit 1
}

require_literal() {
    local literal=$1
    grep -Fq -- "$literal" "$INSTALL" ||
        die "install.sh is missing: $literal"
}

require_before() {
    local first=$1 second=$2 first_line second_line
    first_line=$(grep -Fn -- "$first" "$INSTALL" | head -n 1 | cut -d: -f1)
    second_line=$(grep -Fn -- "$second" "$INSTALL" | tail -n 1 | cut -d: -f1)
    [[ -n "$first_line" && -n "$second_line" && "$first_line" -lt "$second_line" ]] ||
        die "install.sh sequence is invalid: $first must precede $second"
}

bash -n "$INSTALL"

# The check exists, is not an Arch special case, and reads fixed paths.
require_literal 'kernel_running_release() {'
require_literal 'kernel_module_tree_usable() {'
require_literal 'kernel_other_module_tree_present() {'
require_literal 'classify_kernel_reboot_state() {'
require_literal 'report_kernel_reboot_requirement() {'
require_literal 'print_kernel_reboot_closing() {'
require_literal 'KERNEL_RELEASE_FILE=/proc/sys/kernel/osrelease'
require_literal 'KERNEL_MODULE_ROOT=/lib/modules'
require_literal 'KERNEL_REBOOT_MARKER_RUN=/run/reboot-required'
require_literal 'KERNEL_REBOOT_MARKER_VAR=/var/run/reboot-required'
# Distribution-aware, not one distribution's special case.
require_literal '        apt)'
require_literal '        pacman)'

# It runs right after the package step, and the restart is the LAST thing the
# operator reads - after the success banner, not before it.
require_literal 'KERNEL_REBOOT_STATE=$(classify_kernel_reboot_state "$PKG_FAMILY" "$KERNEL_REBOOT_RUNNING_RELEASE")'
require_before 'step "Small prerequisites (curl, tar, xz, nftables, iproute2)" \' \
    'KERNEL_REBOOT_STATE=$(classify_kernel_reboot_state'
require_before 'c '\''1;32'\'' "CelikPanel was installed successfully. / CelikPanel başarıyla kuruldu."' \
    'print_kernel_reboot_closing "$KERNEL_REBOOT_STATE" "$KERNEL_REBOOT_RUNNING_RELEASE"'
last_statement=$(grep -v '^[[:space:]]*#' "$INSTALL" | grep -v '^[[:space:]]*$' | tail -n 1)
[[ "$last_statement" == 'print_kernel_reboot_closing "$KERNEL_REBOOT_STATE" "$KERNEL_REBOOT_RUNNING_RELEASE"' ]] ||
    die "the restart notice is not the last thing install.sh prints: $last_statement"

# The installer finishes rather than refusing, so the completion marker must
# still be written and no kernel state may abort the run.
require_before 'install -m 0600 -o root -g root /dev/null "$INSTALL_COMPLETE"' \
    'print_kernel_reboot_closing "$KERNEL_REBOOT_STATE" "$KERNEL_REBOOT_RUNNING_RELEASE"'
if grep -n 'die .*kernel_reboot\|die .*KERNEL_REBOOT' "$INSTALL"; then
    die 'the kernel check aborts the installation instead of finishing it'
fi

kernel_defs=$(
    sed -n \
        -e '/^kernel_running_release() {$/,/^}$/p' \
        -e '/^kernel_module_tree_usable() {$/,/^}$/p' \
        -e '/^kernel_other_module_tree_present() {$/,/^}$/p' \
        -e '/^classify_kernel_reboot_state() {$/,/^}$/p' \
        -e '/^report_kernel_reboot_requirement() {$/,/^}$/p' \
        -e '/^print_kernel_reboot_closing() {$/,/^}$/p' \
        "$INSTALL"
)
[[ -n "$kernel_defs" ]] || die 'the kernel check definitions could not be extracted'

WORK=$(mktemp -d)
cleanup() { rm -rf -- "$WORK"; }
trap cleanup EXIT

# stage <name> <running release> [module tree]...
stage() {
    local name=$1 release=$2 tree
    shift 2
    rm -rf -- "${WORK:?}/$name"
    mkdir -p -- "$WORK/$name"
    for tree in "$@"; do
        mkdir -p -- "$WORK/$name/modules/$tree"
        printf '\n' > "$WORK/$name/modules/$tree/modules.dep"
    done
    printf '%s\n' "$release" > "$WORK/$name/osrelease"
}

# classify <name> <family> [extra shell]
classify() {
    local name=$1 family=$2 extra=${3:-}
    KERNEL_DEFS="$kernel_defs" \
    FIXTURE="$WORK/$name" \
    FAMILY="$family" \
    EXTRA="$extra" \
    PACMAN_FAKE="$WORK/pacman-owns" \
        /bin/bash <<'BASH'
set -euo pipefail
KERNEL_RELEASE_FILE="$FIXTURE/osrelease"
KERNEL_MODULE_ROOT="$FIXTURE/modules"
KERNEL_REBOOT_MARKER_RUN="$FIXTURE/reboot-required"
KERNEL_REBOOT_MARKER_VAR="$FIXTURE/var-reboot-required"
PACMAN_BIN="$PACMAN_FAKE"
UNAME_BIN=/definitely/not/here
eval "$KERNEL_DEFS"
[[ -z "$EXTRA" ]] || eval "$EXTRA"
classify_kernel_reboot_state "$FAMILY" "$(kernel_running_release)"
BASH
}

expect() {
    local label=$1 got=$2 want=$3
    [[ "$got" == "$want" ]] || die "$label: state=$got, want=$want"
}

# The machine R-054 was found on: the upgrade installed a new kernel and took
# the running kernel's modules with it.
stage replaced 6.16.7-arch1-1 6.17.1-arch1-1
expect 'replaced kernel' "$(classify replaced pacman)" required
# The same condition on any other family is the same answer: not an Arch case.
expect 'replaced kernel on apt' "$(classify replaced apt)" required

# A healthy machine says nothing.
stage healthy 6.16.7-arch1-1 6.16.7-arch1-1 6.17.1-arch1-1
expect 'healthy kernel' "$(classify healthy pacman)" none

# A machine that keeps no kernel modules at all - a container, a monolithic
# kernel - never had a tree, and must not be told to restart.
stage moduleless 6.16.7-arch1-1
expect 'modules-less host' "$(classify moduleless pacman)" none
expect 'modules-less host on apt' "$(classify moduleless apt)" none

# An unreadable or unsafe running release proves nothing either way.
stage escaping ../../etc 6.17.1-arch1-1
expect 'escaping release' "$(classify escaping pacman)" none
stage empty '' 6.17.1-arch1-1
: > "$WORK/empty/osrelease"
expect 'empty release' "$(classify empty pacman)" none

# The milder case, per family. apt keeps the running kernel's modules and marks
# the machine itself; pacman is asked whether it still owns them.
stage marked 6.16.7-generic 6.16.7-generic 6.17.1-generic
expect 'apt without a marker' "$(classify marked apt)" none
expect 'apt with a marker' \
    "$(classify marked apt 'touch "$KERNEL_REBOOT_MARKER_RUN"')" recommended
expect 'apt with the legacy marker' \
    "$(classify marked apt 'touch "$KERNEL_REBOOT_MARKER_VAR"')" recommended

printf '#!/bin/bash\nexit 0\n' > "$WORK/pacman-owns"
chmod +x "$WORK/pacman-owns"
expect 'pacman still owns the running kernel' "$(classify marked pacman)" none
printf '#!/bin/bash\nexit 1\n' > "$WORK/pacman-owns"
expect 'pacman no longer owns the running kernel' "$(classify marked pacman)" recommended

# What the operator actually reads, in both languages, with the release named
# and the one command to run.
closing=$(
    KERNEL_DEFS="$kernel_defs" /bin/bash <<'BASH'
set -euo pipefail
c() { printf '%s\n' "$2"; }
bilingual() { if [[ -n "${2:-}" ]]; then printf '%s / %s' "$1" "$2"; else printf '%s' "$1"; fi; }
warn() { printf '    %s\n' "$(bilingual "$@")"; }
eval "$KERNEL_DEFS"
print_kernel_reboot_closing required 6.16.7-arch1-1
report_kernel_reboot_requirement required 6.16.7-arch1-1
BASH
)
for phrase in \
    'RESTART THIS SERVER NOW / BU SUNUCUYU SIMDI YENIDEN BASLATIN' \
    '6.16.7-arch1-1' \
    'reboot' \
    'cannot load nftables or WireGuard' \
    'yeniden baslatilana kadar nftables veya WireGuard yuklenemez' \
    'must be RESTARTED' \
    'YENIDEN BASLATILMALI'
do
    grep -Fq -- "$phrase" <<< "$closing" ||
        die "the restart notice does not say: $phrase"
done

quiet=$(
    KERNEL_DEFS="$kernel_defs" /bin/bash <<'BASH'
set -euo pipefail
c() { printf '%s\n' "$2"; }
bilingual() { if [[ -n "${2:-}" ]]; then printf '%s / %s' "$1" "$2"; else printf '%s' "$1"; fi; }
warn() { printf '    %s\n' "$(bilingual "$@")"; }
eval "$KERNEL_DEFS"
print_kernel_reboot_closing none 6.16.7-arch1-1
report_kernel_reboot_requirement none 6.16.7-arch1-1
BASH
)
[[ -z "$(tr -d '[:space:]' <<< "$quiet")" ]] ||
    die "a healthy machine was told to restart: $quiet"

printf 'install kernel reboot contract passed\n'
