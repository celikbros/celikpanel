import glob, json, sys
d = sys.argv[1]
s = json.load(open(glob.glob(d + "/steps/*-s0-sites/section.json")[0]))
r = s.get("reference") or {}
print("reference equals creation:", r.get("equals_the_creation_render"), "diff:", r.get("diff_from_the_creation_render"))
print("baseline vhost:", s["baseline"]["vhost"]); print("reference vhost:", r.get("vhost"))
g = json.load(open(glob.glob(d + "/steps/*-g-immutable/section.json")[0]))["g"]
print("g http:", g["http"]); print("g nginx_test after reload rc:", (g["nginx"]["nginx_test_after_the_reload"] or {}).get("returncode"))
