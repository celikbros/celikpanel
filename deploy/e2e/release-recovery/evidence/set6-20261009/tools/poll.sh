#!/bin/bash
# set6: one bounded line per running or finished job; the queue's last lines.
L=/var/tmp/cp-set6-run/logs
for f in $L/*.start; do
  [ -e "$f" ] || continue
  n=$(basename $f .start)
  last=$(grep -h '^{"step"' $L/$n.out 2>/dev/null | tail -n 1 | cut -c1-200)
  cnt=$(grep -c '^{"step"' $L/$n.out 2>/dev/null)
  bad=$(grep -h '^{"step"' $L/$n.out 2>/dev/null | grep -v -c -E '"verdict": "(passed|observed|skipped)"')
  echo "$n start=$(cut -c12-19 $f) end=$(cut -c12-19 $L/$n.end 2>/dev/null) rc=$(cat $L/$n.rc 2>/dev/null) steps=$cnt notpassed=$bad last=$last"
done
tail -n 4 $L/queue.log 2>/dev/null
tail -n 2 /var/tmp/cp-set6-run/c-drive-cells.txt 2>/dev/null
echo "now=$(date -u +%T) qemu=$(pgrep -c qemu) $(free -m | sed -n 2p | awk '{print "mem_used=" $3 " avail=" $7}') $(df -B1G /var/tmp | tail -1 | awk '{print "wsl_free_GiB=" $4}')"
