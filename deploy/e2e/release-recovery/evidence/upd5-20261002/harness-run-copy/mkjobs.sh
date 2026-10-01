#!/bin/bash
# Write one job file per cell (harness = the 6cda60b8 run copy). Needs /var/tmp/cp-upd5-run/ART.
set -euo pipefail
R=/var/tmp/cp-upd5-run
ART=$(cat $R/ART)
while IFS=: read short cell lab port; do
  cat > $R/jobs/job-cell-$short.sh <<EOT
#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash $R/harness/deploy/e2e/release-recovery/run-upd1.sh cell $cell $ART $lab $port
EOT
  echo "harness=$R/harness (git archive 6cda60b8)" > $R/jobs/job-cell-$short.harness
done <<'LIST'
d13good:upd1-debian13-good:upd5-d13-good-a:2541
d13rs:upd1-debian13-realstart:upd5-d13-rs-a:2551
d13oc:upd1-debian13-owner-continuation:upd5-d13-oc-a:2561
archgood:upd1-arch-good:upd5-arch-good-a:2571
archrs:upd1-arch-realstart:upd5-arch-rs-a:2581
archoc:upd1-arch-owner-continuation:upd5-arch-oc-a:2591
LIST
ls $R/jobs; cat $R/jobs/job-cell-d13good.sh
