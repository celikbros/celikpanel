set -euo pipefail
D='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-kill-matrix/evidence/batch10-zero-zone-resume-20261001'
cd "$D"
rm -f SHA256SUMS SHA256SUMS.tmp
T=$(mktemp /var/tmp/cp-b10-sums.XXXXXX)
find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum | sed 's#  \./#  #' > "$T"
cp "$T" SHA256SUMS; rm -f "$T"
sha256sum -c --quiet SHA256SUMS && echo "SHA256SUMS verified: $(wc -l < SHA256SUMS) files"
grep -c 'SHA256SUMS' SHA256SUMS || true
head -3 build/secret-scan.txt
