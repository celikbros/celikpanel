#!/usr/bin/env python3
"""upd13 read-only extract (from upd9 ext9.py) (from upd8 ext7.py): status series, distinct CLI texts, update failure/initial-record lines."""
import glob, json, os, re, sys
lab = sys.argv[1]
ev = sorted(glob.glob(f"/var/tmp/cp-release-drill-{lab}/evidence/*/upd1/*/"))[-1]
print("evidence", ev)
seen = {}
for track in sorted(glob.glob(ev + "steps/*track*/samples")):
    print("==", track.split("/steps/")[1])
    for f in sorted(glob.glob(track + "/*.json")):
        d = json.load(open(f))
        u = ((d.get("update_status") or {}).get("body") or {})
        r = d.get("recovery_api") or {}
        c = d.get("cli") or {}
        try:
            cj = json.loads((c.get("json") or {}).get("stdout") or "null") or {}
        except ValueError:
            cj = {"raw": (c.get("json") or {}).get("stdout")}
        rb = r.get("body") if isinstance(r.get("body"), dict) else {}
        key = "|".join(str(cj.get(k)) for k in ("observation", "phase", "terminal_proof", "reason", "automatic_recovery", "failure_code", "first_failure_code"))
        print(d.get("utc"), "panel=", u.get("status") if u else d.get("panel_error"), "api=", r.get("http"), rb.get("phase"), rb.get("automatic_recovery"),
              "cli=", key, "cli_status=", (c.get("json") or {}).get("status"))
        if key not in seen:
            seen[key] = {lang: ((c.get(lang) or {}).get("stdout") or "") for lang in ("en", "tr")}
            seen[key]["utc"] = d.get("utc")
print("== distinct CLI texts")
for key, v in seen.items():
    print("#", key, "first", v["utc"])
    for lang in ("en", "tr"):
        print(" ", lang.upper(), ":", v[lang].strip().replace("\n", " / "))
print("== journal lines")
pat = re.compile(r"records no update status|update status kaydetmiyor|durumu kaydetmiyor|CELIKPANEL_UPDATE_FAILURE|recovery observation is unavailable|kurtarma gözlemi|Recovery dispatch|automatic recovery|Previous pending|Önceki bekleyen|rollback|geri", re.I)
for f in sorted(glob.glob(ev + "**/*journal*", recursive=True)) + sorted(glob.glob(ev + "**/*.log", recursive=True)):
    if os.path.isdir(f):
        continue
    try:
        text = open(f, errors="replace").read()
    except OSError:
        continue
    hits = [l for l in text.splitlines() if pat.search(l)]
    if hits:
        print("--", f.replace(ev, ""), len(hits))
        for l in hits[:int(os.environ.get("MAXL", "12"))]:
            print("   ", l[:400])
r = json.load(open(ev + "result.json"))
print("== result", json.dumps({k: r.get(k) for k in ("overall", "native_evidence", "request_id")}), json.dumps(r.get("outcome"))[:600])
print("findings", json.dumps(r.get("findings"), ensure_ascii=False)[:3000])
# upd9: every PackageKit observation file of this run, condensed
for f in sorted(glob.glob(ev + "steps/*/packagekit-observations.json")):
    print("== packagekit", f.replace(ev, ""))
    for rec in json.load(open(f)):
        def one(x):
            if not isinstance(x, dict):
                return "-"
            d = [(q["pid"], q.get("etime_s"), q.get("backend_pathnames"), q.get("apt_backend"), [c["comm"] for c in q.get("children", [])],
                  len(q.get("holds_or_waits", [])), q.get("rule_idle")) for q in x.get("packagekitd", [])]
            return (f'{x.get("at", "")[11:23]} rule={x.get("rule")} pk(pid,age,backend_pathnames,apt_backend,children,locks,idle)={d} '
                    f'others={[c["comm"] for c in x.get("other_package_processes", [])]} ctx={[c["comm"] for c in x.get("context_processes", [])]} '
                    f'locks={[(l["path"], l["owner_comm"]) for l in x.get("lock_lines", [])]}')
        rd = rec.get("readiness") or {}
        b = rd.get("body") or {}
        print(f'{rec["label"]:28s} {one(rec.get("before"))} | readiness {rd.get("utc", "")[11:19]} ready={b.get("ready")} '
              f'{b.get("code") or ""}/{b.get("reason") or ""} by={rd.get("answered_by")}')
