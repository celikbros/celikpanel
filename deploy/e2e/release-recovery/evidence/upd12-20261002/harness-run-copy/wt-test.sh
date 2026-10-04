#!/bin/bash
# upd12: the harness tests on the working-tree harness files (read-only on the repository; no bytecode written).
export PYTHONDONTWRITEBYTECODE=1
cd '/mnt/c/CELIKBROS PROJECTS/celikpanel'
python3 -m unittest deploy/e2e/release-recovery/test_owner_update_trial.py 2>&1 | tail -n ${TAILN:-15}
