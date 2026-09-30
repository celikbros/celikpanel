#!/bin/bash
# SHA256SUMS over every file except the top-level SHA256SUMS itself (includes each run's own SHA256SUMS)
set -u
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd3run
S=/var/tmp/cp-upd3-run/stage/upd3-20261001
DEST='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd3-20261001'
cp -p $P/rehash.sh $P/extra.sh "$S/observer-tools/queries/"
cp -p $P/rehash.sh $P/extra.sh "$DEST/observer-tools/queries/"
cd $S && rm -f SHA256SUMS && find . -type f ! -path ./SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS
sha256sum -c --quiet SHA256SUMS && echo "stage ok $(wc -l < SHA256SUMS)"
cp -p $S/SHA256SUMS "$DEST/SHA256SUMS"
cd "$DEST" && sha256sum -c --quiet SHA256SUMS && echo "repo ok $(wc -l < SHA256SUMS) of $(find . -type f ! -path ./SHA256SUMS | wc -l) files"
diff -rq $S "$DEST" && echo "stage and repo copy identical"
