import json, sys, glob
d = sorted(glob.glob(f"/var/tmp/cp-release-drill-{sys.argv[1]}/evidence/*/upd1/*/"))[-1]
f = sorted(glob.glob(d + f"steps/*{sys.argv[2]}*/section.json"))[-1]
s = json.load(open(f))
c = s["cells"][sys.argv[3]]
for e in c["expectations"]:
    print(e["held"], e["expectation"][:100]); print("    ", json.dumps(e["measured"], default=str)[:int(sys.argv[4]) if len(sys.argv) > 4 else 500])
