"""set2: the README's tables are generated from the staged cells and placed into the hand-written text.

usage: mkreadme.py EVIDENCE_DIR TEMPLATE -> EVIDENCE_DIR/README.md
Markers in the template: <!--CELLS-->, <!--GROUP-A-->, <!--GROUP-B-->, <!--GROUP-C-->, <!--DISK-->
"""
import glob
import json
import os
import sys

E, TEMPLATE = sys.argv[1], sys.argv[2]


def load(path):
    with open(path, encoding="utf-8") as stream:
        return json.load(stream)


def text(path):
    try:
        return open(path, encoding="utf-8", errors="replace").read().strip()
    except OSError:
        return ""


runs = []
for directory in sorted(glob.glob(os.path.join(E, "*", "run-*"))):
    name = os.path.relpath(directory, E).replace("\\", "/")
    result = load(os.path.join(directory, "result.json")) if os.path.exists(os.path.join(directory, "result.json")) else None
    runs.append((name, directory, result))
final = {}
for name, directory, result in runs:
    if result is not None:
        final[name.split("/")[0]] = (name, directory, result)     # the latest run of a cell that has a result


def sections(directory):
    return {load(p)["section"]: load(p) for p in sorted(glob.glob(os.path.join(directory, "steps", "*", "section.json")))}


# -- cells -----------------------------------------------------------------------------------------------------------
lines = ["| # | Cell / run | Lab, SSH port | Harness copy | Wrapper (UTC) | Driver steps (UTC) | Overall |", "| --- | --- | --- | --- | --- | --- | --- |"]
ordered = sorted(runs, key=lambda item: text(os.path.join(item[1], "host", "wrapper.start.txt")))
for number, (name, directory, result) in enumerate(ordered, 1):
    host = os.path.join(directory, "host")
    lab = (text(os.path.join(host, "lab.txt")).splitlines() or [""])[0].replace("lab=/var/tmp/cp-release-drill-", "")
    job = text(os.path.join(host, "job.sh")).split()
    port = job[-2] if job else ""
    harness = text(os.path.join(host, "harness.txt")).split(" ")[0].replace("harness=/var/tmp/cp-set2-run/harness-", "")
    start, end = text(os.path.join(host, "wrapper.start.txt")), text(os.path.join(host, "wrapper.end.txt"))
    if result:
        steps = f"{result['steps'][0]['started_at'][11:19]}-{result['steps'][-1]['finished_at'][11:19]}"
        bad = [s["name"] for s in result["steps"] if s["verdict"] in ("failed", "inconclusive")]
        overall = f"`{result['overall']}`" + (f" ({', '.join(bad)})" if bad else "")
    else:
        steps, overall = "-", "stopped by hand; no result was written"
    lines.append(f"| {number} | {name} | {lab}, {port} | {harness} | {start[11:19]}-{end[11:19]} | {steps} | {overall} |")
CELLS = "\n".join(lines)

# -- group A ------------------------------------------------------------------------------------------------------------
GROUP_A = text(os.path.join(E, "group-a-settings-writes.md")).split("\n", 2)[2].strip()

# -- group B: one compact matrix over the final settings runs ---------------------------------------------------------------
platforms = [(cell, final[cell]) for cell in ("set2-debian13", "set2-ubuntu") if cell in final]
table = {}
order = []
for cell, (name, directory, result) in platforms:
    for action in sections(directory).get("S8-service-actions", {}).get("actions", []):
        key = (action["service"], action["situation"] or "healthy", action["action"])
        if key not in table:
            table[key] = {}
            order.append(key)
        code = f"{action['status']}" + (f" {action['code']}" + (f"/{action['reason']}" if action["reason"] else "") if action["code"] else
                                         f" {action['answer'].get('applied') or 'success'}")
        match = {True: "matches", False: "**DOES NOT MATCH**", None: "unknown (truth: " + ("took effect" if action["truth_action_took_effect"] else "no effect") + ")"}[action["matches"]]
        table[key][cell] = f"{code}: {match}"
lines = ["| Service | Situation | Action | " + " | ".join(f"{final[c][0]}" for c, _ in platforms) + " |", "| --- | --- | --- |" + " --- |" * len(platforms)]
for key in order:
    lines.append(f"| {key[0]} | {key[1]} | {key[2]} | " + " | ".join(table[key].get(c, "-") for c, _ in platforms) + " |")
