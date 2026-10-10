#!/bin/bash
# set6 read-only: where the build job is
R=/var/tmp/cp-set6-run; L=$R/logs
date -u +%FT%TZ
cat $R/progress.txt | tail -n 6
for x in start end rc; do [ -f $L/build.$x ] && echo "build.$x: $(cat $L/build.$x)"; done
cat $L/build.out 2>/dev/null | tail -n 6
for b in cur a81; do [ -f $L/build-$b.err ] && { echo "--- build-$b.err (last lines)"; tr '\r' '\n' < $L/build-$b.err | grep -v '^$' | tail -n 4 | cut -c1-220; }; done
ls /var/tmp/cp-pair-accept/dist/ | wc -l
pgrep -af 'build-upd1|build-dist|go build|npm|vite' | grep -v pgrep | cut -c1-120 | head -n 5
