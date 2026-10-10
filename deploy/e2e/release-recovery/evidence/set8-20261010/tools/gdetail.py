import glob, json, sys
d = glob.glob(sys.argv[1] + "/steps/*-g-immutable/section.json")[0]
g = json.load(open(d))["g"]
for site, t in g["per_site"].items():
    for stage, v in t.items():
        print(site, stage, "vhost", {k: v["vhost"].get(k) for k in ("exists", "inode", "ctime_ns", "mtime_ns", "group")}, "link", v["enabled"])
print(json.dumps(g["owner_view"], indent=1)[:3000])
print(json.dumps(g["nginx"]["reload_lines"], indent=1))
print(json.dumps(g["trigger"], indent=1)[:1500])
