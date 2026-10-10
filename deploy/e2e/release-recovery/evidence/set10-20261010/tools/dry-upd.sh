#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set10-run; H=$R/harness-$1/deploy/e2e/release-recovery; A=$(cat $R/artifacts-a81.path)
for c in upd1-debian13-good upd1-debian13-defective upd1-ubuntu-good upd1-ubuntu-defective upd1-arch-good upd1-arch-defective; do
  bash $H/run-set10.sh dry-run $c "$A" dry-$c > $R/build/dry-$1-$c.json 2> $R/build/dry-$1-$c.err; echo "dry $c rc=$?"; done
python3 -I -c "import json,sys; print(json.load(open(sys.argv[1]))['added_steps'])" $R/build/dry-$1-upd1-debian13-good.json
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null || echo "no dry-run lab was created"
