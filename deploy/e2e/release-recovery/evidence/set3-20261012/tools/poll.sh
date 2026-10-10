#!/bin/bash
# set3: one bounded line per running or finished job. usage: poll.sh [JOB...]
L=/var/tmp/cp-set3-run/logs
for f in $L/cell-*.start $L/c2-*.start; do
  [ -e "$f" ] || continue
  n=$(basename $f .start)
  [ $# -gt 0 ] && { case " $* " in *" $n "*) ;; *) continue;; esac; }
  last=$(grep -h '^{"step"' $L/$n.out 2>/dev/null | tail -n 1 | cut -c1-150)
  cnt=$(grep -c '^{"step"' $L/$n.out 2>/dev/null)
  echo "$n start=$(cut -c12-19 $f) end=$(cut -c12-19 $L/$n.end 2>/dev/null) rc=$(cat $L/$n.rc 2>/dev/null) steps=$cnt last=$last"
done
echo "now=$(date -u +%T) qemu=$(pgrep -c qemu) $(free -m | sed -n 2p | awk '{print "mem_used=" $3 " avail=" $7}')"
