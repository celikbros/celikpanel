#!/bin/bash
# usage: peek.sh SHORT -> step lines so far and the last error lines
L=/var/tmp/cp-upd4-run/logs
n=cell-$1
date -u +%FT%TZ
echo "start=$(cat $L/$n.start 2>/dev/null) end=$(cat $L/$n.end 2>/dev/null) rc=$(cat $L/$n.rc 2>/dev/null)"
grep -E '^\{"step"|^UPD1' $L/$n.out | cut -c1-700
echo "--- err tail"; tail -n ${TAILN:-4} $L/$n.err | cut -c1-300
