# Starts a read-only continuous DNS sampler on the peer (queries both guests every ~2 s).
install -m 0600 /tmp/cp-b6a-pairq.py /root/cp-b6a-pairq.py
mkdir -p /var/tmp/cp-b6a-peerloop
systemd-run --unit=cp-b6a-peerloop --collect /usr/bin/python3 /root/cp-b6a-pairq.py loop /var/tmp/cp-b6a-peerloop/samples.log 5400
echo "peerloop start rc=$?"
