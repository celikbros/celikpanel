#!/bin/bash
grep -E '^(ERROR|FAIL):|Error|No such file' /var/tmp/cp-set5-run/logs/offline-a-test_owner_update_trial.txt | cut -c1-300 | head -20
