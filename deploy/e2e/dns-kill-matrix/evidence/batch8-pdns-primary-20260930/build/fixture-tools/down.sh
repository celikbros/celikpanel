set -u
# usage: down.sh SHORT CELL ROOT  (stop and teardown after a successful capture)
L=/var/tmp/cp-b8-0930/logs/$1
ROOT=$3
cd /root/cp-b8-src
F=deploy/e2e/dns-kill-matrix/fixture.py
python3 $F stop --work-root $ROOT --cell-id $2 --execute > $L/stop.json 2>&1; echo "stop rc=$?"
python3 $F teardown --work-root $ROOT --cell-id $2 --execute > $L/teardown.json 2>&1; echo "teardown rc=$?"
cat $L/stop.json $L/teardown.json
mkdir -p /var/tmp/cp-b8-0930/evidence/$1; cp $L/stop.json $L/teardown.json /var/tmp/cp-b8-0930/evidence/$1/
pgrep -a qemu || echo "no qemu"
ls $ROOT/cells
