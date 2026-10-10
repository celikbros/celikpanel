"""set3: generated views of the staged evidence (read-only over the evidence folder).

usage: summary.py EVIDENCE_DIR
Writes summary-per-cell.txt, part1-table.md, part2-table.md, checks-not-passed.txt, texts-en-tr.md and
native-facts.json into EVIDENCE_DIR and prints a short digest.
"""
import glob
import json
import os
import re
import sys

E = sys.argv[1].rstrip("/\\")
PLATFORMS = (("debian13", "Debian 13"), ("ubuntu", "Ubuntu 24.04"), ("arch", "Arch"),
             ("arch_snippet", "Arch, second reading with the owner's nginx snippet"))
ITEMS = (
    ("S1 preview and stored rows hold no hash-shaped value", r"^S1: the preview|no request_identities row holds a hash-shaped|no answer of a guarded route held a hash-shaped|journal lines of this cell hold no hash-shaped"),
    ("S1 imported mailbox authenticates with its original password", r"^S1: the imported mailbox|^S1: the same mailbox refuses"),
    ("P3 PostgreSQL failing hook -> reload_reread", r"answers 502 SERVICE_ACTION_FAILED \(reload_reread\)|\(reload hook\): the server did re-read"),
    ("P3 hook failing before the signal -> reload_not_reread", r"reload_not_reread|hook fails before the signal"),
    ("P3 reload of a stopped Postfix and Dovecot -> 409 not_running", r"\(not_running\)|\(reload while stopped\)"),
    ("P3b Postfix Stop with a refused main.cf -> success, master gone", r"Stop answers success and the master is gone"),
    ("P4 archive with the directory member imports completely", r"^P4: an archive that holds the directory member"),
    ("P4 failed files step -> 200 partial, IMPORT_PARTIAL, truthful lists", r"^P4 \((dotdot|symlink)\): the import answers 200|^P4 \((dotdot|symlink)\): `imported`|^P4: the replay of a partial"),
    ("P4 hostile members refused, nothing written outside", r"^P4 \((dotdot|absolute|symlink)\): nothing of the hostile|^P4 \((dotdot|symlink)\): the files step refuses|^P4 \(absolute\)"),
    ("P5 PHP site created, PHP executed, site deleted", r"^P5:"),
    ("P5 the import completes", r"^import \((i|ii|v)\)"),
    ("O9 CA unreachable -> 502 CERTIFICATE_ISSUE_FAILED authority_unreachable", r"^O9:"),
    ("O11 no server_setup_busy refusal in the service-action section", r"^no service action of this section was refused"),
    ("O10 sent database password not echoed; replay byte-identical", r"^O10 "),
    ("O14 MariaDB version shown is the server's", r"^O14 .*MariaDB"),
)


def load(path):
    try:
        with open(path, encoding="utf-8") as stream:
            return json.load(stream)
    except (OSError, ValueError):
        return None


def runs():
    found = []
    for result in sorted(glob.glob(os.path.join(E, "**", "run-*"), recursive=True)):
        if os.path.isdir(result):
            rel = os.path.relpath(result, E).replace("\\", "/")
            found.append((rel, result))
    return found


def text_of(path):
    try:
        return open(path, encoding="utf-8").read().strip()
    except OSError:
        return ""


def sections_of(run_dir):
    out = []
    for path in sorted(glob.glob(os.path.join(run_dir, "steps", "*", "section.json"))):
        value = load(path)
        if value:
            out.append((os.path.basename(os.path.dirname(path)), value))
    return out


def verdict(checks):
    if not checks:
        return "not run"
    if any(c["ok"] is False for c in checks):
        return "FAIL"
    if any(c["ok"] is None for c in checks):
        return "not established"
    return "pass"


all_runs = runs()
latest = {}
flagged = {}
for rel, path in all_runs:
    cell = rel.rsplit("/", 1)[0]
    flagged[rel] = bool((load(os.path.join(path, "result.json")) or {}).get("owner_placed_the_nginx_php_snippet"))
    latest[(cell, flagged[rel])] = (rel, path)     # run-a < run-b: the last one is the latest of its kind

