#!/bin/bash
set -eu
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set1-20261010"
cd "$S"
find . -type d -name __pycache__ -print
rm -f SHA256SUMS
find . -type f ! -path ./SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > /var/tmp/cp-set1-run/sums.tmp && mv /var/tmp/cp-set1-run/sums.tmp SHA256SUMS
sha256sum -c --quiet SHA256SUMS && echo "SHA256SUMS ok $(wc -l < SHA256SUMS) entries"
find . -type f | wc -l
find . -type f | awk '{ n = length("deploy/e2e/release-recovery/evidence/set1-20261010/") + length($0) - 2; if (n > m) m = n } END { print "longest repo-relative path:", m }'
