#!/bin/bash
# set10: the root SHA256SUMS over every file of the evidence folder (generated last), then verified.
E="<repo>/deploy/e2e/release-recovery/evidence/set10-20261010"
cd "$E" && rm -f SHA256SUMS && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum | sed 's#  \./#  #' > /var/tmp/cp-set10-run/SHA256SUMS.tmp && mv /var/tmp/cp-set10-run/SHA256SUMS.tmp SHA256SUMS
wc -l < SHA256SUMS; sha256sum -c --quiet SHA256SUMS && echo "root SHA256SUMS verified"
for d in */*/driver; do [ -f "$d/SHA256SUMS" ] && ( cd "$d" && sha256sum -c --quiet SHA256SUMS ) && echo "$d sums ok"; done
awk '{print length($2)}' SHA256SUMS | sort -n | tail -1
