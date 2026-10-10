#!/bin/bash
# set4: run the secret scan over the staged evidence (the lab key files are read on this host)
E='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set4-20261009'
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
python3 $J/secretscan.py "$E" /var/tmp/cp-release-drill-s4-* > /tmp/set4-secret-scan.txt 2> /tmp/set4-secret-scan.err; echo "scan rc=$?"
tail -n 3 /tmp/set4-secret-scan.err
grep -v "^  /var/tmp" /tmp/set4-secret-scan.txt | cut -c1-260
