set -euo pipefail
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
D='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-kill-matrix/evidence/batch12-zero-zone-complete-20261001'
for f in $SP/*.sh $SP/*.py $SP/*.txt; do cp $f "$D/build/fixture-tools/$(basename $f)"; done
python3 $SP/secretscan.py "$D" > "$D/build/secret-scan.txt"
cat "$D/build/secret-scan.txt"
cd "$D"
rm -f SHA256SUMS
T=$(mktemp /var/tmp/cp-b12-sums.XXXXXX)
find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum | sed 's#  \./#  #' > "$T"
cp "$T" SHA256SUMS; rm -f "$T"
sha256sum -c --quiet SHA256SUMS && echo "SHA256SUMS verified: $(wc -l < SHA256SUMS) files"
cd '/mnt/c/CELIKBROS PROJECTS/celikpanel'
echo "longest repo-relative path: $(find deploy/e2e/dns-kill-matrix/evidence/batch12-zero-zone-complete-20261001 -type f | awk '{print length($0)}' | sort -n | tail -1)"
du -sh "$D"
