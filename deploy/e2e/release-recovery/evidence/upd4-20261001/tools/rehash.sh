#!/bin/bash
# Replace the README in the stage and the evidence folder, rescan, rehash, verify.
set -euo pipefail
R=/var/tmp/cp-upd4-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd4run
S=$R/stage/upd4-20261001
DEST='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd4-20261001'
cp $P/README.md $S/README.md
cp $P/README.md "$DEST/README.md"
cp -p $P/rehash.sh "$DEST/tools/rehash.sh"; cp -p $P/rehash.sh $S/tools/rehash.sh
python3 $P/scan4.py "$DEST" "$DEST/secret-scan.txt" > /dev/null
for f in "$DEST"/*/run-*/host/current-worker-baseline-*.log; do printf '%s celikpanel.net=%s 185.95.=%s\n' "${f#$DEST/}" "$(grep -c 'celikpanel\.net' "$f" || true)" "$(grep -c '185\.95\.' "$f" || true)"; done >> "$DEST/secret-scan.txt"
grep -E '^(PEM|key body|fixture|CPK|43-char|32-char|password|cookie|\[REDACTED|185|longest)' "$DEST/secret-scan.txt"
cd "$DEST"
rm -f SHA256SUMS
find . -type f ! -path ./SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS
sha256sum -c --quiet SHA256SUMS && echo "SHA256SUMS verified: $(wc -l < SHA256SUMS) files"
sha256sum SHA256SUMS README.md
