# Starts a read-only continuous DNS sampler on the peer (queries both guests every ~2 s).
install -m 0600 /tmp/cp-b7-pairq.py /root/cp-b7-pairq.py
mkdir -p /var/tmp/cp-b7-peerloop
systemd-run --unit=cp-b7-peerloop --collect /usr/bin/python3 /root/cp-b7-pairq.py loop /var/tmp/cp-b7-peerloop/samples.log 5400
echo "peerloop start rc=$?"
