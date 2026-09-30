#!/usr/bin/env bash
# upd1 wrapper for the Linux QEMU host (archlinux), run as root from the repository root.
#
#   run-upd1.sh build [SOURCE_COMMIT]
#       Disposable clone + fixture commits B/G/D + three acceptance-license
#       archives; prints the upd1-artifacts.json path.
#   run-upd1.sh dry-run CELL ARTIFACTS_JSON LAB_NAME
#       Validate the plan without any guest (no lab is created).
#   run-upd1.sh cell CELL ARTIFACTS_JSON LAB_NAME SSH_PORT [LOCAL_PORT]
#       Prepare and start a NEW lab /var/tmp/cp-release-drill-LAB_NAME, run the
#       one cell on its node, then stop the lab (disks and evidence retained).
#
# CELL: upd1-debian13-defective | upd1-debian13-good | upd1-arch-defective | upd1-arch-good
# One cell per new lab. Never reuse a lab, an intent or a guest.
# The image cache defaults to /var/tmp/cp-v3n28/images (UPD1_IMAGE_CACHE overrides).
set -euo pipefail
HERE=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
IMAGES=${UPD1_IMAGE_CACHE:-/var/tmp/cp-v3n28/images}
LAB="python3 $HERE/lab.py"
DRIVER="python3 $HERE/owner_update_trial.py"

usage() { sed -n '2,17p' "$0" >&2; exit 2; }
[[ $# -ge 1 ]] || usage
command=$1; shift
case $command in
    build)
        exec bash "$HERE/build-upd1-artifacts.sh" "$@"
        ;;
    dry-run)
        [[ $# -eq 3 ]] || usage
        cell=$1 artifacts=$2 name=$3
        exec $DRIVER plan --cell "$cell" --artifacts "$artifacts" --work-root "/var/tmp/cp-release-drill-$name" --dry-run
        ;;
    cell)
        [[ $# -ge 4 && $# -le 5 ]] || usage
        cell=$1 artifacts=$2 name=$3 port=$4 local_port=${5:-18443}
        [[ $EUID -eq 0 ]] || { echo "run as root (fixture signing keys are root-only)" >&2; exit 2; }
        root=/var/tmp/cp-release-drill-$name
        [[ ! -e $root ]] || { echo "lab $root exists; every cell needs a NEW lab" >&2; exit 2; }
        $DRIVER plan --cell "$cell" --artifacts "$artifacts" --work-root "$root" > /dev/null
        $LAB prepare --work-root "$root" --image-cache "$IMAGES" --ssh-port "$port" --execute
        $LAB start --work-root "$root" --execute
        $LAB status --work-root "$root"
        status=0
        $DRIVER run --cell "$cell" --artifacts "$artifacts" --work-root "$root" --local-port "$local_port" --execute \
            || status=$?
        # Stop both guests; overlays, logs and evidence stay under $root.
        $LAB stop --work-root "$root" --execute || true
        echo "UPD1 cell=$cell lab=$root driver_exit=$status evidence=$root/evidence/*/upd1"
        exit "$status"
        ;;
    *) usage ;;
esac
