#!/bin/bash
# set1: secret scan over the staged evidence folder (host only).
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set1
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set1-20261010"
python3 $P/scan.py "$S" "$S/secret-scan.txt"
echo "--- installer logs naming the real origin" | tee -a "$S/secret-scan.txt"
for f in "$S"/*/run-*/host/current-worker-baseline-*.log; do printf '%s celikpanel.net=%s 185.95.=%s\n' "${f#$S/}" "$(grep -c 'celikpanel\.net' "$f")" "$(grep -c '185\.95\.' "$f")"; done | tee -a "$S/secret-scan.txt"
echo "--- files containing 185.95." | tee -a "$S/secret-scan.txt"
grep -rl '185\.95\.' "$S" | sed "s#^$S/##" | tee -a "$S/secret-scan.txt"
echo "--- database, mailbox and site-account password fields in recorded API answers (key: distinct values)" | tee -a "$S/secret-scan.txt"
grep -rhoE '"[A-Za-z_]*([Pp]assword|[Ss]ecret)[A-Za-z_]*": *"[^"]*"' "$S" --include=*.json | sed -E 's/: *"(\[REDACTED[^"]*|)"$/: "\1"/' | sort | uniq -c | sort -rn | head -20 | tee -a "$S/secret-scan.txt"
