#!/bin/bash
# set2: run the secret scan over the staged evidence (the lab key files are read on this host)
E='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set2-20261011'
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set2
python3 $J/secretscan.py "$E" /var/tmp/cp-release-drill-set2-* /var/tmp/cp-release-drill-rid-* > /tmp/set2-secret-scan.txt 2> /tmp/set2-secret-scan.err; echo "scan rc=$?"
tail -n 3 /tmp/set2-secret-scan.err
cp /tmp/set2-secret-scan.txt "$E/secret-scan.txt"
grep -v "^  /var/tmp" "$E/secret-scan.txt" | cut -c1-260
