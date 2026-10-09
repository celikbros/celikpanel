#!/bin/bash
# set5: run the secret scan over the staged evidence (the lab key files and raw records are read on this host)
R=/var/tmp/cp-set5-run
E="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/$(cat $R/evidence-name.txt)"
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
SET5_HARNESS=$R/harness-${1:-d}/deploy/e2e/release-recovery python3 -I -B $J/secretscan.py "$E" /var/tmp/cp-release-drill-s5-* > $R/secret-scan-try.txt 2> $R/secret-scan-try.err; echo "scan rc=$?"
tail -n 3 $R/secret-scan-try.err
grep -v "^  /var/tmp" $R/secret-scan-try.txt | cut -c1-260
