#!/bin/bash
# set4b: the last lines of one job's logs.  usage: tail.sh NAME [N]
L=/var/tmp/cp-set4b-run/logs
n=${2:-6}
tail -n $n $L/$1.out | cut -c1-600
echo "--- err"; tail -n $n $L/$1.err | cut -c1-600
