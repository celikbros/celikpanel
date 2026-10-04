# usage: keys.py RESULT KEY... -- print selected result.json keys (truncated), read-only
import json, sys
r = json.load(open(sys.argv[1]))
if len(sys.argv) == 2:
    print(sorted(r.keys())); sys.exit()
for k in sys.argv[2:]:
    v = r
    for p in k.split("."):
        v = v.get(p) if isinstance(v, dict) else None
    print(k, "=", json.dumps(v, sort_keys=True)[:1500])
