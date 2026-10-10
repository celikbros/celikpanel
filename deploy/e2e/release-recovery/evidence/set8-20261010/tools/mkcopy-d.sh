#!/bin/bash
# set8: run copy d = copy a's archive + the set8 harness files of the working tree; the cells run from it.
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set8-run; L=$R/logs
bash $J/mkov.sh d set8_trial.py guest_set8_native.py run-set8.sh test_set8_trial.py > $L/mkov-d.txt 2>&1 || { tail -n 5 $L/mkov-d.txt; exit 1; }
tail -n 8 $L/mkov-d.txt
cd $R/harness-d && python3 -I -c "import ast; [ast.parse(open('deploy/e2e/release-recovery/'+f).read()) for f in ('set8_trial.py','guest_set8_native.py','test_set8_trial.py')]; print('set8 files parse')"
bash -n $R/harness-d/deploy/e2e/release-recovery/run-set8.sh && echo "run-set8.sh parses"
