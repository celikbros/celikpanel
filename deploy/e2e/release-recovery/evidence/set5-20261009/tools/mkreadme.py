"""set5: put the README together. The text is written by hand (README.md, the template, and parts/NAME.md); the
tables that only repeat values of the staged runs are generated here from facts.json and the generated views, so
that no value is copied by hand. usage: mkreadme.py EVIDENCE_DIR TEMPLATE PARTS_DIR OUT
Fails when a placeholder of the template has neither a generator nor a part, or when a part is not used.
"""
import json
import os
import re
import sys

E, TEMPLATE, PARTS, OUT = sys.argv[1].rstrip("/\\"), sys.argv[2], sys.argv[3], sys.argv[4]
facts = json.load(open(os.path.join(E, "facts.json"), encoding="utf-8"))
CELLS = ("upd1-debian13-good", "upd1-ubuntu-good", "upd1-arch-good", "upd1-debian13-defective", "upd1-ubuntu-defective",
         "upd1-arch-defective", "upd1-debian13-startcheck", "upd1-debian13-owner-continuation",
         "upd1-ubuntu-owner-continuation", "upd1-debian13-mgmt-off-reboot")
WHAT = {"good": "good update", "defective": "migration defect + second fault: automatic return",
        "startcheck": "start-check defect + VM reset: automatic return", "owner-continuation": "port held: pause, the owner's one retry",
        "mgmt-off-reboot": "good update, then management off across a reboot"}


def read(name):
    return open(os.path.join(E, name), encoding="utf-8").read()


def cells_table():
    lines = ["One new lab per cell; the cells ran one after the other, in this order. `S/` is `steps/` of the run's folder under",
             "`update-alpha81/`. PASS: every step of set3's method ran with the verdict it had in set3 and the outcome and final",
             "state are the expected ones; the steps set5 added are in their own column.", "",
             "| Cell | What | Run, lab, harness copy | Wrapper (UTC) | By set3's criteria | Outcome / final state | Added steps | Raw files |",
             "| --- | --- | --- | --- | --- | --- | --- | --- |"]
    ordered = sorted(facts["runs"].items(), key=lambda kv: kv[1]["wrapper"][0] or "")
    seen = set()
    for name, run in ordered:
        cell = run["cell"]
        seen.add(cell)
        copy = re.search(r"harness-([a-z])", run.get("harness") or "")
        added = ", ".join(f"{k}: {v}" for k, v in run["added_steps"].items()) or "-"
        reasons = ("; ".join(run["reasons"])) if run["reasons"] else ""
        kind = cell.split("-", 2)[2]
        lines.append(f"| {cell} | {WHAT[kind]} | {run['run']}, `s5-{short(cell)}-{run['run'][-1]}`, {copy.group(1) if copy else '?'} | "
                     f"{(run['wrapper'][0] or '')[11:19]}-{(run['wrapper'][1] or '')[11:19]} | **{run['verdict_by_set3_criteria']}**"
                     + (f" ({reasons})" if reasons else "") + f" | {run['classification']}; {'/'.join(str(x) for x in run['final'])}; overall `{run['overall']}` | {added} | "
                     f"`{name}/result.json`, `S/*/step.json`" + (", `S/*-m10-postfix-stop/section.json`" if "M10-postfix-stop" in run["added_steps"] else "") + " |")
    for cell in CELLS:
        if cell not in seen:
            reason = "; ".join(facts["cells"].get(cell, {}).get("reasons") or ["no run"])
            lines.append(f"| {cell} | {WHAT[cell.split('-', 2)[2]]} | - | - | **NOT-MEASURED** ({reason}) | - | - | - |")
    return "\n".join(lines)


def short(cell):
    platform, kind = cell.split("-", 2)[1:]
    return {"debian13": "d13", "ubuntu": "ub", "arch": "arch"}[platform] + "-" + {"good": "good", "defective": "def", "startcheck": "sc",
                                                                             "owner-continuation": "oc", "mgmt-off-reboot": "mr"}[kind]


def packages_table():
    by = {}
    for name, value in facts["platforms"].items():
        platform = name.split("/")[1].split("-")[1]
        by.setdefault(platform, []).append((name, value))
    names = sorted({k for values in by.values() for _n, v in values for k in (v.get("packages") or {})})
    lines = ["| | Arch | Debian 13 | Ubuntu 24.04 |", "| --- | --- | --- | --- |"]

    def cell_of(platform, key, getter):
        seen = {}
        for name, value in by.get(platform, []):
            got = getter(value)
            if got is not None:
                seen.setdefault(str(got), []).append(name.split("/")[1])
        if not seen:
            return "not read"
        if len(seen) == 1:
            return next(iter(seen))
        return "; ".join(f"{k} ({', '.join(v)})" for k, v in seen.items())
    lines.append("| OS | " + " | ".join(cell_of(p, None, lambda v: (v.get("os_release") or {}).get("PRETTY_NAME") if isinstance(v.get("os_release"), dict) else None)
                                         for p in ("arch", "debian13", "ubuntu")) + " |")
    lines.append("| kernel (read in the cells of copy `e`) | " + " | ".join(cell_of(p, None, lambda v: v.get("kernel")) for p in ("arch", "debian13", "ubuntu")) + " |")
    for key in names:
        lines.append(f"| {key} | " + " | ".join(cell_of(p, key, lambda v, key=key: (v.get("packages") or {}).get(key)) for p in ("arch", "debian13", "ubuntu")) + " |")
    lines.append("")
    lines.append("A cell with one value per platform means every run of that platform that read the package read the same version; where runs")
    lines.append("differ, each value is followed by its cells. `not read`: the package is not installed there, or no run of the platform read it")
    lines.append("(the two good cells of Debian and Ubuntu read set4's shorter package list before the update; the other cells read the list above at their end).")
    return "\n".join(lines)


def table_of(name, start):
    text = read(name)
    index = text.index(start)
    return text[index:].strip()


GENERATED = {"CELLS": cells_table, "PACKAGES": packages_table,
             "PINNING": lambda: table_of("pinning.md", "| Run | Pinned at"),
             "COMPARE": lambda: table_of("compare-with-set3.md", "| Cell | set3 |").replace("## Differences in the compared facts", "Differences in the compared facts:")}
template = open(TEMPLATE, encoding="utf-8").read().replace("\r\n", "\n")
used = set()


def fill(match):
    name = match.group(1)
    path = os.path.join(PARTS, name + ".md")
    if name in GENERATED:
        used.add(name)
        return GENERATED[name]()
    if os.path.isfile(path):
        used.add(name)
        return open(path, encoding="utf-8").read().replace("\r\n", "\n").strip()
    raise SystemExit(f"placeholder @@{name}@@ has neither a generator nor a part")


text = re.sub(r"@@([A-Z0-9_]+)@@", fill, template)
unused = sorted(os.path.splitext(n)[0] for n in os.listdir(PARTS) if n.endswith(".md") and os.path.splitext(n)[0] not in used)
if unused:
    raise SystemExit("parts not used by the template: " + ", ".join(unused))
if "@@" in text:
    raise SystemExit("a placeholder is left in the text")
open(OUT, "w", encoding="utf-8", newline="\n").write(text.rstrip("\n") + "\n")
print(f"README written: {len(text.splitlines())} lines; parts {sorted(used)}")
