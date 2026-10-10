# set4: the facts the README quotes, read from the staged cells (read-only). usage: mkvalues.py EVIDENCE_DIR
# Writes facts.json there and prints it compactly.
import glob
import io
import json
import os
import sys

E = sys.argv[1]


def load(path):
    try:
        return json.load(io.open(path, encoding="utf-8"))
    except Exception as exc:  # noqa: BLE001
        return {"_unreadable": type(exc).__name__}


def section(run, key):
    found = glob.glob(os.path.join(E, run, "steps", "*-" + key, "section.json"))
    return load(found[0]) if found else None


def step(run, key):
    found = glob.glob(os.path.join(E, run, "steps", "*-" + key, "step.json"))
    return load(found[0]) if found else None


def times(run):
    result = load(os.path.join(E, run, "result.json"))
    def read(name):
        try:
            return io.open(os.path.join(E, run, "host", name), encoding="utf-8").read().strip()
        except OSError:
            return None
    return {"overall": result.get("overall"), "native_evidence": result.get("native_evidence"),
            "wrapper": [read("wrapper.start.txt"), read("wrapper.end.txt")], "wrapper_rc": read("wrapper.rc.txt"),
            "harness": read("harness.txt"), "lab": (read("lab.txt") or "").splitlines()[:1],
            "steps_not_passed": [[s.get("name"), s.get("verdict"), str(s.get("reason") or "")[:300]] for s in result.get("steps", [])
                                 if s.get("verdict") not in ("passed", "observed")],
            "artifacts": result.get("artifacts"), "request_id": result.get("request_id"),
            "outcome": (result.get("outcome") or {}).get("classification")}


