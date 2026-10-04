# Read-only: per-file offline test counts of the kill-matrix harness in /root/cp-b9-src (pycache removed after)
set -u
cd /root/cp-b9-src
tot=0; tot2=0
for f in deploy/e2e/dns-kill-matrix/test_*.py; do
  m=$(basename ${f%.py})
  n=$(cd deploy/e2e/dns-kill-matrix && python3 -m unittest $m 2>&1 | grep -oE '^Ran [0-9]+' | grep -oE '[0-9]+')
  r=$(cd deploy/e2e/dns-kill-matrix && python3 -m unittest $m 2>&1 | tail -1)
  n2=$(python3 $f 2>&1 | grep -oE '^Ran [0-9]+' | grep -oE '[0-9]+')
  echo "unittest=$n direct=${n2:-none} $r $f"; tot=$((tot + ${n:-0})); tot2=$((tot2 + ${n2:-0}))
done
echo "sum per-file (python3 -m unittest MODULE): $tot; sum (python3 FILE): $tot2"
find /root/cp-b9-src -name __pycache__ -type d -prune -exec rm -rf {} +
