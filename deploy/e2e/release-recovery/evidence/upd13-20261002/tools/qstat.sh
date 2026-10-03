#!/bin/bash
# read-only: queue log and the current cell's latest steps
L=/var/tmp/cp-upd13-run/logs
date -u +%FT%TZ
tail -n ${QN:-6} $L/queue-main.out | cut -c1-300
tail -n 3 $L/queue-main.err | cut -c1-200
cur=$(ls -t $L/cell-*.start 2>/dev/null | head -1)
[ -n "$cur" ] || exit 0
s=$(basename $cur .start); short=${s#cell-}
lab=$(awk -v s=$short '$1==s{print $4}' /var/tmp/cp-upd13-run/jobs/queue-main.list)
echo "current $short $lab end=$(cat $L/$s.end 2>/dev/null)"
ev=$(ls -d /var/tmp/cp-release-drill-$lab/evidence/*/upd1/*/ 2>/dev/null | tail -1)
[ -n "$ev" ] && for f in $(ls $ev/steps/*/step.json 2>/dev/null | tail -n ${SN:-4}); do python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print(d.get('name'),d.get('verdict'),(d.get('finished_at') or '')[11:19],(d.get('reason') or '')[:200])" $f; done
[ -n "$ev" ] && ls $ev/steps | tail -1
