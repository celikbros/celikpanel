import json, sys, glob, os
E = sys.argv[1]
for plat, run in (("debian13", "debian13/run-c"), ("ubuntu", "ubuntu/run-a"), ("arch", "arch/run-a")):
    root = os.path.join(E, run, "driver")
    def sec(part):
        return json.load(open(sorted(glob.glob(root + f"/steps/*{part}*/section.json"))[-1]))
    s0 = sec("s0-sites")
    print("==", plat, s0.get("platform", {}).get("nginx_version"), s0.get("platform", {}).get("php_fpm_programs"))
    nat = sorted(glob.glob(root + "/steps/*s0-sites/native/*s0-after-creation-read.json"))[-1]
    v = json.load(open(nat))
    a = v["sites"]["set10-a.test"]
    print("  include dir:", {k: a["include_dir"].get(k) for k in ("owner", "group", "mode", "uid", "gid")})
    print("  vhost:", {k: a["vhost"].get(k) for k in ("owner", "group", "mode")}, "first line:", a["vhost"].get("first_line")[:80])
    e = sec("e-removed")["cells"]["e-recreate"]["detail"]
    print("  recreated file:", e.get("mode_of_the_recreated_file"), e.get("owner_group"))
    g = sec("g-immutable")["cells"]["g"]
    for x in g["expectations"]:
        if "journal line" in x["expectation"] or "start line" in x["expectation"] or "site-config" in x["expectation"]:
            print("  g:", x["held"], json.dumps(x["measured"])[:330])
    k = sec("keep")["cells"]["keep"]["detail"]["general_save_after_keep"]
    print("  save after keep:", k.get("status"), k.get("code"))
    t = sec("take-j")["cells"]["take"]
    print("  take backup:", json.dumps(t["expectations"][1]["measured"])[:260])
    h = sec("h-i")["cells"]["h"]["expectations"]
    print("  h reload:", json.dumps(h[2]["measured"])[:200])
    a3 = [c for c in sec("a-owner")["cells"].values()][2]
    print("  a3 save:", json.dumps(a3["expectations"][-1]["measured"])[:200])
