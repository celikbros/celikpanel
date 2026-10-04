#!/bin/bash
# Jobs that use the harness-h12 run copy: the one re-run of the Debian mgmt-off cell and the Arch mgmt-off cell.
set -euo pipefail
R=/var/tmp/cp-upd4-run
ART=$(cat $R/ART)
while IFS=: read short cell lab port; do
  cat > $R/jobs/job-cell-$short.sh <<EOF
#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash $R/harness-h12/deploy/e2e/release-recovery/run-upd1.sh cell $cell $ART $lab $port
EOF
  echo "harness=$R/harness-h12 (git archive a6dd5b1e + H12/H13 run-copy diff)" > $R/jobs/job-cell-$short.harness
done <<'LIST'
d13mrb:upd1-debian13-mgmt-off-reboot:upd4-d13-mr-b:2521
archmr:upd1-arch-mgmt-off-reboot:upd4-arch-mr-a:2511
LIST
cat $R/jobs/job-cell-d13mrb.sh $R/jobs/job-cell-archmr.sh
