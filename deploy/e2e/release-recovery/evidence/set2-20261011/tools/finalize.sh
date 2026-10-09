#!/bin/bash
# set2: regenerate the generated files, run the secret scan, write the root SHA256SUMS and verify it.
set -uo pipefail
E='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set2-20261011'
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set2
for f in $J/*.sh $J/*.py $J/*.ps1 $J/files.txt $J/README.template.md; do case $(basename $f) in orig-*) ;; *) cp "$f" "$E/tools/";; esac; done
python3 $J/summary.py "$E" | tail -n 1
python3 $J/mkreadme.py "$E" $J/README.template.md
rm -f "$E/SHA256SUMS" "$E/secret-scan.txt"
python3 $J/secretscan.py "$E" /var/tmp/cp-release-drill-set2-* /var/tmp/cp-release-drill-rid-* > /tmp/set2-secret-scan.txt 2> /tmp/set2-secret-scan.err; echo "scan rc=$?"; tail -n 2 /tmp/set2-secret-scan.err
cp /tmp/set2-secret-scan.txt "$E/secret-scan.txt"
grep -v "^  /var/tmp" "$E/secret-scan.txt" | cut -c1-200
( cd "$E" && find . -type f ! -path ./SHA256SUMS -print0 | sort -z | xargs -0 sha256sum | sed 's#  \./#  #' > SHA256SUMS && sha256sum -c --quiet SHA256SUMS && echo "root SHA256SUMS verified: $(wc -l < SHA256SUMS) files" )
( cd '/mnt/c/CELIKBROS PROJECTS/celikpanel' && find deploy/e2e/release-recovery/evidence/set2-20261011 -type f | awk '{ if (length($0) > m) { m = length($0); p = $0 } } END { print "longest repository-relative path:", m; print p }' )
grep -L '"native_evidence": false' "$E"/*/run-*/result.json | head -3; echo "result.json files with native_evidence false: $(grep -l '"native_evidence": false' "$E"/*/run-*/result.json | wc -l) of $(ls "$E"/*/run-*/result.json | wc -l)"
du -sh "$E" | cut -f1
