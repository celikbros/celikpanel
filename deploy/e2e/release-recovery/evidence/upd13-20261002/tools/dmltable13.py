#!/usr/bin/env python3
"""upd13 host-only, read-only: the deferred startup mail retry in every staged run, one block per run.
usage: dmltable13.py STAGE HARNESS_DIR OUT
Parses each run's collect journal (journal-product.txt) with the harness's own deferred_mail_view (run copy
f6cdd5a0) and, where present, the deferred-mail-watch record. Only Panel processes started at or after the owner's
start are listed; "+s" is from that process's "Starting CelikPanel Backend..." line. The operation's own times come
from the run's track samples (first root-CLI terminal/pause state) and the step table."""
import datetime as dt
import glob
import json
import sys
from pathlib import Path

stage, harness, out = Path(sys.argv[1]), sys.argv[2], Path(sys.argv[3])
sys.path.insert(0, harness + "/deploy/e2e/release-recovery")
import owner_update_trial as m  # noqa: E402

TERMINAL = ("update_verified", "rollback_verified", "paused_retry_limit")


def ts(v):
    return dt.datetime.fromisoformat(v.replace("Z", "+00:00")) if v else None


def rel(a, b):
    return f"+{(ts(a) - ts(b)).total_seconds():.2f}s" if a and b else "-"


rows = []
lines = ["# upd13 deferred startup mail retry per run (read-only; dmltable13.py)", ""]
for run in sorted(stage.glob("upd1-*/run-*")) + sorted(stage.glob("part2-alpha80/upd1-*/run-*")):
    cell = ("a80:" if "part2-alpha80" in str(run) else "") + run.parent.name + "/" + run.name
    res = json.load(open(run / "result.json"))
    steps = {s["name"]: s for s in res["steps"]}
    start = (steps.get("owner-start") or {}).get("started_at")
    col = sorted(run.glob("steps/*-collect/journal-product.txt"))
    view = m.deferred_mail_view(open(col[-1], errors="replace").read()) if col else {"processes": []}
    terminal = {}
    for f in sorted(run.glob("steps/*track*/samples/*.json")):
        d = json.load(open(f))
        try:
            cj = json.loads(((d.get("cli") or {}).get("json") or {}).get("stdout") or "null") or {}
        except ValueError:
            cj = {}
        for r in (cj.get("reason"), cj.get("automatic_recovery")):
            if r in TERMINAL and r not in terminal:
                terminal[r] = d.get("utc")
    watch = sorted(run.glob("steps/*-deferred-mail-watch/deferred-mail.json"))
    w = json.load(open(watch[-1])) if watch else None
    lines.append(f"## {cell}: overall={res.get('overall')} outcome={(res.get('outcome') or {}).get('classification')}")
    lines.append(f"owner start {start}; root CLI terminal/pause first seen {json.dumps(terminal)}; "
                 f"watch step {'present' if w else 'not in this cell'}")
    procs = [p for p in view["processes"] if start and p["started_at"] >= start[:19]]
    for p in procs:
        a = p["attempts"]
        lines.append(f"- panel[{p['pid']}] start {p['started_at']} ready {rel(p['ready_at'], p['started_at'])} "
                     f"deferred={p['deferred']} startup_lines={len(p['startup_lines'])} attempts={len(a)} "
                     f"finished={p['finished']} gave_up={p['gave_up']} repeated={p['repeated']} "
                     f"other_mail_lines={len(p['other_mail_lines'])}")
        for s in p["startup_lines"]:
            lines.append(f"    startup {rel(s['at'], p['started_at'])} {s['text'][:400]}")
        for x in a:
            lines.append(f"    attempt {x['attempt']} of {x['of']} {x['at']} ({rel(x['at'], p['started_at'])}): {x['text'][:600]}")
        for o in p["other_mail_lines"]:
            lines.append(f"    other {rel(o['at'], p['started_at'])} {o['text'][:300]}")
    if w:
        lines.append(f"watch: {w.get('watch_seconds')} s, polls {len(w.get('polls') or [])}, settle {json.dumps(w.get('settle'))}")
        facts = {f["label"]: f.get("lines") or [f.get("unavailable")] for f in w.get("mail_file_facts", [])}
        a0, b0 = facts.get("watch-start", []), facts.get("watch-end", [])
        changed = [l.split("|")[0] for l in b0 if l not in a0]
        lines.append(f"native mail files changed during the watch: {changed}")
    noted = [p for p in procs if p["deferred"]]
    rows.append({"cell": cell, "panels_after_start": len(procs), "deferring": len(noted),
                 "attempt_lines": sum(len(p["attempts"]) for p in procs),
                 "max_attempt": max([x["attempt"] for p in procs for x in p["attempts"]] or [0]),
                 "gave_up": any(p["gave_up"] for p in procs), "repeated": any(p["repeated"] for p in procs),
                 "unresolved": [p["pid"] for p in noted if not p["finished"]],
                 "terminal": terminal,
                 "first_attempt": [(p["pid"], p["started_at"], p["attempts"][0]["at"] if p["attempts"] else None) for p in noted]})
    lines.append("")
lines.append("## table")
for r in rows:
    lines.append(json.dumps(r))
out.write_text("\n".join(lines) + "\n", encoding="utf-8")
print("\n".join(lines[-len(rows):]))
