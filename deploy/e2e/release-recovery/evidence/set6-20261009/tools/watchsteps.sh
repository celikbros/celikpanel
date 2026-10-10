#!/bin/bash
# set6 read-only: print each new step line of the running queue's jobs as it appears; ends when no queue worker runs.
L=/var/tmp/cp-set6-run/logs
declare -A seen
while true; do
  for f in $L/cell-*.out; do
    [ -f "$f" ] || continue
    n=$(grep -c '^{"step"' "$f"); p=${seen[$f]:-0}
    if [ "$n" -gt "$p" ]; then
      grep '^{"step"' "$f" | sed -n "$((p + 1)),${n}p" | cut -c1-420 | sed "s#^#$(basename $f .out) $(date -u +%T) #"
      seen[$f]=$n
    fi
  done
  if ! pgrep -f "set6/queue.sh" > /dev/null; then
    tail -n 3 $L/queue.log | cut -c1-300
    echo "QUEUE-ENDED $(date -u +%T)"
    exit 0
  fi
  sleep 5
done
