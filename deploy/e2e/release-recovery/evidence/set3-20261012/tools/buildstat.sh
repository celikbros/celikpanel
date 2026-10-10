#!/bin/bash
L=/var/tmp/cp-set3-run/logs; n=${1:-build}
echo "now=$(date -u +%FT%TZ) start=$(cat $L/$n.start) end=$(cat $L/$n.end 2>/dev/null) rc=$(cat $L/$n.rc 2>/dev/null)"
grep -c BUILD-OK $L/$n.err; grep -n 'BUILD-OK\|UPD1-ARTIFACTS-OK\|rror' $L/$n.err | tail -n 6 | cut -c1-200
tail -n 2 $L/$n.out | cut -c1-200
ls -lt /var/tmp/cp-pair-accept/dist | sed -n 2,4p
pgrep -af 'go build|compile|link|npm|vite|tar ' | cut -c1-120 | head -n 5
