#!/bin/bash
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd3run
T=/var/tmp/cp-upd3-run/tools
mkdir -p $T/v1
cp -p $T/cpinspect.py $T/sidecar.sh $T/start-cell.sh $T/v1/ 2>/dev/null
for f in cpinspect.py sidecar.sh start-cell.sh ext.py wait.sh trk.sh last.sh gq.py; do tr -d '\r' < $P/$f > $T/$f.new && chmod 0755 $T/$f.new && mv -f $T/$f.new $T/$f; done
diff $T/v1/sidecar.sh $T/sidecar.sh; diff $T/v1/cpinspect.py $T/cpinspect.py; ls $T
