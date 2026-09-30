cd /root/cp-b11-src
echo "== direct test_native_pdns_peer.py (tail)"
python3 deploy/e2e/dns-kill-matrix/test_native_pdns_peer.py 2>&1 | tail -8
echo "== per-module counts (python3 -m unittest -v MODULE)"
cd deploy/e2e/dns-kill-matrix
tot=0
for t in test_*.py; do m=${t%.py}; n=$(python3 -m unittest $m 2>&1 | grep -oE '^Ran [0-9]+' | grep -oE '[0-9]+'); echo "module $m: $n"; tot=$((tot+${n:-0})); done
echo "module total=$tot"
find /root/cp-b11-src -name __pycache__ -type d -prune -exec rm -rf {} +
