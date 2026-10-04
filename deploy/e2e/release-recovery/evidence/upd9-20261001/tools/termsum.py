import json, sys, glob
ev = sorted(glob.glob(f"/var/tmp/cp-release-drill-{sys.argv[1]}/evidence/*/upd1/*/"))[-1]
for name in ("terminal", "verdicts", "track"):
    f = glob.glob(ev + f"steps/*-{name}/step.json")
    if not f:
        continue
    d = json.load(open(f[0])); c = d["checks"]
    print("==", name, d["verdict"], d["started_at"], d["finished_at"])
    for k, v in c.items():
        s = json.dumps(v, ensure_ascii=False)
        print(" ", k, ":", s[:420])
