"""set8: the scenario x platform tables of the README, generated from the staged driver records (read only).

The Panel's own texts of a lab are: the creation render (S0), the start render (S0's reference restart, when the
run has one) and the bytes the Panel wrote in run e1 into a path where no file existed (provably the Panel's text).
A run's file after the trigger is "owner's bytes kept" when its SHA-256 is the edited file's, "Panel text written"
when it is one of the Panel's texts, otherwise "other". Reported = an answer of the Panel's API an owner sees
(Domains row, General, Dashboard) names the change, or a new audit entry names a vhost, nginx, owner or config.
usage: table.py EVIDENCE_DIR > cells.md
"""
import glob
import json
import os
import sys

E = sys.argv[1]
rows, frows, grows = [], [], []
for run_dir in sorted(glob.glob(os.path.join(E, "*", "run-*", "driver"))):
    group = os.path.relpath(os.path.dirname(run_dir), E).replace("\\", "/")
    s0 = json.load(open(glob.glob(os.path.join(run_dir, "steps", "*-s0-sites", "section.json"))[0], encoding="utf-8"))
    creation = s0["baseline"]["vhost"]["sha256"]
    reference = ((s0.get("reference") or {}).get("vhost") or {}).get("sha256")
    runs = []
    for path in sorted(glob.glob(os.path.join(run_dir, "steps", "*-owner-edit", "section.json"))):
        runs += json.load(open(path, encoding="utf-8"))["runs"]
    e1 = next((r for r in runs if r.get("run") == "e1"), {})
    panel = {creation, reference, ((e1.get("vhost") or {}).get("after") or {}).get("vhost", {}).get("sha256")} - {None}
    for r in runs:
        if "error" in r:
            rows.append((group, r["run"], "ERROR " + r["error"][:120]) + ("",) * 8)
            continue
        e, a = r["vhost"]["edited"]["vhost"], r["vhost"]["after"]["vhost"]
        if r["owner_action"] == "remove":
            kept = not a.get("exists") and not r["vhost"]["after"]["enabled"].get("exists")
            cls = "owner's removal kept" if kept else ("recreated with the Panel's text" if a.get("sha256") in panel else "other")
        else:
            cls = ("owner's bytes kept" if a.get("sha256") == e.get("sha256") else
                   "Panel text written" if a.get("sha256") in panel else "other bytes")
        reported = bool(r["owner_view"]["words_found"]) or any(
            any(w in json.dumps(x).lower() for w in ("vhost", "nginx", "owner", "config")) for x in r["owner_view"]["new_audit_entries"])
        if not r["panel_active"]:
            verdict = "Panel failed to start"
        elif cls in ("owner's bytes kept", "owner's removal kept"):
            verdict = "kept and reported" if reported else "kept silently"
        elif cls in ("Panel text written", "recreated with the Panel's text"):
            verdict = "overwritten and reported" if reported else "overwritten silently"
        else:
            verdict = "unknown"
        line = (r["journal"]["panel"] or ["(no Panel line naming a vhost or nginx)"])[-1]
        line = line.split(": ", 2)[-1][:110] if "panel[" in line else line
        audit = ", ".join(x.get("action", "?") for x in r["owner_view"]["new_audit_entries"]) or "none"
        http = r["http"]
        kind = r["trigger"]["kind"] if isinstance(r["trigger"], dict) else str(r["trigger"])
        trig = "General settings save" if "general" in kind else ("Panel restart" if "restart" in kind else "Panel start")
        if cls == "recreated with the Panel's text":
            verdict += " (recreated)"
        rows.append((group, r["run"], f"{r['owner_action']} / {trig}" + (" (Panel stopped while editing)" if r["panel_stopped_while_the_owner_edits"] else " (Panel running)"),
                     cls, f"{e.get('inode')}->{a.get('inode')}, {e.get('owner')}:{e.get('group')}->{a.get('owner')}:{a.get('group')}",
                     "yes" if r["nginx"]["workers_changed_after_the_edit"] else "no",
                     f"{http['path']}: {http['after_the_edit']['status']} {'owner' if http['after_the_edit']['owner_text_served'] else '-'} -> {http['after_the_trigger']['status']} {'owner' if http['after_the_trigger']['owner_text_served'] else '-'}",
                     line, audit, verdict, r["files"]["after"] or "(absent)"))
    f = json.load(open(glob.glob(os.path.join(run_dir, "steps", "*-f-owner-pool", "section.json"))[0], encoding="utf-8"))["f"]
    st, ini = f["stages"], f["memory_limit_served"]
    frows.append((group, *(f"{len(st[k]['owner_lines_present'])}/2, {ini[k].split('=')[-1]}, inode {st[k]['inode']}" for k in
                           ("edited", "after_the_panel_start", "after_the_pool_save", "after_the_php_switch")),
                  f"{f['pool_save']['status']} {json.dumps(f['pool_save']['answer'])}", f"{f['php_switch']['kind'].split(' (')[0]} {f['php_switch']['to']}: {f['php_switch']['status']}",
                  ", ".join(x.get("action", "?") for x in f["new_audit_entries"]) or "none"))
    g = json.load(open(glob.glob(os.path.join(run_dir, "steps", "*-g-immutable", "section.json"))[0], encoding="utf-8"))["g"]
    ps = g["per_site"]
    touched = g.get("vhost_touched_by_the_start (inode, ctime or bytes changed)") or {
        d: any(ps[d]["locked"]["vhost"].get(k) != ps[d]["after_the_start"]["vhost"].get(k) for k in ("inode", "ctime_ns", "sha256")) for d in ps}
    grows.append((group, g["lsattr_after_lock"], g["trigger"].get("is_active"), str((g["trigger"].get("api") or {}).get("answering")),
                   (g["journal"]["panel"] or ["-"])[-1].split(": ", 2)[-1][:200],
                   ", ".join(f"{d}: {'touched' if t else 'untouched'}" for d, t in touched.items()),
                   f"{ps['set8-a.test']['locked']['enabled'].get('kind')} -> {ps['set8-a.test']['after_the_start']['enabled'].get('kind') or 'absent'}",
                   "no" if g["nginx"]["after_the_start"]["workers"] == g["nginx"]["locked"]["workers"] else "yes",
                   ", ".join(f"{d} {v['status']}" for d, v in g["http"]["after_the_start"].items()),
                   ", ".join(f"{d} {v['status']}" for d, v in g["http"]["after_an_nginx_reload"].items()),
                   "none" if not g["owner_view"] else (", ".join(x.get("action", "?") for x in g["owner_view"]["new_audit_entries"]) or "none")
                   + "; domain row " + ("unchanged" if g["owner_view"]["a_row"] == g["owner_view"]["a_row_before"] else "CHANGED")))

