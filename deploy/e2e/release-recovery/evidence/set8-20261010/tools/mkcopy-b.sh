#!/bin/bash
# set8: run copy b = copy a's archive + the set8 harness files of the working tree; the cells run from it.
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set8-run; L=$R/logs
bash $J/mkov.sh b set8_trial.py guest_set8_native.py run-set8.sh test_set8_trial.py > $L/mkov-b.txt 2>&1 || { tail -n 5 $L/mkov-b.txt; exit 1; }
tail -n 8 $L/mkov-b.txt
cd $R/harness-b && python3 -I -c "import ast; [ast.parse(open('deploy/e2e/release-recovery/'+f).read()) for f in ('set8_trial.py','guest_set8_native.py','test_set8_trial.py')]; print('set8 files parse')"
bash -n $R/harness-b/deploy/e2e/release-recovery/run-set8.sh && echo "run-set8.sh parses"
