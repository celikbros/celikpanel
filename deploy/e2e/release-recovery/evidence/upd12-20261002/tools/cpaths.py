#!/usr/bin/env python3
"""upd12 read-only: which paths changed since the earlier runs show up in one cell's evidence.
usage: cpaths.py LAB   (reads /var/tmp/cp-release-drill-LAB/evidence/*/upd1/*/ only)"""
import glob, json, os, re, sys

lab = sys.argv[1]
ev = sorted(glob.glob(f"/var/tmp/cp-release-drill-{lab}/evidence/*/upd1/*/"))[-1]
print("evidence", ev)

MARKERS = {
    "initial-record (updater wrote it)": r"updater recorded it for request|güncelleyici \S+ işlemi için kaydetti",
    "curl preflight refusal": r"required update tool is missing: /usr/bin/curl",
    "no-marker after child failure": r"left no pending transaction|it left no pending operation|The interrupted update was stopped before",
    "pause_pending journal": r"The last admitted recovery attempt did not finish\. The next run",
    "renewal restore at pause": r"Automatic certificate renewal \(Certbot\) (was returned|is already in its state)",
    "pause recorded": r"Automatic recovery paused after three admitted attempts",
    "start check failure": r"new panel start check|panel startup check failed",
    "package-activity refusal (startup reconcile)": r"another server change or package-manager task is still running",
    "package_manager_active / HOST_MUTATION_BUSY": r"package_manager_active|HOST_MUTATION_BUSY|host package manager is active",
    "startup reconcile success": r"certificate startup reconcile: (mail SNI|completed|restored|removed)",
    "milter chain/wiring": r"milter (chain|wiring)",
}
files = [f for f in sorted(glob.glob(ev + "**/*", recursive=True))
         if os.path.isfile(f) and (f.endswith(".txt") or f.endswith(".log") or f.endswith(".json") or f.endswith(".jsonl"))]
for label, pattern in MARKERS.items():
    rx = re.compile(pattern)
    hits = []
    for f in files:
        try:
            for n, line in enumerate(open(f, errors="replace"), 1):
                if rx.search(line):
                    hits.append((f.replace(ev, ""), n, line.strip()))
        except OSError:
            pass
    # journals only for the line list (json files repeat the same text)
    jl = [h for h in hits if h[0].endswith(".txt")]
    print(f"## {label}: {len(hits)} hits ({len(jl)} in .txt)")
    seen = set()
    for f, n, line in jl:
        key = re.sub(r"^\S+ \S+ \S+ ", "", line)[:160]
        if key in seen:
            continue
        seen.add(key)
        print(f"   {f}:{n}: {line[:330]}")
        if len(seen) >= int(os.environ.get("MAXL", "8")):
            break

# automatic_recovery values seen by the root CLI
vals = {}
for f in sorted(glob.glob(ev + "steps/*track*/samples/*.json")):
    d = json.load(open(f))
    try:
        cj = json.loads(((d.get("cli") or {}).get("json") or {}).get("stdout") or "null") or {}
    except ValueError:
        cj = {}
    k = (cj.get("phase"), cj.get("reason"), cj.get("automatic_recovery"), cj.get("failure_code"), cj.get("first_failure_code"))
    vals.setdefault(k, d.get("utc"))
print("## root CLI states (first seen)")
for k, t in vals.items():
    print("  ", t, k)

# listen value the panel unit / env carries (whatever the evidence recorded)
print("## CELIKPANEL_LISTEN mentions")
for f in files:
    try:
        for line in open(f, errors="replace"):
            if "CELIKPANEL_LISTEN" in line:
                print("  ", f.replace(ev, ""), line.strip()[:200])
                break
    except OSError:
        pass
