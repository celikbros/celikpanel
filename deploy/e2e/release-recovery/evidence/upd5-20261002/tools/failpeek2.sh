#!/bin/bash
# usage: failpeek2.sh LAB -> read-only: the collect step of a stopped cell (journals around the stop)
L=/var/tmp/cp-release-drill-$1
ev=$(ls -d $L/evidence/*/upd1/*/ | tail -1)
c=$(ls -d $ev/steps/*-collect)
ls -la $c
python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print(json.dumps(d.get('checks'),indent=1)[:3000])" $c/step.json
for j in $c/journal-*.txt; do echo "== $j $(wc -l < $j)"; done
echo "== lab journal: pacman/systemd/installer lines"
grep -nEi 'pacman|upgrad|systemd\[1\]: (Reexec|Reload)|daemon-reexec|installing|warning|error|baseline' $c/journal-lab.txt | cut -c1-300 | tail -n 60
echo "== product journal tail"; tail -n 15 $c/journal-product.txt | cut -c1-300
ls $L/cells/*/ 
