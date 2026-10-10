#!/usr/bin/env python3
"""set10: the cell x platform table from the staged evidence (every verdict as the driver judged it; the raw path of
each cell is its section.json, whose `cells` entry holds every expectation with what was measured)."""
import glob, json, os, sys
E = sys.argv[1]
RUNS = {"Debian 13": "debian13/run-c", "Ubuntu 24.04": "ubuntu/run-a", "Arch": "arch/run-a"}
UPD = {"Debian 13": ("debian13/update-good", "debian13/update-defective"), "Ubuntu 24.04": ("ubuntu/update-good", "ubuntu/update-defective"),
       "Arch": ("arch/update-good", "arch/update-defective")}
def cells_of(run):
    out = {}
    for f in sorted(glob.glob(os.path.join(E, run, "driver/steps/*/section.json"))):
        s = json.load(open(f))
        for name, c in (s.get("cells") or {}).items():
            out[name] = (c["verdict"], os.path.relpath(f, E), [e["expectation"] for e in c["expectations"] if e["held"] is not True])
    return out
data = {p: cells_of(r) for p, r in RUNS.items()}
for p, (g, d) in UPD.items():
    data[p].update(cells_of(g)); data[p].update(cells_of(d))
order = ["S0", "h", "i", "a1", "a2", "a3", "b1", "b2", "b3", "c1", "c2", "c3", "d1", "d2", "d3", "keep", "j-kept", "take",
         "take-stale", "take-refused", "e", "e-recreate", "f", "g", "g-resume", "k", "l", "B-before", "B-forward",
         "B-db-restore", "B-return"]
names = sorted({n for p in data for n in data[p]}, key=lambda n: order.index(n) if n in order else 99)
print("| Cell | " + " | ".join(data) + " | raw (Debian 13) |")
print("| --- | " + " | ".join("---" for _ in data) + " | --- |")
for n in names:
    row = []
    for p in data:
        v = data[p].get(n)
        row.append(v[0] if v else "not run")
    raw = (data["Debian 13"].get(n) or next((data[p][n] for p in data if n in data[p]), (None, "")))[1]
    print(f"| {n} | " + " | ".join(row) + f" | `{raw}` |")
print()
for p in data:
    for n in names:
        v = data[p].get(n)
        if v and v[0] != "PASS":
            print(f"- {p} {n} {v[0]}: " + "; ".join(v[2]) + f" (`{v[1]}`)")
