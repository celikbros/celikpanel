# Read-only: zone lifecycle step details (catalog serials, answers, times). usage: zl.py SHORT
import json, sys, glob, os
E = f"/var/tmp/cp-b8r-1001/evidence/{sys.argv[1]}/fresh-primary-peer"
for p in sorted(glob.glob(E + "/zone-lifecycle*.json")):
    v = json.load(open(p)); print("==", os.path.basename(p), v.get("status"))
    for s in v["steps"]:
        res = s.get("result") or {}; ob = s.get("observation") or {}
        print(" step", s["step"], s["verdict"], "| result keys", sorted(res.keys()))
        print("   result", json.dumps({k: res.get(k) for k in res if k not in ("schema",)}, sort_keys=True)[:1500])
        print("   catalog", json.dumps(ob.get("catalog")), "| answers", json.dumps(ob.get("answers"))[:600])
        print("   obs keys", sorted(ob.keys()))
for p in sorted(glob.glob(E + "/peer-verdict*.json")):
    v = json.load(open(p)); ob = v.get("observation") or {}
    print("==", os.path.basename(p), v.get("status"), "| catalog", json.dumps(ob.get("catalog"))[:400], "| child", json.dumps((ob.get("child") or {}).get("catalog"))[:300])
