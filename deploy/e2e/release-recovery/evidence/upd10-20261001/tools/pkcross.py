#!/usr/bin/env python3
"""upd10 (from upd9): cross-table of every PackageKit observation of a lab: /proc state x the Agent's readiness answer."""
import glob, json, sys
for lab in sys.argv[1:]:
    ev = sorted(glob.glob(f"/var/tmp/cp-release-drill-{lab}/evidence/*/upd1/*/"))[-1]
    rows = {}
    ages, aptcc, kids, own, paths, aptb, ctxs = [], set(), set(), 0, set(), set(), set()
    files = sorted(glob.glob(ev + "steps/*/packagekit-observations.json"))
    extra = []
    for f in sorted(glob.glob(ev + "steps/*/step.json")):
        c = json.load(open(f)).get("checks", {})
        for k in ("packagekit_at_arm", "packagekit_before_post", "packagekit_after_answer", "before_op", "after_op"):
            if isinstance(c.get(k), dict):
                extra.append(c[k])
        if isinstance(c.get("idle_alive_attempt"), dict):
            pass
    seen = set()
    records = []
    for f in files:
        records += json.load(open(f))
    records += [r for r in extra if (r.get("label"), r.get("host_utc")) not in {(x.get("label"), x.get("host_utc")) for x in records}]
    for r in records:
        reads = [x for x in (r.get("before"), r.get("after")) if isinstance(x, dict)]
        if not reads:
            continue
        pk = any(x.get("packagekitd") for x in reads)
        oth = any(x.get("other_package_processes") or x.get("general_lock_lines") for x in reads)
        for x in reads:
            for d in x.get("packagekitd") or []:
                ages.append(d.get("etime_s") or 0); aptcc.add(d.get("grep_c_aptcc")); paths.update(d.get("backend_pathnames") or []); aptb.add(d.get("apt_backend"))
                kids.update(c["comm"] for c in d.get("children") or [])
                own += len(d.get("holds_or_waits") or [])
            ctxs.update(c["comm"] for c in x.get("context_processes") or [])
        state = ("packagekitd only" if pk and not oth else "packagekitd + other package activity" if pk and oth
                 else "other package activity only" if oth else "nothing")
        rd = r.get("readiness")
        if not isinstance(rd, dict):
            ans = "(no readiness read)"
        else:
            b = rd.get("body") or {}
            ans = "ready" if b.get("ready") is True else f"{b.get('code')}/{b.get('reason')}"
        rows[(state, ans)] = rows.get((state, ans), 0) + 1
    print(f"== {lab} ({len(records)} observations; files: {[f.split('/steps/')[1] for f in files]})")
    for (state, ans), n in sorted(rows.items()):
        print(f"  {state:38s} | {ans:45s} | {n}")
    print(f"  packagekitd readings: {len(ages)}; age range {min(ages) if ages else '-'}-{max(ages) if ages else '-'} s; "
          f"grep -c aptcc values {sorted(aptcc, key=str)}; apt_backend values {sorted(aptb, key=str)}; children seen {sorted(kids)}; lock lines held/waited by it {own}")
    print(f"  backend pathnames mapped: {sorted(paths)}")
    print(f"  context processes seen (outside the Agent list): {sorted(ctxs)}")
