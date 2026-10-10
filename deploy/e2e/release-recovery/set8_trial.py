#!/usr/bin/env python3
"""set8: a native BASELINE of what the published CelikPanel v0.1.0-alpha.82 does with a site's nginx vhost (and its
PHP-FPM pool) that the server owner edited by hand, on disposable QEMU guests (lab.py). It measures; it judges each
run against D-022 ("detect the owner's change rather than silently overwrite it") only as an observation.

Built on set6's fresh-install cell (``set6_trial.Set6Trial``) and changing none of its steps: the names pinned first
(``set6-name-pinning``), ``preflight``, ``origin``, ``baseline-install``, ``owner-login``, ``license`` (acceptance
fixture, no licence service), ``setup`` (to the wait at ``access_dns``), ``site`` (set1's static site). Then, as the
logged-in owner and as the owner on the server's shell:

  S0  two PHP sites created through the Panel (A ``set8-a.test``, B ``set8-b.test``); the owner uploads a probe page,
      a static file, an index page of their own and a page that prints PHP's memory_limit; A's vhost bytes, inode,
      mode, owner, attributes, enabled link, pool file and ``nginx -T`` are recorded (the creation render); then, as
      lab preparation with no owner edit, the Panel is restarted once and A's vhost read again (the start render,
      the reference every run is restored to; on Debian 13 run-a the two differed by ``www.`` in ``server_name``).
  a   the owner adds ``location /owner-extra/ { return 200 "owner"; }`` to A's vhost: (1) Panel stopped while editing,
      then started; (2) Panel running while editing, then restarted; (3) Panel running, then A's General settings saved
      unchanged through the Panel (which renders the vhost, as set4 item 9).
  b   the owner changes a directive the template sets (``index``: their page first): (1) stopped/start, (3) save.
  c   the owner replaces the file wholesale with a minimal server block of their own: (1), (3).
  d   the owner adds ``include /etc/nginx/owner/<domain>.conf;`` (the file present): (1), (3).
  e   the owner removes the vhost and its sites-enabled link: (1).
  f   the owner adds a directive and a comment to A's PHP-FPM pool: Panel stopped/start; then a pool save through the
      Panel; then the PHP version "switch" the API offers.
  g   the owner sets ``chattr +i`` on A's vhost: Panel stopped/start; A, B and the static site read; then nginx reloaded
      as logrotate's hook would; then ``chattr -i``.

Every run begins with ``lab-restore`` (lab preparation, not an owner action): A's vhost gets back the start render
recorded in S0 with that file's owner, group and mode (a new file renamed into place, as the Panel writes it), its
enabled link, no ``chattr`` flag,
``nginx -t`` and a reload. Each owner edit is written into the same file (same inode), tested with ``nginx -t`` and
followed by ``systemctl reload nginx`` as an owner would. After each trigger: the file, ``stat``, ``nginx -T``, the
journal of the Panel, the Agent, nginx and PHP-FPM since the run began, HTTP answers on the guest's loopback, and the
Panel's API answers an owner sees (Domains list, the site's General and PHP settings, Dashboard, Components, Audit log).

The driver calls only the Panel's HTTP API; every native fact is read over SSH; owner actions and lab preparation are
recorded as such. No certificate authority and no licence service is contacted by the driver. Every result carries
``native_evidence: false``.

  set8_trial.py plan --cell set8-debian13 --artifacts A.json --work-root /var/tmp/cp-release-drill-X [--dry-run]
  set8_trial.py run  --cell set8-debian13 --artifacts A.json --work-root /var/tmp/cp-release-drill-X --execute
"""
from __future__ import annotations

import argparse
import base64
import dataclasses
import hashlib
import json
from pathlib import Path
import sys
import time
from typing import Any

HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    sys.path.insert(0, str(HERE))
import set6_trial as s6  # noqa: E402

s4, sw, base = s6.s4, s6.sw, s6.base
CELL_KIND = "set8-owner-edits-baseline"
HELPER8 = "guest_set8_native.py"
DOMAIN_A, DOMAIN_B = "set8-a.test", "set8-b.test"
CELLS = {
    "set8-debian13": sw.SettingsCell("set8-debian13", "debian13", "web_mail", sw.MAIL_PRESET + ("postgresql",), True),
    "set8-ubuntu": sw.SettingsCell("set8-ubuntu", "ubuntu", "web_mail", sw.MAIL_PRESET + ("postgresql",), True),
    "set8-arch": sw.SettingsCell("set8-arch", "arch", "web", sw.WEB_PRESET + ("postgresql",), False),
}
# (run, scenario, owner action, trigger, Panel stopped while the owner edits)
RUNS = (
    ("a1", "a", "append-location", "start", True),
    ("a2", "a", "append-location", "restart", False),
    ("a3", "a", "append-location", "save", False),
    ("b1", "b", "change-index", "start", True),
    ("b3", "b", "change-index", "save", False),
    ("c1", "c", "replace", "start", True),
    ("c3", "c", "replace", "save", False),
    ("d1", "d", "add-include", "start", True),
    ("d3", "d", "add-include", "save", False),
    ("e1", "e", "remove", "start", True),
)
SCENARIOS = {
    "a": "the owner adds a location block to the vhost",
    "b": "the owner changes a directive the template sets (index)",
    "c": "the owner replaces the vhost wholesale with a minimal server block of their own",
    "d": "the owner adds an include of their own file to the vhost",
    "e": "the owner removes the vhost and its sites-enabled link",
    "f": "the owner adds a directive and a comment to the site's PHP-FPM pool",
    "g": "the owner makes the vhost immutable (chattr +i)",
}
# What the owner asks the web server for, per owner action, and the answer that shows the owner's change is served.
OWNER_PATHS = {"append-location": ("/owner-extra/", "owner"), "change-index": ("/", "set8 owner index page"),
               "replace": ("/", "owner-replaced"), "add-include": ("/owner-include/", "owner-include"),
               "remove": ("/", None)}
