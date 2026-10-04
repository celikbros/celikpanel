# Read-only over retained evidence: anything natively timestamped between the boundary marker and SIGKILL?
# Sources: unit journals (all DNS units), file mtimes in owner-post-state/post-state tree listings, watcher CHANGE/POINTER lines.
import json, sys, re, glob, os, datetime
E, cell = sys.argv[1], sys.argv[2]
_p = f"{E}/raw/results/{cell}/result.json"
r = json.load(open(_p)) if os.path.exists(_p) else json.load(open(f"{E}/raw/results/{cell}/reboot-checkpoint-1.json"))["result"]  # batch 10: held run, result from the checkpoint
def ts(s):
    s = s.replace("Z", "+00:00")
    m = re.match(r"(.*T\d\d:\d\d:\d\d)(\.\d+)?(.*)", s)
    frac = (m.group(2) or ".0")[:7]
    return datetime.datetime.fromisoformat(m.group(1) + frac + (m.group(3) or "+00:00"))
if not (r.get("boundary_marker") or {}).get("recorded_at") or not (r.get("kill") or {}).get("delivered_at"):
    print(f"no boundary marker or kill in result.json (status {r.get('status')}, kill_proven {r.get('kill_proven')}); nothing to check"); sys.exit(0)
marker = ts(r["boundary_marker"]["recorded_at"]); kill = ts(r["kill"]["delivered_at"])
print(f"marker {marker.isoformat()} kill {kill.isoformat()} window_ms {(kill-marker).total_seconds()*1000:.1f}")
hits = []; parsed = {"journal": 0, "mtime": 0}
for f in glob.glob(f"{E}/raw/journald/*.service.txt"):
    for line in open(f, errors="replace"):
        m = re.match(r"(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d+[+-]\d\d:?\d\d)", line)
        if m:
            t = ts(m.group(1)); parsed["journal"] += 1
            if marker <= t <= kill: hits.append(("journal", os.path.basename(f), line.strip()[:300]))
for f in [f"{E}/owner-post-state.txt", f"{E}/post-state.txt", f"{E}/pre-run-units.txt"]:
    if not os.path.exists(f): continue
    for line in open(f, errors="replace"):
        m = re.search(r"(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d+)", line)
        if m:
            try: t = ts(m.group(1)[:26] + "+00:00")
            except Exception: continue
            parsed["mtime"] += 1
            if marker <= t <= kill: hits.append(("mtime", os.path.basename(f), line.strip()[:300]))
print("timestamped lines parsed:", parsed)
print("native entries inside [marker, SIGKILL]:", len(hits))
for h in hits: print(" ", *h)
