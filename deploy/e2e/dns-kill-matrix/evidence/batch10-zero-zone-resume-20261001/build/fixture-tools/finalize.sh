set -euo pipefail
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b10
D='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-kill-matrix/evidence/batch10-zero-zone-resume-20261001'
for f in secretscan.py secretscan.sh z05facts.py z05facts.sh export10.sh remain10.sh finalize.sh; do cp $SP/$f "$D/build/fixture-tools/$f"; done
echo "held record hash now: $(sha256sum "$D/z05-held-resume/fresh-primary-peer/zone-lifecycle-held.json" | cut -c1-16) (bdf6a15b at the hold)"
python3 $SP/secretscan.py "$D" > "$D/build/secret-scan.txt"
cat "$D/build/secret-scan.txt"
cd "$D"
test ! -e SHA256SUMS
find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum | sed 's#  \./#  #' > SHA256SUMS.tmp
mv SHA256SUMS.tmp SHA256SUMS
sha256sum -c --quiet SHA256SUMS && echo "SHA256SUMS verified: $(wc -l < SHA256SUMS) files"
cd '/mnt/c/CELIKBROS PROJECTS/celikpanel'
echo "longest repo-relative path: $(find deploy/e2e/dns-kill-matrix/evidence/batch10-zero-zone-resume-20261001 -type f | awk '{print length($0)}' | sort -n | tail -1)"
git -c safe.directory='*' status --porcelain -- deploy/e2e/dns-kill-matrix/evidence/batch10-zero-zone-resume-20261001 | head -3
