#!/usr/bin/env bash
# Provision and boot the two disposable guests for one acceptance run with the
# unmodified kill-matrix fixture (fixture.py). No CelikPanel is installed here.
#
# usage: prepare-guests.sh TOPOLOGY PRIMARY_NODE|auto RUN_LABEL
#   TOPOLOGY: bind-primary/pdns-secondary | pdns-primary/bind-secondary | bind/bind
set -euo pipefail

topology=${1:?topology}
primary=${2:?primary node or auto}
label=${3:?run label}
REPO=${CELIKPANEL_REPO:-'/mnt/c/CELIKBROS PROJECTS/celikpanel'}
ROOT=${CELIKPANEL_PAIR_ROOT:-/var/tmp/cp-pair-accept/work}
FIXTURE=$REPO/deploy/e2e/dns-kill-matrix/fixture.py
DRIVER=$REPO/deploy/e2e/dns-pair-acceptance/pair_acceptance.py
KEY=/var/tmp/cp-pair-accept/id_ed25519

node_args=()
[[ $primary == auto ]] || node_args=(--primary-node "$primary")
cell=$(python3 "$DRIVER" plan --topology "$topology" "${node_args[@]}" --run-label "$label" \
    --work-root "$ROOT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["cell_id"])')
echo "cell: $cell"

[[ -f $KEY ]] || ssh-keygen -q -t ed25519 -N '' -C cp-pair-accept -f "$KEY"
python3 "$FIXTURE" init-root --work-root "$ROOT" --execute
python3 "$FIXTURE" fetch --work-root "$ROOT" --execute
python3 "$FIXTURE" verify-images --work-root "$ROOT"
python3 "$FIXTURE" prepare --work-root "$ROOT" --cell-id "$cell" --ssh-public-key "$KEY.pub" --execute
python3 "$FIXTURE" start --work-root "$ROOT" --cell-id "$cell" --execute
python3 "$FIXTURE" wait-ssh --work-root "$ROOT" --cell-id "$cell" --identity-file "$KEY" --execute
echo PREPARED
