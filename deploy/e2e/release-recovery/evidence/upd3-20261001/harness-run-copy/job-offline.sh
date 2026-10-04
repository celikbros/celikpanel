#!/bin/bash
cd /var/tmp/cp-upd3-run/harness
export PYTHONDONTWRITEBYTECODE=1
python3 -m unittest deploy/e2e/release-recovery/test_owner_update_trial.py -v > /var/tmp/cp-upd3-run/logs/offline-owner.txt 2>&1; echo "owner rc=$?"
python3 -m unittest deploy/e2e/release-recovery/test_recovery_candidate_archive.py -v > /var/tmp/cp-upd3-run/logs/offline-archive.txt 2>&1; echo "archive rc=$?"
tail -4 /var/tmp/cp-upd3-run/logs/offline-owner.txt /var/tmp/cp-upd3-run/logs/offline-archive.txt
