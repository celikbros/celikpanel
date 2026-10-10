# set6: the generated views of the staged evidence folder. Every value is read from a raw file of a cell and the file
# is named beside it. usage: summary.py EVIDENCE_DIR
#   checks-all.txt, checks-not-passed.txt   every step, section and check of every run with its verdict and file
#   cells.md                                the runs: lab, harness copy, wrapper times, overall
#   part-a-table.md                         item x platform of the fresh-install cells
#   part-b-headers.md                       the header readings: route x reading, exact values
#   hsts-in-recorded-exchanges.md           the same header in every API exchange the driver's own client recorded
#   update-cells.md                         the three good-update cells
#   pinning.md, packages.md                 the name pinning of every run; kernel and package versions
#   facts.json                              the values the README quotes
import collections
import glob
import json
import os
import re
import sys

E = sys.argv[1].rstrip("/")
PLATFORMS = (("arch", "Arch"), ("debian13", "Debian 13"), ("ubuntu", "Ubuntu 24.04"))
HSTS = "Strict-Transport-Security"
NEW, OLD = "max-age=31536000", "max-age=31536000; includeSubDomains"


def load(path):
    try:
        with open(path, encoding="utf-8") as stream:
            return json.load(stream)
    except (OSError, ValueError):
        return None


def text_of(path):
    try:
        with open(path, encoding="utf-8", errors="replace") as stream:
            return stream.read().strip()
    except OSError:
        return ""


def rel(path):
    return os.path.relpath(path, E).replace(os.sep, "/")


def write(name, text):
    with open(os.path.join(E, name), "w", encoding="utf-8", newline="\n") as stream:
        stream.write(text if text.endswith("\n") else text + "\n")


def word(ok):
    return {True: "PASS", False: "FAIL", None: "NOT-ESTABLISHED"}[ok]


def runs_of(group):
    found = []
    for cell in sorted(glob.glob(os.path.join(E, group, "*"))):
        for run in sorted(glob.glob(os.path.join(cell, "run-*"))):
            found.append((os.path.basename(cell), os.path.basename(run), run))
    return found


def step_dirs(run):
    return sorted(glob.glob(os.path.join(run, "steps", "*")))


def step_dir(run, name):
    slug = re.sub(r"[^a-z0-9]+", "-", name.lower()).strip("-")
    for directory in step_dirs(run):
        if re.sub(r"^\d+-", "", os.path.basename(directory)) == slug:
            return directory
    return None


def step_of(run, name):
    directory = step_dir(run, name)
    return (load(os.path.join(directory, "step.json")) if directory else None), directory


def platform_of(cell):
    return next(key for key, _label in PLATFORMS if key in cell)


RUNS = {"fresh-install": runs_of("fresh-install"), "update-alpha81": runs_of("update-alpha81")}
facts = {"runs": {}, "part_a": {}, "part_b": {}, "update": {}, "pinning": {}, "packages": {}, "exchanges": {}}

# ---------------------------------------------------------------------------------------------------------------
# cells.md, checks-all.txt, checks-not-passed.txt
# ---------------------------------------------------------------------------------------------------------------
all_lines, bad_lines, cell_rows = [], [], []
for group in ("fresh-install", "update-alpha81"):
    for cell, run_name, run in RUNS[group]:
        key = f"{group}/{cell}/{run_name}"
        result = load(os.path.join(run, "result.json")) or {}
        outcome = result.get("outcome") or {}
        final = outcome.get("final_status") or {}
        head = (f"## {key}: overall={result.get('overall')} native_evidence={result.get('native_evidence')} "
                f"outcome={outcome.get('classification')}")
        all_lines.append(head)
        not_passed = []
        for step in result.get("steps") or []:
            line = f"step | {key} | {step.get('name')} | {step.get('verdict')} | {str(step.get('reason') or '')[:300]}"
            all_lines.append(line)
            if step.get("verdict") not in ("passed", "observed"):
                bad_lines.append(line)
                not_passed.append(f"{step.get('name')}: {step.get('verdict')}")
        for directory in step_dirs(run):
            section = load(os.path.join(directory, "section.json"))
            where = rel(directory)
            if section:
                all_lines.append(f"section | {where} | {section.get('section')} | {section.get('verdict')} | error={str(section.get('error') or '')[:300]}")
                for check in section.get("checks") or []:
                    line = f"check | {where}/section.json | {word(check.get('ok'))} | {check.get('name')}"
                    all_lines.append(line)
                    if check.get("ok") is not True:
                        bad_lines.append(line)
                for note in section.get("notes") or []:
                    all_lines.append(f"note | {where}/section.json | {str(note.get('note'))[:400]}")
                continue
            step = load(os.path.join(directory, "step.json")) or {}
            for check in (step.get("checks") or {}).get("checks") or []:
                if isinstance(check, dict) and "ok" in check:
                    line = f"check | {where}/step.json | {word(check.get('ok'))} | {check.get('name')}"
                    all_lines.append(line)
                    if check.get("ok") is not True:
                        bad_lines.append(line)
        harness = text_of(os.path.join(run, "host", "harness.txt"))
        copy = (re.search(r"harness-([a-z0-9]+)", harness) or [None, "?"])[1]
        lab = (text_of(os.path.join(run, "host", "lab.txt")).splitlines() or ["?"])[0].replace("lab=/var/tmp/cp-release-drill-", "")
        start, end = text_of(os.path.join(run, "host", "wrapper.start.txt")), text_of(os.path.join(run, "host", "wrapper.end.txt"))
        facts["runs"][key] = {"overall": result.get("overall"), "outcome": outcome.get("classification"),
                              "final": [final.get("phase"), final.get("reason")] if isinstance(final, dict) else None,
                              "lab": lab, "copy": copy, "wrapper": [start, end], "wrapper_rc": text_of(os.path.join(run, "host", "wrapper.rc.txt")),
                              "not_passed": not_passed, "native_evidence": result.get("native_evidence"),
                              "installed": (result.get("artifacts") or {})}
        cell_rows.append(f"| {key} | {lab} | {copy} | {start[11:19]}-{end[11:19]} | `{result.get('overall')}` | "
                         f"{'; '.join(not_passed) or '-'} |")
