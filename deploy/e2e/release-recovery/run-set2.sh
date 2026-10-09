#!/usr/bin/env bash
# set2 wrapper for the Linux QEMU host (archlinux), run as root: the settings-writes cell kind after the corrections of
# 2026-10-10 (with its service-action section) and the request-identity cell kind (D-029).
#
#   run-set2.sh dry-run CELL ARTIFACTS_JSON LAB_NAME
#       Validate the plan without any guest (no lab is created).
#   run-set2.sh cell CELL ARTIFACTS_JSON LAB_NAME SSH_PORT [LOCAL_PORT]
#       Prepare and start a NEW lab /var/tmp/cp-release-drill-LAB_NAME, run the one cell on its node, then
#       stop the lab (disks and evidence retained).
#
# CELL: set2-debian13 | set2-ubuntu | set2-arch   (settings_writes_trial.py; groups A and B)
#       rid-debian13  | rid-ubuntu  | rid-arch    (request_identity_trial.py; group C)
# ARTIFACTS_JSON: an upd1-artifacts.json from run-upd1.sh build (the baseline B is the installed candidate).
# One cell per new lab. Never reuse a lab or a guest. No update is started by either cell kind.
set -euo pipefail
HERE=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
IMAGES=${UPD1_IMAGE_CACHE:-/var/tmp/cp-v3n28/images}
LAB=(python3 "$HERE/lab.py")

usage() { sed -n '2,15p' "${BASH_SOURCE[0]}" >&2; exit 2; }
driver_for() {
    case $1 in
        set2-debian13|set2-ubuntu|set2-arch) DRIVER=(python3 "$HERE/settings_writes_trial.py") ;;
        rid-debian13|rid-ubuntu|rid-arch) DRIVER=(python3 "$HERE/request_identity_trial.py") ;;
        *) usage ;;
    esac
}
[[ $# -ge 1 ]] || usage
command=$1; shift
case $command in
    dry-run)
        [[ $# -eq 3 ]] || usage
        driver_for "$1"
        exec "${DRIVER[@]}" plan --cell "$1" --artifacts "$2" --work-root "/var/tmp/cp-release-drill-$3" --dry-run
        ;;
    cell)
        [[ $# -ge 4 && $# -le 5 ]] || usage
        cell=$1 artifacts=$2 name=$3 port=$4 local_port=${5:-18443}
        driver_for "$cell"
        [[ $EUID -eq 0 ]] || { echo "run as root (fixture signing keys are root-only)" >&2; exit 2; }
        root=/var/tmp/cp-release-drill-$name
        [[ ! -e $root ]] || { echo "lab $root exists; every cell needs a NEW lab" >&2; exit 2; }
        "${DRIVER[@]}" plan --cell "$cell" --artifacts "$artifacts" --work-root "$root" > /dev/null
        platform=debian13-arch
        [[ $cell != *-ubuntu ]] || platform=ubuntu
        "${LAB[@]}" prepare --work-root "$root" --image-cache "$IMAGES" --ssh-port "$port" --platform "$platform" --execute
        "${LAB[@]}" start --work-root "$root" --execute
        "${LAB[@]}" status --work-root "$root"
        status=0
        "${DRIVER[@]}" run --cell "$cell" --artifacts "$artifacts" --work-root "$root" --local-port "$local_port" \
            --execute || status=$?
        "${LAB[@]}" stop --work-root "$root" --execute || true
        echo "SET2 cell=$cell lab=$root driver_exit=$status evidence=$root/evidence/*/upd1"
        exit "$status"
        ;;
    *) usage ;;
esac
