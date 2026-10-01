#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
cd /var/tmp/cp-upd4-run/harness
python3 -m unittest deploy/e2e/release-recovery/test_owner_update_trial.py -v > /var/tmp/cp-upd4-run/logs/offline-owner.txt 2>&1
a=$?
python3 -m unittest deploy/e2e/release-recovery/test_recovery_candidate_archive.py -v > /var/tmp/cp-upd4-run/logs/offline-archive.txt 2>&1
b=$?
echo "owner_rc=$a archive_rc=$b"
tail -n 3 /var/tmp/cp-upd4-run/logs/offline-owner.txt
tail -n 3 /var/tmp/cp-upd4-run/logs/offline-archive.txt