summary, not_passed, texts, facts = [], [], {}, {}
part1 = {item: {} for item, _ in ITEMS}
regression = {}
part2_rows = []


def remember_text(status, body, catalogue, where):
    if not isinstance(body, dict) or not (body.get("code") or body.get("error")):
        return
    key = (status, body.get("code"), body.get("reason"), body.get("error"))
    entry = texts.setdefault(key, {"where": set(), "vars": [], "catalogue": {}, "message": body.get("message")})
    entry["where"].add(where)
    if body.get("vars") and body["vars"] not in entry["vars"] and len(entry["vars"]) < 4:
        entry["vars"].append(body["vars"])
    for name, value in (catalogue or {}).items():
        entry["catalogue"].setdefault(name, value)


for rel, path in all_runs:
    result = load(os.path.join(path, "result.json")) or {}
    cell = rel.rsplit("/", 1)[0]
    is_latest = latest[(cell, flagged[rel])][0] == rel
    start, end = text_of(os.path.join(path, "host", "wrapper.start.txt")), text_of(os.path.join(path, "host", "wrapper.end.txt"))
    harness = text_of(os.path.join(path, "host", "harness.txt"))
    summary.append(f"== {rel}  wrapper {start} - {end}  rc={text_of(os.path.join(path, 'host', 'wrapper.rc.txt'))}")
    summary.append(f"   overall={result.get('overall')} native_evidence={result.get('native_evidence')} outcome={(result.get('outcome') or {}).get('classification')} "
                   f"cell_kind={result.get('cell_kind', 'owner-update')}")
    summary.append(f"   {harness}")
    for step in result.get("steps", []):
        summary.append(f"   step {step.get('name'):<34} {step.get('verdict'):<13} {step.get('started_at')} - {step.get('finished_at')}"
                       + (f"  | {str(step.get('reason'))[:300]}" if step.get("reason") else ""))
    for finding in result.get("findings", []) or []:
        summary.append(f"   finding: {str(finding)[:400]}")
    platform = next((p for p, _ in PLATFORMS if p in cell), None)
    if flagged[rel]:
        platform = "arch_snippet"
    secs = sections_of(path)
    for step_name, section in secs:
        checks = section.get("checks", [])
        bad = [c for c in checks if c["ok"] is not True]
        summary.append(f"   section {section.get('section'):<22} {section.get('verdict'):<12} checks={len(checks)} not passed={len(bad)} "
                       f"{section.get('started_at')} - {section.get('finished_at')}")
        for c in bad:
            not_passed.append(f"{rel} {section.get('section')}: {'FAIL' if c['ok'] is False else 'not established'}: {c['name']}\n"
                              f"    {json.dumps(c.get('detail'), sort_keys=True, ensure_ascii=False)[:1400]}")
        if section.get("error"):
            not_passed.append(f"{rel} {section.get('section')}: ERROR: {str(section.get('error'))[:900]}")
        for call in section.get("calls", []):
            body = call.get("json") if isinstance(call.get("json"), dict) else None
            remember_text(call.get("status"), body, call.get("catalogue_texts"), f"{rel} {section.get('section')}")
        if is_latest and platform:
            regression.setdefault(section.get("section"), {})[platform] = f"{section.get('verdict')} ({len(checks)} checks, {len(bad)} not passed)"
            for item, pattern in ITEMS:
                matched = [c for c in checks if re.search(pattern, c["name"])]
                if matched:
                    part1[item].setdefault(platform, []).extend(matched)
            for key in ("engine_versions", "php_native", "php_page", "php_deleted", "certificate_failure", "sent_password", "busy_refusals",
                        "reload_hook", "reload_hook_before_signal", "mail_logins", "hostile", "archive_as_tar_writes_it", "previews",
                        "answers_searched", "journal_searched", "unit_facts"):
                if key in section and key != "unit_facts":
                    facts.setdefault(platform, {}).setdefault(section.get("section"), {})[key] = section[key]
    if "part2" in rel or (result.get("cell_kind") is None and result):
        outcome = result.get("outcome") or {}
        row = {"run": rel, "cell": cell.split("/")[-1], "wrapper": [start, end], "overall": result.get("overall"),
               "classification": outcome.get("classification"), "final": outcome.get("final_status"),
               "attempts": outcome.get("attempts"), "reboot": outcome.get("reboot"), "steps": {s["name"]: s["verdict"] for s in result.get("steps", [])},
               "findings": result.get("findings"), "request_id": result.get("request_id"), "kind": (result.get("kind") or {}).get("judged"),
               "deferred_mail": result.get("deferred_mail")}
        for name in ("post-update-facts", "post-return-facts", "terminal"):
            found = glob.glob(os.path.join(path, "steps", "*-" + name, "step.json"))
            if found:
                row[name] = (load(found[0]) or {})
        part2_rows.append(row)
    summary.append("")

