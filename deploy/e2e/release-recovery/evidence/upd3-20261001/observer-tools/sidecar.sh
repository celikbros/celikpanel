#!/bin/bash
# usage: sidecar.sh JOBNAME LABNAME NODE  -- read-only inspections at step boundaries of a running cell
job=$1; labname=$2; node=$3
L=/var/tmp/cp-upd3-run/logs
out=/var/tmp/cp-upd3-run/side/$job
mkdir -p $out
T=/var/tmp/cp-upd3-run/tools
export PYTHONDONTWRITEBYTECODE=1
done_labels=""
last_light=0
insp() { python3 -P $T/cpinspect.py /var/tmp/cp-release-drill-$labname $node "$@" $out >> $out/sidecar.log 2>&1; }
while [ ! -f $L/$job.end ]; do
  # before-first-site series: every 30 s from license done until setup done (v2; v1 ran to seed and its
  # light body ended on getent's rc 2, so every light probe was lost)
  if grep -q '"step": "license"' $L/$job.out 2>/dev/null && ! grep -q '"step": "setup"' $L/$job.out 2>/dev/null; then
    now=$(date +%s)
    if [ $((now - last_light)) -ge 30 ]; then python3 -P $T/cpinspect.py /var/tmp/cp-release-drill-$labname $node before-site $out light >> $out/sidecar.log 2>&1; last_light=$now; fi
  fi
  for pair in 'setup:after-setup' 'seed:after-seed' 'track:after-track' 'owner-continuation (required):after-continuation' 'terminal:after-terminal'; do
    step=${pair%%:*}; label=${pair#*:}
    case " $done_labels " in *" $label "*) continue;; esac
    if grep -qF "\"step\": \"$step\"" $L/$job.out 2>/dev/null; then
      python3 -P $T/cpinspect.py /var/tmp/cp-release-drill-$labname $node $label $out >> $out/sidecar.log 2>&1
      done_labels="$done_labels $label"
    fi
  done
  sleep 3
done
echo "sidecar end $(date -u +%FT%TZ) labels:$done_labels" >> $out/sidecar.log
