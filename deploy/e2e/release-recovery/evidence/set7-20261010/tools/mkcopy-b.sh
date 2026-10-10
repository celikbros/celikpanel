#!/bin/bash
# set7: run copy b = copy a's archive + the two set7 harness files of the working tree; the cells run from it.
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set7-run; L=$R/logs
bash $J/mkov.sh b set7_trial.py run-set7.sh > $L/mkov-b.txt 2>&1 || { tail -n 5 $L/mkov-b.txt; exit 1; }
tail -n 6 $L/mkov-b.txt
cd $R/harness-b && python3 -I -c "import ast,sys; ast.parse(open('deploy/e2e/release-recovery/set7_trial.py').read()); print('set7_trial parses')"
bash -n $R/harness-b/deploy/e2e/release-recovery/run-set7.sh && echo "run-set7.sh parses"
