#!/usr/bin/env python3
"""upd12 read-only: the deferred startup mail retry of every Panel process in one cell run, against the update's own
timeline. usage: dml12.py RUN_DIR   (a staged run folder, or a lab's evidence/<node>/upd1/<run>/ folder)"""
import datetime as dt
import glob
import json
import os
import re
import sys

run = sys.argv[1].rstrip("/") + "/"
NOTE = "the Panel retries this by itself every 30 seconds for up to 10 minutes"
LINE = re.compile(r"^(\S+) \S+ (\S+?)(?:\[(\d+)\])?: (?:\d{4}/\d\d/\d\d \d\d:\d\d:\d\d )?(.*)$")


def ts(value):
    return dt.datetime.fromisoformat(value)


def one(pattern):
    found = sorted(glob.glob(run + pattern))
    return found[-1] if found else None


collect = one("steps/*-collect/journal-product.txt")
watch = one("steps/*-deferred-mail-watch/deferred-mail.json")
result = json.load(open(run + "result.json"))
print("run", run)
print("overall", result.get("overall"), "outcome", (result.get("outcome") or {}).get("classification"))
print("steps", [(s["name"], s["verdict"], s.get("started_at", "")[11:19], s.get("finished_at", "")[11:19])
                for s in result["steps"]])
lines = open(collect, errors="replace").read().splitlines() if collect else []
parsed = []
for line in lines:
    m = LINE.match(line)
    if m:
        parsed.append((ts(m[1]), m[2], m[3], m[4], line))

# Panel processes
procs = []
for at, unit, pid, msg, line in parsed:
    if unit == "panel" and msg.startswith("Starting CelikPanel Backend..."):
        procs.append({"pid": pid, "start": at, "lines": []})
    for p in procs:
        if unit == "panel" and pid == p["pid"]:
            p["lines"].append((at, msg))
print("\n== Panel processes (journal-product of collect)")
for p in procs:
    start = p["start"]
    print(f"-- panel[{p['pid']}] start {start.isoformat()}")
    for at, msg in p["lines"]:
        if (msg.startswith("certificate startup reconcile") or msg.startswith("milter") or msg.startswith("Panel ready")
                or msg.startswith("startup mail work") or "mail SNI" in msg or msg.startswith("Agent is unavailable")
                or msg.startswith("Connected to Agent")):
            print(f"   +{(at - start).total_seconds():7.2f}s {at.isoformat()[11:26]} {msg[:900]}")

# Side effects and the update's own units after the first Panel start of the update window
first = procs[-1]["start"] if procs else None
if procs:
    # the earliest Panel process that logged the retry note (inside the update), else the last one
    noted = [p for p in procs if any(NOTE in m for _, m in p["lines"])]
    if noted:
        first = noted[0]["start"]
print("\n== update / recovery units and mail side effects from", first.isoformat() if first else None)
for at, unit, pid, msg, line in parsed:
    if first and at < first - dt.timedelta(seconds=90):
        continue
    keep = (unit.startswith("postfix") or unit in ("postmap", "postalias", "newaliases", "dovecot")
            or "self-update" in msg or "release-recovery.service" in msg or "owner-update-observer" in msg
            or "lab-owner-retry" in msg or "recovery-fault" in msg
            or (unit == "agent" and re.search(r"(?i)mail|milter|sni|tls|lease|mutation", msg)))
    if keep:
        rel = f"{(at - first).total_seconds():+8.2f}s" if first else ""
        print(f"   {rel} {at.isoformat()[11:26]} {unit}[{pid}] {msg[:300]}")

# Status timeline from the track samples (root CLI and Panel API)
print("\n== status timeline (track samples, first time each CLI state was seen)")
seen = {}
for f in sorted(glob.glob(run + "steps/*track*/samples/*.json")):
    d = json.load(open(f))
    c = ((d.get("cli") or {}).get("json") or {}).get("stdout")
    try:
        cj = json.loads(c or "null") or {}
    except ValueError:
        cj = {}
    key = (cj.get("observation"), cj.get("phase"), cj.get("terminal_proof"), cj.get("automatic_recovery"))
    u = ((d.get("update_status") or {}).get("body") or {}).get("status") if isinstance((d.get("update_status") or {}).get("body"), dict) else None
    if key not in seen:
        seen[key] = d.get("utc")
        print("  ", d.get("utc"), f.split("/steps/")[1][:28], key, "panel_api=", u)

if watch:
    w = json.load(open(watch))
    print("\n== deferred-mail-watch", watch.replace(run, ""))
    print("watch_seconds", w.get("watch_seconds"), "polls", len(w.get("polls") or []), "settle", json.dumps(w.get("settle")))
    for p in w["processes"]:
        print("  pid", p["pid"], "start", p["started_at"], "ready", p["ready_at"], "deferred", p["deferred"],
              "finished", p["finished"], "gave_up", p["gave_up"], "repeated", p["repeated"])
        for a in p["attempts"]:
            print("     attempt", a["attempt"], "of", a["of"], a["at"], "|", a["text"][:900])
    facts = {f["label"]: f.get("lines") or [f.get("unavailable")] for f in w.get("mail_file_facts", [])}
    a, b = facts.get("watch-start", []), facts.get("watch-end", [])
    print("  mail file facts at watch start:")
    for l in a:
        print("    ", l)
    print("  changed by watch end:")
    for l in b:
        if l not in a:
            print("    ", l)
    if a == b:
        print("     (none)")
    print("  polls:", [(p.get("at", "")[11:19], p.get("attempts"), p.get("finished")) for p in w.get("polls", [])])
