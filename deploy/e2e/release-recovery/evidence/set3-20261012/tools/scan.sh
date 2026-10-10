#!/bin/bash
# set3: run the secret scan over the staged evidence (the lab key files are read on this host)
E='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set3-20261012'
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3
python3 $J/secretscan.py "$E" /var/tmp/cp-release-drill-set3-* /var/tmp/cp-release-drill-rid3-* /var/tmp/cp-release-drill-u14-* > /tmp/set3-secret-scan.txt 2> /tmp/set3-secret-scan.err; echo "scan rc=$?"
tail -n 3 /tmp/set3-secret-scan.err
grep -v "^  /var/tmp" /tmp/set3-secret-scan.txt | cut -c1-260
python3 $J/summary.py "$E" | tail -n 1
