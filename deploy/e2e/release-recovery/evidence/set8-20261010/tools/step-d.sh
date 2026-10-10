#!/bin/bash
bash <scratchpad>/set8/tools/quicktest.sh
bash <scratchpad>/set8/tools/mkcopy-d.sh && bash <scratchpad>/set8/tools/suite.sh d && diff /var/tmp/cp-set8-run/logs/suite-a-notok.txt /var/tmp/cp-set8-run/logs/suite-d-notok.txt && echo same-as-a
H=/var/tmp/cp-set8-run/harness-d/deploy/e2e/release-recovery
for c in set8-debian13 set8-ubuntu set8-arch; do bash $H/run-set8.sh dry-run $c "$(cat /var/tmp/cp-set8-run/artifacts-cur.path)" dry-$c > /var/tmp/cp-set8-run/build/dry-d-$c.json 2>&1; echo "dry-d $c rc=$?"; done
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null || echo "no dry-run lab was created"
