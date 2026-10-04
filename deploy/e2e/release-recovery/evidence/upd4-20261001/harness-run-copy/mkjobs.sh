#!/bin/bash
# Write one job file per cell (harness = the a6dd5b1e run copy). Needs /var/tmp/cp-upd4-run/ART.
set -euo pipefail
R=/var/tmp/cp-upd4-run
ART=$(cat $R/ART)
while IFS=: read short cell lab port; do
  cat > $R/jobs/job-cell-$short.sh <<EOF
#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash $R/harness/deploy/e2e/release-recovery/run-upd1.sh cell $cell $ART $lab $port
EOF
  echo "harness=$R/harness (git archive a6dd5b1e)" > $R/jobs/job-cell-$short.harness
done <<'LIST'
d13good:upd1-debian13-good:upd4-d13-good-a:2371
d13def:upd1-debian13-defective:upd4-d13-def-a:2361
d13sc:upd1-debian13-startcheck:upd4-d13-sc-a:2401
d13rs:upd1-debian13-realstart:upd4-d13-rs-a:2421
d13oc:upd1-debian13-owner-continuation:upd4-d13-oc-a:2481
d13mr:upd1-debian13-mgmt-off-reboot:upd4-d13-mr-a:2501
archgood:upd1-arch-good:upd4-arch-good-a:2391
archdef:upd1-arch-defective:upd4-arch-def-a:2381
archsc:upd1-arch-startcheck:upd4-arch-sc-a:2411
archrs:upd1-arch-realstart:upd4-arch-rs-a:2431
archoc:upd1-arch-owner-continuation:upd4-arch-oc-a:2491
archmr:upd1-arch-mgmt-off-reboot:upd4-arch-mr-a:2511
LIST
ls $R/jobs; cat $R/jobs/job-cell-d13good.sh
