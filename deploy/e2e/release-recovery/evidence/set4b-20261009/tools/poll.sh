#!/bin/bash
# set4b: one bounded line per running or finished job.
L=/var/tmp/cp-set4b-run/logs
for f in $L/*.start; do
  [ -e "$f" ] || continue
  n=$(basename $f .start)
  last=$(grep -h '^{"step"' $L/$n.out 2>/dev/null | tail -n 1 | cut -c1-300)
  cnt=$(grep -c '^{"step"' $L/$n.out 2>/dev/null)
  echo "$n start=$(cut -c12-19 $f) end=$(cut -c12-19 $L/$n.end 2>/dev/null) rc=$(cat $L/$n.rc 2>/dev/null) steps=$cnt last=$last"
done
tail -n 4 /var/tmp/cp-set4b-run/progress.txt
echo "now=$(date -u +%T) qemu=$(pgrep -c qemu) $(free -m | sed -n 2p | awk '{print "mem_used=" $3 " avail=" $7}')"
