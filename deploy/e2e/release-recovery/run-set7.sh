#!/usr/bin/env bash
# set7 wrapper for the Linux QEMU host (archlinux), run as root: the interface in a real Chrome across a Panel
# restart. The driver (set7_trial.py) prepares the guest as set6's good-update cells do and then hands the signed-in
# owner's part to the browser (web/tools/browser-inspect/live-restart.mjs); it never starts the update itself.
#
#   run-set7.sh dry-run CELL ARTIFACTS_JSON LAB_NAME HAND
#       Validate the plan without any guest (no lab is created).
#   run-set7.sh cell CELL ARTIFACTS_JSON LAB_NAME SSH_PORT LOCAL_PORT HAND [WAIT_SECONDS]
#       Prepare and start a NEW lab /var/tmp/cp-release-drill-LAB_NAME, run the cell on its node (the browser
#       window waits for /var/tmp/cp-set7-run/hand/HAND/done.json), then stop the lab (disks and evidence retained).
#
# CELL: upd1-debian13-good (upd1-ubuntu-good, upd1-arch-good offered, not used by set7's first run).
# One cell per new lab. SET7_DISK_GATE, when set, is a program that must exit 0 immediately before the guests are
# started (the host's free-disk rule); otherwise no guest is started.
set -euo pipefail
HERE=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
IMAGES=${UPD1_IMAGE_CACHE:-/var/tmp/cp-v3n28/images}
LAB=(python3 "$HERE/lab.py")
DRIVER=(python3 "$HERE/set7_trial.py")

usage() { sed -n '2,15p' "${BASH_SOURCE[0]}" >&2; exit 2; }
[[ $# -ge 1 ]] || usage
command=$1; shift
case $command in
    dry-run)
        [[ $# -eq 4 ]] || usage
        exec "${DRIVER[@]}" plan --cell "$1" --artifacts "$2" --work-root "/var/tmp/cp-release-drill-$3" --hand "$4" --dry-run
        ;;
    cell)
        [[ $# -ge 6 && $# -le 7 ]] || usage
        cell=$1 artifacts=$2 name=$3 port=$4 local_port=$5 hand=$6 wait=${7:-14400}
        [[ $EUID -eq 0 ]] || { echo "run as root (fixture signing keys are root-only)" >&2; exit 2; }
        root=/var/tmp/cp-release-drill-$name
        [[ ! -e $root ]] || { echo "lab $root exists; every cell needs a NEW lab" >&2; exit 2; }
        "${DRIVER[@]}" plan --cell "$cell" --artifacts "$artifacts" --work-root "$root" --hand "$hand" > /dev/null
        platform=debian13-arch
        [[ $cell != upd1-ubuntu-* ]] || platform=ubuntu
        "${LAB[@]}" prepare --work-root "$root" --image-cache "$IMAGES" --ssh-port "$port" --platform "$platform" --execute
        if [[ -n ${SET7_DISK_GATE:-} ]]; then
            bash "$SET7_DISK_GATE" "$cell $name (guest start)" || { echo "SET7 cell=$cell lab=$root NOT STARTED: the disk gate refused the guest start" >&2; exit 75; }
        fi
        "${LAB[@]}" start --work-root "$root" --execute
        "${LAB[@]}" status --work-root "$root"
        status=0
        "${DRIVER[@]}" run --cell "$cell" --artifacts "$artifacts" --work-root "$root" --local-port "$local_port" \
            --hand "$hand" --wait-seconds "$wait" --execute || status=$?
        "${LAB[@]}" stop --work-root "$root" --execute || true
        echo "SET7 cell=$cell lab=$root driver_exit=$status evidence=$root/evidence/*/upd1"
        exit "$status"
        ;;
    *) usage ;;
esac