PROBE_PATH, INI_PATH = "/set4-probe.php", "/set8-ini.php"
SETTLE = 3.0
RUNNING_EDIT_WAIT = 20.0


def b64(text: str) -> str:
    return base64.b64encode(text.encode()).decode()


def file_class(after: dict | None, panel_shas, edited_sha: str | None) -> str:
    """What the vhost is after a trigger, against the Panel's own texts of S0 (the creation render and the start-time
    render) and the owner's edited bytes."""
    after = after or {}
    if not after.get("exists"):
        return "absent"
    sha = after.get("sha256")
    shas = {panel_shas} if isinstance(panel_shas, str) else set(panel_shas or ())
    if edited_sha and sha == edited_sha:
        return "owner-bytes-kept"
    if sha in shas:
        return "panel-text-written"
    return "other-bytes"


def provisional_verdict(cls: str, panel_active: bool, trigger_ok: bool | None, reported: bool) -> str:
    """The D-022 reading of one run, from what was measured (the README states the final word per run)."""
    if not panel_active:
        return "Panel failed to start"
    if cls == "owner-bytes-kept":
        if trigger_ok is False:
            return "refused"
        return "kept and reported" if reported else "kept silently"
    if cls == "panel-text-written":
        return "overwritten and reported" if reported else "overwritten silently"
    return "unknown"


def answers_text(view: dict) -> str:
    return json.dumps(view, sort_keys=True, default=str).lower()


