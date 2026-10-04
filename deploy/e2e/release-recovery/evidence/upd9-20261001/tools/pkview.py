import json, sys, glob
paths = sorted(glob.glob(sys.argv[1]))
for p in paths:
    print("==", p)
    for r in json.load(open(p)):
        def short(x):
            if not isinstance(x, dict): return str(x)[:80]
            d = [(q["pid"], q.get("etime_s"), q.get("grep_c_aptcc"), [c["comm"] for c in q.get("children", [])],
                  [h["line"][:60] for h in q.get("holds_or_waits", [])], q.get("rule_idle")) for q in x.get("packagekitd", [])]
            o = [c["comm"] for c in x.get("other_package_processes", [])]
            g = [(l["path"], l["owner_comm"]) for l in x.get("general_lock_lines", [])]
            ll = [(l["path"], l["owner_comm"], l["owner"]) for l in x.get("lock_lines", [])]
            return f'{x.get("at","")[11:23]} rule={x.get("rule")} pk={d} others={o} locks={ll} unp={len(x.get("unparsed_lock_lines",[]))}'
        rd = r.get("readiness") or {}
        b = rd.get("body") or {}
        print(r["label"], "|", short(r.get("before")), "| R:", rd.get("utc","")[11:19], b.get("ready"), b.get("reason"), rd.get("answered_by"), "|", short(r.get("after"))[:30], r.get("before_error",""), r.get("readiness_error",""))
