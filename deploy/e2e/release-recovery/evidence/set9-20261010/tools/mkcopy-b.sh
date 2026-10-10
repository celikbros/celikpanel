#!/bin/bash
# set9: run copy b = copy a's archive + the two set9 harness files of the working tree; the cell runs from it.
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set9-run; L=$R/logs
bash $J/mkov.sh b set9_trial.py run-set9.sh > $L/mkov-b.txt 2>&1 || { tail -n 5 $L/mkov-b.txt; exit 1; }
tail -n 6 $L/mkov-b.txt
cd $R/harness-b && python3 -I -c "import ast,sys; ast.parse(open('deploy/e2e/release-recovery/set9_trial.py').read()); print('set9_trial parses')"
bash -n $R/harness-b/deploy/e2e/release-recovery/run-set9.sh && echo "run-set9.sh parses"