write("checks-all.txt", "\n".join(all_lines))
write("checks-not-passed.txt", "\n".join(bad_lines) if bad_lines else "(every step is passed or observed and every check is PASS)")
write("cells.md", "# The runs (generated by tools/summary.py)\n\n| Run (folder) | Lab | Copy | Wrapper (UTC) | Overall | Steps not passed |\n"
      "| --- | --- | --- | --- | --- | --- |\n" + "\n".join(cell_rows))


def latest(group, platform):
    found = [(cell, run_name, run) for cell, run_name, run in RUNS[group] if platform_of(cell) == platform]
    return found[-1] if found else None


# ---------------------------------------------------------------------------------------------------------------
# part-a-table.md
# ---------------------------------------------------------------------------------------------------------------
ITEMS = (
    ("A1", "PHP site created through the Panel (200 with the new domain's id)", "M1-php-site", r"a PHP site is created through the Panel"),
    ("A2", "`nginx -t` passes with the site's vhost; the vhost's PHP location is the generated one", "M1-php-site", r"^php-site: (`nginx -t` passes|the vhost names no file)"),
    ("A3", "a PHP page is executed by PHP-FPM as the site's own account", "M1-php-site", r"^php-site: (the PHP page asked for|the page runs as)"),
    ("A4", "a PHP script that does not exist answers 404", "M1-php-site", r"^php-site: a PHP script that does not exist"),
    ("A5", "PATH_INFO reaches PHP", "M1-php-site", r"^php-site: PATH_INFO"),
    ("A6", "the site deleted through the Panel: nothing left serving, the site's home seen with the site and gone after", "M1-php-site", r"^php-site: (the site is deleted|PHP-FPM is still active)"),
    ("A7", "the recorded PHP version and PHP-FPM socket are the installed version's (the created site and the imported site)", "M1-php-site+M2-import", r"\(item 3\): the (PHP version recorded|recorded socket)"),
    ("A8", "the cPanel-archive import completes and the imported site serves", "M2-import", r"^(item 2:|the preview|S1:|imported-site: (`nginx|the vhost|the PHP page|the page runs|a PHP script|PATH_INFO|a static))"),
    ("A9", "the import's answer: `imported`, `not_imported`, `left_out`, each step's `state`", "M2-import", r"^item 3: "),
    ("A10", "an absolute-path archive member is listed, the import is `partial`, the rest imported", "M5-import-absolute", r"."),
    ("A11", "a new site the web server refuses: `502 SITE_WEB_SERVER_REFUSED`, what the answer says was removed against the guest", "M6-site-refused", r"."),
    ("A12", "Reload of a stopped nginx: 409 `not_running`, nothing sent to the unit", "M4-reload-stopped", r"nginx"),
    ("A13", "Reload of a stopped MariaDB: 409 `not_running`, nothing sent to the unit", "M4-reload-stopped", r"mariadb"),
    ("A14", "Reload of a stopped PostgreSQL: 409 `not_running`, nothing sent to the unit", "M4-reload-stopped", r"postgresql"),
    ("A15", "Postfix Stop while `postfix check` refuses `main.cf`: 200 with the note `unit_marked_failed_config` naming the failed unit, unit still `failed`, no `reset-failed`, Start after `main.cf` is restored", "M10-postfix-stop", r"."),
)
rows = ["# Part A: the candidate installed fresh, item x platform (generated by tools/summary.py)", "",
        "PASS: at least one check of the item was made and every check of it is PASS. FAIL: a check of it failed. "
        "NOT-ESTABLISHED: a check of it could not be judged. NOT-MEASURED: the section did not run on this platform. "
        "The number is how many checks stand behind the cell; the raw file is the section's `section.json` under the run named in the column head.", "",
        "| Item | " + " | ".join(f"{label} (`{(latest('fresh-install', key) or ['', '?'])[0]}/{(latest('fresh-install', key) or ['', '?'])[1]}`)" for key, label in PLATFORMS) + " | Raw file (under `fresh-install/<cell>/<run>/steps/`) |",
        "| --- | --- | --- | --- | --- |"]
home_rows = []
for item, title, sections, pattern in ITEMS:
    cells, files = [], []
    for platform, _label in PLATFORMS:
        chosen = latest("fresh-install", platform)
        if not chosen:
            cells.append("NOT-MEASURED (no run)")
            continue
        cell, run_name, run = chosen
        matched, missing = [], []
        for section_key in sections.split("+"):
            directory = step_dir(run, section_key)
            section = load(os.path.join(directory, "section.json")) if directory else None
            if not section:
                step, _ = step_of(run, section_key)
                missing.append((step or {}).get("verdict") or "no step")
                reason = ((step or {}).get("checks") or {}).get("reason") or (step or {}).get("reason")
                if reason:
                    missing[-1] += f": {reason}"
                continue
            files.append(os.path.basename(directory) + "/section.json")
            matched += [c for c in section.get("checks") or [] if re.search(pattern, str(c.get("name")))]
        oks = [c.get("ok") for c in matched]
        if not matched:
            verdict = "NOT-MEASURED" + (f" ({'; '.join(missing)})" if missing else " (no check of this item in the section)")
        elif False in oks:
            verdict = f"**FAIL** ({oks.count(False)} of {len(oks)} checks)"
        elif None in oks:
            verdict = f"NOT-ESTABLISHED ({oks.count(None)} of {len(oks)} checks)"
        else:
            verdict = f"PASS ({len(oks)})"
        cells.append(verdict)
        facts["part_a"].setdefault(item, {})[platform] = {
            "verdict": verdict, "run": f"fresh-install/{cell}/{run_name}",
            "checks": [{"name": c.get("name"), "ok": c.get("ok")} for c in matched]}
    rows.append(f"| {item}. {title} | " + " | ".join(cells) + " | " + ", ".join(f"`{name}`" for name in sorted(set(files))) + " |")
write("part-a-table.md", "\n".join(rows))

