# Starts a read-only continuous DNS sampler on the native BIND secondary (queries both guests every ~2 s). Arg: N (boot ordinal)
N=${1:-1}
if [ -f /tmp/cp-b12-pairq.py ]; then install -m 0600 /tmp/cp-b12-pairq.py /root/cp-b12-pairq.py; fi
test -f /root/cp-b12-pairq.py || { echo "sampler script absent"; exit 1; }
mkdir -p /var/tmp/cp-b12-peerloop$N
systemd-run --unit=cp-b12-peerloop$N --collect /usr/bin/python3 /root/cp-b12-pairq.py loop /var/tmp/cp-b12-peerloop$N/samples.log 5400
echo "peerloop$N start rc=$?"
