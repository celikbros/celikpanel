#!/bin/bash
# As upd6 tools/scan.sh, over the upd7 repo folder.
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd7
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd7-20261001"
python3 $P/scan7.py "$S" "$S/secret-scan.txt"
echo "--- installer logs naming the real origin" | tee -a "$S/secret-scan.txt"
for f in "$S"/*/run-*/host/current-worker-baseline-*.log; do printf '%s celikpanel.net=%s 185.95.=%s\n' "${f#$S/}" "$(grep -c 'celikpanel\.net' "$f")" "$(grep -c '185\.95\.' "$f")"; done | tee -a "$S/secret-scan.txt"
echo "--- files containing 185.95." | tee -a "$S/secret-scan.txt"
grep -rl '185\.95\.' "$S" | sed "s#^$S/##" | tee -a "$S/secret-scan.txt"