# part-a-readings.md: what the checks of each item read, per platform (the latest run), with the file
lines = ["# Part A: what the checks read (generated by tools/summary.py)", "",
         "Per platform, the latest fresh-install run. Every value is copied from the section's `section.json`; nothing is judged here.", ""]
for platform, label in PLATFORMS:
    chosen = latest("fresh-install", platform)
    if not chosen:
        continue
    cell, run_name, run = chosen
    key = f"fresh-install/{cell}/{run_name}"
    lines += [f"## {label}: `{key}`", ""]
    got = {}

    def section_of(name):
        directory = step_dir(run, name)
        return (load(os.path.join(directory, "section.json")) or {}) if directory else {}, (rel(directory) + "/section.json" if directory else "-")
    m1, f1 = section_of("M1-php-site")
    if m1:
        created = next((c for c in m1.get("calls") or [] if "create a PHP site" in str(c.get("label"))), {})
        php = (m1.get("php") or {}).get("php-site") or {}
        version = (m1.get("php_version") or {}).get("php-site") or {}
        deleted = (m1.get("deleted") or {}).get("php-site") or {}
        probe = php.get("probe") or {}
        tail = ((php.get("path_info") or {}).get("/set4-probe.php/tail.php") or {}).get("values") or {}
        got["m1"] = {"create_status": created.get("status"), "create_answer": {k: v for k, v in (created.get("json") or {}).items() if k != "FTPPassword"},
                     "runs_as": probe.get("runs_as"), "php_version_of_the_page": probe.get("php_version"), "server_api": probe.get("server_api"),
                     "request_statuses": {name: (a or {}).get("status") for name, a in (php.get("answers") or {}).items()},
                     "path_info_tail": {k: tail.get(k) for k in ("PATH_INFO", "SCRIPT_NAME")},
                     "recorded_php_version": version.get("recorded_php_version"), "recorded_php_fpm_socket": version.get("recorded_php_fpm_socket"),
                     "pool_listen": version.get("pool_listen"), "vhost_fastcgi_pass": version.get("vhost_fastcgi_pass"), "socket": version.get("socket"),
                     "delete": {k: deleted.get(k) for k in ("delete_status", "delete_answer", "no_longer_listed", "site_home", "site_home_existed_with_the_site",
                                                           "site_home_left", "site_directories_after", "sockets_after", "socket_left", "account",
                                                           "pool_files_left", "acme_challenge_root", "acme_challenge_root_left")},
                     "nginx_test_after_delete": (deleted.get("nginx_test") or {}).get("returncode")}
        g = got["m1"]
        lines += [f"**PHP site (M1, `{f1}`).** `POST /api/v1/domains/create` answered {g['create_status']} `{json.dumps(g['create_answer'], sort_keys=True)}`. "
                  f"The probe page: executed as `{g['runs_as']}` by PHP {g['php_version_of_the_page']} ({g['server_api']}). Request statuses: `{json.dumps(g['request_statuses'], sort_keys=True)}`. "
                  f"`/set4-probe.php/tail.php`: `{json.dumps(g['path_info_tail'], sort_keys=True)}`. Recorded: PHP `{g['recorded_php_version']}`, socket `{g['recorded_php_fpm_socket']}`; "
                  f"pool `listen` `{g['pool_listen']}`; vhost `fastcgi_pass` `{g['vhost_fastcgi_pass']}`; the socket on the guest: `{json.dumps(g['socket'], sort_keys=True)}`. "
                  f"Delete: `{json.dumps(g['delete'], sort_keys=True)}`; `nginx -t` afterwards exit {g['nginx_test_after_delete']}.", ""]
    m6, f6 = section_of("M6-site-refused")
    for reading_key, record in sorted((m6.get("refused") or {}).items()):
        native = record.get("native") or {}
        got.setdefault("m6", {})[reading_key] = {
            "domain": record.get("domain"), "status": record.get("status"), "code": record.get("code"), "reason": record.get("reason"),
            "vars": record.get("vars"), "details": record.get("details"), "error_equals_the_documented_sentence": record.get("error_equals_the_documented_sentence"),
            "claims": record.get("what_the_removed_answer_claims_and_whether_it_is_true"),
            "home_named_by_useradd_in_the_journal": native.get("home_named_by_useradd_in_the_journal"),
            "that_home_is_listed_after": native.get("that_home_is_listed_after"), "site_directories_before_after": native.get("site_directories"),
            "system_account_after": native.get("system_account"), "new_sockets": native.get("new_sockets"),
            "pool_files_of_the_account": native.get("pool_files_of_the_account"), "nginx_unit": native.get("nginx_unit"),
            "not_claimed_by_the_answer": record.get("not_claimed_by_the_answer")}
        g = got["m6"][reading_key]
        lines += [f"**A site the web server refuses, reading `{reading_key}` (M6, `{f6}`).** `POST /api/v1/domains/create` for `{g['domain']}` answered {g['status']} "
                  f"`{g['code']}`, reason `{g['reason']}`, vars `{json.dumps(g['vars'], sort_keys=True)}`, details `{json.dumps(g['details'])}`; the sentence equals the documented one: "
                  f"{g['error_equals_the_documented_sentence']}. Against the guest: `{json.dumps(g['claims'], sort_keys=True)}`; the home `useradd` logged "
                  f"`{g['home_named_by_useradd_in_the_journal']}`, listed afterwards `{g['that_home_is_listed_after']}`; site directories before and after `{json.dumps(g['site_directories_before_after'])}`; "
                  f"account afterwards `{g['system_account_after']}`; new sockets `{g['new_sockets']}`; pool files `{g['pool_files_of_the_account']}`; nginx unit before/after "
                  f"`{json.dumps(g['nginx_unit'], sort_keys=True)}`; not claimed by the answer and left: `{json.dumps(g['not_claimed_by_the_answer'], sort_keys=True)}`.", ""]
    m2, f2 = section_of("M2-import")
    if m2:
        imp, lists = m2.get("import") or {}, m2.get("lists") or {}
        got["m2"] = {"status": imp.get("status"), "import_status": imp.get("import_status"), "code": imp.get("code"), "domain_status": imp.get("domain_status"),
                     "imported": imp.get("imported"), "not_imported": imp.get("not_imported"), "left_out": lists.get("left_out"),
                     "steps": [{k: s.get(k) for k in ("step", "ok", "state") if s.get(k) is not None} for s in imp.get("steps") or []],
                     "dns_step": lists.get("dns_step"), "forwarders_step": lists.get("forwarders_step"), "request_do_dns": lists.get("request_do_dns"),
                     "served": m2.get("import_served"), "site": m2.get("import_site")}
        g = got["m2"]
        lines += [f"**Import (M2, `{f2}`).** `POST /api/v1/import/cpanel/apply` (`do_dns: {json.dumps(g['request_do_dns'])}`) answered {g['status']}, `status: {g['import_status']}`, "
                  f"`code: {g['code']}`, `domain_status: {g['domain_status']}`, `imported: {json.dumps(g['imported'])}`, `not_imported: {json.dumps(g['not_imported'])}`, "
                  f"`left_out: {json.dumps(g['left_out'])}`. Steps: `{json.dumps(g['steps'])}`. The `dns` step: `{json.dumps(g['dns_step'], sort_keys=True)}`. "
                  f"Served: `{json.dumps(g['served'], sort_keys=True)}`. The imported site: `{json.dumps(g['site'], sort_keys=True)}`.", ""]
    m5, f5 = section_of("M5-import-absolute")
    if m5:
        a, lists = m5.get("absolute") or {}, m5.get("lists") or {}
        got["m5"] = {k: a.get(k) for k in ("status", "import_status", "code", "domain_status", "imported", "not_imported", "member_steps", "entries_outside", "message")}
        got["m5"]["left_out"] = lists.get("left_out")
        got["m5"]["steps"] = lists.get("steps")
        g = got["m5"]
        lines += [f"**Absolute-path member (M5, `{f5}`).** The answer: {g['status']}, `status: {g['import_status']}`, `code: {g['code']}`, `domain_status: {g['domain_status']}`, "
                  f"`imported: {json.dumps(g['imported'])}`, `not_imported: {json.dumps(g['not_imported'])}`, `left_out: {json.dumps(g['left_out'])}`; member steps "
                  f"`{json.dumps(g['member_steps'], sort_keys=True)}`; entries found outside the import directory: `{g['entries_outside']}`. Message: \"{g['message']}\"", ""]
    m4, f4 = section_of("M4-reload-stopped")
    for service, record in sorted((m4.get("reloads") or {}).items()):
        answer = record.get("answer") or {}
        got.setdefault("m4", {})[service] = {"status": answer.get("status"), "code": answer.get("code"), "reason": answer.get("reason"), "vars": answer.get("vars"),
                                           "state_before": record.get("state_before"), "state_after": record.get("state_after"),
                                           "unit_properties_unchanged": record.get("unit_properties_unchanged"),
                                           "journal_lines_of_the_units": {u: len(v or []) for u, v in (record.get("journal_of_the_units_since") or {}).items()},
                                           "journal_lines_of_systemd_about_them": len(record.get("journal_of_systemd_about_them_since") or [])}
        g = got["m4"][service]
        lines += [f"**Reload of the stopped {service} (M4, `{f4}`).** {g['status']} `{g['code']}`, reason `{g['reason']}`, vars `{json.dumps(g['vars'], sort_keys=True)}`; units before "
                  f"`{json.dumps(g['state_before'], sort_keys=True)}`, after `{json.dumps(g['state_after'], sort_keys=True)}`; systemd's record of the units unchanged: {g['unit_properties_unchanged']}; "
                  f"journal lines of the units since the request `{json.dumps(g['journal_lines_of_the_units'], sort_keys=True)}`, of systemd about them {g['journal_lines_of_systemd_about_them']}.", ""]
    m10, f10 = section_of("M10-postfix-stop")
    if m10:
        stop, start = m10.get("postfix_stop") or {}, m10.get("postfix_start") or {}
        got["m10"] = {"status": stop.get("status"), "answer": stop.get("answer"), "note": stop.get("note"), "units_before": stop.get("units_before"),
                      "units_after": stop.get("units_after"), "units_8s_later": stop.get("units_8s_later"), "unit_results_after": stop.get("unit_results_after"),
                      "failed_after": stop.get("failed_after"), "failed_8s_later": stop.get("failed_8s_later"), "master_running_after": stop.get("master_running_after"),
                      "reset_failed_in_the_product_journal": stop.get("reset_failed_in_the_product_journal"), "start": start}
        g = got["m10"]
        lines += [f"**Postfix Stop (M10, `{f10}`).** `POST /api/v1/service/action {{\"name\":\"postfix\",\"action\":\"stop\"}}` answered {g['status']} `{json.dumps(g['answer'], sort_keys=True)}`. "
                  f"Units before `{json.dumps(g['units_before'], sort_keys=True)}`, after the answer `{json.dumps(g['units_after'], sort_keys=True)}` (results `{json.dumps(g['unit_results_after'], sort_keys=True)}`), "
                  f"8 s later `{json.dumps(g['units_8s_later'], sort_keys=True)}`; master running after the answer: {g['master_running_after']}; `reset-failed` named in the product's journal of the window: "
                  f"{g['reset_failed_in_the_product_journal']}. Start after `main.cf` was restored: `{json.dumps(g['start'], sort_keys=True)}`.", ""]
    else:
        step, _ = step_of(run, "M10-postfix-stop")
        lines += [f"**Postfix Stop (M10).** Not run: {((step or {}).get('checks') or {}).get('reason') or (step or {}).get('verdict')}.", ""]
    facts["part_a"].setdefault("readings", {})[platform] = dict(got, run=key)
