#!/bin/bash
# usage: sidecar.sh JOBNAME LABNAME NODE  -- read-only inspections at step boundaries of a running cell
job=$1; labname=$2; node=$3
L=/var/tmp/cp-upd2-run/logs
out=/var/tmp/cp-upd2-run/side/$job
mkdir -p $out
S=$(dirname "$0")
done_labels=""
while [ ! -f $L/$job.end ]; do
  for pair in 'seed:after-seed' 'pre-state:pre-update' 'track:after-track'; do
    step=${pair%%:*}; label=${pair#*:}
    case " $done_labels " in *" $label "*) continue;; esac
    if grep -q "\"step\": \"$step\"" $L/$job.out 2>/dev/null; then
      PYTHONDONTWRITEBYTECODE=1 python3 -P "$S/tools/cpinspect.py" /var/tmp/cp-release-drill-$labname $node $label $out >> $out/sidecar.log 2>&1
      done_labels="$done_labels $label"
    fi
  done
  sleep 3
done
echo "sidecar end $(date -u +%FT%TZ) labels:$done_labels" >> $out/sidecar.log
