# Starts a read-only continuous DNS sampler on the native BIND secondary (queries both guests every ~2 s). Arg: N (boot ordinal)
N=${1:-1}
if [ -f /tmp/cp-b8r-pairq.py ]; then install -m 0600 /tmp/cp-b8r-pairq.py /root/cp-b8r-pairq.py; fi
test -f /root/cp-b8r-pairq.py || { echo "sampler script absent"; exit 1; }
mkdir -p /var/tmp/cp-b8r-peerloop$N
systemd-run --unit=cp-b8r-peerloop$N --collect /usr/bin/python3 /root/cp-b8r-pairq.py loop /var/tmp/cp-b8r-peerloop$N/samples.log 5400
echo "peerloop$N start rc=$?"