write("part-a-readings.md", "\n".join(lines))

# ---------------------------------------------------------------------------------------------------------------
# part-b-headers.md
# ---------------------------------------------------------------------------------------------------------------
readings = []      # (column label, run key, file, reading, checks, expected)
for platform, label in PLATFORMS:
    chosen = latest("fresh-install", platform)
    if chosen:
        cell, run_name, run = chosen
        for section_key, tag in (("B-headers-after-setup", "after-setup"), ("B-headers-at-the-end", "at-the-end")):
            directory = step_dir(run, section_key)
            if not directory:
                continue
            section = load(os.path.join(directory, "section.json")) or {}
            reading = load(os.path.join(directory, f"headers-{tag}.json"))
            readings.append((f"{label}, fresh, {tag.replace('-', ' ')}", f"fresh-install/{cell}/{run_name}",
                             rel(os.path.join(directory, f"headers-{tag}.json")), reading,
                             [(c.get("name"), c.get("ok")) for c in section.get("checks") or []], NEW, "fresh"))
# the readings of a run that was repeated: in the short table only (the wide tables show the latest run of a cell)
for cell, run_name, run in RUNS["fresh-install"]:
    platform = platform_of(cell)
    if (cell, run_name, run) == latest("fresh-install", platform):
        continue
    label = dict(PLATFORMS)[platform]
    for section_key, tag in (("B-headers-after-setup", "after-setup"), ("B-headers-at-the-end", "at-the-end")):
        directory = step_dir(run, section_key)
        if not directory:
            continue
        section = load(os.path.join(directory, "section.json")) or {}
        readings.append((f"{label}, fresh, {tag.replace('-', ' ')} (the earlier run, {run_name})", f"fresh-install/{cell}/{run_name}",
                         rel(os.path.join(directory, f"headers-{tag}.json")), load(os.path.join(directory, f"headers-{tag}.json")),
                         [(c.get("name"), c.get("ok")) for c in section.get("checks") or []], NEW, "earlier"))
