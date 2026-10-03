#!/usr/bin/env python3
"""upd13 host-only, read-only: per-run hit counts of every changed-path marker (from each run's
side/changed-paths.txt, written by cpaths.py at staging). usage: cpsum13.py STAGE OUT"""
import re
import sys
from pathlib import Path

stage, out = Path(sys.argv[1]), Path(sys.argv[2])
HEAD = re.compile(r"^## (.+?): (\d+) hits \((\d+) in \.txt\)$")
runs = sorted(stage.glob("upd1-*/run-*")) + sorted(stage.glob("part2-alpha80/upd1-*/run-*"))
table, labels = {}, []
for run in runs:
    cell = ("a80:" if "part2-alpha80" in str(run) else "") + run.parent.name.replace("upd1-", "") + "/" + run.name
    f = run / "side" / "changed-paths.txt"
    if not f.exists():
        continue
    for line in f.read_text(errors="replace").splitlines():
        m = HEAD.match(line)
        if m:
            if m[1] not in labels:
                labels.append(m[1])
            table.setdefault(m[1], {})[cell] = int(m[3])
cells = [("a80:" if "part2-alpha80" in str(r) else "") + r.parent.name.replace("upd1-", "") + "/" + r.name for r in runs]
lines = ["# changed-path markers: journal/text-file hits per run (cpaths.py at staging; 0 = not seen)", ""]
lines.append("| marker | " + " | ".join(cells) + " |")
lines.append("| --- |" + " --- |" * len(cells))
for label in labels:
    lines.append(f"| {label} | " + " | ".join(str(table[label].get(c, "-")) for c in cells) + " |")
out.write_text("\n".join(lines) + "\n", encoding="utf-8")
print("\n".join(lines))