counts = []
for cell, (name, directory, result) in platforms:
    actions = sections(directory).get("S8-service-actions", {}).get("actions", [])
    counts.append(f"{name}: {len(actions)} actions, {sum(1 for a in actions if a['matches'] is True)} answers match the service, "
                  f"{sum(1 for a in actions if a['matches'] is False)} do not, {sum(1 for a in actions if a['matches'] is None)} answered as unknown; "
                  f"{sum(len(a.get('refused_while_busy') or []) for a in actions)} refusals `server_setup_busy` before an action was sent")
GROUP_B = "\n".join(lines) + "\n\n" + "\n".join("- " + c for c in counts)

# -- group C ----------------------------------------------------------------------------------------------------------------
routes = [("domain-database-mariadb", "`domains/{id}/databases` (MariaDB)"), ("domain-database-postgresql", "`domains/{id}/databases` (PostgreSQL)"),
          ("admin-account-mariadb", "`database-servers/{id}/admin-account` (MariaDB)"),
          ("admin-account-postgresql", "`database-servers/{id}/admin-account` (PostgreSQL)"),
          ("server-database-mariadb", "`database-servers/{id}/databases` (MariaDB)"),
          ("server-database-postgresql", "`database-servers/{id}/databases` (PostgreSQL)"),
          ("backup", "`domains/{id}/backups` (manual backup)"), ("restore", "`domains/{id}/backups/restore`"),
          ("letsencrypt", "`domains/{id}/ssl/letsencrypt` (guard only)"), ("vpn-peer", "`vpn/peers`"), ("import", "`import/cpanel/apply`"),
          ("rows", "`request_identities` rows")]
parts = []
for cell in ("rid-debian13", "rid-ubuntu", "rid-arch"):
    if cell not in final:
        continue
    name, directory, result = final[cell]
    secs = sections(directory)
    lines = [f"**{name}** (overall `{result['overall']}`; sections " + ", ".join(
        f"{k.split('-')[0]} {v['verdict']}" for k, v in secs.items()) + ")", "",
        "| Route (POST) | i | ii | iii | iv | v | vi | vii | Times (UTC) |", "| --- | --- | --- | --- | --- | --- | --- | --- | --- |"]
    for route, label in routes:
        cells = result["matrix"].get(route, {})
        times = result.get("times", {}).get(route, {})
        if route == "restore" and "C8-restore" in secs:
            times = secs["C8-restore"]
        if route == "rows" and "C9-identities" in secs:
            times = secs["C9-identities"]
        when = f"{str(times.get('started_at', ''))[11:19]}-{str(times.get('finished_at', ''))[11:19]}" if times else "-"
        lines.append(f"| {label} | " + " | ".join(cells.get(r, "-") for r in ("i", "ii", "iii", "iv", "v", "vi", "vii")) + f" | {when} |")
    parts.append("\n".join(lines))
GROUP_C = "\n\n".join(parts)

# -- disk ---------------------------------------------------------------------------------------------------------------------
readings = []
for line in text(os.path.join(E, "host", "c-drive.txt")).lstrip("﻿").splitlines():
    stamp, rest = line.split(" ", 1)
    label, gib = rest.rsplit(" free_bytes=", 1)[0], rest.rsplit("free_GiB=", 1)[1]
    readings.append((stamp, label, float(gib)))
DISK = "; ".join(f"{gib:g} ({label}, {stamp[11:19]}Z)" for stamp, label, gib in readings)
if readings:
    DISK += f". Lowest reading: **{min(r[2] for r in readings):g} GiB**."

template = open(TEMPLATE, encoding="utf-8").read()
for marker, value in (("<!--CELLS-->", CELLS), ("<!--GROUP-A-->", GROUP_A), ("<!--GROUP-B-->", GROUP_B), ("<!--GROUP-C-->", GROUP_C),
                      ("<!--DISK-->", DISK)):
    assert marker in template, marker
    template = template.replace(marker, value)
open(os.path.join(E, "README.md"), "w", encoding="utf-8", newline="\n").write(template)
print("README.md written:", len(template.splitlines()), "lines; longest line", max(len(l) for l in template.splitlines()))
