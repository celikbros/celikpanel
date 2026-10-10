#!/bin/bash
# usage: prep-a.sh NAME : make run copy NAME, record the artifacts path, dry-run the four cells, start the offline suites
set -u
name=$1
R=/var/tmp/cp-set2-run
ART=/var/tmp/cp-upd1-build/20261009t030455z/upd1-artifacts.json
echo $ART > $R/art-cur.txt
bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set2/mkset2.sh $name > $R/logs/mkov-$name.txt 2>&1; echo "mkov rc=$?"; tail -n 14 $R/logs/mkov-$name.txt
export PYTHONDONTWRITEBYTECODE=1
for cell in set2-debian13 set2-ubuntu rid-debian13 rid-ubuntu; do
  bash $R/harness-$name/deploy/e2e/release-recovery/run-set2.sh dry-run $cell $ART dry-$cell > $R/build/set2-dry-$name-$cell.json 2> $R/build/set2-dry-$name-$cell.err; echo "dry-run $cell rc=$? native_evidence=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))['native_evidence'])" $R/build/set2-dry-$name-$cell.json 2>&1 | tail -n 1)"
done
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null || echo "no lab was created by the dry runs"
printf '#!/bin/bash\nHARNESS_DIR=harness-%s exec bash %s/job-offline.sh %s\n' $name /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set2 $name > $R/job-offline-$name.sh
bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set2/bg.sh offline-$name $R/job-offline-$name.sh
