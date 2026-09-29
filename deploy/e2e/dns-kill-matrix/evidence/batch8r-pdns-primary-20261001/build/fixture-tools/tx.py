# usage: tx.py SHORT CELL [MAXCHARS] -- read-only: every transcript event, truncated
import json, sys, glob
S, C = sys.argv[1], sys.argv[2]
n = int(sys.argv[3]) if len(sys.argv) > 3 else 400
for f in sorted(glob.glob(f"/var/tmp/cp-b8r-1001/evidence/{S}/raw/results/{C}/transcript*.jsonl")):
    for line in open(f):
        print(line.strip()[:n])
