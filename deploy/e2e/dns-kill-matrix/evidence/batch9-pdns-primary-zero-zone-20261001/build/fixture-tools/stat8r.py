# Read-only: every recovery status text in a cell's evidence. usage: stat8r.py SHORT CELL
import json, sys, re, os
S, C = sys.argv[1:3]
E = f"/var/tmp/cp-b9-1001/evidence/{S}"
r = json.load(open(f"{E}/raw/results/{C}/result.json"))
def walk(o, p):
    if isinstance(o, dict):
        for k, v in o.items():
            if k == "output" and isinstance(v, str) and ("DNS switch request" in v or "recover" in v.lower() or "journal" in v):
                print(f"--- result.json {'.'.join(p)} (rc {o.get('returncode')}):\n{v}")
            else:
                walk(v, p + [k])
    elif isinstance(o, list):
        for i, v in enumerate(o): walk(v, p + [str(i)])
walk(r, [])
op = f"{E}/owner-post-state.txt"
if os.path.exists(op):
    t = open(op, errors="replace").read()
    m = re.search(r"== recovery dns-switch-status --quiesced.*?\n(.*?)STATUS_RC=(\d+)", t, re.S)
    if m: print(f"--- owner-post-state.txt (post-collect, rc {m.group(2)}):\n{m.group(1)}")
sm = f"{E}/service-mutation-status-post-collect.json"
if os.path.exists(sm): print("--- service-mutation-status-post-collect.json:\n" + open(sm).read()[:3000])
