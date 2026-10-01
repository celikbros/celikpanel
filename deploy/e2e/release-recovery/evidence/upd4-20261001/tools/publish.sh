#!/bin/bash
# Final: README into the stage, scan again, copy the stage into the repository evidence folder, hash, verify.
set -euo pipefail
R=/var/tmp/cp-upd4-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd4run
S=$R/stage/upd4-20261001
DEST='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd4-20261001'
cp $P/README.md $S/README.md
cp -p $P/views.py $P/publish.sh $P/final-summary.sh $P/scan.sh $P/where.sh $S/tools/
python3 $P/scan4.py $S $S/secret-scan.txt > /dev/null
for f in $S/*/run-*/host/current-worker-baseline-*.log; do printf '%s celikpanel.net=%s 185.95.=%s\n' "${f#$S/}" "$(grep -c 'celikpanel\.net' "$f" || true)" "$(grep -c '185\.95\.' "$f" || true)"; done >> $S/secret-scan.txt
grep -E '^(PEM|key body|fixture|CPK|password|cookie|longest)' $S/secret-scan.txt
# every result.json keeps native_evidence false
python3 - $S <<'PY'
import glob, json, sys
bad = [f for f in glob.glob(sys.argv[1] + "/*/run-*/result.json") if json.load(open(f)).get("native_evidence") is not False]
kinds = [f for f in glob.glob(sys.argv[1] + "/*/run-*/result.json") if (json.load(open(f)).get("kind") or {}).get("judged", {}).get("native_evidence") is not False and json.load(open(f)).get("kind")]
print("result.json files:", len(glob.glob(sys.argv[1] + "/*/run-*/result.json")), "not false:", bad, "kind not false:", kinds)
PY
[ -e "$DEST" ] && { echo "refusing: $DEST exists"; exit 2; }
mkdir -p "$DEST"
cp -a $S/. "$DEST/"
cd "$DEST"
find . -type f ! -path ./SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS
sha256sum -c --quiet SHA256SUMS && echo "SHA256SUMS verified: $(wc -l < SHA256SUMS) files"
find . -type f | awk '{ print length("deploy/e2e/release-recovery/evidence/upd4-20261001/" substr($0,3)) }' | sort -n | tail -1
du -sh "$DEST"