lines = ["# Part 1: item x platform (latest run of each cell; generated by tools/summary.py)", "",
         "| Item | " + " | ".join(label for _, label in PLATFORMS) + " |", "| --- | --- | --- | --- | --- |"]
for item, _ in ITEMS:
    cells = []
    for platform, _label in PLATFORMS:
        checks = part1[item].get(platform, [])
        cells.append("not run" if not checks else f"{verdict(checks)} ({sum(1 for c in checks if c['ok'] is True)} of {len(checks)} checks)")
    lines.append(f"| {item} | " + " | ".join(cells) + " |")
lines += ["", "Regression: every section of the latest run of each cell.", "", "| Section | " + " | ".join(label for _, label in PLATFORMS) + " |",
          "| --- | --- | --- | --- | --- |"]
for section in sorted(regression):
    lines.append(f"| {section} | " + " | ".join(regression[section].get(p, "not run") for p, _ in PLATFORMS) + " |")
open(os.path.join(E, "part1-table.md"), "w", encoding="utf-8", newline="\n").write("\n".join(lines) + "\n")

lines = ["# Part 2: owner-started update from the published v0.1.0-alpha.81 (generated by tools/summary.py)", "",
         "| Run | Wrapper (UTC) | Overall | Outcome | Final state | Steps not passed |", "| --- | --- | --- | --- | --- | --- |"]
for row in part2_rows:
    final = row.get("final") or {}
    bad = [f"{k}: {v}" for k, v in row["steps"].items() if v not in ("passed", "observed", "skipped")]
    lines.append(f"| {row['run']} | {row['wrapper'][0][11:19]}-{row['wrapper'][1][11:19]} | {row['overall']} | {row['classification']} | "
                 f"{final.get('phase')}/{final.get('terminal_proof')} | {'; '.join(bad) or '-'} |")
