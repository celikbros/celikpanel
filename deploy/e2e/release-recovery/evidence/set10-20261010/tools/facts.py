import json, sys, glob, os
root = sys.argv[1]  # staged driver dir
def sec(part):
    f = sorted(glob.glob(root + f"/steps/*{part}*/section.json"))[-1]
    return json.load(open(f))
def cell(part, name):
    return sec(part)["cells"][name]
s0 = sec("s0-sites")
inc = [e for e in s0["cells"]["S0"]["expectations"] if "include directory" in e["expectation"]]
print("S0 include dirs:", [e["measured"] for e in inc][:1])
e = sec("e-removed")
print("e-recreate detail:", json.dumps(e["cells"]["e-recreate"]["detail"])[:300])
k = sec("keep")
print("keep detail:", json.dumps(k["cells"]["keep"]["detail"])[:400])
h = cell("h-i", "h")
print("h start line:", [x["measured"] for x in h["expectations"] if "start line" in x["expectation"]])
l = cell("l-delete", "l")
print("l:", json.dumps(l["detail"])[:300])
for x in l["expectations"]: print("  l", x["held"], x["expectation"][:70], json.dumps(x["measured"])[:300])
tr = cell("take-refused", "take-refused")
for x in tr["expectations"]: print("  tr", x["held"], x["expectation"][:60], json.dumps(x["measured"])[:400])
print("tr detail", json.dumps(tr["detail"])[:300])
