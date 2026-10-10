#!/bin/bash
# set4b: wait (at most SECONDS) until job NAME has more than COUNT step lines or has ended.  usage: waitsteps.sh NAME COUNT SECONDS
L=/var/tmp/cp-set4b-run/logs
end=$(( $(date +%s) + $3 ))
while [ $(date +%s) -lt $end ]; do
  c=$(grep -c '^{"step"' $L/$1.out 2>/dev/null)
  [ "${c:-0}" -gt "$2" ] && break
  [ -e $L/$1.end ] && break
  sleep 5
done
echo "steps=$(grep -c '^{"step"' $L/$1.out 2>/dev/null) end=$(cat $L/$1.end 2>/dev/null) rc=$(cat $L/$1.rc 2>/dev/null) now=$(date -u +%T)"
grep -h '^{"step"' $L/$1.out | tail -n 3 | cut -c1-400
