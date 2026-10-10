"""set5: generated views of the staged evidence (read-only over the evidence folder and over set3's Part 2).

usage: summary.py EVIDENCE_DIR
Writes into EVIDENCE_DIR: summary-per-cell.txt, part2-table.md (set3's generator, so the two tables read alike),
compare-with-set3.md, item9.md, m10.md, pinning.md, timeline.md, sampler-gaps.txt, checks-all.txt, checks-not-passed.txt and
facts.json; prints a short digest. A cell is PASS only when every step of set3's method ran and passed (or was
`observed` / `skipped` exactly as in set3) and the outcome and final state are the expected ones; the steps set5 added
are judged beside it, not inside it.
"""
import datetime as dt
import glob
import json
import os
import re
import sys

E = sys.argv[1].rstrip("/\\")
SET3 = os.path.join(os.path.dirname(E), "set3-20261012", "part2-alpha81")
CELLS = ("upd1-debian13-good", "upd1-ubuntu-good", "upd1-arch-good", "upd1-debian13-defective", "upd1-ubuntu-defective",
         "upd1-arch-defective", "upd1-debian13-startcheck", "upd1-debian13-owner-continuation",
         "upd1-ubuntu-owner-continuation", "upd1-debian13-mgmt-off-reboot")
EXPECTED = {"good": ("update-verified", "succeeded", "update_verified"),
            "defective": ("recovered-automatically", "recovered", "rollback_verified"),
            "startcheck": ("recovered-automatically", "recovered", "rollback_verified"),
            "owner-continuation": ("recovered-after-owner-continuation", "succeeded", "update_verified"),
            "mgmt-off-reboot": ("update-verified", "succeeded", "update_verified")}
ADDED = ("set5-name-pinning", "set4-php-site-before-the-update", "set4-php-site-after-the-update", "M10-postfix-stop",
         "set5-name-pinning-at-the-end")
OK = ("passed", "observed", "skipped")


def load(path):
    try:
        with open(path, encoding="utf-8") as stream:
            return json.load(stream)
    except (OSError, ValueError):
        return None


def text_of(path):
    try:
        return open(path, encoding="utf-8").read().strip()
    except OSError:
        return ""


def step_file(run_dir, name, file="step.json"):
    found = sorted(glob.glob(os.path.join(run_dir, "steps", "*-" + name, file)))
    return found[0] if found else None


def rel(path):
    return os.path.relpath(path, E).replace("\\", "/") if path else None


def kind_of(cell):
    return cell.split("-", 2)[2]


