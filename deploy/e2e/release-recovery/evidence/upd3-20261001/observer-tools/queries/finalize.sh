#!/bin/bash
set -u
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd3run
S=/var/tmp/cp-upd3-run/stage/upd3-20261001
DEST='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd3-20261001'
[ -e "$DEST" ] && { echo "refusing: $DEST exists"; exit 2; }
tr -d '\r' < $P/README.md > $S/README.md
cp -p $P/scan.py $P/finalize.sh $S/observer-tools/queries/
python3 -B $P/scan.py $S $S/secret-scan.txt > /dev/null
grep -E '^(PEM|key body|fixture|CPK|43-char|32-char|password|cookie|\[REDACTED)' $S/secret-scan.txt
cd $S && rm -f SHA256SUMS && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS
sha256sum -c --quiet SHA256SUMS && echo "stage SHA256SUMS ok ($(wc -l < SHA256SUMS) files)"
mkdir -p "$DEST"
cp -r $S/. "$DEST/"
cd "$DEST" && sha256sum -c --quiet SHA256SUMS && echo "repo copy SHA256SUMS ok ($(wc -l < SHA256SUMS) files)"
find "$DEST" -type f | wc -l
pgrep -a qemu || echo "no qemu running"
git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' status --short | head
git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse HEAD
date -u +%FT%TZ
