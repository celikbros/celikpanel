import json, sys, glob, os
d = sorted(glob.glob(f"/var/tmp/cp-release-drill-{sys.argv[1]}/evidence/*/upd1/*/"))[-1]
for f in sorted(glob.glob(d + "steps/*/section.json")):
    s = json.load(open(f))
    for name, c in (s.get("cells") or {}).items():
        bad = [e["expectation"][:90] for e in c["expectations"] if e["held"] is not True]
        print(f"{os.path.basename(os.path.dirname(f))[:22]:22} {name:14} {c['verdict']:12} {bad}")
    for r in s.get("runs") or []:
        if r.get("error"):
            print("RUN-ERROR", r["run"], r["error"][:300])