for platform, label in PLATFORMS:
    chosen = latest("update-alpha81", platform)
    if chosen:
        cell, run_name, run = chosen
        for step_name, tag, expected in (("set6-headers-before-the-update", "before-the-update", OLD),
                                         ("set6-headers-after-the-update", "after-the-update", NEW)):
            step, directory = step_of(run, step_name)
            if not directory:
                continue
            reading = load(os.path.join(directory, f"headers-{tag}.json"))
            checks = [(c.get("name"), c.get("ok")) for c in ((step or {}).get("checks") or {}).get("checks") or []]
            readings.append((f"{label}, update, {tag.replace('-', ' ')}", f"update-alpha81/{cell}/{run_name}",
                             rel(os.path.join(directory, f"headers-{tag}.json")), reading, checks, expected, "update"))


def canon(name):
    """A plain-HTTP request to a port of the Panel's process is named with the port; one row for all of them."""
    return re.sub(r"^plain-http-to-the-panel-port-[0-9]+", "plain-http-to-the-panel-port", name)


def value_cell(request):
    if request is None:
        return "-"
    if request.get("not_sent"):
        return "not sent"
    if request.get("scheme") == "https":
        if not isinstance(request.get("status"), int):
            return "no answer (" + str(request.get("error"))[:60] + ")"
        values = request.get("strict_transport_security") or []
        return f"{request['status']} · " + (" + ".join(f"`{v}`" for v in values) if values else "(no such header)")
    if not request.get("answered"):
        return "no answer (" + str(request.get("error"))[:60] + ")"
    values = request.get("strict_transport_security") or []
    server = [v for n, v in request.get("headers") or [] if n.lower() == "server"]
    return (f"`{request.get('status_line')}`" + (f", Server `{server[0]}`" if server else "") + " · "
            + (" + ".join(f"`{v}`" for v in values) if values else "(no such header)"))


out = ["# Part B: the Panel's `Strict-Transport-Security` header, read on the guest from the running Panel (generated by tools/summary.py)", "",
       "Each cell is `status · header value(s)` exactly as the answer carried them; `(no such header)` means the answer has no "
       f"`{HSTS}` line. HTTPS requests go to `https://127.0.0.1:2083` on the guest (certificate not verified; the leaf's "
       "SHA-256 is in the raw file); plain-HTTP requests are sent over a raw socket. \"as owner\" means the owner's session "
       "cookie was sent. The raw file of a column holds every answer with all its headers; the `.txt` beside it is the same as text.", ""]
summary = ["## The readings in short", "",
           "| Reading | Run | Panel that answered (its own version route) | HTTPS answers read | with exactly the expected value | expected value | plain-HTTP answers carrying the header | checks | raw file |",
           "| --- | --- | --- | --- | --- | --- | --- | --- | --- |"]
for kind, title in (("fresh", "## Fresh-install cells (the candidate; expected `max-age=31536000`)"),
                    ("update", "## Update cells (before: the published alpha.81, expected `max-age=31536000; includeSubDomains`; after: the candidate, expected `max-age=31536000`)")):
    chosen = [r for r in readings if r[6] == kind]
    if not chosen:
        continue
    names, shown = [], {}
    for _label, _run, _file, reading, _checks, _expected, _kind in chosen:
        for request in (reading or {}).get("requests") or []:
            name = canon(request.get("name", ""))
            if name not in names:
                names.append(name)
                shown[name] = (request.get("method"), request.get("scheme"), request.get("path"), request.get("as_logged_in_owner"), request.get("port"))
    out += [title, "", "| Request | " + " | ".join(r[0] for r in chosen) + " |", "| --- |" + " --- |" * len(chosen)]
    for name in names:
        method, scheme, path, owner, port = shown[name]
        port_text = port
        cells = []
        for _label, _run, _file, reading, _checks, _expected, _kind in chosen:
            match = [q for q in (reading or {}).get("requests") or [] if canon(q.get("name", "")) == name]
            cells.append("<br>".join(value_cell(q) for q in match) if match else "-")
        out.append(f"| `{name}`: {method} {scheme}://127.0.0.1:{port_text}{path}{' (as owner)' if owner else ''} | " + " | ".join(cells) + " |")
    out.append("")
