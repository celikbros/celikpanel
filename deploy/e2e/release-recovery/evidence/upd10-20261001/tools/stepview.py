import json, sys
d = json.load(open(sys.argv[1]))
c = d["checks"]
print(d["name"], d["verdict"], d.get("started_at"), d.get("finished_at"))
for k in sys.argv[2:]:
    print("--", k); print(json.dumps(c.get(k), indent=1, ensure_ascii=False)[:6000])
