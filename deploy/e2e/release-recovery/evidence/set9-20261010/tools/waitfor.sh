#!/bin/bash
# set9: wait (at most $2 seconds, default 1800) until the file $1 exists; print the poll lines.
f=$1; max=${2:-1800}; n=0
while [ ! -e "$f" ] && [ $n -lt $max ]; do sleep 10; n=$((n+10)); done
[ -e "$f" ] && echo "present: $f after ~${n}s" || echo "absent after ${max}s: $f"
bash <scratchpad>/set9/tools/poll.sh
