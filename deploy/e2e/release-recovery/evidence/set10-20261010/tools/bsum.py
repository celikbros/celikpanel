import json, sys, glob, os
E = sys.argv[1]
for run in sorted(glob.glob(E + "/*/update-*")):
    for f in sorted(glob.glob(run + "/driver/steps/*set10-*/section.json")):
        s = json.load(open(f))
        for name, c in (s.get("cells") or {}).items():
            print(os.path.relpath(run, E), name, c["verdict"])
            if name == "B-forward":
                for e in c["expectations"]:
                    if "N adopted" in e["expectation"]:
                        print("   ", [l.split("panel[")[1][30:] if "panel[" in l else l for l in e["measured"]])
                print("   sites:", {d: (v or {}).get("adopted_from") or (v or {}).get("state") for d, v in (c["expectations"][5]["measured"] or {}).items()})
            if name == "B-return":
                print("   ", json.dumps(c["detail"]["what_alpha81_did_per_file"])[:600])
            if name == "B-db-restore":
                print("   ", [l[l.find("site configuration files"):][:200] for l in c["detail"]["start_lines"]])
