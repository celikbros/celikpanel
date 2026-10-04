#!/bin/bash
# usage: jwin.sh LAB FILE FROM TO   (read-only time window of a collected journal)
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/ | tail -1)
f=$(ls $ev/steps/*collect/$2 | tail -1)
awk -v a="$3" -v b="$4" '{t=substr($1,12,8)} t>=a && t<=b' "$f" | cut -c1-300 | grep -v 'milter chain: nothing'
