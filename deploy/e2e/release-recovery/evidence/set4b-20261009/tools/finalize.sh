#!/bin/bash
# set4b: stage the common files, regenerate the generated ones, record what is left on the host, run the secret scan,
# write the root SHA256SUMS last and verify it. The README is written by hand in the scratch folder and copied in.
set -uo pipefail
E='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set4b-20261009'
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
bash $J/stagecommon.sh | tail -n 1
bash $J/leftovers.sh > "$E/host/host-leftovers.txt" 2>&1
[ -f $J/README.md ] && sed 's/\r$//' $J/README.md > "$E/README.md"
python3 $J/summary.py "$E" | tail -n 1
rm -f "$E/SHA256SUMS" "$E/secret-scan.txt"
python3 $J/secretscan.py "$E" /var/tmp/cp-release-drill-s4b-* > /var/tmp/cp-set4b-run/secret-scan.txt 2> /var/tmp/cp-set4b-run/secret-scan.err
echo "scan rc=$?"; tail -n 2 /var/tmp/cp-set4b-run/secret-scan.err
cp /var/tmp/cp-set4b-run/secret-scan.txt "$E/secret-scan.txt"
grep -v "^  /var/tmp" "$E/secret-scan.txt" | cut -c1-220
# a path below a Windows user profile, in either spelling (the scratch folder's own path is replaced in tools/)
PROFILE='/Users/[A-Za-z0-9._-]+/AppData|Users\\[A-Za-z0-9._-]+\\AppData|@gmail\.com'
grep -rIl -E "$PROFILE" "$E" | head -n 5; echo "files naming a local user profile or a mail address: $(grep -rIl -E "$PROFILE" "$E" | wc -l)"
( cd "$E" && find . -type f ! -path ./SHA256SUMS -print0 | sort -z | xargs -0 sha256sum | sed 's#  \./#  #' > SHA256SUMS && sha256sum -c --quiet SHA256SUMS && echo "root SHA256SUMS verified: $(wc -l < SHA256SUMS) files" )
echo "result.json files with native_evidence false: $(grep -rl '"native_evidence": false' "$E" --include=result.json | wc -l) of $(find "$E" -name result.json | wc -l)"
du -sh "$E" | cut -f1