lines.append("")
for row in part2_rows:
    facts2 = (row.get("post-update-facts") or {}).get("checks") or {}
    if facts2:
        a, b, c, d, e = (facts2.get(k) or {} for k in ("a_ledger", "b_guard", "c_version_tokens", "d_deferred_mail", "e_agreement"))
        lines += [f"## {row['run']}: after the verified update ({(row.get('post-update-facts') or {}).get('verdict')})",
                  f"- (a) ledger {a.get('schema_version')} ({a.get('ledger_rows')} rows), verdict {a.get('verdict')}; facts {json.dumps(a.get('facts'), sort_keys=True)}; "
                  f"`request_identities` {json.dumps(a.get('request_identities'), sort_keys=True)[:200]}",
                  f"- (b) without the header: {json.dumps((b.get('without_the_header') or {}), sort_keys=True, ensure_ascii=False)[:900]}; archives before/after/after-with-header "
                  f"{b.get('archives')}; with the header {json.dumps(b.get('with_the_header'), sort_keys=True)[:300]}; row {json.dumps(b.get('row'), sort_keys=True)}; "
                  f"refused and nothing changed: {b.get('refused_and_nothing_changed')}; works with the header: {b.get('works_with_the_header')}" + (f"; error {b.get('error')}" if b.get("error") else "")]
        for name, item in c.items():
            if isinstance(item, dict):
                lines.append(f"- (c) {name}: ok={item.get('ok')} measured={item.get('measured', True)} version {item.get('version_before')} -> {item.get('version_after')}; "
                             f"without a version: {json.dumps(item.get('without_a_version'), sort_keys=True, ensure_ascii=False)[:400]}; with: http {(item.get('with_the_version') or {}).get('http')}; "
                             + "; ".join(f"{k}={item[k]}" for k in ("native_crontab_lines_with_the_command", "native_postconf", "reason") if k in item))
            else:
                lines.append(f"- (c) {name}: {str(item)[:300]}")
        lines += [f"- (d) deferred mail: {json.dumps(d, sort_keys=True)[:600]}",
                  f"- (e) root CLI {e.get('cli_pair')}, recovery reader {e.get('api_pair')}, card judged {json.dumps(e.get('card_judged'), sort_keys=True)[:300]}", ""]
    back = (row.get("post-return-facts") or {}).get("checks") or {}
    if back:
        ledger, views = back.get("ledger") or {}, back.get("views") or {}
        card = views.get("update_card") or {}
        lines += [f"## {row['run']}: after the automatic return ({(row.get('post-return-facts') or {}).get('verdict')})",
                  f"- ledger {ledger.get('schema_version')} ({ledger.get('ledger_rows')} rows), verdict {ledger.get('verdict')}; facts {json.dumps(ledger.get('facts'), sort_keys=True)}",
                  f"- database against the pre-update digest: {back.get('database_against_the_pre_update_digest')}",
                  f"- root CLI {views.get('cli_pair')} (previous failure {views.get('cli_previous_failure')}); recovery reader http {views.get('recovery_api_http')} {views.get('api_pair')}",
                  f"- update status: {json.dumps(views.get('update_status'), sort_keys=True, ensure_ascii=False)[:700]}",
                  f"- update card (rendered from the returned build's own rules): {json.dumps(card, sort_keys=True, ensure_ascii=False)[:1200]}",
                  f"- offered again: {json.dumps(back.get('offered_again'), sort_keys=True, ensure_ascii=False)[:500]}",
                  f"- version: {json.dumps(back.get('version'), sort_keys=True)[:300]}",
                  f"- CLI text EN: {str((views.get('cli_text') or {}).get('en'))[:900]}",
                  f"- CLI text TR: {str((views.get('cli_text') or {}).get('tr'))[:900]}", ""]
open(os.path.join(E, "part2-table.md"), "w", encoding="utf-8", newline="\n").write("\n".join(lines) + "\n")

open(os.path.join(E, "summary-per-cell.txt"), "w", encoding="utf-8", newline="\n").write("\n".join(summary) + "\n")
open(os.path.join(E, "checks-not-passed.txt"), "w", encoding="utf-8", newline="\n").write(
    ("\n".join(not_passed) if not_passed else "every check of every staged run passed") + "\n")
out = ["# Verbatim answers of the settings and request-identity cells (status, code, reason; the API's sentence; the values it carried;",
       "# the catalogue sentences of this build's screens, EN and TR). Generated by tools/summary.py; the driver renders no screen.", ""]
for (status, code, reason, error), entry in sorted(texts.items(), key=lambda kv: (str(kv[0][1]), str(kv[0][2]), str(kv[0][0]))):
    out.append(f"## {status} {code}" + (f" / {reason}" if reason else ""))
    out.append(f"- API: {error}")
    if entry.get("message"):
        out.append(f"- message: {entry['message']}")
    for item in entry["vars"]:
        out.append(f"- vars: {json.dumps(item, sort_keys=True, ensure_ascii=False)[:700]}")
    for name, value in entry["catalogue"].items():
        out.append(f"- `{name}` EN: {value.get('en')}")
        out.append(f"- `{name}` TR: {value.get('tr')}")
    out.append(f"- seen in: {', '.join(sorted(entry['where'])[:6])}" + (" ..." if len(entry["where"]) > 6 else ""))
    out.append("")
open(os.path.join(E, "texts-en-tr.md"), "w", encoding="utf-8", newline="\n").write("\n".join(out) + "\n")
json.dump(facts, open(os.path.join(E, "native-facts.json"), "w", encoding="utf-8", newline="\n"), indent=1, sort_keys=True, ensure_ascii=False)
print(f"runs: {len(all_runs)}; checks not passed: {len(not_passed)}; distinct answers: {len(texts)}; part 2 rows: {len(part2_rows)}")
