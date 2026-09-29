#!/usr/bin/env bash
# Run the acceptance driver against guests prepared by prepare-guests.sh.
#
# usage: run-topology.sh TOPOLOGY PRIMARY_NODE|auto RUN_LABEL DIST_JSON [extra driver flags...]
#   DIST_JSON: the dist.json written by build-dist.sh
# Extra flags are passed through, e.g. --edit-method api-put, --infrastructure-dns,
# or (owner decision only) --license-mode owner-key --allow-license-service
# --license-key-file-primary FILE --license-key-file-secondary FILE, or
# --license-mode acceptance-fixture with the dist.json of
# build-dist.sh --acceptance-license (test only; does not evidence licensing).
# The product's web/src exported by build-dist.sh (dist.json product_web_src)
# is passed as --product-web-src, so the guidance texts are the product's.
set -euo pipefail

topology=${1:?topology}
primary=${2:?primary node or auto}
label=${3:?run label}
dist=${4:?dist.json}
shift 4
REPO=${CELIKPANEL_REPO:-'/mnt/c/CELIKBROS PROJECTS/celikpanel'}
ROOT=${CELIKPANEL_PAIR_ROOT:-/var/tmp/cp-pair-accept/work}
EVIDENCE=${CELIKPANEL_PAIR_EVIDENCE:-/var/tmp/cp-pair-accept/evidence}
DRIVER=$REPO/deploy/e2e/dns-pair-acceptance/pair_acceptance.py
KEY=/var/tmp/cp-pair-accept/id_ed25519

read_dist() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get(sys.argv[2], ""))' "$dist" "$1"; }
web_src=$(read_dist product_web_src)
# A dist built before build-dist.sh exported it: export-product-web-src.sh puts it next to dist.json.
[[ -n $web_src || ! -d $(dirname "$dist")/product-web-src ]] || web_src=$(dirname "$dist")/product-web-src
web_args=()
[[ -z $web_src ]] || web_args=(--product-web-src "$web_src")
node_args=()
[[ $primary == auto ]] || node_args=(--primary-node "$primary")
mkdir -p -m 0700 "$EVIDENCE"
args=(--topology "$topology" "${node_args[@]}" --run-label "$label" --work-root "$ROOT"
      --identity-file "$KEY" --dist-archive "$(read_dist archive)" --dist-sha256 "$(read_dist sha256)"
      --dist-commit "$(read_dist commit)" --dist-tree "$(read_dist tree)" --evidence-root "$EVIDENCE"
      "${web_args[@]}" "$@")
# Dry run first: prints the plan and validates every flag without touching a guest.
python3 "$DRIVER" run "${args[@]}"
python3 "$DRIVER" run "${args[@]}" --execute
