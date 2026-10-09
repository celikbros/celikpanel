#!/bin/bash
# set4b: make run copy NAME from the file list, compile the new files, run the offline suites that cover the drivers
# set4b is built on, and the dry run of the named cells against ARTIFACTS.
# usage: mkcopy.sh NAME ARTIFACTS_JSON CELL...
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set4b-run; L=$R/logs
name=$1; art=$2; shift 2
mkdir -p $L $R/build
bash $J/mkov.sh $name $(tr -d '\r' < $J/files.txt | tr '\n' ' ') > $L/mkov-$name.txt 2>&1 || { tail -n 3 $L/mkov-$name.txt; exit 1; }
head -n 1 $L/mkov-$name.txt
cd $R/harness-$name
H=$R/harness-$name/deploy/e2e/release-recovery
rc=0
python3 -c "import ast,sys;[ast.parse(open(f).read(),f) for f in sys.argv[1:]];print('parsed',len(sys.argv)-1)" $H/set4b_trial.py $H/guest_set4b_native.py $H/set4_trial.py $H/guest_set4_native.py || rc=1
for t in test_set4b_trial test_settings_writes_trial test_request_identity_trial test_lab test_guest_probe; do
  python3 -m unittest deploy/e2e/release-recovery/$t.py > $L/offline-$name-$t.txt 2>&1; r=$?
  echo "$t rc=$r $(grep -E '^Ran ' $L/offline-$name-$t.txt) $(tail -n 1 $L/offline-$name-$t.txt)"; [ $r -eq 0 ] || rc=1
done
for c in "$@"; do
  bash $H/run-set4b.sh dry-run $c $art dry-$c > $R/build/dry-$name-$c.json 2> $R/build/dry-$name-$c.stderr.txt; r=$?; echo "dry $c rc=$r"; [ $r -eq 0 ] || { rc=1; tail -n 3 $R/build/dry-$name-$c.stderr.txt; }
done
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null || echo "no dry-run lab was created"
exit $rc
