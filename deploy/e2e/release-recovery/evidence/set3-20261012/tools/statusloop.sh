#!/bin/bash
# set3: read-only status of the run, rewritten every 30 s into the scratchpad (status.txt), so that the host side
# can read it without opening a new WSL session. Ends when /var/tmp/cp-set3-run/status.stop exists.
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3
R=/var/tmp/cp-set3-run; L=$R/logs
while [ ! -e $R/status.stop ]; do
  {
    for f in $L/cell-*.start $L/c2-*.start; do
      [ -e "$f" ] || continue
      n=$(basename $f .start)
      last=$(grep -h '^{"step"' $L/$n.out 2>/dev/null | tail -n 1 | cut -c1-330)
      cnt=$(grep -c '^{"step"' $L/$n.out 2>/dev/null)
      echo "$n start=$(cut -c12-19 $f) end=$(cut -c12-19 $L/$n.end 2>/dev/null) rc=$(cat $L/$n.rc 2>/dev/null) steps=$cnt last=$last"
    done
    echo "--- queue"; tail -n 14 $L/queue.log 2>/dev/null
    echo "--- staged"; for f in $L/stage-*.txt; do [ -e "$f" ] && echo "$(basename $f .txt): $(grep -m1 '^overall' $f | cut -c1-120) $(grep -c '^  step\|^  section' $f) not passed"; done
    echo "now=$(date -u +%T) qemu=$(pgrep -c qemu) $(free -m | sed -n 2p | awk '{print "mem_used=" $3 " avail=" $7}') load=$(cut -d' ' -f1-3 /proc/loadavg)"
  } > $R/status.tmp 2>&1
  cp $R/status.tmp $J/status.txt
  sleep 30
done
