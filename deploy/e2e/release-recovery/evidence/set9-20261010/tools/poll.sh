#!/bin/bash
# set9: one bounded line per job; the progress file's last lines.
L=/var/tmp/cp-set9-run/logs
for f in $L/*.start; do
  [ -e "$f" ] || continue
  n=$(basename $f .start)
  last=$(grep -h '^{"step"' $L/$n.out 2>/dev/null | tail -n 1 | cut -c1-220)
  echo "$n start=$(cut -c12-19 $f) end=$(cut -c12-19 $L/$n.end 2>/dev/null) rc=$(cat $L/$n.rc 2>/dev/null) last=$last"
done
tail -n 5 /var/tmp/cp-set9-run/progress.txt
echo "now=$(date -u +%T) qemu=$(pgrep -c qemu) $(free -m | sed -n 2p | awk '{print "mem_used=" $3 " avail=" $7}') $(df -B1G /var/tmp | tail -1 | awk '{print "wsl_free_GiB=" $4}')"