class Set8Trial(s6.Set6Trial):
    def upload_helpers(self) -> dict:
        helpers = super().upload_helpers()
        helpers[HELPER8] = self.lab.put_file(self.root, self.record, self.plan, self.node_name, HERE / HELPER8, HELPER8)[1]
        return helpers

    # -- guest access -------------------------------------------------------------------------------------------

    def native8(self, mode: str, **arguments: Any) -> dict:
        payload = base64.b64encode(json.dumps(arguments).encode()).decode()
        return self.helper(HELPER8, mode, "--args-b64", payload, timeout=420)

    def snap8(self, label: str, mode: str, **arguments: Any) -> dict:
        value = self.native8(mode, **arguments)
        dump = value.pop("nginx_dump", None)
        if isinstance(dump, dict):
            name = f"nginx-T/{dump['sha256'][:16]}.txt"
            seen = self.state.setdefault("set8_dumps", {})
            if dump["sha256"] not in seen:
                seen[dump["sha256"]] = self.ev.write_text(name, dump.pop("text") or "")
            dump.pop("text", None)
            dump["file"] = name
            value["nginx_dump"] = dump
        self.native_sequence += 1
        name = f"native/{self.native_sequence:03d}-{label}.json"
        self.record_json(name, value)
        self.current.setdefault("natives", []).append({"label": label, "mode": mode, "file": name,
                                                       "owner_action": mode.startswith("owner-")})
        value["_file"] = f"{self.step_dir}/{name}"
        return value

    def owner8(self, label: str, mode: str, **arguments: Any) -> dict:
        value = self.snap8("owner-" + label, mode, **arguments)
        self.current.setdefault("owner_actions", []).append({"label": label, "mode": mode, "at": value.get("at")})
        return value

    def keep_file(self, label: str, record: dict | None) -> str | None:
        """The exact text of a vhost or pool as read, kept as its own file beside its SHA-256."""
        if not isinstance(record, dict) or not record.get("exists") or record.get("text") is None:
            return None
        name = f"files/{label}.txt"
        self.keep_text(name, record["text"])
        return f"{self.step_dir}/{name}"

    def read(self, label: str, *, dump: bool = False, pool: bool = False, others: bool = True) -> dict:
        domains = [DOMAIN_A] + ([DOMAIN_B, sw.SITE_DOMAIN] if others else [])
        value = self.snap8(f"{label}-read", "read-site", domains=domains, dump=dump, pool=pool)
        site = value["sites"][DOMAIN_A]
        value["_kept"] = {"vhost": self.keep_file(f"{label}-{DOMAIN_A}.conf", site["vhost"])}
        if pool:
            value["_kept"]["pool"] = self.keep_file(f"{label}-{DOMAIN_A}-pool.conf", site.get("pool"))
        return value

    def http(self, label: str, domain: str, path: str) -> dict:
        return self.snap8(f"http-{label}-{domain}{path.replace('/', '_')}", "read-http", domain=domain, path=path)

    # -- the Panel's side ---------------------------------------------------------------------------------------

    def wait_api(self, limit: float = 150) -> dict:
        started, last = time.time(), None
        while time.time() - started < limit:
            try:
                response = self.api("GET", "/api/v1/panel/version", purpose="set8: is the Panel answering", timeout=10)
                last = response.status
                if response.status == 401:
                    self.session_cookie()
                    continue
                if response.status == 200:
                    return {"answering": True, "seconds": round(time.time() - started, 1), "status": 200}
            except Exception as exc:  # noqa: BLE001 - the Panel may still be starting
                last = type(exc).__name__
            time.sleep(2)
        return {"answering": False, "seconds": round(time.time() - started, 1), "last": last}

    def owner_view(self, label: str) -> dict:
        """What the owner sees through the Panel's API after a trigger (the screens' own routes)."""
        domain_id = self.state["set8"]["a_id"]
        view = {}
        for key, path in (("domains", "/api/v1/domains"), ("general", f"/api/v1/domains/{domain_id}/general"),
                          ("php", f"/api/v1/domains/{domain_id}/php"), ("dashboard", "/api/v1/dashboard"),
                          ("components", "/api/v1/managed-services"), ("audit", "/api/v1/audit-logs?limit=25")):
            entry = self.call(f"set8 {label}: {key} (GET {path})", "GET", path, timeout=90)
            view[key] = {"status": entry["status"], "json": entry.get("json"), "text": entry.get("text")}
        rows = view["domains"]["json"] if isinstance(view["domains"]["json"], list) else []
        view["a_row"] = next((r for r in rows if isinstance(r, dict) and r.get("domain_name") == DOMAIN_A), None)
        audit = view["audit"]["json"]
        entries = audit if isinstance(audit, list) else (audit or {}).get("entries") or []
        view["audit_ids"] = [e.get("id") for e in entries if isinstance(e, dict)]
        view["_entries"] = entries
        return view

    def general_save(self, label: str) -> dict:
        domain_id = self.state["set8"]["a_id"]
        shown = self.call(f"set8 {label}: General settings read", "GET", f"/api/v1/domains/{domain_id}/general")
        body = shown["_parsed"] if isinstance(shown["_parsed"], dict) else {}
        sent = {"document_root": body.get("document_root"), "web_server": body.get("web_server") or "nginx",
                "redirect_www": bool(body.get("redirect_www"))}
        saved = self.call(f"set8 {label}: General settings saved unchanged (renders the vhost)", "POST",
                          f"/api/v1/domains/{domain_id}/general", sent, timeout=300)
        return {"read_status": shown["status"], "sent": sent, "status": saved["status"], "answer": saved.get("json")}

    # -- S0 -----------------------------------------------------------------------------------------------------

    def s0_sites(self) -> None:
        platform = self.snap4("platform", "read-platform")
        self.current["platform"] = {k: platform.get(k) for k in ("os_release", "kernel", "packages", "nginx_version",
                                                                  "php_fpm_programs")}
        ids = {}
        for domain in (DOMAIN_A, DOMAIN_B):
            created = self.call(f"S0 create PHP site {domain}", "POST", "/api/v1/domains/create",
                                {"domain": domain, "project_type": "php", "ssl_type": "none"}, timeout=600)
            answer = created["_parsed"] if isinstance(created["_parsed"], dict) else {}
            ids[domain] = answer.get("DomainID") or answer.get("domain_id")
            self.check(f"S0: the PHP site {domain} is created through the Panel (200 with its id)",
                       created["status"] == 200 and isinstance(ids[domain], int), created.get("answer") or answer)
            if not isinstance(ids[domain], int):
                raise base.StepFailed(f"{domain} was not created")
            self.owner8(f"files-{domain}", "owner-files", domain_id=ids[domain])
        baseline = self.read("s0-baseline", dump=True, pool=True)
        site = baseline["sites"][DOMAIN_A]
        vhost, pool = site["vhost"], site.get("pool") or {}
        self.state["set8"] = {"a_id": ids[DOMAIN_A], "b_id": ids[DOMAIN_B], "baseline_text": vhost.get("text"),
                              "baseline_sha": vhost.get("sha256"), "site_id": site.get("site_id"),
                              "pool_path": pool.get("path"), "pool_text": pool.get("text"), "pool_sha": pool.get("sha256")}
        self.current["baseline"] = {"vhost": {k: vhost.get(k) for k in ("path", "sha256", "size", "inode", "mode", "owner",
                                                                        "group", "uid", "gid", "lsattr", "mtime_ns")},
                                    "enabled": site["enabled"], "pool": {k: pool.get(k) for k in ("path", "sha256", "inode",
                                                                                                 "mode", "owner", "group")},
                                    "site_id": site.get("site_id"), "files": baseline["_kept"],
                                    "nginx_dump": baseline.get("nginx_dump"), "nginx_test": baseline.get("nginx_test")}
        answers = {d: self.http(f"s0-{d}", d, PROBE_PATH) for d in (DOMAIN_A, DOMAIN_B)}
        ini = self.http("s0-ini", DOMAIN_A, INI_PATH)
        page = {d: s4.parse_probe(a) for d, a in answers.items()}
        self.current["baseline"]["probe"] = page
        self.current["baseline"]["ini"] = {"status": ini.get("status"), "body": ini.get("body")}
        # Lab preparation: the Panel restarted once with no owner edit, so that the text the Panel writes at a start
        # (the reference every run is restored to) is recorded beside the text it wrote when it created the site.
        self.snap8("s0-lab-panel-restart-reference", "owner-panel", action="restart", wait=150)
        self.current["lab_reference_restart"] = self.wait_api()
        reference = self.read("s0-reference-after-a-panel-restart", dump=True, pool=True)
        ref = reference["sites"][DOMAIN_A]["vhost"]
        self.state["set8"].update(reference_text=ref.get("text"), reference_sha=ref.get("sha256"),
                                  reference_uid=ref.get("uid"), reference_gid=ref.get("gid"), reference_mode=ref.get("mode"))
        self.current["reference"] = {"vhost": {k: ref.get(k) for k in ("sha256", "size", "inode", "mode", "owner", "group",
                                                                      "uid", "gid", "mtime_ns", "ctime_ns")},
                                     "file": reference["_kept"]["vhost"],
                                     "equals_the_creation_render": ref.get("sha256") == vhost.get("sha256"),
                                     "diff_from_the_creation_render": self.diff("creation-render-vs-start-render",
                                                                                vhost.get("text") or "", ref.get("text") or "")}
        self.check("S0: A's vhost, its enabled link and its pool file were read (bytes, inode, mode, owner, attributes)",
                   bool(vhost.get("sha256")) and site["enabled"].get("kind") == "symlink" and bool(pool.get("sha256")),
                   self.current["baseline"])
        self.check("S0: `nginx -t` passes and `nginx -T` was read", (baseline.get("nginx_test") or {}).get("returncode") == 0
                   and (baseline.get("nginx_dump") or {}).get("returncode") == 0, baseline.get("nginx_dump"))
        self.check("S0: both PHP sites execute their probe page through nginx and PHP-FPM",
                   all(p["executed"] for p in page.values()), page)
        self.current["view"] = self.owner_view("s0")

    # -- one run of a, b, c, d, e --------------------------------------------------------------------------------

    def restore(self, label: str, pool: bool = False) -> dict:
        seen = self.state["set8"]
        arguments = {"domain": DOMAIN_A, "content_b64": b64(seen.get("reference_text") or seen["baseline_text"]),
                     "uid": seen.get("reference_uid", 0), "gid": seen.get("reference_gid", 0),
                     "file_mode": seen.get("reference_mode") or "0644"}
        if pool and seen.get("pool_path"):
            arguments.update(pool_path=seen["pool_path"], pool_b64=b64(seen["pool_text"]))
        value = self.snap8(f"{label}-lab-restore", "lab-restore", **arguments)
        self.current.setdefault("lab_preparation", []).append({"label": label, "file": value["_file"],
                                                              "sha256_matches": value.get("sha256_matches")})
        return value

    def trigger(self, label: str, trigger: str) -> dict:
        if trigger in ("start", "restart"):
            acted = self.owner8(f"{label}-panel-{trigger}", "owner-panel", action=trigger, wait=150)
            api = self.wait_api()
            return {"kind": f"owner: systemctl {trigger} celikpanel-panel", "file": acted["_file"],
                    "is_active": acted.get("is_active"), "waited_seconds": acted.get("waited_seconds"),
                    "reconcile_lines_seen": acted.get("reconcile_lines_seen"), "api": api,
                    "ok": acted.get("is_active") == "active" and api["answering"]}
        saved = self.general_save(label)
        return {"kind": "Panel: POST /api/v1/domains/{id}/general with the values GET answered", **saved,
                "ok": saved["status"] == 200, "is_active": "active"}

    def run_case(self, run: str, scenario: str, action: str, trigger: str, stopped: bool) -> dict:
        seen = self.state["set8"]
        record = {"run": run, "scenario": scenario, "owner_action": action, "trigger": trigger,
                  "panel_stopped_while_the_owner_edits": stopped, "started_at": base.utc_now()}
        self.restore(run)
        pre = self.read(f"{run}-0-restored")
        since = int(pre["epoch"]) - 2
        pre_view = self.owner_view(f"{run} before")
        if stopped:
            record["panel_stop"] = self.owner8(f"{run}-panel-stop", "owner-panel", action="stop")["_file"]
        edit = self.owner8(f"{run}-edit", "owner-vhost", domain=DOMAIN_A, action=action, reload=True)
        edited = self.read(f"{run}-1-edited")
        path, wanted = OWNER_PATHS[action]
        http_edited = self.http(f"{run}-1-edited", DOMAIN_A, path)
        if not stopped:
            time.sleep(RUNNING_EDIT_WAIT)
            waited = self.read(f"{run}-1b-before-the-trigger")
            record["file_before_the_trigger_equals_the_edit"] = (waited["sites"][DOMAIN_A]["vhost"].get("sha256")
                                                                 == edited["sites"][DOMAIN_A]["vhost"].get("sha256")
                                                                 and waited["sites"][DOMAIN_A]["vhost"].get("inode")
                                                                 == edited["sites"][DOMAIN_A]["vhost"].get("inode"))
        fired = self.trigger(run, trigger)
        time.sleep(SETTLE)
        after = self.read(f"{run}-2-after-the-trigger", dump=True)
        http_after = self.http(f"{run}-2-after", DOMAIN_A, path)
        probes = {d: s4.parse_probe(self.http(f"{run}-2-after-probe", d, PROBE_PATH))["executed"] for d in (DOMAIN_A, DOMAIN_B)}
        window = self.snap8(f"{run}-journal", "read-window", since_epoch=since)
        view = self.owner_view(f"{run} after") if fired.get("ok") or fired.get("is_active") == "active" else None
        record.update(self.judge(run, action, pre, edit, edited, after, http_edited, http_after, wanted, fired, window,
                                 pre_view, view, probes))
        record["finished_at"] = base.utc_now()
        return record

    def judge(self, run, action, pre, edit, edited, after, http_edited, http_after, wanted, fired, window,
              pre_view, view, probes) -> dict:
        seen = self.state["set8"]
        e_v, a_v = edited["sites"][DOMAIN_A], after["sites"][DOMAIN_A]
        panel_shas = {seen["baseline_sha"], seen.get("reference_sha")} - {None}
        cls = file_class(a_v["vhost"], panel_shas, e_v["vhost"].get("sha256"))
        if action == "remove":
            # the owner's change is the absence of the file and of its link
            cls = ("owner-bytes-kept" if not a_v["vhost"].get("exists") and not a_v["enabled"].get("exists")
                   else "panel-text-written" if a_v["vhost"].get("sha256") in panel_shas else "other-bytes")
        panel_lines = window["by_unit"].get("celikpanel-panel.service") or []
        agent_lines = window["by_unit"].get("celikpanel-agent.service") or []
        nginx_lines = window["by_unit"].get("nginx.service") or []
        workers = {"edited": edited.get("nginx"), "after": after.get("nginx")}
        reloaded_after_edit = (edited.get("nginx") or {}).get("workers") != (after.get("nginx") or {}).get("workers")
        new_audit = [e for e in (view or {}).get("_entries") or []
                     if isinstance(e, dict) and e.get("id") not in (pre_view.get("audit_ids") or [])]
        row_before, row_after = pre_view.get("a_row") or {}, (view or {}).get("a_row") or {}
        row_changes = {k: [row_before.get(k), row_after.get(k)] for k in sorted(set(row_before) | set(row_after))
                       if row_before.get(k) != row_after.get(k) and k not in ("updated_at",)}
        words = ("owner", "edited", "by hand", "modified", "differs", "conflict", "changed on disk", "not written by")
        mentioned = [w for w in words if view and any(w in answers_text(view[k]) for k in ("a_row", "general", "dashboard"))]
        audit_mentions = [e for e in new_audit if any(w in json.dumps(e).lower() for w in ("vhost", "nginx", "owner", "config"))]
        reported = bool(mentioned or audit_mentions)
        panel_active = fired.get("is_active") == "active"
        verdict = provisional_verdict(cls, panel_active, fired.get("ok"), reported)
        served_edited = wanted is not None and wanted in str(http_edited.get("body") or "")
        served_after = wanted is not None and wanted in str(http_after.get("body") or "")
        out = {
            "files": {"restored": pre["_kept"]["vhost"], "edited": edited["_kept"]["vhost"], "after": after["_kept"]["vhost"],
                      "nginx_dump_after": (after.get("nginx_dump") or {}).get("file"), "journal": window["_file"]},
            "vhost": {"panel_text_sha256": {"creation_render": seen["baseline_sha"], "start_render": seen.get("reference_sha")}, "edited": self.brief(e_v), "after": self.brief(a_v)},
            "class": cls, "inode_changed": e_v["vhost"].get("inode") != a_v["vhost"].get("inode"),
            "owner_action_nginx_test": (edit.get("nginx_test") or {}).get("returncode"),
            "owner_action_reload": (edit.get("reload") or {}).get("returncode"),
            "trigger": {k: v for k, v in fired.items() if k != "answer"}, "trigger_answer": fired.get("answer"),
            "http": {"path": OWNER_PATHS[action][0], "owner_text": wanted,
                     "after_the_edit": {"status": http_edited.get("status"), "body": (http_edited.get("body") or "")[:200],
                                        "owner_text_served": served_edited},
                     "after_the_trigger": {"status": http_after.get("status"), "body": (http_after.get("body") or "")[:200],
                                           "owner_text_served": served_after},
                     "probe_pages_executed_after": probes},
            "nginx": {"workers": workers, "workers_changed_after_the_edit": reloaded_after_edit,
                      "reload_lines": [l for l in nginx_lines if "eload" in l],
                      "nginx_test_after": after.get("nginx_test")},
            "journal": {"panel": [l for l in panel_lines if "vhost" in l.lower() or "nginx" in l.lower() or DOMAIN_A in l],
                        "agent": [l for l in agent_lines if "vhost" in l.lower() or "nginx" in l.lower() or DOMAIN_A in l],
                        "panel_lines": len(panel_lines), "agent_lines": len(agent_lines)},
            "owner_view": {"domains_row_changes": row_changes, "new_audit_entries": new_audit, "words_found": mentioned,
                           "statuses": {k: (view or {}).get(k, {}).get("status") for k in
                                        ("domains", "general", "php", "dashboard", "components", "audit")}},
            "reported_to_the_owner_by_the_panel": reported, "panel_active": panel_active,
            "provisional_verdict": verdict}
        self.check(f"{run}: measured the file before and after the trigger, the journal window and the HTTP answers",
                   "exists" in e_v["vhost"] and "exists" in a_v["vhost"] and bool(window["by_unit"])
                   and (http_after.get("status") is not None or http_after.get("error") is not None), out["files"])
        self.check(f"{run}: the owner's change was in service before the trigger (nginx -t passed, reloaded"
                   + (", served" if wanted else ", site no longer served") + ")",
                   (edit.get("nginx_test") or {}).get("returncode") == 0
                   and (served_edited if wanted else http_edited.get("status") is not None), out["http"]["after_the_edit"])
        self.check(f"{run}: the trigger ran ({fired['kind']})", True if fired.get("ok") or not panel_active else None,
                   out["trigger"])
        self.note(f"{run}: {verdict} (file {cls}, inode {'changed' if out['inode_changed'] else 'same'}, owner's answer "
                  f"{'still served' if served_after else 'not served'} after the trigger)", out["vhost"])
        return out

    @staticmethod
    def brief(site: dict) -> dict:
        v, l = site.get("vhost") or {}, site.get("enabled") or {}
        return {"vhost": {k: v.get(k) for k in ("exists", "kind", "sha256", "size", "inode", "mode", "owner", "group",
                                                 "lsattr", "immutable", "mtime_ns", "ctime_ns")},
                "enabled": {k: l.get(k) for k in ("exists", "kind", "link_target", "inode")}}

    def scenario(self, key: str) -> None:
        self.current["runs"] = []
        for spec in [r for r in RUNS if r[1] == key]:
            try:
                self.current["runs"].append(self.run_case(*spec))
            except Exception as exc:  # noqa: BLE001 - one run's harness error never hides the other runs
                error = self.redactor.text(f"{type(exc).__name__}: {exc}")[:1200]
                stderr = getattr(exc, "stderr", None)
                if stderr:
                    error += " | guest stderr: " + (stderr if isinstance(stderr, str) else stderr.decode("utf-8", "replace"))[-600:]
                self.current["runs"].append({"run": spec[0], "error": error})
                self.check(f"{spec[0]}: the run completed", None, error)
                self.recover_panel(spec[0])
        self.current["verdicts"] = {r["run"]: r.get("provisional_verdict") for r in self.current["runs"]}

    def recover_panel(self, label: str) -> None:
        """Lab recovery after a harness error inside a run: the Panel's unit is started again if it is not active, so
        that the next run begins from a running Panel. Recorded as lab preparation."""
        try:
            state = self.snap8(f"{label}-lab-panel-state", "read-site", domains=[DOMAIN_A])
            if (state["units"].get("celikpanel-panel.service") or {}).get("ActiveState") != "active":
                self.snap8(f"{label}-lab-panel-start", "owner-panel", action="start", wait=150)
                self.current.setdefault("lab_preparation", []).append({"label": label, "what": "Panel unit started again"})
            self.wait_api()
        except Exception as exc:  # noqa: BLE001
            self.current.setdefault("lab_preparation", []).append({"label": label, "recovery_error": type(exc).__name__})

    # -- f ------------------------------------------------------------------------------------------------------

    def scenario_f(self) -> None:
        seen = self.state["set8"]
        domain_id = seen["a_id"]
        self.restore("f", pool=True)
        pre = self.read("f-0-restored", pool=True)
        since = int(pre["epoch"]) - 2
        pre_view = self.owner_view("f before")
        ini0 = self.http("f-0", DOMAIN_A, INI_PATH)
        self.owner8("f-panel-stop", "owner-panel", action="stop")
        edit = self.owner8("f-pool-edit", "owner-pool", site_id=seen["site_id"])
        edited = self.read("f-1-edited", pool=True)
        ini1 = self.http("f-1-edited", DOMAIN_A, INI_PATH)
        fired = self.trigger("f1", "start")
        time.sleep(SETTLE)
        started = self.read("f-2-after-the-start", pool=True)
        ini2 = self.http("f-2-after-the-start", DOMAIN_A, INI_PATH)
        shown = self.call("f: pool settings read (GET /php/pool)", "GET", f"/api/v1/domains/{domain_id}/php/pool")
        cfg = shown["_parsed"] if isinstance(shown["_parsed"], dict) else {}
        tunables = {k: cfg.get(k) for k in ("pm", "pm_max_children", "pm_start_servers", "pm_min_spare_servers",
                                            "pm_max_spare_servers", "pm_max_requests")}
        saved = self.call("f: pool settings saved unchanged (POST /php/pool, process-manager values only)", "POST",
                          f"/api/v1/domains/{domain_id}/php/pool", {"version": "", "pool_config": tunables}, timeout=300)
        time.sleep(SETTLE)
        after_save = self.read("f-3-after-the-pool-save", pool=True)
        ini3 = self.http("f-3-after-the-pool-save", DOMAIN_A, INI_PATH)
        php = self.call("f: PHP settings read (GET /php)", "GET", f"/api/v1/domains/{domain_id}/php")
        body = php["_parsed"] if isinstance(php["_parsed"], dict) else {}
        current, available = body.get("php_version"), body.get("available_versions") or []
        others = [v for v in available if v != current]
        switch = {"current": current, "available": available}
        if others:
            moved = self.call(f"f: PHP version switched to {others[0]} (POST /php)", "POST", f"/api/v1/domains/{domain_id}/php",
                              {"php_version": others[0]}, timeout=300)
            switch.update(kind="a real switch", to=others[0], status=moved["status"], answer=moved.get("json"))
        else:
            same = self.call(f"f: PHP version 'switch' to the same version {current} (POST /php; the only installed one)",
                             "POST", f"/api/v1/domains/{domain_id}/php", {"php_version": current}, timeout=300)
            switch.update(kind="same version (no other version is installed; the handler then only updates the row)",
                          to=current, status=same["status"], answer=same.get("json"))
        time.sleep(SETTLE)
        after_switch = self.read("f-4-after-the-php-switch", pool=True)
        ini4 = self.http("f-4-after-the-php-switch", DOMAIN_A, INI_PATH)
        window = self.snap8("f-journal", "read-window", since_epoch=since)
        view = self.owner_view("f after")
        added = edit.get("added_lines") or []

        def pool_state(reading: dict) -> dict:
            pool = reading["sites"][DOMAIN_A].get("pool") or {}
            text = pool.get("text") or ""
            return {"path": pool.get("path"), "exists": pool.get("exists"), "sha256": pool.get("sha256"),
                    "inode": pool.get("inode"), "mode": pool.get("mode"), "owner": pool.get("owner"),
                    "owner_lines_present": [line for line in added if line in text], "file": reading["_kept"].get("pool")}
        stages = {"restored": pool_state(pre), "edited": pool_state(edited), "after_the_panel_start": pool_state(started),
                  "after_the_pool_save": pool_state(after_save), "after_the_php_switch": pool_state(after_switch)}
        ini = {k: (v.get("body") or "").strip()[:120] for k, v in
               (("restored", ini0), ("edited", ini1), ("after_the_panel_start", ini2), ("after_the_pool_save", ini3),
                ("after_the_php_switch", ini4))}
        self.current["f"] = {"owner_action": {"added_lines": added, "php_fpm_test": (edit.get("php_fpm_test") or {}).get("returncode"),
                                              "reload": (edit.get("reload") or {}).get("returncode"), "file": edit["_file"]},
                             "stages": stages, "memory_limit_served": ini, "trigger": fired,
                             "pool_save": {"read_status": shown["status"], "sent": tunables, "status": saved["status"],
                                           "answer": saved.get("json")},
                             "php_switch": switch, "journal": window["_file"],
                             "journal_lines": {u: [l for l in lines if "pool" in l.lower() or "php" in l.lower()][:60]
                                               for u, lines in window["by_unit"].items()},
                             "owner_view_statuses": {k: view.get(k, {}).get("status") for k in ("domains", "php", "audit")},
                             "new_audit_entries": [e for e in view.get("_entries") or []
                                                   if isinstance(e, dict) and e.get("id") not in pre_view.get("audit_ids", [])]}
        self.check("f: the pool was read at every stage and the owner's lines were in it after the edit",
                   all(s["exists"] for s in stages.values()) and len(stages["edited"]["owner_lines_present"]) == len(added) == 2,
                   stages)
        self.check("f: the owner's directive was in service before the Panel was started (memory_limit 193M served)",
                   "193M" in ini["edited"], ini)
        self.check("f: the Panel start, the pool save and the PHP request were answered",
                   True if fired.get("ok") and saved["status"] is not None and switch.get("status") is not None else None,
                   {"start": fired.get("ok"), "save": saved["status"], "switch": switch.get("status")})
        for stage in ("after_the_panel_start", "after_the_pool_save", "after_the_php_switch"):
            self.note(f"f {stage}: owner's lines present {len(stages[stage]['owner_lines_present'])}/2, pool sha "
                      f"{'unchanged' if stages[stage]['sha256'] == stages['edited']['sha256'] else 'changed'}, inode "
                      f"{'same' if stages[stage]['inode'] == stages['edited']['inode'] else 'changed'}, served {ini[stage]}",
                      stages[stage])

    # -- g ------------------------------------------------------------------------------------------------------

    def scenario_g(self) -> None:
        self.restore("g")
        pre = self.read("g-0-restored", dump=True)
        since = int(pre["epoch"]) - 2
        pre_view = self.owner_view("g before")
        self.owner8("g-panel-stop", "owner-panel", action="stop")
        lock = self.owner8("g-chattr", "owner-vhost", domain=DOMAIN_A, action="chattr-plus-i", reload=False)
        locked = self.read("g-1-locked")
        fired = self.trigger("g", "start")
        time.sleep(SETTLE)
        after = self.read("g-2-after-the-start", dump=True)
        http_after = {d: self.http("g-2-after", d, PROBE_PATH) for d in (DOMAIN_A, DOMAIN_B)}
        window = self.snap8("g-journal-start", "read-window", since_epoch=since)
        view = self.owner_view("g after") if fired.get("api", {}).get("answering") else None
        rotated = self.owner8("g-nginx-reload-as-logrotate", "owner-nginx-reload")
        later = self.read("g-3-after-an-nginx-reload")
        http_later = {d: self.http("g-3-after-reload", d, PROBE_PATH) for d in (DOMAIN_A, DOMAIN_B)}
        unlock = self.owner8("g-chattr-minus-i", "owner-vhost", domain=DOMAIN_A, action="chattr-minus-i", reload=False)
        window2 = self.snap8("g-journal-all", "read-window", since_epoch=since)
        sites = (DOMAIN_A, DOMAIN_B, sw.SITE_DOMAIN)
        table = {d: {"locked": self.brief(locked["sites"][d]), "after_the_start": self.brief(after["sites"][d]),
                     "after_an_nginx_reload": self.brief(later["sites"][d])} for d in sites}
        rewritten = {d: any(table[d]["locked"]["vhost"].get(k) != table[d]["after_the_start"]["vhost"].get(k)
                            for k in ("inode", "ctime_ns", "sha256")) for d in sites}
        panel_lines = window["by_unit"].get("celikpanel-panel.service") or []
        agent_lines = window["by_unit"].get("celikpanel-agent.service") or []
        self.current["g"] = {
            "chattr": (lock.get("chattr") or {}).get("returncode"), "lsattr_after_lock": locked["sites"][DOMAIN_A]["vhost"].get("lsattr"),
            "files": {"locked": locked["_kept"]["vhost"], "after_the_start": after["_kept"]["vhost"],
                      "after_an_nginx_reload": later["_kept"]["vhost"], "journal": window2["_file"]},
            "trigger": fired, "per_site": table, "vhost_touched_by_the_start (inode, ctime or bytes changed)": rewritten,
            "nginx": {"locked": locked.get("nginx"), "after_the_start": after.get("nginx"), "after_an_nginx_reload": later.get("nginx"),
                      "reload_lines": [l for l in window2["by_unit"].get("nginx.service") or [] if "eload" in l],
                      "nginx_test_after_the_start": after.get("nginx_test"), "nginx_test_after_the_reload": later.get("nginx_test")},
            "owner_reload": {"nginx_test": (rotated.get("nginx_test") or {}).get("returncode"),
                             "reload": (rotated.get("reload") or {}).get("returncode"), "file": rotated["_file"]},
            "http": {"after_the_start": {d: {"status": a.get("status"), "executed": s4.parse_probe(a)["executed"]} for d, a in http_after.items()},
                     "after_an_nginx_reload": {d: {"status": a.get("status"), "executed": s4.parse_probe(a)["executed"]} for d, a in http_later.items()}},
            "journal": {"panel": [l for l in panel_lines if "vhost" in l.lower() or "nginx" in l.lower() or "reconcile" in l.lower()],
                        "agent": [l for l in agent_lines if "vhost" in l.lower() or "nginx" in l.lower() or DOMAIN_A in l]},
            "owner_view": None if view is None else {
                "statuses": {k: view.get(k, {}).get("status") for k in ("domains", "general", "php", "dashboard", "components", "audit")},
                "a_row": view.get("a_row"), "a_row_before": pre_view.get("a_row"),
                "new_audit_entries": [e for e in view.get("_entries") or [] if isinstance(e, dict) and e.get("id") not in pre_view.get("audit_ids", [])]},
            "unlock": (unlock.get("chattr") or {}).get("returncode")}
        self.check("g: the immutable flag was set (lsattr shows i) before the Panel was started",
                   bool(locked["sites"][DOMAIN_A]["vhost"].get("immutable")), self.current["g"]["lsattr_after_lock"])
        self.check("g: measured A, B and the static site after the start and after an nginx reload, with the journal",
                   all(after["sites"][d]["vhost"].get("exists") is not None for d in sites) and bool(window2["by_unit"]),
                   self.current["g"]["files"])
        self.note("g: Panel active after the start: " + str(fired.get("is_active")) + "; API answering: "
                  + str((fired.get("api") or {}).get("answering")), fired)
        self.note("g: vhost files touched by the start (inode, ctime or bytes changed): " + json.dumps(rewritten), table)

    # -- the cell -----------------------------------------------------------------------------------------------

    def execute(self) -> dict:
        self.step("preflight", self.preflight)
        self.step("origin", self.origin, needs=("preflight",))
        self.step("baseline-install", self.baseline_install, needs=("origin",))
        self.step("owner-login", self.owner_login, needs=("baseline-install",))
        self.step("license", self.license, needs=("owner-login",))
        self.step("setup", self.setup, needs=("license",))
        self.step("site", self.site, needs=("setup",))
        self.section("S0-sites", "two PHP sites and A's vhost as the Panel wrote it", self.s0_sites, needs=("site",))
        for key in ("a", "b", "c", "d", "e"):
            self.section(f"{key}-owner-edit", SCENARIOS[key], lambda key=key: self.scenario(key), needs=("S0-sites",))
        self.section("f-owner-pool", SCENARIOS["f"], self.scenario_f, needs=("S0-sites",))
        self.section("g-immutable", SCENARIOS["g"], self.scenario_g, needs=("S0-sites",))
        self.step(s6.PIN_END_STEP, self.pin_at_the_end6)
        self.step("collect", self.collect)
        self.tunnel.close()
        verdicts = [s["verdict"] for s in self.steps]
        result = {"schema": base.RESULT_SCHEMA, "cell_kind": CELL_KIND, "native_evidence": False,
                  "cell": dataclasses.asdict(self.settings),
                  "identity": {k: self.identity[k] for k in ("cell_id", "node", "vm_uuid")},
                  "request_id": None, "provenance": base.provenance_for("good"),
                  "artifacts": {"baseline": {k: self.artifacts["baseline"][k] for k in ("version", "commit", "sha256")}},
                  "outcome": {"classification": "set8-measured", "final_status": None},
                  "setup": {"purpose": self.settings.purpose, "components": sorted(self.settings.components),
                            "waiting": self.state.get("setup_waiting")},
                  "set8": {k: v for k, v in (self.state.get("set8") or {}).items() if not k.endswith("_text")},
                  "sections": self.sections, "findings": self.state["findings"],
                  "steps": [{k: s.get(k) for k in ("name", "verdict", "reason", "started_at", "finished_at")} for s in self.steps],
                  "overall": base.overall(verdicts),
                  "note": "set8: a baseline of the published alpha.82 code; no update is started and no P0 row is judged."}
        self.step_dir = "result"
        return self.ev.finalize_upd1(result)


