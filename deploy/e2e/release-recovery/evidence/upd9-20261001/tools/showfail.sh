#!/bin/bash
grep -E "^(ERROR|FAIL|[A-Za-z]*Error)" -A0 /var/tmp/cp-upd9-run/logs/offline-${1}-test_owner_update_trial.txt | head -30
grep -B2 -A12 "^ERROR" /var/tmp/cp-upd9-run/logs/offline-${1}-test_owner_update_trial.txt | head -120
