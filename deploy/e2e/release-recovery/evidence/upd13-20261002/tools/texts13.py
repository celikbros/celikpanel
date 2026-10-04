#!/usr/bin/env python3
"""upd13 host-only, read-only: verbatim owner-visible texts across the staged runs, grouped by state.
usage: texts13.py STAGE OUT
Sources (all product output recorded by the driver): root CLI `recovery status --lang en|tr` per track sample;
the update card and recovery-screen texts the driver renders from the installed build's catalogues at the terminal
step; setup refusals (API message and catalogue entry)."""
import glob, json, re, sys
from pathlib import Path

stage, out = Path(sys.argv[1]), Path(sys.argv[2])
DROP = re.compile(r"^(Request|Recorded at|İşlem|Kayıt zamanı): ")


def clean(text):
    lines = [l for l in (text or "").strip().splitlines() if not DROP.match(l)]
    return " / ".join(l.strip() for l in lines if l.strip())


cli = {}      # key -> {"en":..., "tr":..., "cells": [(cell, first utc)]}
cards = {}    # (kind, state) -> {...}
setup = {}
runs = sorted(stage.glob("upd1-*/run-*")) + sorted(stage.glob("part2-alpha80/upd1-*/run-*"))
for run in runs:
    cell = ("a80:" if "part2-alpha80" in str(run) else "") + run.parent.name + "/" + run.name
    for f in sorted(run.glob("steps/*track*/samples/*.json")):
        d = json.load(open(f, encoding="utf-8"))
        c = d.get("cli") or {}
        try:
            cj = json.loads((c.get("json") or {}).get("stdout") or "null") or {}
        except ValueError:
            cj = {}
        key = " ".join(f"{k}={cj.get(k)}" for k in ("observation", "phase", "reason", "automatic_recovery",
                                                     "failure_code", "first_failure_code", "previous_failure")
                       if cj.get(k) is not None) or f"no-json status={(c.get('json') or {}).get('status')}"
        en, tr = clean((c.get("en") or {}).get("stdout")), clean((c.get("tr") or {}).get("stdout"))
        if not en and not tr:
            continue
        slot = cli.setdefault((key, en, tr), {"cells": {}})
        slot["cells"].setdefault(cell, d.get("utc"))
    for f in sorted(run.glob("steps/*terminal/step.json")):
        uc = (json.load(open(f, encoding="utf-8")).get("checks") or {}).get("update_card") or {}
        for kind in ("update_card", "recovery_guidance"):
            v = uc.get(kind) or {}
            texts = v.get("texts") or {}
            if not texts:
                continue
            state = v.get("state") or json.dumps({k: (v.get("observation") or {}).get(k) for k in ("phase", "reason",
                                                   "failure_code", "previous_failure")})
            k = (kind, state, " || ".join(texts.get("en") or []), " || ".join(texts.get("tr") or []))
            cards.setdefault(k, []).append(cell)
        judged = uc.get("update_card_judged") or {}
        if judged.get("server_message_line"):
            cards.setdefault(("update_card server line", "", judged["server_message_line"], ""), []).append(cell)
    for f in sorted(run.glob("steps/*setup/step.json")):
        for a in (json.load(open(f, encoding="utf-8")).get("checks") or {}).get("owner_attempts") or []:
            t = a.get("owner_texts") or {}
            if not t:
                continue
            k = (a.get("phase"), t.get("code"), t.get("api_message"),
                 json.dumps({kk: vv for kk, vv in t.items() if kk.startswith("err.")}, ensure_ascii=False))
            setup.setdefault(k, []).append(f"{cell} attempt {a.get('attempt')} {a.get('status')}")

lines = ["# upd13 owner-visible texts (verbatim product output; Request/Recorded-at lines removed for grouping)", ""]
lines.append("## Root CLI `sudo /usr/libexec/celikpanel/recovery status --request-id <rid> --lang en|tr`, per recorded state")
for (key, en, tr), v in cli.items():
    lines.append(f"\n### {key}")
    lines.append("seen in: " + "; ".join(f"{c} (first {t})" for c, t in v["cells"].items()))
    lines.append(f"EN: {en}")
    lines.append(f"TR: {tr}")
lines.append("\n## Update card and recovery screen at the terminal step (rendered by the driver from the installed "
             "build's catalogues and source; not a browser)")
for (kind, state, en, tr), cells in cards.items():
    lines.append(f"\n### {kind} {state}")
    lines.append("seen in: " + "; ".join(cells))
    lines.append(f"EN: {en}")
    if tr:
        lines.append(f"TR: {tr}")
lines.append("\n## Setup step refusals shown to the owner")
for (phase, code, api, cat), cells in setup.items():
    lines.append(f"\n### {phase} {code}")
    lines.append("seen in: " + "; ".join(cells))
    lines.append(f"API: {api}")
    lines.append(f"catalogue: {cat}")
out.write_text("\n".join(lines) + "\n", encoding="utf-8")
print(f"{len(cli)} CLI texts, {len(cards)} card/screen texts, {len(setup)} setup texts")
