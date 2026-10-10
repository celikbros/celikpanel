#!/usr/bin/env bash
# set8 wrapper for the Linux QEMU host (archlinux), run as root: the published alpha.82 code installed fresh (set6's
# method, names pinned first) and measured while the owner edits a site's nginx vhost and PHP-FPM pool by hand.
#
#   run-set8.sh dry-run CELL ARTIFACTS_JSON LAB_NAME
#       Validate the plan without any guest (no lab is created).
#   run-set8.sh cell CELL ARTIFACTS_JSON LAB_NAME SSH_PORT [LOCAL_PORT]
#       Prepare and start a NEW lab /var/tmp/cp-release-drill-LAB_NAME, run the one cell on its node, then
#       stop the lab (disks and evidence retained).
#
# CELL: set8-debian13 | set8-ubuntu | set8-arch   (ARTIFACTS_JSON from `run-upd1.sh build SOURCE`; its baseline is
#       the code installed fresh)
# One cell per new lab. SET8_DISK_GATE, when set, is a program that must exit 0 immediately before the guests are
# started (the host's free-disk rule); otherwise no guest is started.
set -euo pipefail
HERE=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
IMAGES=${UPD1_IMAGE_CACHE:-/var/tmp/cp-v3n28/images}
LAB=(python3 "$HERE/lab.py")
DRIVER=(python3 "$HERE/set8_trial.py")

usage() { sed -n '2,15p' "${BASH_SOURCE[0]}" >&2; exit 2; }
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
        [[ $cell != set8-ubuntu ]] || platform=ubuntu
        "${LAB[@]}" prepare --work-root "$root" --image-cache "$IMAGES" --ssh-port "$port" --platform "$platform" --execute
        if [[ -n ${SET8_DISK_GATE:-} ]]; then
            bash "$SET8_DISK_GATE" "$cell $name (guest start)" || { echo "SET8 cell=$cell lab=$root NOT STARTED: the disk gate refused the guest start" >&2; exit 75; }
        fi
        "${LAB[@]}" start --work-root "$root" --execute
        "${LAB[@]}" status --work-root "$root"
        status=0
        "${DRIVER[@]}" run --cell "$cell" --artifacts "$artifacts" --work-root "$root" --local-port "$local_port" \
            --execute || status=$?
        "${LAB[@]}" stop --work-root "$root" --execute || true
        echo "SET8 cell=$cell lab=$root driver_exit=$status evidence=$root/evidence/*/upd1"
        exit "$status"
        ;;
    *) usage ;;
esac
