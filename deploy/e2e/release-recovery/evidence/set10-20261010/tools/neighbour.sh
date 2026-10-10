#!/bin/bash
# set10: before a guest start, no other measurement may be running on this host: no qemu-system process and no live
# job of /var/tmp/cp-set9-run. Polls every 60 s (each poll logged) for up to 8 hours. usage: neighbour.sh LABEL
R=/var/tmp/cp-set10-run
label=${1:-guest start}
for i in $(seq 1 480); do
  q=$(pgrep -fc qemu-system || true)
  j=$(pgrep -fc '/var/tmp/cp-set9-run/(job-|harness-)' || true)
  echo "$(date -u +%FT%TZ) neighbour gate for $label poll=$i qemu_processes=$q set9_jobs=$j" >> $R/neighbour-gate.txt
  if [ "$q" = 0 ] && [ "$j" = 0 ]; then echo "$(date -u +%FT%TZ) neighbour gate for $label: clear" >> $R/neighbour-gate.txt; exit 0; fi
  sleep 60
done
echo "$(date -u +%FT%TZ) neighbour gate for $label: still busy after 8 hours; no guest started" >> $R/neighbour-gate.txt
exit 1
