#!/bin/bash
# Read-only: job state and the latest step records of the running cell.
L=/var/tmp/cp-release-drill-upd6-arch-oc-a
date -u +%FT%TZ
echo "start=$(cat /var/tmp/cp-upd6-run/logs/cell-archoc.start) end=$(cat /var/tmp/cp-upd6-run/logs/cell-archoc.end 2>/dev/null) rc=$(cat /var/tmp/cp-upd6-run/logs/cell-archoc.rc 2>/dev/null)"
tail -n 4 /var/tmp/cp-upd6-run/logs/cell-archoc.err | cut -c1-250
ev=$(ls -d $L/evidence/*/upd1/*/ 2>/dev/null | tail -1)
[ -n "$ev" ] && ls $ev/steps 2>/dev/null | tr '\n' ' '; echo
for f in $(ls $ev/steps/*/step.json 2>/dev/null); do python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print(d.get('name'),d.get('verdict'),(d.get('reason') or '')[:200])" $f; done
pgrep -c qemu-system >/dev/null && echo "qemu running: $(pgrep -c qemu-system)" || echo "no qemu"
df -BG /var/tmp | tail -1
