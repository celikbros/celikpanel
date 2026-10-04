# Starts a read-only continuous DNS sampler on the peer (queries both guests every ~2 s).
install -m 0600 /tmp/cp-b6b-pairq.py /root/cp-b6b-pairq.py
mkdir -p /var/tmp/cp-b6b-peerloop
systemd-run --unit=cp-b6b-peerloop --collect /usr/bin/python3 /root/cp-b6b-pairq.py loop /var/tmp/cp-b6b-peerloop/samples.log 5400
echo "peerloop start rc=$?"
