#!/bin/bash
# set9: the whole offline suite on a run copy. usage: suite.sh COPY
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set9-run
cd $R/harness-$1 && python3 -m unittest discover -s deploy/e2e/release-recovery -p 'test_*.py' > $R/logs/suite-$1.txt 2>&1
echo "rc=$?"; tail -n 3 $R/logs/suite-$1.txt
grep -E '^(ERROR|FAIL):' $R/logs/suite-$1.txt | sort > $R/logs/suite-$1-notok.txt; wc -l < $R/logs/suite-$1-notok.txt
