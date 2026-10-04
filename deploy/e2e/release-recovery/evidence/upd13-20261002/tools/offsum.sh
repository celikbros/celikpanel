#!/bin/bash
cat /var/tmp/cp-upd13-run/logs/offline-p.out
for f in /var/tmp/cp-upd13-run/logs/offline-p-*.txt; do echo "$(basename $f): $(grep -E '^Ran ' $f)"; done
