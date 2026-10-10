#!/bin/bash
# set4: regenerate the generated files, record what is left on the host, run the secret scan, write the root
# SHA256SUMS and verify it. The README is written by hand and copied in from the scratchpad (README.md there).
set -uo pipefail
E='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set4-20261009'
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
bash $J/stagecommon.sh | tail -n 1
bash $J/leftovers.sh > "$E/host/host-leftovers.txt" 2>&1
[ -f $J/README.md ] && sed 's/\r$//' $J/README.md > "$E/README.md"
python3 $J/summary.py "$E" | tail -n 1
python3 $J/mkvalues.py "$E" > /dev/null
rm -f "$E/SHA256SUMS" "$E/secret-scan.txt"
python3 $J/secretscan.py "$E" /var/tmp/cp-release-drill-s4-* > /tmp/set4-secret-scan.txt 2> /tmp/set4-secret-scan.err
echo "scan rc=$?"; tail -n 2 /tmp/set4-secret-scan.err
cp /tmp/set4-secret-scan.txt "$E/secret-scan.txt"
grep -v "^  /var/tmp" "$E/secret-scan.txt" | cut -c1-220
( cd "$E" && find . -type f ! -path ./SHA256SUMS -print0 | sort -z | xargs -0 sha256sum | sed 's#  \./#  #' > SHA256SUMS && sha256sum -c --quiet SHA256SUMS && echo "root SHA256SUMS verified: $(wc -l < SHA256SUMS) files" )
( cd '/mnt/c/CELIKBROS PROJECTS/celikpanel' && find deploy/e2e/release-recovery/evidence/set4-20261009 -type f | awk '{ if (length($0) > m) { m = length($0); p = $0 } } END { print "longest repository-relative path:", m; print p }' )
echo "result.json files with native_evidence false: $(grep -rl '"native_evidence": false' "$E" --include=result.json | wc -l) of $(find "$E" -name result.json | wc -l)"
du -sh "$E" | cut -f1
