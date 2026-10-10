#!/bin/bash
L=/var/tmp/cp-set5-run/logs
cat $L/afterbuild.out; echo "--- err"; tail -n 5 $L/afterbuild.err | cut -c1-300
echo "rc=$(cat $L/afterbuild.rc 2>/dev/null) end=$(cat $L/afterbuild.end 2>/dev/null) now=$(date -u +%T)"
tail -n 3 /var/tmp/cp-set5-run/build/a81-prove.stderr.txt 2>/dev/null | cut -c1-300