for label, run_key, file, reading, checks, expected, kind in readings:
    requests = (reading or {}).get("requests") or []
    https = [q for q in requests if q.get("scheme") == "https" and isinstance(q.get("status"), int)]
    exact = [q for q in https if q.get("strict_transport_security") == [expected]]
    plain = [q for q in requests if q.get("scheme") == "http"]
    plain_with = [q for q in plain if q.get("strict_transport_security") or q.get("header_name_anywhere_in_the_answer")]
    panel = next((q.get("panel") for q in requests if q.get("name") == "api-version-as-owner"), None) or {}
    oks = [ok for _name, ok in checks]
    verdict = "FAIL" if False in oks else "NOT-ESTABLISHED" if None in oks or not oks else "PASS"
    summary.append(f"| {label} | `{run_key}` | {panel.get('version')} `{str(panel.get('commit'))[:12]}` schema {panel.get('schema_version')} | {len(https)} | "
                   f"{len(exact)} | `{expected}` | {len(plain_with)} of {len([q for q in plain if q.get('answered')])} answered ({len(plain)} sent) | "
                   f"{verdict} ({oks.count(True)} of {len(oks)}) | `{file}` |")
    facts["part_b"][label] = {"run": run_key, "file": file, "expected": expected, "panel": panel,
                              "https": {q["name"]: {"status": q.get("status"), HSTS: q.get("strict_transport_security"),
                                                    "request": f"{q.get('method')} {q.get('path')}", "as_owner": q.get("as_logged_in_owner")} for q in https},
                              "https_not_read": [q.get("name") for q in requests if q.get("scheme") == "https" and not isinstance(q.get("status"), int)],
                              "plain": {q["name"]: {"to": f"{q.get('host')}:{q.get('port')}", "answered": q.get("answered"),
                                                    "status_line": q.get("status_line"), "error": q.get("error"), HSTS: q.get("strict_transport_security"),
                                                    "server": [v for n, v in q.get("headers") or [] if n.lower() == "server"],
                                                    "location": [v for n, v in q.get("headers") or [] if n.lower() == "location"],
                                                    "body_start": (q.get("body_start") or "")[:160]} for q in plain},
                              "panel_ports": (reading or {}).get("panel_ports"),
                              "listening_of_the_panel": (reading or {}).get("listening_tcp_of_the_panel_process"),
                              "listening_80_443_panel": (reading or {}).get("listening_tcp_on_80_443_and_the_panel_port"),
                              "tls_leaf_sha256": (reading or {}).get("tls_leaf_sha256"),
                              "at": [(reading or {}).get("at_start"), (reading or {}).get("at_end")],
                              "checks": [{"name": name, "ok": ok} for name, ok in checks], "verdict": verdict}
write("part-b-headers.md", "\n".join(out + summary))

# ---------------------------------------------------------------------------------------------------------------
# hsts-in-recorded-exchanges.md: the driver's own client (through the SSH forward), every exchange of every run
# ---------------------------------------------------------------------------------------------------------------
lines = ["# The same header in every API exchange the driver's own client recorded (generated by tools/summary.py)", "",
         "Not the reading of Part B: these are the answers the driver's HTTPS client got on the WSL host through the SSH "
         "forward to the guest's `127.0.0.1:2083`, as each `steps/*/api/*.json` holds them. Counted per run and per part of "
         "the run; `(none)` is an answer without the header, `(no answer)` an exchange that recorded no response.", "",
         "| Run | Part of the run | Exchanges | Header value: count | Status codes seen |", "| --- | --- | --- | --- | --- |"]
for group in ("fresh-install", "update-alpha81"):
    for cell, run_name, run in RUNS[group]:
        key = f"{group}/{cell}/{run_name}"
        start_dir, track_dir = step_dir(run, "owner-start"), step_dir(run, "track")
        parts = collections.OrderedDict()
        for directory in step_dirs(run):
            base_name = os.path.basename(directory)
            if group == "fresh-install" or not start_dir:
                part = "the whole run (the candidate installed fresh)" if group == "fresh-install" else "the whole run"
            elif base_name < os.path.basename(start_dir):
                part = "before the owner's start (the published alpha.81)"
            elif track_dir and base_name <= os.path.basename(track_dir):
                part = "the owner's start and the tracking of the update (either Panel may answer)"
            else:
                part = "after the update ended"
            for path in sorted(glob.glob(os.path.join(directory, "api", "*.json"))):
                exchange = load(path) or {}
                response = exchange.get("response")
                entry = parts.setdefault(part, {"n": 0, "values": collections.Counter(), "status": collections.Counter()})
                entry["n"] += 1
                if not isinstance(response, dict):
                    entry["values"]["(no answer)"] += 1
                    continue
                values = [v for n, v in response.get("headers") or [] if str(n).lower() == HSTS.lower()]
                entry["values"][" + ".join(values) if values else "(none)"] += 1
                entry["status"][str(response.get("status"))] += 1
        for part, entry in parts.items():
            lines.append(f"| `{key}` | {part} | {entry['n']} | " + "; ".join(f"`{v}`: {n}" for v, n in sorted(entry["values"].items())) + " | "
                         + ", ".join(f"{s}: {n}" for s, n in sorted(entry["status"].items())) + " |")
            facts["exchanges"].setdefault(key, {})[part] = {"exchanges": entry["n"], "values": dict(entry["values"]), "status": dict(entry["status"])}
write("hsts-in-recorded-exchanges.md", "\n".join(lines))

# ---------------------------------------------------------------------------------------------------------------
# update-cells.md
# ---------------------------------------------------------------------------------------------------------------
lines = ["# The three good-update cells: the published v0.1.0-alpha.81 to the candidate (generated by tools/summary.py)", ""]
table = ["| Run | Wrapper (UTC) | Overall | Outcome | Final state | Ledger after the update | Page loaded before the update: backup without the request id | Versioned writes without a version | Installed at the end | Header before / after | Steps not passed |",
         "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |"]
