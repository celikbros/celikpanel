set -u
# usage: stoponly.sh SHORT CELL  (failure: stop the guest, keep its overlay)
ROOT=/var/tmp/cp-b4-0929
L=$ROOT/logs/$1
cd /root/cp-b4-src
python3 deploy/e2e/dns-kill-matrix/fixture.py stop --work-root $ROOT --cell-id $2 --execute > $L/stop.json 2>&1; echo "stop rc=$?"
cat $L/stop.json
mkdir -p $ROOT/evidence/$1; cp $L/stop.json $ROOT/evidence/$1/
pgrep -a qemu || echo "no qemu"
ls -la $ROOT/cells
