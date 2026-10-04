#!/bin/bash
L=/var/tmp/cp-upd12-run/logs
names=("$@")
[ ${#names[@]} -eq 0 ] && names=($(ls $L/*.start 2>/dev/null | xargs -n1 basename | sed 's/\.start$//'))
date -u +%FT%TZ
for n in "${names[@]}"; do
  echo "=== $n start=$(cat $L/$n.start 2>/dev/null) end=$(cat $L/$n.end 2>/dev/null) rc=$(cat $L/$n.rc 2>/dev/null)"
  echo "--- out (tail)"; tail -n ${TAILN:-6} $L/$n.out 2>/dev/null | cut -c1-400
  echo "--- err (tail)"; tail -n ${TAILN:-6} $L/$n.err 2>/dev/null | cut -c1-400
done
pgrep -a qemu | cut -c1-120 || echo "no qemu"
