#!/bin/bash
R=/var/tmp/cp-upd13-run
echo /var/tmp/cp-upd1-build/20261002t203047z/upd1-artifacts.json > $R/art-cur.txt
a=$(grep -h 'upd1-artifacts.json' $R/logs/build-a80.out | tail -1)
[ -n "$a" ] && [ "$a" != "$(cat $R/art-cur.txt)" ] && echo "$a" > $R/art-a80.txt
cat $R/art-*.txt
