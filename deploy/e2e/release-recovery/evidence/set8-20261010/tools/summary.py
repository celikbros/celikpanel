"""set8: one table per lab from the driver's section records (read only). usage: summary.py RUN_DIR [--detail]"""
import glob
import json
import os
import sys

root = sys.argv[1]
detail = "--detail" in sys.argv
for path in sorted(glob.glob(os.path.join(root, "steps", "*", "section.json"))):
    s = json.load(open(path))
    print("##", os.path.basename(os.path.dirname(path)), s.get("verdict"), s.get("error") or "")
    for c in s.get("checks", []):
        if c["ok"] is not True:
            print("  CHECK", c["ok"], c["name"][:150])
    for r in s.get("runs", []) or []:
        if "error" in r:
            print("  RUN", r["run"], "ERROR", r["error"][:300]); continue
        v = r["vhost"]
        print("  RUN %s %s/%s stopped=%s -> %s | class=%s inode_changed=%s | http edit=%s after=%s %s | reload_lines=%d workers_changed=%s | trig=%s %s"
              % (r["run"], r["owner_action"], r["trigger"], r["panel_stopped_while_the_owner_edits"], r["provisional_verdict"],
                 r["class"], r["inode_changed"], r["http"]["after_the_edit"]["owner_text_served"],
                 r["http"]["after_the_trigger"]["owner_text_served"], r["http"]["after_the_trigger"]["status"],
                 len(r["nginx"]["reload_lines"]), r["nginx"]["workers_changed_after_the_edit"],
                 r["trigger"].get("ok"), r["trigger"].get("status") or r["trigger"].get("reconcile_lines_seen")))
        print("     after:", json.dumps(v["after"])[:400])
        if r.get("file_before_the_trigger_equals_the_edit") is not None:
            print("     unchanged-before-trigger:", r["file_before_the_trigger_equals_the_edit"])
        print("     panel journal:", [l[33:260] for l in r["journal"]["panel"]][:6])
        print("     agent journal:", [l[33:260] for l in r["journal"]["agent"]][:6])
        print("     owner view:", json.dumps(r["owner_view"])[:600])
        if detail:
            print("     http:", json.dumps(r["http"])[:600])
    for key in ("f", "g"):
        if key in s:
            print("  ", key, json.dumps(s[key], default=str)[:6000])
    for n in s.get("notes", []):
        print("  NOTE", n["note"][:300])