def stamp(text):
    try:
        return dt.datetime.strptime(text, "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=dt.timezone.utc).timestamp()
    except (TypeError, ValueError):
        return None


def final_of(result):
    final = (result.get("outcome") or {}).get("final_status") or {}
    return (final.get("phase"), final.get("terminal_proof")) if isinstance(final, dict) else (None, None)


def judge(cell, result, reference):
    """PASS / FAIL / NOT-MEASURED for one run by set3's criteria, with the reasons."""
    if not result:
        return "NOT-MEASURED", ["the run has no result.json"]
    steps = {s["name"]: s for s in result.get("steps", [])}
    base_steps = [s for s in result.get("steps", []) if s["name"] not in ADDED]
    reasons = []
    wanted = EXPECTED[kind_of(cell)]
    got = ((result.get("outcome") or {}).get("classification"),) + final_of(result)
    not_ok = [f"{s['name']}: {s['verdict']}" for s in base_steps if s["verdict"] not in OK]
    if steps.get("set5-name-pinning", {}).get("verdict") != "passed":
        return "NOT-MEASURED", ["the names were not pinned, so the cell did not run"]
    if reference:
        names3 = [s["name"] for s in reference.get("steps", [])]
        missing = [n for n in names3 if n not in steps]
        if missing:
            reasons.append("steps of set3 that did not run here: " + ", ".join(missing))
        for s3 in reference.get("steps", []):
            mine = steps.get(s3["name"])
            if mine and mine["verdict"] != s3["verdict"] and mine["verdict"] in OK:
                reasons.append(f"{s3['name']}: {mine['verdict']} here, {s3['verdict']} in set3")
    failed = [x for x in not_ok if x.endswith(": failed")]
    if failed or (got != wanted and "terminal" in steps and steps["terminal"]["verdict"] in OK + ("failed",)):
        return "FAIL", not_ok + ([f"outcome {got}, expected {wanted}"] if got != wanted else []) + reasons
    if not_ok or got != wanted:
        return "NOT-MEASURED", not_ok + ([f"outcome {got}, expected {wanted}"] if got != wanted else []) + reasons
    return "PASS", reasons


runs = []
for cell in CELLS:
    for run_dir in sorted(glob.glob(os.path.join(E, "update-alpha81", cell, "run-*"))):
        runs.append((cell, os.path.basename(run_dir), run_dir))
latest = {}
for cell, run, run_dir in runs:
    latest[cell] = (run, run_dir)

facts = {"cells": {}, "runs": {}}
summary, checks_all, not_passed = [], [], []
part2_rows = []

for cell, run, run_dir in runs:
    name = f"update-alpha81/{cell}/{run}"
    result = load(os.path.join(run_dir, "result.json")) or {}
    reference = load(os.path.join(SET3, cell, "run-a", "result.json")) or {}
    verdict, reasons = judge(cell, result, reference)
    start, end = text_of(os.path.join(run_dir, "host", "wrapper.start.txt")), text_of(os.path.join(run_dir, "host", "wrapper.end.txt"))
    outcome = result.get("outcome") or {}
    record = {"cell": cell, "run": run, "wrapper": [start, end], "wrapper_rc": text_of(os.path.join(run_dir, "host", "wrapper.rc.txt")),
              "overall": result.get("overall"), "native_evidence": result.get("native_evidence"),
              "classification": outcome.get("classification"), "final": list(final_of(result)),
              "verdict_by_set3_criteria": verdict, "reasons": reasons,
              "steps": [[s["name"], s["verdict"]] for s in result.get("steps", [])],
              "added_steps": {s["name"]: s["verdict"] for s in result.get("steps", []) if s["name"] in ADDED},
              "artifacts": result.get("artifacts"), "request_id": result.get("request_id"), "findings": result.get("findings"),
              "harness": text_of(os.path.join(run_dir, "host", "harness.txt")), "is_latest": latest[cell][0] == run}
    facts["runs"][name] = record
    summary.append(f"== {name}  wrapper {start} - {end}  rc={record['wrapper_rc']}")
    summary.append(f"   by set3's criteria: {verdict}" + (f"  ({'; '.join(reasons)})" if reasons else ""))
    summary.append(f"   overall={result.get('overall')} native_evidence={result.get('native_evidence')} outcome={outcome.get('classification')} "
                   f"final={'/'.join(str(x) for x in final_of(result))}")
    summary.append(f"   {record['harness']}")
    checks_all.append(f"## {name}: by set3's criteria {verdict}; overall={result.get('overall')} native_evidence={result.get('native_evidence')} "
                      f"outcome={outcome.get('classification')}")
    for step in result.get("steps", []):
        mark = " (added by set5)" if step["name"] in ADDED else ""
        summary.append(f"   step {step.get('name'):<34} {step.get('verdict'):<13} {step.get('started_at')} - {step.get('finished_at')}{mark}"
                       + (f"  | {str(step.get('reason'))[:300]}" if step.get("reason") else ""))
        line = f"step | {name} | {step.get('name')}{mark} | {step.get('verdict')} | {str(step.get('reason') or '')[:300]} | {rel(step_file(run_dir, step['name'].lower().replace(' (required)', '-required')))}"
        checks_all.append(line)
        if step.get("verdict") not in OK:
            not_passed.append(line)
    for finding in result.get("findings", []) or []:
        summary.append(f"   finding: {str(finding)[:400]}")
    for path in sorted(glob.glob(os.path.join(run_dir, "steps", "*", "section.json"))):
        section = load(path) or {}
        checks_all.append(f"section | {rel(path)} | {section.get('section')} | {section.get('verdict')} | error={str(section.get('error') or '')[:300]}")
        if section.get("error"):
            not_passed.append(f"section-error | {rel(path)} | {section.get('section')} | {str(section.get('error'))[:600]}")
        for c in section.get("checks", []):
            v = "PASS" if c.get("ok") is True else "FAIL" if c.get("ok") is False else "NOT-ESTABLISHED"
            line = f"check | {rel(path)} | {v} | {c.get('name')}"
            checks_all.append(line)
            if v != "PASS":
                not_passed.append(line + "\n    " + json.dumps(c.get("detail"), sort_keys=True, ensure_ascii=False)[:1600])
    summary.append("")
    row = {"run": name, "cell": cell, "wrapper": [start, end], "overall": result.get("overall"), "verdict": verdict,
           "classification": outcome.get("classification"), "final": outcome.get("final_status"),
           "steps": {s["name"]: s["verdict"] for s in result.get("steps", [])}}
    for step in ("post-update-facts", "post-return-facts", "terminal"):
        path = step_file(run_dir, step)
        if path:
            row[step] = load(path) or {}
            row[step + "_file"] = rel(path)
    part2_rows.append(row)

# ---------------------------------------------------------------------------- part2-table.md (set3's generator)
lines = ["# The owner-started update from the published v0.1.0-alpha.81 to the candidate 67b62cc0f (generated by tools/summary.py)", "",
         "| Run | Wrapper (UTC) | By set3's criteria | Overall (with the added steps) | Outcome | Final state | Steps not passed |",
         "| --- | --- | --- | --- | --- | --- | --- |"]
for row in part2_rows:
    final = row.get("final") or {}
    bad = [f"{k}: {v}" for k, v in row["steps"].items() if v not in OK]
    lines.append(f"| {row['run']} | {row['wrapper'][0][11:19]}-{row['wrapper'][1][11:19]} | {row['verdict']} | {row['overall']} | {row['classification']} | "
                 f"{final.get('phase') if isinstance(final, dict) else None}/{final.get('terminal_proof') if isinstance(final, dict) else None} | {'; '.join(bad) or '-'} |")
lines.append("")
for row in part2_rows:
    facts2 = (row.get("post-update-facts") or {}).get("checks") or {}
    if facts2:
        a, b, c, d, e = (facts2.get(k) or {} for k in ("a_ledger", "b_guard", "c_version_tokens", "d_deferred_mail", "e_agreement"))
        lines += [f"## {row['run']}: after the verified update ({(row.get('post-update-facts') or {}).get('verdict')}; `{row.get('post-update-facts_file')}`)",
                  f"- (a) ledger {a.get('schema_version')} ({a.get('ledger_rows')} rows), verdict {a.get('verdict')}; facts {json.dumps(a.get('facts'), sort_keys=True)}; "
                  f"`request_identities` {json.dumps(a.get('request_identities'), sort_keys=True)[:200]}",
                  f"- (b) without the header: {json.dumps((b.get('without_the_header') or {}), sort_keys=True, ensure_ascii=False)[:900]}; archives before/after/after-with-header "
                  f"{b.get('archives')}; with the header {json.dumps(b.get('with_the_header'), sort_keys=True)[:300]}; row {json.dumps(b.get('row'), sort_keys=True)}; "
                  f"refused and nothing changed: {b.get('refused_and_nothing_changed')}; works with the header: {b.get('works_with_the_header')}" + (f"; error {b.get('error')}" if b.get("error") else "")]
        for key, item in c.items():
            if isinstance(item, dict):
                lines.append(f"- (c) {key}: ok={item.get('ok')} measured={item.get('measured', True)} version {item.get('version_before')} -> {item.get('version_after')}; "
                             f"without a version: {json.dumps(item.get('without_a_version'), sort_keys=True, ensure_ascii=False)[:400]}; with: http {(item.get('with_the_version') or {}).get('http')}; "
                             + "; ".join(f"{k}={item[k]}" for k in ("native_crontab_lines_with_the_command", "native_postconf", "reason") if k in item))
            else:
                lines.append(f"- (c) {key}: {str(item)[:300]}")
        lines += [f"- (d) deferred mail: {json.dumps(d, sort_keys=True)[:600]}",
                  f"- (e) root CLI {e.get('cli_pair')}, recovery reader {e.get('api_pair')}, card judged {json.dumps(e.get('card_judged'), sort_keys=True)[:300]}", ""]
    back = (row.get("post-return-facts") or {}).get("checks") or {}
    if back:
        ledger, views = back.get("ledger") or {}, back.get("views") or {}
        card = views.get("update_card") or {}
        lines += [f"## {row['run']}: after the automatic return ({(row.get('post-return-facts') or {}).get('verdict')}; `{row.get('post-return-facts_file')}`)",
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


# ---------------------------------------------------------------------------- compare-with-set3.md
def brief(step):
    """The facts of post-update-facts / post-return-facts that can be compared between two runs (no time, no id)."""
    checks = (step or {}).get("checks") or {}
    out = {}
    if "a_ledger" in checks:
        a, b, c, d, e = (checks.get(k) or {} for k in ("a_ledger", "b_guard", "c_version_tokens", "d_deferred_mail", "e_agreement"))
        out["ledger"] = [a.get("schema_version"), a.get("ledger_rows"), a.get("verdict"), a.get("ledger_sha256"), a.get("schema_sha256")]
        without = b.get("without_the_header") or {}
        out["guard"] = [without.get("http"), without.get("code"), b.get("archives"), (b.get("with_the_header") or {}).get("http"),
                        b.get("refused_and_nothing_changed"), b.get("works_with_the_header")]
        out["versioned_writes"] = {k: [v.get("ok"), v.get("measured", True), (v.get("without_a_version") or {}).get("http"),
                                       (v.get("without_a_version") or {}).get("code"), (v.get("with_the_version") or {}).get("http")]
                                   for k, v in c.items() if isinstance(v, dict)}
        out["deferred_mail"] = [d.get("measured"), d.get("completed"), sorted(((d.get("last") or {}).get("resolved") or {}))]
        out["agreement"] = [e.get("cli_pair"), e.get("api_pair"), (e.get("card_judged") or {}).get("verdict")]
    if "ledger" in checks:
        ledger, views = checks.get("ledger") or {}, checks.get("views") or {}
        out["ledger"] = [ledger.get("schema_version"), ledger.get("ledger_rows"), ledger.get("verdict"), ledger.get("ledger_sha256"), ledger.get("schema_sha256")]
        out["database_against_the_pre_update_digest"] = checks.get("database_against_the_pre_update_digest")
        out["views"] = [views.get("cli_pair"), views.get("api_pair"), views.get("recovery_api_http"), views.get("cli_previous_failure")]
        card = views.get("update_card") or {}
        out["card"] = [card.get(k) for k in ("state", "role", "missing_keys", "unavailable", "observation_error")] if isinstance(card, dict) else None
        # the CLI's sentence only: the lines after it carry the request id and the time
        out["cli_sentence_en"] = str((views.get("cli_text") or {}).get("en") or "").split("\nRequest:", 1)[0]
        out["cli_sentence_tr"] = str((views.get("cli_text") or {}).get("tr") or "").split("\n", 1)[0]
        out["card_texts_en"] = (card.get("texts") or {}).get("en") if isinstance(card, dict) else None
        again, version = checks.get("offered_again") or {}, checks.get("version") or {}
        again_body, version_body = again.get("body") or {}, version.get("body") or {}
        out["offered_again"] = [again.get("http"), again_body.get("available"), again_body.get("current_version"),
                                (again_body.get("previous_attempt") or {}).get("phase"), (again_body.get("target") or {}).get("version")]
        out["version_after_the_return"] = [version.get("http"), version_body.get("version"), version_body.get("commit"),
                                           version_body.get("schema_version"), version_body.get("agent_matches")]
    return out


def result_brief(result):
    """What result.json says of the path taken: the recovery attempts in their order (no time), the owner's retry,
    the VM reset or the owner's reboot, the judged kind, and the workloads' verdicts are read from the steps."""
    outcome = (result or {}).get("outcome") or {}
    attempts = outcome.get("attempts") or {}
    kind = ((result or {}).get("kind") or {}).get("judged") or {}
    reboot, owner_reboot = outcome.get("reboot") or {}, outcome.get("owner_reboot") or {}
    hold, final = outcome.get("port_hold") or {}, outcome.get("final_status") or {}
    return {"automatic_attempts": [[a.get("attempt"), a.get("direction"), a.get("operation"), a.get("phase")] for a in attempts.get("automatic") or []],
            "owner_attempts": attempts.get("owner_count"), "owner_continuation": outcome.get("owner_continuation"),
            "vm_reset": [reboot.get("status"), reboot.get("new_boot")] if reboot else None,
            "owner_reboot": [owner_reboot.get("command"), owner_reboot.get("new_boot")] if owner_reboot else None,
            "kind_judged": [kind.get("verdict"), len(kind.get("findings") or []), len(kind.get("unknown") or [])] if kind else None,
            "port_hold": [hold.get(k) for k in ("armed", "held", "held_at_pause", "released", "released_before_retry", "release_reason", "phases_seen")] if hold else None,
            "final_status": [final.get(k) for k in ("phase", "terminal_proof", "previous_failure", "failure_code")] if isinstance(final, dict) else None,
            "printed_retry_command": re.sub(r"--snapshot \S+", "--snapshot <snapshot>", str(((result or {}).get("kind") or {}).get("printed_retry_command")))
            if ((result or {}).get("kind") or {}).get("printed_retry_command") else None,
            "kind_rules": len(kind.get("expected") or []) if kind else None,
            "findings": [re.sub(r"^(debian13|ubuntu|arch): ", "", str(x)) for x in (result or {}).get("findings") or []]}


def terminal_brief(step):
    checks = (step or {}).get("checks") or {}
    return {k: checks.get(k) for k in ("build_identity_ok", "running_matches_installed", "login_ok", "firewall_equal", "site_marker", "smtp")}


compare = ["# set5 against set3's Part 2, cell by cell (generated by tools/summary.py)", "",
           "set3: candidate `cfa329676`, `evidence/set3-20261012/part2-alpha81/<cell>/run-a`. set5: candidate `67b62cc0f`, the one run of each cell.",
           "Compared: overall, outcome, final state, the verdict of every step set3 ran (by name), and the facts of the",
           "`post-update-facts` / `post-return-facts` and `terminal` steps that hold no time and no identifier. `same` means the",
           "listed values are equal in the two result files; a ledger or schema digest is equal by value.", "",
           "| Cell | set3 | set5 (run) | Steps of set3: verdicts | Outcome / final state | Compared facts |", "| --- | --- | --- | --- | --- | --- |"]
details = []
for cell in CELLS:
    reference = load(os.path.join(SET3, cell, "run-a", "result.json")) or {}
    if cell not in latest:
        compare.append(f"| {cell} | {reference.get('overall')} | NOT-MEASURED (no run staged) | - | - | - |")
        facts["cells"][cell] = {"verdict": "NOT-MEASURED", "reasons": ["no run of this cell was staged"]}
        continue
    run, run_dir = latest[cell]
    result = load(os.path.join(run_dir, "result.json")) or {}
    verdict, reasons = judge(cell, result, reference)
    mine = {s["name"]: s["verdict"] for s in result.get("steps", [])}
    differing = [f"{s['name']}: set3 {s['verdict']}, set5 {mine.get(s['name'], 'not run')}" for s in reference.get("steps", [])
                 if mine.get(s["name"]) != s["verdict"]]
    out3 = ((reference.get("outcome") or {}).get("classification"),) + final_of(reference)
    out5 = ((result.get("outcome") or {}).get("classification"),) + final_of(result)
    fact_lines = []
    compared_values = {}
    r3, r5 = result_brief(reference), result_brief(result)
    compared_values["result.json"] = {"set3": r3, "set5": r5}
    keys = sorted(k for k in set(r3) | set(r5) if r3.get(k) not in (None, []) or r5.get(k) not in (None, []))
    diff = [k for k in keys if r3.get(k) != r5.get(k)]
    fact_lines.append("result.json: " + ("same (" + ", ".join(keys) + ")" if not diff else "DIFFERENT in " + ", ".join(diff)))
    for k in diff:
        details.append(f"- {cell} result.json `{k}`: set3 {json.dumps(r3.get(k), sort_keys=True, ensure_ascii=False)[:500]} | set5 {json.dumps(r5.get(k), sort_keys=True, ensure_ascii=False)[:500]}")
    for step in ("post-update-facts", "post-return-facts", "terminal"):
        p3, p5 = step_file(os.path.join(SET3, cell, "run-a"), step), step_file(run_dir, step)
        if not p3 and not p5:
            continue
        get = terminal_brief if step == "terminal" else brief
        b3, b5 = get(load(p3) if p3 else None), get(load(p5) if p5 else None)
        keys = sorted(k for k in set(b3) | set(b5) if b3.get(k) is not None or b5.get(k) is not None)
        diff = [k for k in keys if b3.get(k) != b5.get(k)]
        fact_lines.append(f"{step}: " + ("same (" + ", ".join(keys) + ")" if not diff else "DIFFERENT in " + ", ".join(diff)))
        compared_values[step] = {"set3": b3, "set5": b5}
        for k in diff:
            details.append(f"- {cell} {step} `{k}`: set3 {json.dumps(b3.get(k), sort_keys=True, ensure_ascii=False)[:500]} | set5 {json.dumps(b5.get(k), sort_keys=True, ensure_ascii=False)[:500]}")
    compare.append(f"| {cell} | {reference.get('overall')} | {verdict} ({run}; overall with the added steps: {result.get('overall')}) | "
                   f"{'same (' + str(len(reference.get('steps', []))) + ' steps)' if not differing else 'DIFFERENT: ' + '; '.join(differing)} | "
                   f"{'same: ' + '/'.join(str(x) for x in out5) if out3 == out5 else 'DIFFERENT: set3 ' + '/'.join(str(x) for x in out3) + ', set5 ' + '/'.join(str(x) for x in out5)} | "
                   f"{'; '.join(fact_lines) or '-'} |")
    facts["cells"][cell] = {"run": run, "verdict": verdict, "reasons": reasons, "overall": result.get("overall"),
                            "outcome": list(out5), "set3_outcome": list(out3), "steps_differing_from_set3": differing,
                            "facts_compared": fact_lines, "compared_values": compared_values,
                            "added_steps": {s["name"]: s["verdict"] for s in result.get("steps", []) if s["name"] in ADDED},
                            "raw": f"update-alpha81/{cell}/{run}/result.json"}
compare += ["", "## Differences in the compared facts", ""] + (details or ["none"])
open(os.path.join(E, "compare-with-set3.md"), "w", encoding="utf-8", newline="\n").write("\n".join(compare) + "\n")

# ---------------------------------------------------------------------------- item9.md
item9 = ["# Item 9 (set4): a PHP site created by the published alpha.81, across the update (generated by tools/summary.py)", ""]
facts["item9"] = {}
for cell, run, run_dir in runs:
    before_path, after_path = step_file(run_dir, "set4-php-site-before-the-update"), step_file(run_dir, "set4-php-site-after-the-update")
    if not before_path and not after_path:
        continue
    before, after = load(before_path) or {}, load(after_path) or {}
    bc, ac = before.get("checks") or {}, after.get("checks") or {}
    name = f"update-alpha81/{cell}/{run}"
    untouched, saved = ac.get("answers_before_the_update_and_after_it") or {}, ac.get("answers_before_the_update_and_after_the_save") or {}
    record = {"before_step": before.get("verdict"), "after_step": after.get("verdict"), "reason": after.get("reason") or before.get("reason"),
              "panel_version_before": bc.get("panel_version"), "panel_version_after": ac.get("panel_version"),
              "requests": sorted(saved) or sorted(untouched),
              "equal_after_the_update": {k: v.get("equal") for k, v in untouched.items()},
              "equal_after_the_save": {k: v.get("equal") for k, v in saved.items()},
              "vhost_sha256": [(ac.get("vhost_before_the_update") or {}).get("sha256"), (ac.get("vhost_after_the_update_before_the_save") or {}).get("sha256"),
                               (ac.get("vhost_after_the_save") or {}).get("sha256")],
              "vhost_names_the_snippet": [(ac.get("vhost_before_the_update") or {}).get("names_the_php_snippet"),
                                          (ac.get("vhost_after_the_update_before_the_save") or {}).get("names_the_php_snippet"),
                                          (ac.get("vhost_after_the_save") or {}).get("names_the_php_snippet")],
              "vhost_file_written_again_by_the_save": ac.get("vhost_file_written_again_by_the_save"),
              "the_save_rendered_the_vhost_again": ac.get("the_save_rendered_the_vhost_again"),
              "save": {k: (ac.get("render_action") or {}).get(k) for k in ("http", "body", "sent")},
              "nginx_test": [((bc.get("nginx_test") or {}).get("returncode")), ((ac.get("nginx_test_after_the_update") or {}).get("returncode")),
                             ((ac.get("nginx_test_after_the_save") or {}).get("returncode"))],
              "probe_before": bc.get("probe"), "probe_after_the_save": ac.get("probe_after_the_save"), "site_account": bc.get("site_account"),
              "files": [rel(before_path), rel(after_path)]}
    facts["item9"][name] = record
    item9 += [f"## {name}: before {record['before_step']}, after {record['after_step']}" + (f" ({record['reason']})" if record["reason"] else ""),
              f"- files: `{record['files'][0]}`, `{record['files'][1]}` (and their `native/` readings, one file per request and moment)",
              f"- Panel before: {json.dumps(record['panel_version_before'], sort_keys=True)[:200]}; after: {json.dumps(record['panel_version_after'], sort_keys=True)[:200]}",
              f"- vhost SHA-256 before the update / after it / after the save: {record['vhost_sha256']}; names `snippets/fastcgi-php.conf`: {record['vhost_names_the_snippet']}",
              f"- the save: {json.dumps(record['save'], sort_keys=True)[:300]}; file written again (inode, mtime, ctime before/after): {json.dumps(record['vhost_file_written_again_by_the_save'], sort_keys=True)}; "
              f"{json.dumps(record['the_save_rendered_the_vhost_again'], sort_keys=True)}",
              f"- `nginx -t` exit before / after the update / after the save: {record['nginx_test']}",
              f"- PHP page executed as: before {json.dumps(record['probe_before'], sort_keys=True)[:260]}; after the save {json.dumps(record['probe_after_the_save'], sort_keys=True)[:260]} (site account {record['site_account']})",
              "", "| Request | HTTP status before the update / after it / after the save | Body SHA-256 before the update | after it | after the save | Equal after the update | Equal after the save |",
              "| --- | --- | --- | --- | --- | --- | --- |"]
    for request in record["requests"]:
        u, s = untouched.get(request) or {}, saved.get(request) or {}
        status = (u.get("status") or [None, None]) + [(s.get("status") or [None, None])[1]]
        digest = (u.get("body_sha256") or [None, None]) + [(s.get("body_sha256") or [None, None])[1]]
        item9.append(f"| {request} | {status[0]} / {status[1]} / {status[2]} | {digest[0]} | {digest[1]} | {digest[2]} | {u.get('equal')} | {s.get('equal')} |")
    record["status_by_request"] = {r: ((untouched.get(r) or {}).get("status") or [None, None]) + [((saved.get(r) or {}).get("status") or [None, None])[1]] for r in record["requests"]}
    item9.append("")
if len(item9) == 2:
    item9.append("no run with the item-9 steps was staged")
open(os.path.join(E, "item9.md"), "w", encoding="utf-8", newline="\n").write("\n".join(item9) + "\n")

# ---------------------------------------------------------------------------- m10.md
m10 = ["# Postfix Stop through the Panel while `postfix check` refuses main.cf (set4's M10 section; generated by tools/summary.py)", ""]
facts["m10"] = {}
for cell, run, run_dir in runs:
    path = step_file(run_dir, "m10-postfix-stop", "section.json")
    step = load(step_file(run_dir, "m10-postfix-stop") or "") or {}
    if not path and not step:
        continue
    name = f"update-alpha81/{cell}/{run}"
    section = load(path) if path else {}
    section = section or {}
    stop, start = section.get("postfix_stop") or {}, section.get("postfix_start") or {}
    note = stop.get("note") if isinstance(stop.get("note"), dict) else {}
    calls = [c for c in section.get("calls", []) if (c.get("request") or {}).get("path") == "/api/v1/service/action"]
    record = {"step": step.get("verdict"), "section": section.get("verdict"), "reason": step.get("reason"),
              "request": [(c.get("request") or {}).get("body") for c in calls], "status": [c.get("status") for c in calls],
              "stop_answer": stop.get("answer"), "note_code": note.get("code"), "note_reason": note.get("reason"), "note_vars": note.get("vars"),
              "units_before": stop.get("units_before"), "units_after": stop.get("units_after"), "units_8s_later": stop.get("units_8s_later"),
              "unit_results_after": stop.get("unit_results_after"), "master_running_after": stop.get("master_running_after"),
              "reset_failed_in_the_product_journal": stop.get("reset_failed_in_the_product_journal"),
              "start": start, "checks": [[c.get("name"), c.get("ok")] for c in section.get("checks", [])],
              "error": section.get("error"), "file": rel(path)}
    facts["m10"][name] = record
    m10 += [f"## {name}: step {record['step']}, section {record['section']}" + (f" ({str(record['reason'])[:300]})" if record["reason"] else ""),
            f"- file: `{record['file']}`; exchanges in the step's `api/`, readings in `native/`, `journal/postfix-since-the-stop.txt`",
            f"- requests `POST /api/v1/service/action`: {json.dumps(record['request'])}; HTTP {record['status']}",
            f"- Stop answered: {json.dumps(record['stop_answer'], sort_keys=True, ensure_ascii=False)[:1500]}",
            f"- note: code {record['note_code']}, reason {record['note_reason']}, vars {json.dumps(record['note_vars'], sort_keys=True, ensure_ascii=False)[:600]}",
            f"- units before the stop {json.dumps(record['units_before'], sort_keys=True)}; after the answer {json.dumps(record['units_after'], sort_keys=True)} (results {json.dumps(record['unit_results_after'], sort_keys=True)}); "
            f"8 s later {json.dumps(record['units_8s_later'], sort_keys=True)}; master running after: {record['master_running_after']}; `reset-failed` in the product's journal: {record['reset_failed_in_the_product_journal']}",
            f"- Start after main.cf was restored: {json.dumps(record['start'], sort_keys=True, ensure_ascii=False)[:500]}"]
    for check, ok in record["checks"]:
        m10.append(f"- {'PASS' if ok is True else 'FAIL' if ok is False else 'NOT-ESTABLISHED'}: {check}")
    if record["error"]:
        m10.append(f"- section error: {str(record['error'])[:800]}")
    m10.append("")
if len(m10) == 2:
    m10.append("no run with the M10 section was staged")
open(os.path.join(E, "m10.md"), "w", encoding="utf-8", newline="\n").write("\n".join(m10) + "\n")

# ---------------------------------------------------------------------------- pinning.md
pin = ["# Name pinning in each guest's hosts file (generated by tools/summary.py)", "",
       "`pinned at` is the guest's clock when the certificate authorities' names were written and read back (step `set5-name-pinning`,",
       "the first step of the cell). `origin` is when `celikpanel.net` was read as loopback-only (step `origin`), `install starts` the",
       "start of the step `baseline-install` (host clock). `at the end`: the same names read again, with the boot id.", "",
       "| Run | Pinned at (guest) | Product paths present then | All four CA names loopback-only | origin step: celikpanel.net loopback-only, HTTP | Install starts (host) | At the end: all five loopback-only | Boot id changed | certbot log directory |",
       "| --- | --- | --- | --- | --- | --- | --- | --- | --- |"]
facts["pinning"] = {}
for cell, run, run_dir in runs:
    name = f"update-alpha81/{cell}/{run}"
    first = load(step_file(run_dir, "set5-name-pinning") or "") or {}
    last = load(step_file(run_dir, "set5-name-pinning-at-the-end") or "") or {}
    origin = load(step_file(run_dir, "origin") or "") or {}
    install = load(step_file(run_dir, "baseline-install") or "") or {}
    fc, lc, oc = first.get("checks") or {}, last.get("checks") or {}, (origin.get("checks") or {}).get("origin_check") or {}
    logs = [d for d in (lc.get("certbot_directories") or []) if d.get("path") == "/var/log/letsencrypt"]
    record = {"pinned_at": fc.get("at"), "seconds_after_boot": fc.get("uptime_seconds"), "pin_step": first.get("verdict"), "product_paths_present": fc.get("product_paths_present"),
              "loopback_only_at_pinning": fc.get("loopback_only"), "origin_step": origin.get("verdict"),
              "origin_loopback_only": oc.get("loopback_only"), "origin_http": oc.get("http"), "origin_started_at": origin.get("started_at"),
              "install_started_at": install.get("started_at"), "end_step": last.get("verdict"), "loopback_only_at_the_end": lc.get("loopback_only"),
              "boot_id_changed": (lc.get("boot_id") != lc.get("boot_id_at_the_pinning")) if lc.get("boot_id") else None,
              "certbot_log_directory": (logs[0].get("entries") if logs else None), "certbot_program_at_the_end": lc.get("certbot_program"),
              "files": [rel(step_file(run_dir, "set5-name-pinning", "name-pinning.json")), rel(step_file(run_dir, "set5-name-pinning-at-the-end", "name-pinning-at-the-end.json"))]}
    facts["pinning"][name] = record
    pin.append(f"| {name} | {record['pinned_at']}, {record['seconds_after_boot']} s after boot ({record['pin_step']}) | {record['product_paths_present']} | "
               f"{all((record['loopback_only_at_pinning'] or {'x': False}).values())} | {record['origin_loopback_only']}, {record['origin_http']} ({record['origin_step']}) | "
               f"{record['install_started_at']} | {all((record['loopback_only_at_the_end'] or {'x': False}).values()) if record['end_step'] else None} ({record['end_step']}) | "
               f"{record['boot_id_changed']} | {'absent' if record['certbot_log_directory'] is None else record['certbot_log_directory']} |")
open(os.path.join(E, "pinning.md"), "w", encoding="utf-8", newline="\n").write("\n".join(pin) + "\n")

# ---------------------------------------------------------------------------- timeline.md and platforms
tl = ["# Timeline and installed versions per run (generated by tools/summary.py)", "",
      "`owner start`: the start of the step `owner-start` (host clock); `final`: `observed_at` of the final recovery status the step `track`",
      "(or `track-after-owner-continuation`) read; `Panel down`: the outage windows of the guest sampler (one probe every 5 s) as lower-upper",
      "bounds in seconds, from the step `verdicts`; `installed at the end`: the identity files of Agent and Panel read by the step `terminal`.", "",
      "| Run | Owner start | Final status read | Final | Panel down (s) | VM resets or reboots | Installed at the end (Agent / Panel) | Kernel |",
      "| --- | --- | --- | --- | --- | --- | --- | --- |"]
facts["timeline"], facts["platforms"] = {}, {}
for cell, run, run_dir in runs:
    name = f"update-alpha81/{cell}/{run}"
    start = load(step_file(run_dir, "owner-start") or "") or {}
    track = load(step_file(run_dir, "track-after-owner-continuation") or step_file(run_dir, "track") or "") or {}
    verd = load(step_file(run_dir, "verdicts") or "") or {}
    term = load(step_file(run_dir, "terminal") or "") or {}
    last = load(step_file(run_dir, "set5-name-pinning-at-the-end") or "") or {}
    before = load(step_file(run_dir, "set4-php-site-before-the-update") or "") or {}
    result = load(os.path.join(run_dir, "result.json")) or {}
    final = (track.get("checks") or {}).get("final") or {}
    panel = (((verd.get("checks") or {}).get("workloads") or {}).get("panel") or {})
    windows = [f"{w.get('lower_bound_s'):.0f}-{w.get('upper_bound_s'):.0f} ({w.get('cause')})" for w in panel.get("windows") or []
               if isinstance(w.get("lower_bound_s"), (int, float)) and isinstance(w.get("upper_bound_s"), (int, float))]
    builds = (term.get("checks") or {}).get("builds") or {}
    identity = [" ".join(str((builds.get(k) or {}).get("identity") or "").split()) for k in ("agent", "panel")]
    lc, platform = last.get("checks") or {}, (before.get("checks") or {}).get("platform") or {}
    packages = lc.get("packages_installed") or platform.get("packages")
    os_release = lc.get("os_release") or platform.get("os_release")
    reboot = (result.get("outcome") or {}).get("reboot") or (result.get("outcome") or {}).get("owner_reboot")
    record = {"owner_start": start.get("started_at"), "final_read_at": final.get("observed_at"),
              "final": [final.get("phase"), final.get("terminal_proof")], "panel_verdict": panel.get("verdict"),
              "panel_windows": windows, "reboot": reboot, "installed": identity, "kernel": lc.get("kernel")}
    facts["timeline"][name] = record
    facts["platforms"][name] = {"os_release": os_release, "kernel": lc.get("kernel"), "packages": packages,
                                "source": "the last step's reading (`set5-name-pinning-at-the-end`)" if lc.get("packages_installed")
                                else "the item-9 platform reading before the update (`set4-php-site-before-the-update`)" if packages else None}
    tl.append(f"| {name} | {record['owner_start']} | {record['final_read_at']} | {'/'.join(str(x) for x in record['final'])} | {'; '.join(windows) or '-'} "
              f"({panel.get('verdict')}) | {json.dumps(reboot, sort_keys=True)[:160] if reboot else '-'} | {identity[0]} / {identity[1]} | {lc.get('kernel') or '(not read in this run)'} |")
tl += ["", "## Packages", "", "| Run | OS | Packages (name version) | Read by |", "| --- | --- | --- | --- |"]
for name, value in facts["platforms"].items():
    packages = value.get("packages") or {}
    tl.append(f"| {name} | {(value.get('os_release') or {}).get('PRETTY_NAME') if isinstance(value.get('os_release'), dict) else value.get('os_release')} | "
              f"{', '.join(k + ' ' + str(v) for k, v in sorted(packages.items())) or '(not read)'} | {value.get('source')} |")
open(os.path.join(E, "timeline.md"), "w", encoding="utf-8", newline="\n").write("\n".join(tl) + "\n")

# ---------------------------------------------------------------------------- api-routes.txt
ROUTE_WORDS = re.compile(r"(?i)ssl|certificate|acme|letsencrypt|certbot")
routes = ["# Every route the drivers called, from the recorded API exchanges (steps/*/api/*.json of every staged run); digits in a",
          "# path are written as N and query values are dropped. Generated by tools/summary.py.",
          "# A route whose path names ssl, certificate, acme, letsencrypt or certbot is marked CERTIFICATE-ROUTE.", ""]
facts["api_routes"] = {}
total_marked = 0
for cell, run, run_dir in runs:
    name = f"update-alpha81/{cell}/{run}"
    counts, exchanges = {}, 0
    for path in glob.glob(os.path.join(run_dir, "steps", "*", "api", "*.json")):
        request = (load(path) or {}).get("request") or {}
        exchanges += 1
        route = re.sub(r"\d+", "N", str(request.get("path") or "").split("?", 1)[0])
        route = re.sub(r"[0-9a-f]{32}", "HEX", route)
        key = f"{request.get('method')} {route}"
        counts[key] = counts.get(key, 0) + 1
    marked = sorted(k for k in counts if ROUTE_WORDS.search(k))
    total_marked += len(marked)
    facts["api_routes"][name] = {"exchanges": exchanges, "distinct_routes": len(counts), "certificate_routes": marked,
                                 "license_routes": sorted(k for k in counts if "licen" in k.lower())}
    routes.append(f"## {name}: {exchanges} exchanges, {len(counts)} distinct routes, certificate routes: {len(marked)}")
    for key in sorted(counts):
        routes.append(f"{counts[key]:>4}  {key}" + ("   CERTIFICATE-ROUTE" if key in marked else ""))
    routes.append("")
facts["api_routes_total_certificate_routes"] = total_marked
open(os.path.join(E, "api-routes.txt"), "w", encoding="utf-8", newline="\n").write("\n".join(routes) + "\n")

# ---------------------------------------------------------------------------- host/cron-death-lines.txt
cron_lines = ["# Lines `(CRON) DEATH (can't lock /var/run/crond.pid, ...)` in each run's journal of the Panel's and the Agent's units",
              "# (steps/NN-collect/journal-product.txt): count, first and last time. An observation; the cause was not looked into.", ""]
facts["cron_death_lines"] = {}
for cell, run, run_dir in runs:
    name = f"update-alpha81/{cell}/{run}"
    found = []
    for path in glob.glob(os.path.join(run_dir, "steps", "*-collect", "journal-product.txt")):
        found += [line for line in open(path, encoding="utf-8", errors="replace") if "(CRON) DEATH" in line]
    facts["cron_death_lines"][name] = len(found)
    cron_lines.append(f"{name}: {len(found)}" + (f" (first {found[0][:19]}, last {found[-1][:19]})" if found else ""))
os.makedirs(os.path.join(E, "host"), exist_ok=True)
open(os.path.join(E, "host", "cron-death-lines.txt"), "w", encoding="utf-8", newline="\n").write("\n".join(cron_lines) + "\n")

# ---------------------------------------------------------------------------- host-clock-gaps.txt
def series(name):
    """The UTC stamps (first field) of a host watcher's lines."""
    out = []
    try:
        for line in open(os.path.join(E, "host", name), encoding="utf-8"):
            value = stamp(line.split(" ", 1)[0])
            if value is not None:
                out.append(value)
    except OSError:
        pass
    return sorted(out)


def iso(value):
    return dt.datetime.fromtimestamp(value, dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ") if value else None


clock = ["# Did the host stop executing? Two watchers wrote one line every 30 s for the whole run: tools/cwatch.ps1 on Windows",
         "# (host/c-drive-watch.txt) and tools/job-memwatch.sh inside WSL (host/mem-watch.txt, started while the third cell ran). A host that",
         "# sleeps writes no line; a gap much longer than 30 s is a pause of that side. Generated by tools/summary.py.", ""]
facts["host_clock"] = {}
for name in ("c-drive-watch.txt", "mem-watch.txt"):
    times = series(name)
    gaps = [(b - a, a) for a, b in zip(times, times[1:])]
    long = [(g, a) for g, a in gaps if g > 60]
    facts["host_clock"][name] = {"lines": len(times), "first": iso(times[0]) if times else None, "last": iso(times[-1]) if times else None,
                                 "largest_gap_s": max((g for g, _ in gaps), default=None),
                                 "gaps_over_60_s": [[round(g), iso(a)] for g, a in long]}
    clock.append(f"{name}: {len(times)} lines, {iso(times[0]) if times else '-'} to {iso(times[-1]) if times else '-'}; largest gap "
                 f"{max((g for g, _ in gaps), default=0):.0f} s; gaps over 60 s: {[[round(g), iso(a)] for g, a in long] or 'none'}")
clock.append("")
clock.append("run | wrapper start - end (UTC) | Windows watcher lines inside | largest gap inside (s) | WSL watcher lines inside | largest gap inside (s)")
for cell, run, run_dir in runs:
    name = f"update-alpha81/{cell}/{run}"
    a, b = stamp(text_of(os.path.join(run_dir, "host", "wrapper.start.txt"))), stamp(text_of(os.path.join(run_dir, "host", "wrapper.end.txt")))
    row = {}
    for key in ("c-drive-watch.txt", "mem-watch.txt"):
        inside = [t for t in series(key) if a is not None and b is not None and a - 31 <= t <= b + 31]
        row[key] = {"lines": len(inside), "largest_gap_s": max((y - x for x, y in zip(inside, inside[1:])), default=None)}
    facts["host_clock"][name] = row
    clock.append(f"{name} | {iso(a)} - {iso(b)} | {row['c-drive-watch.txt']['lines']} | {row['c-drive-watch.txt']['largest_gap_s']} | "
                 f"{row['mem-watch.txt']['lines']} | {row['mem-watch.txt']['largest_gap_s']}")
# the host's own power events (host/sleep-events.txt, read by tools/sleepevents.ps1) against each run's window
events = []
try:
    for line in open(os.path.join(E, "host", "sleep-events.txt"), encoding="utf-8"):
        parts = line.split()
        if len(parts) == 2 and parts[1].startswith("id=") and stamp(parts[0]) is not None:
            events.append((stamp(parts[0]), int(parts[1][3:])))
except OSError:
    pass
MEANING = {42: "entering sleep", 107: "resumed from sleep", 506: "entering modern standby", 507: "leaving modern standby", 105: "power source change"}
clock += ["", "Power events of the host's System log (host/sleep-events.txt) and each run's window. `state at the start`: the last",
          "standby or sleep event before the wrapper started.", "",
          "run | state at the start (last event before it) | events inside the run's window | last resume from sleep (id 107) before the run"]
facts["power_events"] = {"events": [[iso(t), i, MEANING.get(i)] for t, i in events]}
for cell, run, run_dir in runs:
    name = f"update-alpha81/{cell}/{run}"
    a, b = stamp(text_of(os.path.join(run_dir, "host", "wrapper.start.txt"))), stamp(text_of(os.path.join(run_dir, "host", "wrapper.end.txt")))
    before = [(t, i) for t, i in events if a is not None and t < a and i in (42, 107, 506, 507)]
    inside = [(t, i) for t, i in events if a is not None and b is not None and a <= t <= b]
    resumes = [t for t, i in events if a is not None and t < a and i == 107]
    state = f"{MEANING[before[-1][1]]} at {iso(before[-1][0])}" if before else "no event in the log's window"
    facts["power_events"][name] = {"state_at_start": state, "inside": [[iso(t), i, MEANING.get(i)] for t, i in inside],
                                   "last_resume_from_sleep_before": iso(resumes[-1]) if resumes else None}
    clock.append(f"{name} | {state} | {', '.join(MEANING.get(i, str(i)) + ' ' + iso(t)[11:19] for t, i in inside) or 'none'} | {iso(resumes[-1]) if resumes else 'none in the log window'}")
open(os.path.join(E, "host-clock-gaps.txt"), "w", encoding="utf-8", newline="\n").write("\n".join(clock) + "\n")

# ---------------------------------------------------------------------------- sampler-gaps.txt
gaps = ["run | steps | largest gap between a step's end and the next step's start (s) | longest step (s), name | host sampler samples | largest gap between two host samples (s), starting at (UTC) | first step start - last step end (UTC)"]
facts["gaps"] = {}
for cell, run, run_dir in runs:
    name = f"update-alpha81/{cell}/{run}"
    steps = (load(os.path.join(run_dir, "result.json")) or {}).get("steps", [])
    gap = 0.0
    for a, b in zip(steps, steps[1:]):
        x, y = stamp(a.get("finished_at")), stamp(b.get("started_at"))
        if x is not None and y is not None:
            gap = max(gap, y - x)
    longest = max(((stamp(s.get("finished_at")) or 0) - (stamp(s.get("started_at")) or 0), s["name"]) for s in steps) if steps else (0, "-")
    times = []
    for path in glob.glob(os.path.join(run_dir, "steps", "*", "host-samples.jsonl")):
        for line in open(path, encoding="utf-8"):
            try:
                sample = json.loads(line)
            except ValueError:
                continue
            if isinstance(sample, dict) and isinstance(sample.get("t"), (int, float)):
                times.append(float(sample["t"]))
    times = sorted(set(times))
    largest, at = 0.0, None
    for a, b in zip(times, times[1:]):
        if b - a > largest:
            largest, at = b - a, a
    when = dt.datetime.fromtimestamp(at, dt.timezone.utc).strftime("%H:%M:%S") if at else "-"
    window = [steps[0].get("started_at"), steps[-1].get("finished_at")] if steps else [None, None]
    facts["gaps"][name] = {"steps": len(steps), "largest_step_gap_s": gap, "longest_step": list(longest), "host_samples": len(times),
                           "largest_host_sample_gap_s": round(largest, 1), "at": when, "window": window}
    gaps.append(f"{name} | {len(steps)} | {gap:.0f} | {longest[0]:.0f}, {longest[1]} | {len(times)} | {largest:.1f} at {when} | {window[0]} - {window[1]}")
open(os.path.join(E, "sampler-gaps.txt"), "w", encoding="utf-8", newline="\n").write("\n".join(gaps) + "\n")

open(os.path.join(E, "summary-per-cell.txt"), "w", encoding="utf-8", newline="\n").write("\n".join(summary) + "\n")
open(os.path.join(E, "checks-all.txt"), "w", encoding="utf-8", newline="\n").write("\n".join(checks_all) + "\n")
open(os.path.join(E, "checks-not-passed.txt"), "w", encoding="utf-8", newline="\n").write(
    ("\n".join(not_passed) if not_passed else "every step and every check of every staged run passed (or was `observed` / `skipped` as recorded)") + "\n")
json.dump(facts, open(os.path.join(E, "facts.json"), "w", encoding="utf-8", newline="\n"), indent=1, sort_keys=True, ensure_ascii=False)
print(f"runs: {len(runs)}; cells with a run: {len(latest)} of {len(CELLS)}; steps or checks not passed: {len(not_passed)}")
for cell in CELLS:
    value = facts["cells"].get(cell, {})
    print(f"  {cell}: {value.get('verdict')} {value.get('run', '')} {value.get('outcome', '')} added={value.get('added_steps', '')} {'; '.join(value.get('reasons') or [])[:300]}")
