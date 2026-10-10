#!/bin/bash
# set5: stage what is common, regenerate the generated files, record what is left on the host, run the secret scan,
# write the root SHA256SUMS and verify it. The README is written by hand and copied in from the scratch folder
# (README.md there). usage: finalize.sh COPY      (COPY: the run copy whose set5_redact.py the scan imports)
set -uo pipefail
R=/var/tmp/cp-set5-run
NAME=$(cat $R/evidence-name.txt)
E="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/$NAME"
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
copy=$1
bash $J/stagecommon.sh | tail -n 1
bash $J/leftovers.sh > "$E/host/host-leftovers.txt" 2>&1
python3 -I $J/summary.py "$E" | tail -n 14
bash $J/searchnames.sh "$E" | tail -n 8
# the README's text is written by hand (README.md, the template, and parts/NAME.md in the scratch folder);
# mkreadme.py puts it together with the tables generated from facts.json
python3 -I $J/mkreadme.py "$E" $J/README.md $J/parts $R/README.out.md || { echo "README NOT WRITTEN"; exit 1; }
cp $R/README.out.md "$E/README.md"
[ -f "$E/SHA256SUMS" ] && mv "$E/SHA256SUMS" $R/sha256sums.previous
[ -f "$E/secret-scan.txt" ] && mv "$E/secret-scan.txt" $R/secret-scan.previous
SET5_HARNESS=$R/harness-$copy/deploy/e2e/release-recovery python3 -I -B $J/secretscan.py "$E" /var/tmp/cp-release-drill-s5-* > $R/secret-scan.txt 2> $R/secret-scan.err
echo "scan rc=$?"; tail -n 2 $R/secret-scan.err
cp $R/secret-scan.txt "$E/secret-scan.txt"
grep -v "^  /var/tmp" "$E/secret-scan.txt" | cut -c1-240
# every text file of the folder has LF line ends
crlf=$(grep -rlI $'\r' "$E" 2>/dev/null | wc -l); echo "files with a carriage return: $crlf"
( cd "$E" && find . -type f ! -path ./SHA256SUMS -print0 | sort -z | xargs -0 sha256sum | sed 's#  \./#  #' > SHA256SUMS && sha256sum -c --quiet SHA256SUMS && echo "root SHA256SUMS verified: $(wc -l < SHA256SUMS) files" )
( cd '/mnt/c/CELIKBROS PROJECTS/celikpanel' && find "deploy/e2e/release-recovery/evidence/$NAME" -type f | awk '{ if (length($0) > m) { m = length($0); p = $0 } } END { print "longest repository-relative path:", m; print p }' )
echo "result.json files with native_evidence false: $(grep -rl '"native_evidence": false' "$E" --include=result.json | wc -l) of $(find "$E" -name result.json | wc -l)"
du -sh "$E" | cut -f1
