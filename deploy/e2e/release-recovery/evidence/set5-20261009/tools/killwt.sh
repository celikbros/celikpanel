#!/bin/bash
# set5: stop this run's own read-only working-tree check (tools/wtcheck.sh as first written), which was too slow over drvfs
pkill -f 'wtcheck.sh' ; pkill -f 'diff --stat HEAD -- cmd internal web docs ROADMAP.md ROADMAP.tr.md'
sleep 1; pgrep -af 'wtcheck|diff --stat HEAD' | grep -v pgrep | wc -l
ls /mnt/c/CELIKBROS\ PROJECTS/celikpanel/.git/index.lock 2>/dev/null || echo "no index.lock"
