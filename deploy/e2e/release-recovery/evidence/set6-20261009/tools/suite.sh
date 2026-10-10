#!/bin/bash
# set6: the whole offline suite on run copy NAME (python3 -m unittest discover), kept as a file. usage: suite.sh NAME
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set6-run; L=$R/logs
cd $R/harness-$1 || exit 2
python3 -m unittest discover -s deploy/e2e/release-recovery -p 'test_*.py' > $L/suite-$1.txt 2>&1; rc=$?
echo "suite $1 rc=$rc $(grep -E '^Ran ' $L/suite-$1.txt) | $(tail -n 1 $L/suite-$1.txt)"
grep -E '^(ERROR|FAIL):' $L/suite-$1.txt | sort > $L/suite-$1-notok.txt
wc -l < $L/suite-$1-notok.txt
ls deploy/e2e/release-recovery/test_*.py | wc -l
