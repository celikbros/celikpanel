# set4b (the summary of set4, unchanged): every check of every staged cell, one line each, with the file that holds it (read-only over the evidence folder).
# usage: summary.py EVIDENCE_DIR  -> writes checks-all.txt and checks-not-passed.txt there, prints a short count.
import io
import json
import os
import sys

E = sys.argv[1]
lines, bad = [], []
for root, dirs, files in sorted(os.walk(E)):
    dirs.sort()
    rel = os.path.relpath(root, E).replace("\\", "/")
    if "result.json" in files and os.path.basename(os.path.dirname(root)) != "steps":
        try:
            r = json.load(io.open(os.path.join(root, "result.json"), encoding="utf-8"))
        except Exception as exc:  # noqa: BLE001
            lines.append(f"## {rel}: result.json unreadable ({type(exc).__name__})")
            continue
        lines.append(f"## {rel}: overall={r.get('overall')} native_evidence={r.get('native_evidence')} "
                     f"outcome={(r.get('outcome') or {}).get('classification')}")
        for s in r.get("steps", []):
            line = f"step | {rel} | {s.get('name')} | {s.get('verdict')} | {str(s.get('reason') or '')[:300]}"
            lines.append(line)
            if s.get("verdict") not in ("passed", "observed", "skipped"):
                bad.append(line)
    if "section.json" in files:
        try:
            s = json.load(io.open(os.path.join(root, "section.json"), encoding="utf-8"))
        except Exception as exc:  # noqa: BLE001
            lines.append(f"section | {rel} | unreadable ({type(exc).__name__})")
            continue
        lines.append(f"section | {rel} | {s.get('section')} | {s.get('verdict')} | error={str(s.get('error') or '')[:300]}")
        if s.get("error"):
            bad.append(f"section-error | {rel}/section.json | {s.get('section')} | {str(s.get('error'))[:600]}")
        for c in s.get("checks", []):
            verdict = "PASS" if c.get("ok") is True else "FAIL" if c.get("ok") is False else "NOT-ESTABLISHED"
            line = f"check | {rel}/section.json | {verdict} | {c.get('name')}"
            lines.append(line)
            if verdict != "PASS":
                bad.append(line)
        for n in s.get("notes", []):
            lines.append(f"note | {rel}/section.json | {n.get('note')}")
io.open(os.path.join(E, "checks-all.txt"), "w", encoding="utf-8", newline="\n").write("\n".join(lines) + "\n")
io.open(os.path.join(E, "checks-not-passed.txt"), "w", encoding="utf-8", newline="\n").write(("\n".join(bad) or "(none)") + "\n")
print(len(lines), "lines;", len(bad), "not passed")
