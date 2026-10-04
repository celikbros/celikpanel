set -u
# usage: down.sh SHORT CELL  (stop and teardown after a successful capture)
ROOT=/var/tmp/cp-b4-0929
L=$ROOT/logs/$1
cd /root/cp-b4-src
F=deploy/e2e/dns-kill-matrix/fixture.py
python3 $F stop --work-root $ROOT --cell-id $2 --execute > $L/stop.json 2>&1; echo "stop rc=$?"
python3 $F teardown --work-root $ROOT --cell-id $2 --execute > $L/teardown.json 2>&1; echo "teardown rc=$?"
cat $L/stop.json $L/teardown.json
mkdir -p $ROOT/evidence/$1; cp $L/stop.json $L/teardown.json $ROOT/evidence/$1/
pgrep -a qemu || echo "no qemu"
ls $ROOT/cells
