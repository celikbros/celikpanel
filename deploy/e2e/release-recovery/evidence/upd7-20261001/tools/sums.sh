#!/bin/bash
set -eu
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd7-20261001"
cd "$S"
find . -type d -name __pycache__ -print
rm -f SHA256SUMS
find . -type f ! -path ./SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > /tmp/upd7-sums && mv /tmp/upd7-sums SHA256SUMS
sha256sum -c --quiet SHA256SUMS && echo "SHA256SUMS ok $(wc -l < SHA256SUMS) entries"
find . -type f | wc -l
