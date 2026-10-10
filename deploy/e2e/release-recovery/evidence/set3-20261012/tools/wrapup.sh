#!/bin/bash
# set3: end of the run on the WSL host: stop this run's own background helpers (status writer; the idle session when
# "hold" is given), record what is left, and show that nothing of the run still executes. Read-only otherwise.
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3
E='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set3-20261012'
R=/var/tmp/cp-set3-run
touch $R/status.stop
[ "${1:-}" = hold ] && touch $R/hold.stop
sleep 3
cp $R/logs/queue.log "$E/harness-run-copy/queue.log"
cp $R/c-drive-cells.txt "$E/host/c-drive-cells.txt"
bash $J/leftovers.sh > "$E/host/host-leftovers.txt" 2>&1
tail -n 9 "$E/host/host-leftovers.txt" | cut -c1-200
echo "removals listed: $(wc -l < "$E/host/removals.txt")"
ls /var/tmp/cp-release-drill-set3-*/cells/*/*/overlay.qcow2 /var/tmp/cp-release-drill-rid3-*/cells/*/*/overlay.qcow2 /var/tmp/cp-release-drill-u14-*/cells/*/*/overlay.qcow2 2>/dev/null | wc -l
