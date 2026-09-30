import glob, json
for lab in ("upd3-arch-rs-a", "upd3-d13-rs-a", "upd3-arch-good-a"):
    for f in sorted(glob.glob(f"/var/tmp/cp-release-drill-{lab}/evidence/*/upd1/*/steps/*/workload-*.json")):
        d = json.load(open(f))
        print(lab, f.split("/steps/")[1], json.dumps(d.get("timers")))
