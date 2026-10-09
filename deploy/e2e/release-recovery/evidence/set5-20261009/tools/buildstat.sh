#!/bin/bash
L=/var/tmp/cp-set5-run/logs
cat $L/build.out 2>/dev/null | tail -5
echo "--- err tail"; tail -n 6 $L/build-a81.err 2>/dev/null | cut -c1-240
echo "--- out"; cat $L/build-a81.out 2>/dev/null | tail -3
echo "rc=$(cat $L/build.rc 2>/dev/null) end=$(cat $L/build.end 2>/dev/null) now=$(date -u +%T)"
ls /var/tmp/cp-upd1-build/ | tail -2
grep -c . $L/build-a81.err
grep -n 'BUILD-UPD1' $L/build-a81.err | cut -c1-200