def build_plan(name: str, artifacts: dict, work_root: str, local_port: int) -> dict:
    settings = CELLS[name]
    return {"schema": "celikpanel/set8-plan/v1", "native_evidence": False, "cell": name, "cell_kind": CELL_KIND,
            "work_root": work_root, "local_port": local_port, "pinned_names": list(s6.PINNED_NAMES),
            "candidate": {k: artifacts["baseline"][k] for k in ("version", "commit", "sha256")},
            "setup": {"purpose": settings.purpose, "components": sorted(settings.components), "dns_mode": "external"},
            "steps": [s6.PIN_STEP, "preflight", "origin", "baseline-install", "owner-login", "license", "setup", "site",
                      "S0-sites"] + [f"{k}-owner-edit" for k in "abcde"] + ["f-owner-pool", "g-immutable",
                                                                            s6.PIN_END_STEP, "collect"],
            "runs": [dict(zip(("run", "scenario", "owner_action", "trigger", "panel_stopped_while_the_owner_edits"), r)) for r in RUNS],
            "rule": "the driver calls only the Panel's HTTP API as the logged-in owner; every native fact is an SSH reading; "
                    "owner actions and lab preparation on the guest are recorded as such; no certificate authority and no "
                    "licence service is contacted by the driver"}


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="command", required=True)
    for name in ("plan", "run"):
        cmd = sub.add_parser(name)
        cmd.add_argument("--cell", required=True, choices=sorted(CELLS))
        cmd.add_argument("--artifacts", required=True, type=Path)
        cmd.add_argument("--work-root", required=True)
        cmd.add_argument("--local-port", type=int, default=18443)
        if name == "plan":
            cmd.add_argument("--dry-run", action="store_true", help="validate the plan without any guest")
        else:
            cmd.add_argument("--execute", action="store_true")
    args = parser.parse_args(argv)
    base.validate_work_root(args.work_root)
    if not 1024 < args.local_port < 65536:
        parser.error("--local-port must be an unprivileged loopback port")
    document = json.loads(args.artifacts.read_text())
    base.configure_labels(document)
    cell = CELLS[args.cell].cell
    if args.command == "plan":
        base.validate_cell_artifacts(document, cell, check_files=not args.dry_run)
        print(json.dumps(build_plan(args.cell, document, args.work_root, args.local_port), indent=2, sort_keys=True))
        return 0
    if not args.execute:
        parser.error("run mutates one registered disposable guest and requires --execute")
    base.validate_cell_artifacts(document, cell)
    result = Set8Trial(CELLS[args.cell], document, args.work_root, args.local_port).execute()
    print(json.dumps({"overall": result["overall"], "cell_kind": CELL_KIND,
                      "sections": {k: v.get("verdict") for k, v in result["sections"].items()}}, sort_keys=True))
    return 0 if result["overall"] != "failed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
