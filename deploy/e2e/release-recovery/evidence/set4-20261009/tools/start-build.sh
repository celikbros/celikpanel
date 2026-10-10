#!/bin/bash
J=<scratchpad>/set4
mkdir -p /var/tmp/cp-set4-run/logs
[ -e /var/tmp/cp-set4-run/progress.txt ] || echo "$(date -u +%FT%TZ) set4 run directory created" > /var/tmp/cp-set4-run/progress.txt
bash $J/mkov.sh b build-upd1-artifacts.sh > /var/tmp/cp-set4-run/logs/mkov-b.txt 2>&1 || { tail -n 5 /var/tmp/cp-set4-run/logs/mkov-b.txt; exit 1; }
tail -n 4 /var/tmp/cp-set4-run/logs/mkov-b.txt
bash $J/bg.sh build $J/job-build.sh
