import json, sys, glob
d = sorted(glob.glob(f"/var/tmp/cp-release-drill-{sys.argv[1]}/evidence/*/upd1/*/"))[-1]
f = sorted(glob.glob(d + "steps/*site-files-after*/section.json"))[-1]
s = json.load(open(f))
c = s["cells"]["B-return"]
print(json.dumps(c["detail"]["what_alpha81_did_per_file"], indent=1))
