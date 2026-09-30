#!/usr/bin/env bash
# upd1 wrapper for the Linux QEMU host (archlinux), run as root from the repository root.
#
#   run-upd1.sh build [SOURCE_COMMIT]
#       Disposable clone + fixture commits B/G/D/S/R + five acceptance-license
#       archives; prints the upd1-artifacts.json path.
#   run-upd1.sh prove ARTIFACTS_JSON
#       Read-only host proof of every archive in it (inventory, policy, source).
#   run-upd1.sh dry-run CELL ARTIFACTS_JSON LAB_NAME
#       Validate the plan without any guest (no lab is created).
#   run-upd1.sh cell CELL ARTIFACTS_JSON LAB_NAME SSH_PORT [LOCAL_PORT]
#       Prepare and start a NEW lab /var/tmp/cp-release-drill-LAB_NAME, run the
#       one cell on its node, then stop the lab (disks and evidence retained).
#
# CELL: upd1-debian13-defective | upd1-debian13-good | upd1-arch-defective | upd1-arch-good
#       upd1-debian13-startcheck | upd1-arch-startcheck | upd1-debian13-realstart | upd1-arch-realstart
#       (those four need an upd1-artifacts.json built with the startcheck/realstart roles)
#       upd1-debian13-owner-continuation | upd1-arch-owner-continuation
#       upd1-debian13-mgmt-off-reboot | upd1-arch-mgmt-off-reboot   (good candidate G; any document)
# One cell per new lab. Never reuse a lab, an intent or a guest.
# The image cache defaults to /var/tmp/cp-v3n28/images (UPD1_IMAGE_CACHE overrides).
# UPD1_DNS_MODE: external (default; DNS not provided by this run) | local (two-node variant only).
# UPD1_SETUP_DRAFT_JSON: optional owner draft choices (local mode: peer_ip and peer_ns).
set -euo pipefail
HERE=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
IMAGES=${UPD1_IMAGE_CACHE:-/var/tmp/cp-v3n28/images}
DNS_MODE=${UPD1_DNS_MODE:-external}
# Arrays: the repository path may contain spaces ("/mnt/c/CELIKBROS PROJECTS/...").
LAB=(python3 "$HERE/lab.py")
DRIVER=(python3 "$HERE/owner_update_trial.py")
CHOICES=(--dns-mode "$DNS_MODE")
[[ -z ${UPD1_SETUP_DRAFT_JSON:-} ]] || CHOICES+=(--setup-draft-json "$UPD1_SETUP_DRAFT_JSON")

usage() { sed -n '2,23p' "${BASH_SOURCE[0]}" >&2; exit 2; }
[[ $# -ge 1 ]] || usage
command=$1; shift
case $command in
    build)
        exec bash "$HERE/build-upd1-artifacts.sh" "$@"
        ;;
    prove)
        [[ $# -eq 1 ]] || usage
        exec "${DRIVER[@]}" prove --artifacts "$1"
        ;;
    dry-run)
        [[ $# -eq 3 ]] || usage
        cell=$1 artifacts=$2 name=$3
        exec "${DRIVER[@]}" plan --cell "$cell" --artifacts "$artifacts" --work-root "/var/tmp/cp-release-drill-$name" \
            "${CHOICES[@]}" --dry-run
        ;;
    cell)
        [[ $# -ge 4 && $# -le 5 ]] || usage
        cell=$1 artifacts=$2 name=$3 port=$4 local_port=${5:-18443}
        [[ $EUID -eq 0 ]] || { echo "run as root (fixture signing keys are root-only)" >&2; exit 2; }
        root=/var/tmp/cp-release-drill-$name
        [[ ! -e $root ]] || { echo "lab $root exists; every cell needs a NEW lab" >&2; exit 2; }
        "${DRIVER[@]}" plan --cell "$cell" --artifacts "$artifacts" --work-root "$root" "${CHOICES[@]}" > /dev/null
        "${LAB[@]}" prepare --work-root "$root" --image-cache "$IMAGES" --ssh-port "$port" --execute
        "${LAB[@]}" start --work-root "$root" --execute
        "${LAB[@]}" status --work-root "$root"
        status=0
        "${DRIVER[@]}" run --cell "$cell" --artifacts "$artifacts" --work-root "$root" --local-port "$local_port" \
            "${CHOICES[@]}" --execute || status=$?
        # Stop both guests; overlays, logs and evidence stay under $root.
        "${LAB[@]}" stop --work-root "$root" --execute || true
        echo "UPD1 cell=$cell dns_mode=$DNS_MODE lab=$root driver_exit=$status evidence=$root/evidence/*/upd1"
        exit "$status"
        ;;
    *) usage ;;
esac
