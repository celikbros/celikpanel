#!/usr/bin/env bash
# Stop and delete one run's disposable guests (QMP quit; no signal fallback).
# Copy the evidence directory out first; teardown deletes only the cell overlay.
#
# usage: teardown.sh TOPOLOGY PRIMARY_NODE|auto RUN_LABEL
set -euo pipefail

topology=${1:?topology}
primary=${2:?primary node or auto}
label=${3:?run label}
REPO=${CELIKPANEL_REPO:-'/mnt/c/CELIKBROS PROJECTS/celikpanel'}
ROOT=${CELIKPANEL_PAIR_ROOT:-/var/tmp/cp-pair-accept/work}
FIXTURE=$REPO/deploy/e2e/dns-kill-matrix/fixture.py
DRIVER=$REPO/deploy/e2e/dns-pair-acceptance/pair_acceptance.py

node_args=()
[[ $primary == auto ]] || node_args=(--primary-node "$primary")
cell=$(python3 "$DRIVER" plan --topology "$topology" "${node_args[@]}" --run-label "$label" \
    --work-root "$ROOT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["cell_id"])')
python3 "$FIXTURE" stop --work-root "$ROOT" --cell-id "$cell" --execute
python3 "$FIXTURE" teardown --work-root "$ROOT" --cell-id "$cell" --execute
