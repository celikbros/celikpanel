#!/bin/bash
# set3: regenerate the generated files, run the secret scan, write the root SHA256SUMS and verify it.
set -uo pipefail
E='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set3-20261012'
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3
for f in $J/*.sh $J/*.py $J/*.ps1 $J/files.txt; do cp "$f" "$E/tools/"; done
cp $J/README.*.md $J/readme_values.json $J/scan_text.md $J/oc_text.md $J/part2_other.md "$E/tools/"
cp $J/keepawake.log $J/sleep-events.txt $J/c-drive.txt $J/c-drive-watch.txt "$E/host/"
python3 $J/summary.py "$E" | tail -n 1
rm -f "$E/SHA256SUMS" "$E/secret-scan.txt"
python3 $J/secretscan.py "$E" /var/tmp/cp-release-drill-set3-* /var/tmp/cp-release-drill-rid3-* /var/tmp/cp-release-drill-u14-* > /tmp/set3-secret-scan.txt 2> /tmp/set3-secret-scan.err
echo "scan rc=$?"; tail -n 2 /tmp/set3-secret-scan.err
cp /tmp/set3-secret-scan.txt "$E/secret-scan.txt"
grep -v "^  /var/tmp" "$E/secret-scan.txt" | cut -c1-220
( cd "$E" && find . -type f ! -path ./SHA256SUMS -print0 | sort -z | xargs -0 sha256sum | sed 's#  \./#  #' > SHA256SUMS && sha256sum -c --quiet SHA256SUMS && echo "root SHA256SUMS verified: $(wc -l < SHA256SUMS) files" )
( cd '/mnt/c/CELIKBROS PROJECTS/celikpanel' && find deploy/e2e/release-recovery/evidence/set3-20261012 -type f | awk '{ if (length($0) > m) { m = length($0); p = $0 } } END { print "longest repository-relative path:", m; print p }' )
echo "result.json files with native_evidence false: $(grep -rl '"native_evidence": false' "$E" --include=result.json | wc -l) of $(find "$E" -name result.json | wc -l)"
du -sh "$E" | cut -f1
