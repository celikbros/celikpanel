#!/bin/bash
# set10: run copy NAME = copy a's archive (cd46ca595) + the set10 harness files of the working tree; the cells run
# from the newest such copy. usage: mkcopy.sh NAME
J=<scratchpad>/set10/tools
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set10-run; L=$R/logs
bash $J/mkov.sh $1 set10_trial.py guest_set10_native.py run-set10.sh test_set10_trial.py > $L/mkov-$1.txt 2>&1 || { tail -n 5 $L/mkov-$1.txt; exit 1; }
tail -n 8 $L/mkov-$1.txt
cd $R/harness-$1 && python3 -I -c "import ast; [ast.parse(open('deploy/e2e/release-recovery/'+f).read()) for f in ('set10_trial.py','guest_set10_native.py','test_set10_trial.py')]; print('set10 files parse')"
bash -n $R/harness-$1/deploy/e2e/release-recovery/run-set10.sh && echo "run-set10.sh parses"
cd $R/harness-$1/deploy/e2e/release-recovery && python3 -m unittest test_set10_trial 2>&1 | tail -n 4