detail = []
for cell, run_name, run in RUNS["update-alpha81"]:
    key = f"update-alpha81/{cell}/{run_name}"
    info = facts["runs"][key]
    facts_step, facts_dir = step_of(run, "post-update-facts")
    checks = (facts_step or {}).get("checks") or {}
    ledger, guard, tokens = checks.get("a_ledger") or {}, checks.get("b_guard") or {}, checks.get("c_version_tokens") or {}
    terminal, terminal_dir = step_of(run, "terminal")
    builds = ((terminal or {}).get("checks") or {}).get("builds")
    without = guard.get("without_the_header") or {}
    versioned = {name: (item.get("without_a_version") or {}) for name, item in tokens.items() if isinstance(item, dict) and item.get("measured") is not False}
    unmeasured = {name: item.get("reason") for name, item in tokens.items() if isinstance(item, dict) and item.get("measured") is False}
    before = next((v for k, v in facts["part_b"].items() if v["run"] == key and "before" in k), {})
    after = next((v for k, v in facts["part_b"].items() if v["run"] == key and "after" in k), {})
    table.append(
        f"| `{key}` | {info['wrapper'][0][11:19]}-{info['wrapper'][1][11:19]} | `{info['overall']}` | {info['outcome']} | "
        f"{'/'.join(str(x) for x in info['final'] or [])} | schema {ledger.get('schema_version')}, {ledger.get('ledger_rows')} rows, {ledger.get('verdict')} | "
        f"{without.get('http')} `{without.get('code')}`; archives {guard.get('archives')}; with it {(guard.get('with_the_header') or {}).get('http')} | "
        + "; ".join(f"{name}: {v.get('http')} `{v.get('code')}`" for name, v in sorted(versioned.items()))
        + ("; not measured: " + ", ".join(sorted(unmeasured)) if unmeasured else "")
        + f" | {'; '.join(who + ': ' + ' '.join(str((item or {}).get('identity') or '').split()) for who, item in sorted(builds.items()) if isinstance(item, dict)) if isinstance(builds, dict) else '-'}"
        + f" | {before.get('verdict')} / {after.get('verdict')} | {'; '.join(info['not_passed']) or '-'} |")
    facts["update"][key] = {"ledger": {k: ledger.get(k) for k in ("schema_version", "ledger_rows", "verdict", "facts", "expected_version")},
                            "guard": {"without_the_header": {k: without.get(k) for k in ("http", "code")}, "archives": guard.get("archives"),
                                      "with_the_header": {k: (guard.get("with_the_header") or {}).get(k) for k in ("http", "code")},
                                      "refused_and_nothing_changed": guard.get("refused_and_nothing_changed"), "row": guard.get("row")},
                            "versioned": {name: {k: v.get(k) for k in ("http", "code", "reason")} for name, v in versioned.items()},
                            "versioned_not_measured": unmeasured, "builds": builds,
                            "files": {"post_update_facts": rel(os.path.join(facts_dir, "step.json")) if facts_dir else None,
                                      "terminal": rel(os.path.join(terminal_dir, "step.json")) if terminal_dir else None},
                            "steps": {s.get("name"): s.get("verdict") for s in (load(os.path.join(run, "result.json")) or {}).get("steps") or []}}
    result = load(os.path.join(run, "result.json")) or {}
    start_step, start_dir = step_of(run, "owner-start")
    verdicts_step, verdicts_dir = step_of(run, "verdicts")
    workloads = ((verdicts_step or {}).get("checks") or {}).get("workloads") or {}
    final = (result.get("outcome") or {}).get("final_status") or {}
    target = ((((start_step or {}).get("checks") or {}).get("start") or {}).get("body") or {}).get("target") or {}
    panel_windows = [[w.get("lower_bound_s"), w.get("upper_bound_s")] for w in (workloads.get("panel") or {}).get("windows") or []]
    facts["update"][key].update(
        owner_start={"http": (((start_step or {}).get("checks") or {}).get("start") or {}).get("http"), "at": (start_step or {}).get("started_at"),
                     "target": {k: target.get(k) for k in ("version", "commit", "sequence", "archive_sha256")}},
        verified_read_at=final.get("observed_at"), final_status={k: final.get(k) for k in ("phase", "reason", "terminal_proof")},
        workloads={name: (item or {}).get("verdict") for name, item in workloads.items()}, panel_outage_windows_s=panel_windows,
        artifacts=result.get("artifacts"), findings=result.get("findings"),
        files_more={"owner_start": rel(os.path.join(start_dir, "step.json")) if start_dir else None,
                    "verdicts": rel(os.path.join(verdicts_dir, "step.json")) if verdicts_dir else None})
    detail.append(
        f"## {key}\n\n"
        f"- The owner's start: `POST` answered {facts['update'][key]['owner_start']['http']}, step started {facts['update'][key]['owner_start']['at']}; "
        f"target `{json.dumps(facts['update'][key]['owner_start']['target'], sort_keys=True)}` (`{facts['update'][key]['files_more']['owner_start']}`).\n"
        f"- Final state `{json.dumps(facts['update'][key]['final_status'], sort_keys=True)}`, read {final.get('observed_at')} (`result.json`, `outcome.final_status`).\n"
        f"- Workloads during the update (`{facts['update'][key]['files_more']['verdicts']}`): `{json.dumps(facts['update'][key]['workloads'], sort_keys=True)}`; "
        f"Panel outage windows by the guest sampler, lower and upper bound in seconds: {panel_windows}.\n"
        f"- Ledger: `{json.dumps(facts['update'][key]['ledger'], sort_keys=True)}` (`{facts['update'][key]['files']['post_update_facts']}`, `a_ledger`).\n"
        f"- Guard: `{json.dumps(facts['update'][key]['guard'], sort_keys=True)}` (same file, `b_guard`).\n"
        f"- Versioned writes without a version: `{json.dumps(facts['update'][key]['versioned'], sort_keys=True)}`; not measured: `{json.dumps(unmeasured, sort_keys=True)}` (same file, `c_version_tokens`).\n"
        f"- Identity files read on the guest at the end: `{json.dumps(builds, sort_keys=True)}` (`{facts['update'][key]['files']['terminal']}`, `builds`).\n"
        f"- Archives named by the cell (`result.json`, `artifacts`): `{json.dumps(result.get('artifacts'), sort_keys=True)}`.\n"
        "- Steps: " + "; ".join(f"{s.get('name')}: {s.get('verdict')}" + (f" ({str(s.get('reason'))[:200]})" if s.get("reason") else "")
                               for s in result.get("steps") or []) + "\n")
