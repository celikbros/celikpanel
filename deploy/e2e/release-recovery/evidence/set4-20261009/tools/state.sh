#!/bin/bash
cat /var/tmp/cp-set4-run/progress.txt 2>/dev/null | tail -40
echo ---procs
pgrep -af "qemu-system|set4|run-set4|owner_update_trial" | cut -c1-180 | head -20
echo ---ls
ls -la /var/tmp/cp-set4-run | head -40
date -u +"%F %T UTC"
