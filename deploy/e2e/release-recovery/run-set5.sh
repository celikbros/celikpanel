#!/usr/bin/env bash
# set5 wrapper for the Linux QEMU host (archlinux), run as root: the closing native measurement of the release
# candidate, set3's Part 2 again (the owner-started update from the published v0.1.0-alpha.81).
#
#   run-set5.sh dry-run CELL ARTIFACTS_JSON LAB_NAME
#       Validate the plan without any guest (no lab is created).
#   run-set5.sh cell CELL ARTIFACTS_JSON LAB_NAME SSH_PORT [LOCAL_PORT]
#       Prepare and start a NEW lab /var/tmp/cp-release-drill-LAB_NAME, run the one cell on its node, then
#       stop the lab (disks and evidence retained).
#
# CELL: upd1-debian13-good | upd1-ubuntu-good | upd1-arch-good
#       upd1-debian13-defective | upd1-ubuntu-defective | upd1-arch-defective | upd1-debian13-startcheck
#       upd1-debian13-owner-continuation | upd1-ubuntu-owner-continuation | upd1-debian13-mgmt-off-reboot
# ARTIFACTS_JSON: an upd1-artifacts.json from run-upd1.sh build --baseline-ref v0.1.0-alpha.81.
# One cell per new lab. Never reuse a lab or a guest. SET5_DISK_GATE, when set, is a program that must exit 0
# immediately before the guests are started (the host's free-disk rule); otherwise no guest is started.
# SET5_AFTER_PREPARE, when set, is a program run with the lab root after the lab was prepared and before the
# gate (set5: it moves the guests' node directories, overlay disks included, onto RAM-backed tmpfs mounts).
# CELIKPANEL_LAB_LINK_BASE_IMAGES=1 (lab.py) hard-links the cached base images instead of copying them.
set -euo pipefail
HERE=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
IMAGES=${UPD1_IMAGE_CACHE:-/var/tmp/cp-v3n28/images}
LAB=(python3 "$HERE/lab.py")
DRIVER=(python3 "$HERE/set5_trial.py")

usage() { sed -n '2,19p' "${BASH_SOURCE[0]}" >&2; exit 2; }
[[ $# -ge 1 ]] || usage
command=$1; shift
case $command in
    dry-run)
        [[ $# -eq 3 ]] || usage
        exec "${DRIVER[@]}" plan --cell "$1" --artifacts "$2" --work-root "/var/tmp/cp-release-drill-$3" --dry-run
        ;;
    cell)
        [[ $# -ge 4 && $# -le 5 ]] || usage
        cell=$1 artifacts=$2 name=$3 port=$4 local_port=${5:-18443}
        [[ $EUID -eq 0 ]] || { echo "run as root (fixture signing keys are root-only)" >&2; exit 2; }
        root=/var/tmp/cp-release-drill-$name
        [[ ! -e $root ]] || { echo "lab $root exists; every cell needs a NEW lab" >&2; exit 2; }
        "${DRIVER[@]}" plan --cell "$cell" --artifacts "$artifacts" --work-root "$root" > /dev/null
        platform=debian13-arch
        [[ $cell != upd1-ubuntu-* ]] || platform=ubuntu
        "${LAB[@]}" prepare --work-root "$root" --image-cache "$IMAGES" --ssh-port "$port" --platform "$platform" --execute
        if [[ -n ${SET5_AFTER_PREPARE:-} ]]; then
            bash "$SET5_AFTER_PREPARE" "$root" || { echo "SET5 cell=$cell lab=$root NOT STARTED: the after-prepare program failed" >&2; exit 76; }
        fi
        if [[ -n ${SET5_DISK_GATE:-} ]]; then
            bash "$SET5_DISK_GATE" "$cell run-${name##*-} (guest start)" || { echo "SET5 cell=$cell lab=$root NOT STARTED: the disk gate refused the guest start" >&2; exit 75; }
        fi
        "${LAB[@]}" start --work-root "$root" --execute
        "${LAB[@]}" status --work-root "$root"
        status=0
        "${DRIVER[@]}" run --cell "$cell" --artifacts "$artifacts" --work-root "$root" --local-port "$local_port" \
            --execute || status=$?
        "${LAB[@]}" stop --work-root "$root" --execute || true
        echo "SET5 cell=$cell lab=$root driver_exit=$status evidence=$root/evidence/*/upd1"
        exit "$status"
        ;;
    *) usage ;;
esac