facts = {"cells": {}, "fresh": {}, "update_good": {}, "update_return": {}}
for run in sorted(glob.glob(os.path.join(E, "set4-*", "run-*")) + glob.glob(os.path.join(E, "update-*", "upd1-*", "run-*"))):
    rel = os.path.relpath(run, E).replace("\\", "/")
    facts["cells"][rel] = times(rel)
    plan = load(os.path.join(run, "host", "fixture-plan.json"))
    node = rel.split("/")[-2].split("-")[1] if rel.startswith("set4-") else rel.split("/")[-2].split("-")[1]
    base = ((plan.get("nodes") or {}).get(node) or {}).get("base") or {}
    facts["cells"][rel]["image"] = {"file": os.path.basename(str(base.get("path"))), "digest_algorithm": (base.get("digest") or {}).get("algorithm"), "digest": (base.get("digest") or {}).get("value")}
    if rel.startswith("set4-"):
        out = {}
        m0 = section(rel, "m0-prepare") or {}
        out["platform"] = m0.get("platform")
        out["snippet"] = m0.get("nginx_php_snippet")
        out["isolation"] = m0.get("isolation")
        m1 = section(rel, "m1-php-site") or {}
        out["php_version"] = m1.get("php_version")
        php = (m1.get("php") or {}).get("php-site") or {}
        out["php_probe"] = php.get("probe")
        out["php_answers"] = {k: [v.get("status"), (v.get("body") or "")[:120]] for k, v in (php.get("answers") or {}).items()}
        out["php_path_info"] = {k: {"executed": v.get("executed"), "PATH_INFO": (v.get("values") or {}).get("PATH_INFO"),
                                    "SCRIPT_NAME": (v.get("values") or {}).get("SCRIPT_NAME")} for k, v in (php.get("path_info") or {}).items()}
        deleted = (m1.get("deleted") or {}).get("php-site") or {}
        out["deleted"] = {k: deleted.get(k) for k in ("delete_status", "delete_answer", "no_longer_listed", "account", "site_home_left",
                                                      "acme_challenge_root", "acme_challenge_root_left", "pool_files_left", "socket_left",
                                                      "tables_still_naming_the_domain", "probe_executed_after", "static_served_after")}
        out["deleted_answers_after"] = {k: v.get("status") for k, v in (deleted.get("answers_after") or {}).items()}
        m6 = section(rel, "m6-site-refused") or {}
        out["refused"] = {k: {x: r.get(x) for x in ("domain", "what_made_nginx_refuse", "status", "code", "reason", "vars", "details",
                                                     "error_equals_the_documented_sentence",
                                                     "what_the_removed_answer_claims_and_whether_it_is_true", "not_claimed_by_the_answer")}
                          for k, r in (m6.get("refused") or {}).items()}
        for k, r in (m6.get("refused") or {}).items():
            n = r.get("native") or {}
            out["refused"][k]["autoincrement"] = n.get("autoincrement")
            out["refused"][k]["nginx_unit_pid"] = (n.get("nginx_unit") or {}).get("MainPID")
            out["refused"][k]["tables_naming_the_domain"] = n.get("tables_naming_the_domain")
        out["owner_moved"] = {k: ((m6.get("owner_moved_fastcgi_conf") or {}).get(k) or {}).get("returncode")
                              for k in ("nginx_test_before", "nginx_test_after_the_move")}
        m2 = section(rel, "m2-import") or {}
        out["import"] = {k: (m2.get("import") or {}).get(k) for k in ("status", "import_status", "code", "domain_status", "imported",
                                                                       "not_imported", "steps")}
        out["import_site"] = m2.get("import_site")
        out["import_php_version"] = m2.get("php_version")
        out["import_served"] = m2.get("import_served")
        m5 = section(rel, "m5-import-absolute") or {}
        a = m5.get("absolute") or {}
        out["absolute"] = {k: a.get(k) for k in ("request", "status", "import_status", "code", "domain_status", "message", "imported",
                                                 "not_imported", "steps", "entries_outside", "hostile_members")}
        m4 = section(rel, "m4-reload-stopped") or {}
        out["reload"] = {k: {"stopped_by": r.get("stopped_by"), "units": r.get("units"), "answer": r.get("answer"),
                             "state_after": r.get("state_after"), "unchanged": r.get("unit_properties_unchanged"),
                             "journal_units": r.get("journal_of_the_units_since"), "journal_pid1": r.get("journal_of_systemd_about_them_since")}
                         for k, r in (m4.get("reloads") or {}).items()}
        m10 = section(rel, "m10-postfix-stop") or {}
        p = m10.get("postfix_stop") or {}
        out["postfix_stop"] = {k: p.get(k) for k in ("status", "answer", "units_before", "units_after", "units_8s_later", "unit_results_after",
                                                     "master_running_after", "reset_failed_in_the_product_journal")}
        out["postfix_start"] = m10.get("postfix_start")
        out["sections"] = {os.path.basename(os.path.dirname(f)): [load(f).get("verdict"),
                                                                    sum(1 for c in load(f).get("checks", []) if c.get("ok") is True),
                                                                    sum(1 for c in load(f).get("checks", []) if c.get("ok") is False),
                                                                    sum(1 for c in load(f).get("checks", []) if c.get("ok") is None),
                                                                    load(f).get("error")]
                           for f in sorted(glob.glob(os.path.join(run, "steps", "*", "section.json")))}
        facts["fresh"][rel] = out
    else:
        before = step(rel, "set4-php-site-before-the-update")
        after = step(rel, "set4-php-site-after-the-update")
        returned = step(rel, "set4-update-check-after-the-return")
        if before or after:
            b, a = (before or {}).get("checks") or {}, (after or {}).get("checks") or {}
            facts["update_good"][rel] = {
                "before_verdict": (before or {}).get("verdict"), "after_verdict": (after or {}).get("verdict"),
                "after_reason": (after or {}).get("reason"),
                "version_before": b.get("panel_version"), "version_after": a.get("panel_version"),
                "platform": b.get("platform"), "snippet": b.get("nginx_php_snippet"), "recorded": b.get("recorded"),
                "probe_before": b.get("probe"), "probe_after": a.get("probe_after_the_save"),
                "vhosts": {k: {x: (a.get(k) or {}).get(x) for x in ("sha256", "inode", "mtime_ns", "names_the_php_snippet", "includes",
                                                                    "php_location_lines")}
                           for k in ("vhost_before_the_update", "vhost_after_the_update_before_the_save", "vhost_after_the_save")},
                "nginx_test": [(a.get("nginx_test_after_the_update") or {}).get("returncode"),
                               (a.get("nginx_test_after_the_save") or {}).get("returncode")],
                "render_action": {k: (a.get("render_action") or {}).get(k) for k in ("what", "read_http", "sent", "http", "body")},
                "changed_by_update": a.get("vhost_file_changed_by_the_update_itself"), "changed_by_save": a.get("vhost_file_changed_by_the_save"),
                "written_again": a.get("vhost_file_written_again_by_the_save"), "save_seen": a.get("the_save_rendered_the_vhost_again"),
                "nginx_journal_since_the_save": a.get("nginx_journal_since_the_save"),
                "compare_after_update": {k: [v.get("equal"), v.get("status")] for k, v in (a.get("answers_before_the_update_and_after_it") or {}).items()},
                "compare_after_save": {k: [v.get("equal"), v.get("status")] for k, v in (a.get("answers_before_the_update_and_after_the_save") or {}).items()}}
        if returned:
            c = returned.get("checks") or {}
            builds = (c.get("served_web_build") or {}).get("web_builds") or []
            facts["update_return"][rel] = {
                "verdict": returned.get("verdict"), "reason": returned.get("reason"), "update_check": c.get("update_check"),
                "version": c.get("version"), "previous_attempt": c.get("previous_attempt"),
                "catalogue_keys": c.get("catalogue_of_the_installed_baseline"),
                "catalogue_texts": c.get("catalogue_texts_of_the_installed_baseline"),
                "heading_key": c.get("heading_key_the_candidate_card_would_choose_for_this_answer"),
                "heading_in_served_catalogue": c.get("heading_key_exists_in_the_served_build_catalogue"),
                "web_builds": [{"dist": b.get("dist"), "files_read": b.get("files_read"), "files_containing": b.get("files_containing")} for b in builds],
                "panel_process": (c.get("served_web_build") or {}).get("panel_process")}
io.open(os.path.join(E, "facts.json"), "w", encoding="utf-8", newline="\n").write(json.dumps(facts, indent=1, sort_keys=True, ensure_ascii=False) + "\n")
print(json.dumps({"cells": facts["cells"]}, indent=1, ensure_ascii=False)[:6000])
