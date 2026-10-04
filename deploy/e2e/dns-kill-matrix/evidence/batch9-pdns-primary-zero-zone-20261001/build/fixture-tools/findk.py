# Read-only: print paths and values of keys matching a substring in a cell's result.json. usage: findk.py SHORT CELL SUBSTR [MAXLEN]
import json, sys
s, c, sub = sys.argv[1:4]; ml = int(sys.argv[4]) if len(sys.argv) > 4 else 800
r = json.load(open(f"/var/tmp/cp-b9-1001/evidence/{s}/raw/results/{c}/result.json"))
def walk(o, p):
    if isinstance(o, dict):
        for k, v in o.items():
            q = p + "." + k
            if sub in k: print(q, "=", json.dumps(v)[:ml])
            walk(v, q)
    elif isinstance(o, list):
        for i, v in enumerate(o): walk(v, f"{p}[{i}]")
walk(r, "")
