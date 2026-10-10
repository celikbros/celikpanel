"""set7: one lab's update timeline from its staged raw files (browser samples, browser network log, host probe,
guest probe, guest journal), every instant in UTC on the host clock (guest instants corrected by the driver's measured
guest-minus-host difference). usage: timeline.py STAGED_LAB_DIR  -> markdown table on stdout"""
import datetime
import glob
import json
import os
import re
import sys

lab = sys.argv[1]
B = os.path.join(lab, "browser", "update")
rows = []


def add(at, source, what):
    rows.append((at, source, what))


def iso(epoch):
    return datetime.datetime.fromtimestamp(epoch, datetime.timezone.utc).strftime("%H:%M:%S.%f")[:12]


for line in open(os.path.join(B, "events.jsonl"), encoding="utf-8"):
    e = json.loads(line)
    if e["ev"] in ("check-clicked", "offered", "start-clicked", "steady"):
        add(e["at"][11:23], "browser", e["ev"])
prev = {}
for line in open(os.path.join(B, "samples-update.jsonl"), encoding="utf-8"):
    s = json.loads(line)
    state = {
        "document": "same document" if s.get("document") else "NEW document (marker gone: reload or replacement)",
        "screen": ("full-screen replacement: " + " / ".join(s.get("headings") or [])) if not (s.get("settings_updates_panel") or {}).get("mounted")
        else "Settings page mounted, section " + str(s.get("selected_tab")),
        "hold": ("hold layer over the page: " + s["hold"].split("\n")[0]) if s.get("hold") else "no hold layer",
        "held": "page inert (data-access-hold=blocked)" if s.get("held_subtree") else "page not inert",
        "update": ("update window: " + (s.get("update_window") or "").split("\n\n")[1][:90]) if s.get("update_window") and "\n\n" in s["update_window"] else ("update window: " + (s.get("update_window") or "")[:60] if s.get("update_window") else "no full update window"),
        "path": s.get("path"),
    }
    changed = [v for k, v in state.items() if prev.get(k) != v]
    if changed:
        add(s["at"][11:23], "screen", "; ".join(changed) + f" [{s.get('shot')}]")
    prev = state
for line in open(os.path.join(B, "network-update.jsonl"), encoding="utf-8"):
    e = json.loads(line)
    if e["ev"] == "failed" and "/api/" in e.get("url", ""):
        add(e["at"][11:23], "network", f"{e['method']} {re.sub(r'=[^&]*', '=...', e['url'])} FAILED {e.get('error')}")
    elif e["ev"] == "response" and "/api/" in e.get("url", "") and ("license/access" in e["url"] or "update/status" in e["url"] or "recovery/status" in e["url"]):
        add(e["at"][11:23], "network", f"{e['method']} {re.sub(r'=[^&]*', '=...', e['url'])} {e['status']}")
    elif e["ev"] == "navigated":
        add(e["at"][11:23], "network", f"navigation to {re.sub(r'_cp_update=[^&]*', '_cp_update=...', e['url'])}")
prevp = None
for line in open(os.path.join(B, "host-probe.jsonl"), encoding="utf-8"):
    e = json.loads(line)
    up = e["status"].isdigit()
    if up != prevp:
        add(e["at"][11:23], "host probe", ("answers " + e["status"]) if up else ("no answer " + e["status"]))
    prevp = up
timeline = glob.glob(os.path.join(lab, "driver", "steps", "*set7-guest-timeline", "step.json"))[0]
checks = json.load(open(timeline))["checks"]
skew = checks.get("guest_minus_host_seconds") or 0.0
for t in checks.get("probe_transitions") or []:
    add(iso(t["guest_epoch"] - skew), "guest probe", f"{t['state']} ({t['status']})")
journal = os.path.join(os.path.dirname(timeline), "journal-celikpanel-units.txt")
first = min(r[0] for r in rows)
last = max(r[0] for r in rows)
for line in open(journal, encoding="utf-8"):
    m = re.match(r"(\d{4}-\d\d-\d\dT(\d\d:\d\d:\d\d\.\d{6}))\+00:00 \S+ (\S+): (.*)", line)
    if not m:
        continue
    when = datetime.datetime.fromisoformat(m.group(1)).replace(tzinfo=datetime.timezone.utc).timestamp() - skew
    at = iso(when)
    if not (first <= at <= last) or "release-recovery.service" in line or "lab-set7-probe" in line:
        continue
    text = m.group(4)
    if re.search(r"SIGSTOP|Stopping celikpanel-(panel|agent)|Stopped celikpanel-(panel|agent)|Starting celikpanel-(panel|agent|self-update)|Started celikpanel-(panel|agent)|startup listener|self-update-.*Deactivated", text):
        add(at, "guest journal", re.sub(r"[0-9a-f]{32}", "<request>", text)[:150])
rows.sort()
print(f"guest clock minus host clock (driver, measured once after the window): {skew:+.3f} s; guest instants below are corrected by it")
print()
print("| UTC (host) | source | what |")
print("| --- | --- | --- |")
for at, source, what in rows:
    print(f"| {at} | {source} | {what.replace('|', '/')} |")
