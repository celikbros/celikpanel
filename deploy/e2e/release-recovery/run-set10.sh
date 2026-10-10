#!/usr/bin/env bash
# set10 wrapper for the Linux QEMU host (archlinux), run as root: D-031 as implemented in cd46ca595 measured on
# disposable guests: installed fresh (cells set10-*) and reached by the owner's update from the published alpha.81
# (cells upd1-*-good, upd1-*-defective).
#
#   run-set10.sh dry-run CELL ARTIFACTS_JSON LAB_NAME
#       Validate the plan without any guest (no lab is created).
#   run-set10.sh cell CELL ARTIFACTS_JSON LAB_NAME SSH_PORT [LOCAL_PORT]
#       Prepare and start a NEW lab /var/tmp/cp-release-drill-LAB_NAME, run the one cell on its node, then
#       stop the lab (disks and evidence retained).
#
# CELL: set10-debian13 | set10-ubuntu | set10-arch   (ARTIFACTS_JSON from `run-upd1.sh build SOURCE`; its baseline is
#       the code installed fresh)
#       upd1-debian13-good | upd1-ubuntu-good | upd1-arch-good | upd1-debian13-defective | upd1-ubuntu-defective |
#       upd1-arch-defective   (ARTIFACTS_JSON from `run-upd1.sh build --baseline-ref v0.1.0-alpha.81 SOURCE`)
# One cell per new lab. SET10_NEIGHBOUR_GATE and then SET10_DISK_GATE, when set, are programs that must exit 0
# immediately before the guests are started (no other measurement's guest running; the host's free-disk rule);
# otherwise no guest is started.
set -euo pipefail
HERE=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
IMAGES=${UPD1_IMAGE_CACHE:-/var/tmp/cp-v3n28/images}
LAB=(python3 "$HERE/lab.py")
DRIVER=(python3 "$HERE/set10_trial.py")

usage() { sed -n '2,18p' "${BASH_SOURCE[0]}" >&2; exit 2; }
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
        [[ $cell != set10-ubuntu && $cell != upd1-ubuntu-* ]] || platform=ubuntu
        "${LAB[@]}" prepare --work-root "$root" --image-cache "$IMAGES" --ssh-port "$port" --platform "$platform" --execute
        if [[ -n ${SET10_NEIGHBOUR_GATE:-} ]]; then
            bash "$SET10_NEIGHBOUR_GATE" "$cell $name" || { echo "SET10 cell=$cell lab=$root NOT STARTED: another measurement's guest is running" >&2; exit 75; }
        fi
        if [[ -n ${SET10_DISK_GATE:-} ]]; then
            bash "$SET10_DISK_GATE" "$cell $name (guest start)" || { echo "SET10 cell=$cell lab=$root NOT STARTED: the disk gate refused the guest start" >&2; exit 75; }
        fi
        "${LAB[@]}" start --work-root "$root" --execute
        "${LAB[@]}" status --work-root "$root"
        status=0
        "${DRIVER[@]}" run --cell "$cell" --artifacts "$artifacts" --work-root "$root" --local-port "$local_port" \
            --execute || status=$?
        "${LAB[@]}" stop --work-root "$root" --execute || true
        echo "SET10 cell=$cell lab=$root driver_exit=$status evidence=$root/evidence/*/upd1"
        exit "$status"
        ;;
    *) usage ;;
esac
