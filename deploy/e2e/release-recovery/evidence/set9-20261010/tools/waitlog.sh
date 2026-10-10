#!/bin/bash
# set9: wait (at most $2 s) until runner.log has a line containing "rc=" after the given start count $1
L=<scratchpad>/set9/browser/runner.log
n=0; while [ "$(grep -c ' rc=' $L)" -lt "$1" ] && [ $n -lt "$2" ]; do sleep 5; n=$((n+5)); done
tail -n 3 $L
