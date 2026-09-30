#!/bin/bash
S=/var/tmp/cp-upd3-run/stage/upd3-20261001
echo "files containing 185.95.:"; grep -rl "185\.95\." $S | sed "s#$S/##"
echo "installer logs naming celikpanel.net or 185.95.:"
for f in $S/*/run-a/host/current-worker-baseline-*.log; do
  echo "  $(echo $f | sed "s#$S/##"): celikpanel.net=$(grep -c 'celikpanel\.net' $f) 185.95=$(grep -c '185\.95\.' $f)"
done
echo "origin checks recorded by the driver (result.json scope.origin):"
for f in $S/*/run-a/result.json; do python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print('  ',sys.argv[1].split('/')[-3], json.dumps(d['scope']['origin']))" $f; done
