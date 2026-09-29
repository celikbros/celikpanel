set -u
# usage: stoponly.sh SHORT CELL ROOT  (failure: stop both guests, keep their overlays)
L=/var/tmp/cp-b5-0929/logs/$1
ROOT=$3
cd /root/cp-b5-src
python3 deploy/e2e/dns-kill-matrix/fixture.py stop --work-root $ROOT --cell-id $2 --execute > $L/stop.json 2>&1; echo "stop rc=$?"
cat $L/stop.json
mkdir -p /var/tmp/cp-b5-0929/evidence/$1; cp $L/stop.json /var/tmp/cp-b5-0929/evidence/$1/
pgrep -a qemu || echo "no qemu"
ls -la $ROOT/cells