write("update-cells.md", "\n".join(lines + table + [""] + detail))

# ---------------------------------------------------------------------------------------------------------------
# pinning.md, packages.md
# ---------------------------------------------------------------------------------------------------------------
lines = ["# Name pinning, per run (generated by tools/summary.py)", "",
         "`hosts file`: `getent -s files ahosts NAME` and `getent -s files hosts NAME`. `default path`: `getent ahosts NAME` and "
         "`getent hosts NAME` (the order of the guest's nsswitch.conf). True means every address read is the guest's own loopback. "
         "`resolved transactions` is systemd-resolved's own `Total Transactions` before and after the default-path lookups of the step.", "",
         "| Run | Pinned at (guest), after boot | Product paths present then | nsswitch `hosts:` | All five names loopback-only: hosts file / default path | resolved transactions before -> after | At the end: hosts file / default path | Boot id changed | certbot directories at the end |",
         "| --- | --- | --- | --- | --- | --- | --- | --- | --- |"]
answers = ["", "## What the default lookup path answered for each name at the pinning (raw: `steps/01-set6-name-pinning/name-pinning.json`)", ""]
packages = {}
for group in ("fresh-install", "update-alpha81"):
    for cell, run_name, run in RUNS[group]:
        key = f"{group}/{cell}/{run_name}"
        first, _ = step_of(run, "set6-name-pinning")
        last, _ = step_of(run, "set6-name-pinning-at-the-end")
        c1, c2 = (first or {}).get("checks") or {}, (last or {}).get("checks") or {}

        def both(checks):
            verdict = checks.get("loopback_only") or {}
            if not verdict:
                return "- / -"
            return f"{all(v.get('hosts_file') is True for v in verdict.values())} / {all(v.get('default_path') is True for v in verdict.values())}"
        tx1 = c1.get("resolved_total_transactions") or {}
        certbot = [(d.get("path"), d.get("entries")) for d in c2.get("certbot_directories") or []]
        lines.append(f"| `{key}` | {c1.get('at')}, {c1.get('uptime_seconds')} s ({(first or {}).get('verdict')}) | {c1.get('product_paths_present')} | "
                     f"`{'; '.join(c1.get('nsswitch_hosts') or [])}` | {both(c1)} | {tx1.get('before_the_default_path_lookups')} -> {tx1.get('after_them')} | "
                     f"{both(c2)} ({(last or {}).get('verdict')}) | {(c2.get('boot_id') != c2.get('boot_id_at_the_pinning')) if c2 else '-'} | "
                     f"{'; '.join(f'{os.path.basename(str(p))}: {e if e is not None else chr(97) + chr(98) + chr(115) + chr(101) + chr(110) + chr(116)}' for p, e in certbot) or '-'} |")
        answers.append(f"- `{key}`: " + "; ".join(f"{name}: hosts=`{(v or {}).get('hosts', '').replace(chr(10), ' / ')}` ahosts=`{' / '.join(sorted(set(l.split()[0] for l in (v or {}).get('ahosts', '').splitlines() if l.split())))}`"
                                                  for name, v in sorted((c1.get("default_path_answers") or {}).items())))
        facts["pinning"][key] = {"first": {k: c1.get(k) for k in ("at", "uptime_seconds", "product_paths_present", "nsswitch_hosts", "loopback_only",
                                                                 "resolved_total_transactions", "systemd_resolved", "default_path_answers", "written_at")},
                                 "first_verdict": (first or {}).get("verdict"), "last_verdict": (last or {}).get("verdict"),
                                 "last": {k: c2.get(k) for k in ("at", "loopback_only", "boot_id", "boot_id_at_the_pinning", "certbot_directories",
                                                                "certbot_program", "resolved_total_transactions", "hosts_lines")}}
        if c2.get("packages_installed"):
            packages[key] = {"platform": platform_of(cell), "kernel": c2.get("kernel"), "os": (c2.get("os_release") or {}).get("PRETTY_NAME"),
                             "packages": c2.get("packages_installed")}
write("pinning.md", "\n".join(lines + answers))
facts["packages"] = packages
names = sorted({name for item in packages.values() for name in item["packages"]})
lines = ["# Kernel and package versions as read on the guest at the end of each run (generated by tools/summary.py)", "",
         "Source: `steps/NN-set6-name-pinning-at-the-end/name-pinning-at-the-end.json` of each run. One value per platform when every run "
         "of the platform read the same; otherwise each value with its runs. `-`: not installed on that platform.", "",
         "| | " + " | ".join(label for _key, label in PLATFORMS) + " |", "| --- | --- | --- | --- |"]


def per_platform(getter):
    cells = []
    for platform, _label in PLATFORMS:
        values = collections.OrderedDict()
        for key, item in packages.items():
            if item["platform"] == platform:
                values.setdefault(getter(item), []).append(key)
        values.pop(None, None)
        cells.append("-" if not values else next(iter(values)) if len(values) == 1 else
                     "; ".join(f"{v} ({', '.join(k.split('/', 1)[1] for k in keys)})" for v, keys in values.items()))
    return cells


lines.append("| OS | " + " | ".join(per_platform(lambda item: item["os"])) + " |")
lines.append("| kernel | " + " | ".join(per_platform(lambda item: item["kernel"])) + " |")
for name in names:
    lines.append(f"| {name} | " + " | ".join(per_platform(lambda item, name=name: item["packages"].get(name))) + " |")
write("packages.md", "\n".join(lines))
with open(os.path.join(E, "facts.json"), "w", encoding="utf-8", newline="\n") as stream:
    json.dump(facts, stream, indent=1, sort_keys=True)
    stream.write("\n")
print("runs:", len(facts["runs"]), "| checks not passed:", len(bad_lines), "| readings:", len(readings))
for key, info in facts["runs"].items():
    print(key, info["overall"], info["outcome"], info["not_passed"])