print("# Runs, generated by tools/table.py from the staged driver records\n")
print("## Vhost scenarios a-e\n")
print("| Lab | Run | Owner action / trigger | File after the trigger | inode, owner:group (edited -> after) | nginx reloaded after the edit | HTTP (after edit -> after trigger) | Last Panel journal line naming a vhost | New audit entries | D-022 verdict | File after (raw) |")
print("|" + " --- |" * 11)
for r in rows:
    print("| " + " | ".join(str(x).replace("|", "\\|") for x in r) + " |")
print("\n## Scenario f, the PHP-FPM pool (owner's lines present / memory_limit served / inode)\n")
print("| Lab | After the owner's edit | After the Panel start | After a pool save | After the PHP 'switch' | Pool save answer | PHP switch | New audit entries |")
print("|" + " --- |" * 8)
for r in frows:
    print("| " + " | ".join(str(x).replace("|", "\\|") for x in r) + " |")
print("\n## Scenario g, chattr +i on A's vhost, then the Panel started\n")
print("| Lab | lsattr | Panel unit after start | API answering | Panel journal | Vhost files touched by the start | A's enabled link | nginx reloaded by the start | probe after start | probe after a later nginx reload | Owner view |")
print("|" + " --- |" * 11)
for r in grows:
    print("| " + " | ".join(str(x).replace("|", "\\|") for x in r) + " |")
