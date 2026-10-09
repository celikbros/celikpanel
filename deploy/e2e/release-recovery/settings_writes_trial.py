#!/usr/bin/env python3
"""set1 cell kind ``settings-writes``: the Panel's writes of the owner's native configuration, measured natively.

One cell on one fresh disposable QEMU guest (lab.py). The driver installs the candidate as the upd1 cells do
(current-source baseline B with the acceptance-fixture license; ``owner_update_trial.Trial`` steps preflight,
origin, baseline-install, owner-login, license, setup), runs the owner's server setup with the profile that gives
web + mail + databases, creates one site, and then runs seven sections. In every section the driver is the owner's
browser: it only calls the Panel's HTTP API, and every native fact is read over SSH by ``guest_settings_native.py``
(``read-*`` modes). Where a section needs the owner to have done something on the server by hand (a crontab edit,
a main.cf edit, a hardening step, a stopped service), the lab does exactly that through the helper's ``owner-*``
modes and records it as an owner action.

  S1 scheduled tasks (crontab)        S2 server mail policy (main.cf)     S3 automatic backup schedule
  S4 postgresql.conf and pg_hba.conf  S5 the MariaDB option file          S6 catch-all and mail queue
  S7 owner, group and mode of every file the Panel wrote
  S8 (set2) service actions on the real units: start, stop, restart and reload through the Services page's route

set2 (2026-10-11): the expectations of S1 f, S2 c/f/g/h, S4 g, S5 c and S6 follow the corrections the product made
after the first native run (set1), and the cells ``set2-*`` name that run. The ``set1-*`` names stay valid.

No update is started. Every result carries ``native_evidence: false``; the owner judges it.

  settings_writes_trial.py plan --cell set1-debian13 --artifacts A.json --work-root /var/tmp/cp-release-drill-X [--dry-run]
  settings_writes_trial.py run  --cell set1-debian13 --artifacts A.json --work-root /var/tmp/cp-release-drill-X --execute
"""
from __future__ import annotations

import argparse
import base64
import dataclasses
import difflib
import hashlib
import json
from pathlib import Path
import re
import sys
import time
from typing import Any, Callable
from urllib.parse import quote

HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    sys.path.insert(0, str(HERE))
import owner_update_trial as base  # noqa: E402

CELL_KIND = "settings-writes"
HELPER = "guest_settings_native.py"
SITE_DOMAIN = "set1-owner.test"
MEBIBYTE = 1024 * 1024
MAIL_PRESET = ("nginx", "php-fpm", "mariadb", "postfix", "dovecot", "roundcube", "rspamd")
WEB_PRESET = ("nginx", "php-fpm", "mariadb")
DNSBL_ZONE = "dnsbl.set1-lab.test"
OWNER_RESTRICTIONS = ("reject_unknown_sender_domain", "check_policy_service unix:private/policyd-spf")
POLICY_BASELINE = ("permit_mynetworks", "permit_sasl_authenticated", "reject_unauth_destination")
OWNER_MACRO = "set1_owner_checks"
MAIN_CF = "/etc/postfix/main.cf"
MAIN_CF_TYPO = "default_process_limit = 200 # raised for the campaign"
PANEL_JOB = {"schedule": "*/15 * * * *", "command": "/usr/bin/true set1-panel-job"}
PANEL_JOB_EDITED = "*/20 * * * *"
OWNER_CRON_LINES = "# set1 owner note: keep this line\n17 3 * * * /usr/bin/true set1-owner-job\n"
CRON_FAULTS = ("cron-allow", "spool-relocated")
# Not "plenty" or "many": MariaDB reads a leading size suffix (k, m, g, t, p, e) and takes them as 0 with a suffix,
# which it adjusts to the minimum and accepts (set1 Debian run-a). "unlimited" starts with no suffix letter.
MARIADB_UNUSABLE_VALUE = "unlimited"
MARIADB_ADJUSTED_VALUE = "plenty"
HBA_GOOD_LINE = "host    all             all             10.99.0.0/24            scram-sha-256"
HBA_BAD_LINE = "host    all             all             10.99.1.0/24"
OWNER_NOTE = "# set1 owner note: edited by hand on the server"
AUTO_SCAN_MAX_AGE_SECONDS = 300   # web/src/components/ServiceList.tsx
DEBIAN_PG = re.compile(r"^/etc/postgresql/([0-9]+(?:\.[0-9]+)?)/([A-Za-z0-9][A-Za-z0-9_.-]*)/postgresql\.conf$")


@dataclasses.dataclass(frozen=True)
class SettingsCell:
    name: str
    node: str
    purpose: str
    components: tuple
    mail: bool

    @property
    def cell(self) -> base.Cell:
        # The upd1 steps this cell reuses read these fields; "good" names the sealed candidate the fixture origin
        # serves (never installed here: no update is started).
        return base.Cell(self.name, self.node, "good", None, self.mail)

    @property
    def platform(self) -> str:
        return "ubuntu" if self.node == "ubuntu" else "debian13-arch"


CELLS = {
    "set1-debian13": SettingsCell("set1-debian13", "debian13", "web_mail", MAIL_PRESET + ("postgresql",), True),
    "set1-ubuntu": SettingsCell("set1-ubuntu", "ubuntu", "web_mail", MAIL_PRESET + ("postgresql",), True),
    # Mail is unsupported on Arch (the plan refuses dovecot); PostgreSQL and MariaDB are in its catalogue.
    "set1-arch": SettingsCell("set1-arch", "arch", "web", WEB_PRESET + ("postgresql",), False),
    # set2: the same three guests and profiles, measured after the corrections of 2026-10-10.
    "set2-debian13": SettingsCell("set2-debian13", "debian13", "web_mail", MAIL_PRESET + ("postgresql",), True),
    "set2-ubuntu": SettingsCell("set2-ubuntu", "ubuntu", "web_mail", MAIL_PRESET + ("postgresql",), True),
    "set2-arch": SettingsCell("set2-arch", "arch", "web", WEB_PRESET + ("postgresql",), False),
}

SECTIONS = (
    ("S1-cron", "scheduled tasks: the owner's crontab", False),
    ("S2-mail-policy", "server mail policy: the owner's main.cf", True),
    ("S3-backup-schedule", "automatic backup schedule", False),
    ("S4-postgresql", "postgresql.conf and pg_hba.conf", False),
    ("S5-mariadb", "the MariaDB option file", False),
    ("S6-catchall-queue", "catch-all and mail queue", True),
    ("S7-file-metadata", "owner, group and mode of the files the Panel wrote", False),
    ("S8-service-actions", "service actions on the real units (the Services page's route)", False),
)
# What each daemon writes to the journal when it has taken a reload (S8); PostgreSQL is asked for its load time.
RELOAD_MARKERS = {"postfix": ("reload -- version", "refreshing the Postfix mail system"),
                  "dovecot": ("SIGHUP received", "reloading configuration"),
                  "nginx": ("Reloaded nginx", "Reloaded A high performance", "Reloaded nginx.service", "signal process started"),
                  "postgresql": ("received SIGHUP",), "mariadb": ()}
PG_REFUSED_VALUE = "eight-megabytes"
# Refusals of a service action that are sent before anything is done (the setup or another package task is at work).
BUSY_CODES = ("server_setup_busy", "HOST_MUTATION_BUSY", "service_operation_busy")


# ---------------------------------------------------------------------------
# Pure rules (covered offline by test_settings_writes_trial.py)
# ---------------------------------------------------------------------------

def site_username(domain: str) -> str:
    """internal/services.SiteUsername: '.' and '-' become '_', cut at 32."""
    return domain.replace(".", "_").replace("-", "_")[:32]


def split_restrictions(value: str) -> list:
    """Postfix's own split of a restriction list: commas and whitespace, braces group one element."""
    elements, depth, current = [], 0, ""
    for char in value:
        if char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
        if depth == 0 and char in ", \t\r\n":
            if current:
                elements.append(current)
            current = ""
        else:
            current += char
    if current:
        elements.append(current)
    return elements


def owner_restriction_elements(current: str) -> list:
    """What the owner's hand edit holds: the list as it is (or the stock three), then the owner's two additions."""
    elements = split_restrictions(current) or list(POLICY_BASELINE)
    for addition in OWNER_RESTRICTIONS:
        elements += split_restrictions(addition)
    return elements


def restriction_block(name: str, elements: list) -> list:
    """The owner's layout: one restriction per continuation line (an argument stays on its restriction's line)."""
    items, index = [], 0
    while index < len(elements):
        item = elements[index]
        takes_argument = item.startswith(("check_", "reject_rbl", "reject_rhsbl", "permit_dnswl", "permit_rhswl"))
        if takes_argument and index + 1 < len(elements):
            item += " " + elements[index + 1]
            index += 1
        items.append(item)
        index += 1
    return [name + " ="] + ["    " + item + ("," if position < len(items) - 1 else "")
                            for position, item in enumerate(items)]


def set_main_cf_parameter(text: str, name: str, block: list | None) -> str:
    """Replace the parameter's logical line (with its continuation lines) by ``block``; append when absent;
    ``None`` removes it."""
    lines = text.split("\n")
    trailing = lines and lines[-1] == ""
    if trailing:
        lines = lines[:-1]
    start = re.compile(r"^" + re.escape(name) + r"\s*=")
    out, index, placed = [], 0, False
    while index < len(lines):
        if start.match(lines[index]):
            index += 1
            while index < len(lines) and lines[index][:1] in (" ", "\t"):
                index += 1
            if block is not None and not placed:
                out.extend(block)
                placed = True
            continue
        out.append(lines[index])
        index += 1
    if block is not None and not placed:
        out.extend(block)
    return "\n".join(out) + ("\n" if trailing or out else "")


def main_cf_logical_line(text: str, name: str) -> str | None:
    lines = text.split("\n")
    start = re.compile(r"^" + re.escape(name) + r"\s*=")
    found = None
    for index, line in enumerate(lines):
        if start.match(line):
            end = index + 1
            while end < len(lines) and lines[end][:1] in (" ", "\t"):
                end += 1
            found = "\n".join(lines[index:end])
    return found


def add_line(text: str, line: str) -> str:
    return text + ("" if text.endswith("\n") or not text else "\n") + line + "\n"


def remove_line(text: str, line: str) -> str:
    kept = [item for item in text.split("\n") if item != line]
    return "\n".join(kept)


def set_pg_setting(text: str, name: str, value: str) -> tuple:
    """As the settings editor does: only the setting's own line changes. An active line keeps its spacing and
    trailing comment; a commented default is replaced by the active setting; else the setting is added at the end.
    Returns (new text, how)."""
    lines = text.split("\n")
    active = re.compile(r"^(\s*)(" + re.escape(name) + r")(\s*=\s*|\s+)('(?:[^']|'')*'|[^\s#]+)(.*)$")
    commented = re.compile(r"^(\s*)#\s*(" + re.escape(name) + r")(\s*=\s*)('(?:[^']|'')*'|[^\s#]+)(.*)$")
    for pattern, how in ((active, "active-line"), (commented, "commented-default")):
        for index, line in enumerate(lines):
            match = pattern.match(line)
            if match:
                lines[index] = match.group(1) + name + match.group(3) + value + match.group(5)
                return "\n".join(lines), how
    return add_line(text, name + " = " + value), "appended"


SERVER_GROUPS = ("[mysqld]", "[mariadbd]", "[mariadb]", "[server]")


def set_mariadb_option(text: str, name: str, value: str) -> tuple:
    """Only the option's own line changes: an active line, else its commented default, else a new line under the
    server's group, else a new [mysqld] group at the end. Returns (new text, how, line index of the option)."""
    lines = text.split("\n")
    active = re.compile(r"^(\s*)(" + re.escape(name) + r")(\s*=\s*)(\S+)(.*)$")
    commented = re.compile(r"^(\s*)#\s*(" + re.escape(name) + r")(\s*=\s*)(\S+)(.*)$")
    for pattern, how in ((active, "active-line"), (commented, "commented-default")):
        for index, line in enumerate(lines):
            match = pattern.match(line)
            if match:
                lines[index] = match.group(1) + name + match.group(3) + value + match.group(5)
                return "\n".join(lines), how, index
    for index, line in enumerate(lines):
        if line.strip().lower() in SERVER_GROUPS:
            lines.insert(index + 1, name + " = " + value)
            return "\n".join(lines), "added-under-" + line.strip().lower(), index + 1
    body = text + ("" if text.endswith("\n") or not text else "\n")
    new = body + "\n[mysqld]\n" + name + " = " + value + "\n"
    return new, "added-new-group", len(new.split("\n")) - 2


def insert_after(text: str, index: int, line: str) -> str:
    lines = text.split("\n")
    lines.insert(index + 1, line)
    return "\n".join(lines)


def remove_local_rules(text: str) -> tuple:
    """pg_hba.conf without its ``local`` rules (the local administrator access among them)."""
    kept, removed = [], []
    for line in text.split("\n"):
        (removed if re.match(r"^\s*local\s", line) else kept).append(line)
    return "\n".join(kept), removed


def line_changes(before: str, after: str) -> dict:
    """Removed and added lines between two texts (order kept), and the unified diff."""
    diff = list(difflib.unified_diff(before.splitlines(), after.splitlines(), "before", "after", lineterm="", n=2))
    removed = [line[1:] for line in diff if line.startswith("-") and not line.startswith("---")]
    added = [line[1:] for line in diff if line.startswith("+") and not line.startswith("+++")]
    return {"removed": removed, "added": added, "diff": "\n".join(diff)}


def crontab_native_kind(listed: dict, user: str) -> str:
    """How ``crontab -u <user> -l`` answered: the crontab, the product's exact no-crontab rule, or a failed read."""
    if listed.get("status") != "ok":
        return "failed"
    if listed.get("returncode") == 0:
        return "crontab"
    if (listed.get("returncode") == 1 and not listed.get("stdout", "").strip()
            and listed.get("stderr", "").strip() == "no crontab for " + user):
        return "no-crontab"
    return "failed"


def would_prune(backups: Any, retention: int) -> dict:
    """cmd/panel/backup_schedule.go pruneBackups, read-only: of the scheduled files/full copies, newest first,
    everything after the first ``retention`` would be deleted by the next scheduled run."""
    items = backups.get("backups") if isinstance(backups, dict) else backups
    items = [item for item in (items or []) if isinstance(item, dict)]

    def field(item, *names):
        for name in names:
            if name in item:
                return item[name]
        return None
    scheduled = [item for item in items
                 if field(item, "origin", "Origin") == "scheduled" and field(item, "type", "Type") in ("files", "full")]
    scheduled.sort(key=lambda item: (str(field(item, "created_at", "CreatedAt") or ""), str(field(item, "name", "Name") or "")),
                   reverse=True)
    keep = max(1, retention)
    return {"listed": len(items), "scheduled_files_or_full": len(scheduled), "retention": retention,
            "would_keep": [field(i, "name", "Name") for i in scheduled[:keep]],
            "would_prune": [field(i, "name", "Name") for i in scheduled[keep:]]}


def pg_unit(conf_path: str) -> str:
    """cmd/agent/db_config.go dbConfigTargetFor: the unit that owns the file."""
    match = DEBIAN_PG.match(conf_path)
    return f"postgresql@{match.group(1)}-{match.group(2)}.service" if match else "postgresql.service"


def choose_config_files(snapshot: Any) -> dict:
    """What the PostgreSQL and MariaDB pages pick from GET /api/v1/managed-services (web: PostgreSQLManagement,
    MariaDBManagement)."""
    services = snapshot.get("services") if isinstance(snapshot, dict) else snapshot
    found: dict[str, list] = {}
    for service in services or []:
        if isinstance(service, dict) and service.get("id") in ("postgresql", "mariadb"):
            found[service["id"]] = [f.get("path") for f in (service.get("config_files") or [])
                                    if isinstance(f, dict) and f.get("path")]
    pg, maria = found.get("postgresql", []), found.get("mariadb", [])
    preferred = (next((p for p in maria if p.endswith("50-server.cnf")), None)
                 or next((p for p in maria if p.endswith("my.cnf")), None) or (maria[0] if maria else None))
    return {"pg_conf": next((p for p in pg if p.endswith("postgresql.conf")), None),
            "pg_hba": next((p for p in pg if p.endswith("pg_hba.conf")), None),
            "mariadb_conf": preferred, "listed": found}


def same_metadata(before: dict, after: dict) -> dict:
    fields = ("uid", "gid", "owner", "group", "mode", "type")
    differences = {f: [before.get(f), after.get(f)] for f in fields if before.get(f) != after.get(f)}
    return {"equal": not differences and bool(before.get("exists")) and bool(after.get("exists")),
            "differences": differences}


def substep_verdicts(checks: list) -> dict:
    """Per sub-step (the check name's prefix before the first colon, without a bracketed variant): its verdict."""
    groups: dict[str, list] = {}
    for check in checks:
        prefix = check["name"].split(":", 1)[0].split(" (", 1)[0].strip() if ":" in check["name"] else "section"
        groups.setdefault(prefix, []).append(check)
    return {prefix: section_verdict(items) for prefix, items in groups.items()}


STALE_KEYS = {"cron": "cron.stale", "mailpolicy": "mailpolicy.stale", "backup": "backup.auto.stale",
              "config": "dbconf.stale", "catchall": "mail.catchAll.stale"}
UNKNOWN_KEYS = {"cron": "cron.unknown", "mailpolicy": "mailpolicy.unknown", "backup": "backup.auto.unknown",
                "config": "dbconf.unknown", "catchall": "mail.catchAll.unknown", "queue": "postfix.queue.unknown"}
SAVED_KEYS = {"reloaded": "dbconf.saved.reloaded", "restart_required": "dbconf.saved.restartRequired",
              "not_running": "dbconf.saved.notRunning"}


def screen_keys(kind: str | None, status: int, body: Any) -> list:
    """The catalogue keys the screen of ``kind`` shows for this answer (web/src: CurrentSettings, ConfigFileNotices,
    PostfixManagement, DomainCronManager, DomainBackupManager, the catch-all card, the queue tab)."""
    body = body if isinstance(body, dict) else {}
    code, reason = str(body.get("code") or ""), str(body.get("reason") or "")
    keys = ([f"err.{code}.{reason}"] if code and reason else []) + ([f"err.{code}"] if code else [])
    if code in ("SETTINGS_CHANGED", "SETTINGS_VERSION_REQUIRED") and kind in STALE_KEYS:
        keys.append(STALE_KEYS[kind])
    if code == "CURRENT_SETTINGS_UNREADABLE" and kind == "cron":
        # H37 (set2): DomainCronManager shows the verified cause, or the neutral sentence, and crontab's own line
        keys += ["cron.unknown." + str(body["detail"]) if body.get("detail") else "cron.unknown", "cron.unknown.said"]
    elif code == "MAIL_QUEUE_UNREADABLE" and kind == "queue":
        keys += ["postfix.queue.unreadable" + ("." + reason if reason else ""), "postfix.queue.said"]
    elif code in ("CURRENT_SETTINGS_UNREADABLE", "MAIL_QUEUE_UNREADABLE") and kind in UNKNOWN_KEYS:
        keys.append(UNKNOWN_KEYS[kind])
    lock = reason if code == "MAIL_POLICY_RESTRICTIONS_UNMANAGED" else str(body.get("dnsbl_locked") or "") \
        if kind == "mailpolicy" and status == 200 else ""
    if lock:
        keys += [f"mailpolicy.dnsblLocked.{lock}", "mailpolicy.dnsblLockedAction"]
    if code == "MAIL_POLICY_NOT_RELOADED":
        keys.append("mailpolicy.postfixSaid" if reason == "check" else "mailpolicy.reloadSaid")
    if code == "CONFIG_INVALID":
        keys.append(f"dbconf.refused.{reason}")
    if code == "CONFIG_RELOAD_FAILED":
        # H37 (set2): ConfigFileNotices has a sentence per reason; only an unknown reason falls back to notRestored
        keys.append("dbconf.reloadFailed." + (reason if reason in ("restored", "restored_unit_reload_failed",
                                                                  "restored_running_unknown") else "notRestored"))
        keys.append("dbconf.reloadSaid")
    if kind == "config" and status == 200 and body.get("success"):
        if body.get("unchanged"):
            keys.append("dbconf.saved.unchanged")
        else:
            keys.append(SAVED_KEYS.get(body.get("applied"), "dbconf.saved.written"))
            if body.get("restart_required"):
                keys.append("dbconf.saved.waitsForRestart")
            if body.get("applied") == "reloaded" and body.get("daemon_check") == "not_checked":
                keys.append("dbconf.saved.notChecked")
            if body.get("backup"):
                keys.append("dbconf.saved.backup")
    return keys


def section_verdict(checks: list, error: str | None = None) -> str:
    if any(check["ok"] is False for check in checks):
        return "failed"
    if error or any(check["ok"] is None for check in checks):
        return "inconclusive"
    return "passed"


def build_plan(settings: SettingsCell, artifacts: dict, work_root: str, local_port: int) -> dict:
    sections = [{"id": key, "title": title, "runs": (not mail_only) or settings.mail} for key, title, mail_only in SECTIONS]
    return {"schema": "celikpanel/set1-settings-writes-plan/v1", "cell_kind": CELL_KIND, "native_evidence": False,
            "cell": dataclasses.asdict(settings), "work_root": work_root, "local_port": local_port,
            "candidate": {k: artifacts["baseline"][k] for k in ("version", "commit", "sha256")},
            "setup": {"purpose": settings.purpose, "dns_mode": "external",
                      "customization": {"components": sorted(settings.components)}},
            "site": {"domain": SITE_DOMAIN, "site_user": site_username(SITE_DOMAIN)},
            "steps": ["preflight", "origin", "baseline-install", "owner-login", "license", "setup", "site"]
                     + [s["id"] for s in sections] + ["collect"],
            "sections": sections,
            "rule": "the driver calls only the Panel's HTTP API as the logged-in owner; every native fact is a "
                    "read-only SSH inspection; owner actions on the guest are recorded as such; no update is started"}


# ---------------------------------------------------------------------------
# Native execution
# ---------------------------------------------------------------------------

def b64(text: str) -> str:
    return base64.b64encode(text.encode()).decode()


class SettingsTrial(base.Trial):
    def __init__(self, settings: SettingsCell, artifacts: dict, work_root: str, local_port: int) -> None:
        super().__init__(settings.cell, artifacts, work_root, local_port,
                         {"customization": {"components": sorted(settings.components)}}, "external")
        self.settings = settings
        self.sections: dict[str, dict] = {}
        self.current: dict[str, Any] = {}
        self.native_sequence = 0

    # -- reused upd1 steps, with this cell's choices ------------------------------------

    def upload_helpers(self) -> dict:
        helpers = super().upload_helpers()
        helpers[HELPER] = self.lab.put_file(self.root, self.record, self.plan, self.node_name, HERE / HELPER, HELPER)[1]
        return helpers

    def draft(self, purpose: str, local_ip: str) -> dict:
        # The owner chose this profile; the base loop's fallback purpose is not offered.
        return super().draft(self.settings.purpose, local_ip)

    def setup(self, checks: dict) -> str:
        verdict = super().setup(checks)
        self.state["purpose"] = self.settings.purpose
        checks["set1_purpose"] = self.settings.purpose
        checks["set1_components"] = sorted(self.settings.components)
        return verdict

    # -- recording --------------------------------------------------------------------

    def native(self, mode: str, **arguments: Any) -> dict:
        payload = base64.b64encode(json.dumps(arguments).encode()).decode()
        return self.helper(HELPER, mode, "--args-b64", payload, timeout=240)

    def snap(self, label: str, mode: str, **arguments: Any) -> dict:
        """One read-only native inspection, kept as its own evidence file."""
        value = self.native(mode, **arguments)
        self.native_sequence += 1
        name = f"native/{self.native_sequence:03d}-{label}.json"
        self.record_json(name, value)
        self.current.setdefault("natives", []).append({"label": label, "mode": mode, "file": name,
                                                       "owner_action": mode.startswith("owner-")})
        return value

    def owner(self, label: str, mode: str, **arguments: Any) -> dict:
        """An action the server owner takes on their own server by hand (never the Panel, never the driver's API)."""
        value = self.snap("owner-" + label, mode, **arguments)
        self.current.setdefault("owner_actions", []).append({"label": label, "mode": mode, "at": value.get("at")})
        return value

    def keep_text(self, name: str, text: str) -> str:
        return self.ev.write_text(f"{self.step_dir}/{name}", text)

    def diff(self, name: str, before: str, after: str) -> dict:
        changes = line_changes(before, after)
        self.keep_text(f"diff/{name}.diff", changes["diff"] or "(no difference)")
        return {"removed": changes["removed"], "added": changes["added"], "file": f"diff/{name}.diff"}

    def check(self, name: str, ok: bool | None, detail: Any = None) -> bool | None:
        self.current.setdefault("checks", []).append({"name": name, "ok": ok, "detail": detail})
        return ok

    def note(self, text: str, detail: Any = None) -> None:
        self.current.setdefault("notes", []).append({"note": text, "detail": detail})

    def catalogue(self, kind: str | None, status: int, body: Any, values: dict | None = None) -> dict:
        """The sentences the installed build's catalogues hold for this answer on its screen (EN and TR), beside the
        API's own English sentence. Looked up by key; the driver does not render a screen."""
        english = self.translator.catalog["en"]
        merged = dict((body or {}).get("vars") or {}) if isinstance(body, dict) else {}
        if isinstance(body, dict) and body.get("restart_required"):
            merged["names"] = ", ".join(body["restart_required"])
        merged.update(values or {})
        texts = {}
        for key in screen_keys(kind, status, body):
            if key in english and key not in texts:
                texts[key] = {language: self.translator.text(key, merged, language=language) for language in ("en", "tr")}
        return texts

    def call(self, label: str, method: str, path: str, body: Any = None, *, timeout: float = 120,
             kind: str | None = None, values: dict | None = None) -> dict:
        response = self.api(method, path, body, purpose=label, timeout=timeout)
        parsed = response.json()
        shown = parsed
        if isinstance(parsed, dict) and isinstance(parsed.get("Content"), str):
            shown = dict(parsed, Content={"sha256": hashlib.sha256(parsed["Content"].encode()).hexdigest(),
                                          "bytes": len(parsed["Content"].encode())})
        sent = body
        if isinstance(body, dict) and isinstance(body.get("content"), str):
            sent = dict(body, content={"sha256": hashlib.sha256(body["content"].encode()).hexdigest(),
                                       "bytes": len(body["content"].encode())})
        entry = {"label": label, "at": base.utc_now(), "request": {"method": method, "path": path, "body": sent},
                 "status": response.status, "json": shown,
                 "text": None if parsed is not None else response.text[:600]}
        if isinstance(parsed, dict) and ("error" in parsed or "code" in parsed):
            entry["answer"] = {k: parsed.get(k) for k in ("code", "reason", "detail", "error", "vars", "partial_success",
                                                          "mutation_applied") if parsed.get(k) is not None}
        texts = self.catalogue(kind, response.status, parsed, values)
        if texts:
            entry["catalogue_texts"] = texts
        self.current.setdefault("calls", []).append(entry)
        entry["_parsed"] = parsed
        return entry

    def refused(self, name: str, entry: dict, status: int, code: str, reason: str | None = None) -> bool | None:
        body = entry.get("_parsed") if isinstance(entry.get("_parsed"), dict) else {}
        ok = entry["status"] == status and body.get("code") == code and (reason is None or body.get("reason") == reason)
        return self.check(name, ok, {"status": entry["status"], "code": body.get("code"), "reason": body.get("reason"),
                                     "error": body.get("error"), "vars": body.get("vars")})

    def section(self, key: str, title: str, function: Callable[[], None], *, needs: tuple = ()) -> str:
        def run(checks: dict) -> str:
            self.current = {"section": key, "title": title, "node": self.node_name, "native_evidence": False,
                            "started_at": base.utc_now(), "calls": [], "checks": [], "natives": [], "notes": [],
                            "owner_actions": []}
            error = None
            try:
                if self.owner_waits():
                    self.current["owner_waited_for_idle"] = self.wait_host_package_manager_idle()
                function()
            except Exception as exc:  # noqa: BLE001 - recorded with the section, then judged by the step
                error = self.redactor.text(f"{type(exc).__name__}: {exc}")[:1500]
                stderr = getattr(exc, "stderr", None)
                if stderr:
                    error += " | guest stderr: " + (stderr if isinstance(stderr, str) else stderr.decode("utf-8", "replace"))[-800:]
                self.current["error"] = error
            current = self.current
            current["finished_at"] = base.utc_now()
            for entry in current["calls"]:
                entry.pop("_parsed", None)
            current["verdict"] = section_verdict(current["checks"], error)
            current["substeps"] = substep_verdicts(current["checks"])
            failed = [c["name"] for c in current["checks"] if c["ok"] is False]
            unknown = [c["name"] for c in current["checks"] if c["ok"] is None]
            self.record_json("section.json", current)
            self.sections[key] = {"title": title, "verdict": current["verdict"], "started_at": current["started_at"],
                                  "finished_at": current["finished_at"], "checks": len(current["checks"]),
                                  "substeps": current["substeps"],
                                  "failed": failed, "unknown": unknown, "error": error,
                                  "notes": [n["note"] for n in current["notes"]]}
            checks.update(self.sections[key])
            if failed:
                raise base.StepFailed("; ".join(failed)[:1500])
            if error:
                raise base.StepInconclusive(error)
            if unknown:
                raise base.StepInconclusive("not established: " + "; ".join(unknown)[:1200])
            return "passed"
        return self.step(key, run, needs=needs)

    # -- site -------------------------------------------------------------------------

    def site(self, checks: dict) -> str:
        if self.owner_waits():
            checks["owner_waited_for_idle"] = self.wait_host_package_manager_idle()
        response = self.api("POST", "/api/v1/domains/create",
                            {"domain": SITE_DOMAIN, "project_type": "static", "ssl_type": "none"},
                            purpose="AddDomainModal create (website)", timeout=600)
        body = response.json() or {}
        domain_id = body.get("DomainID") or body.get("domain_id")
        if response.status != 200 or not isinstance(domain_id, int):
            raise base.StepFailed(f"domain create returned HTTP {response.status}: {self.redactor.value(body)}")
        user = site_username(SITE_DOMAIN)
        self.state.update(domain_id=domain_id, site_user=user)
        checks.update(domain=SITE_DOMAIN, domain_id=domain_id, site_user=user)
        self.current = {}
        account = self.snap("site-account", "read-crontab", user=user)
        checks["site_account"] = account.get("account")
        checks["crontab_binary"] = account.get("crontab_binary")
        checks["crontab_package"] = account.get("crontab_package")
        if not account.get("account"):
            raise base.StepFailed(f"the site account {user} does not exist on the guest")
        if self.settings.mail:
            import secrets
            password = secrets.token_urlsafe(24)
            self.redactor.register(password)
            address = f"owner@{SITE_DOMAIN}"
            created = self.api("POST", f"/api/v1/domains/{domain_id}/mail/accounts",
                               {"address": address, "password": password, "quota_mb": 100},
                               purpose="DomainMailManager create account")
            listed = self.api("GET", f"/api/v1/domains/{domain_id}/mail/accounts", purpose="DomainMailManager list")
            checks["mailbox"] = {"address": address, "create_http": created.status, "listed": address in listed.text}
            if address not in listed.text:
                raise base.StepFailed(f"mailbox was not created: HTTP {created.status}")
            self.state["mailbox"] = address
        # As the Components list does when it opens: a scan unless the stored one is recent; then the stored scan.
        rescan = self.api("POST", f"/api/v1/managed-services/scan?max_age_seconds={AUTO_SCAN_MAX_AGE_SECONDS}", None,
                          purpose="ServiceList automatic scan", timeout=300)
        checks["scan_http"] = rescan.status
        scan = self.api("GET", "/api/v1/managed-services", purpose="useManagedServices (component config files)")
        files = choose_config_files(scan.json())
        if not (files["pg_conf"] and files["pg_hba"] and files["mariadb_conf"]):
            # The component page's own "scan again" (ServiceShell), which an owner presses when a file is not listed.
            checks["config_files_before_manual_scan"] = files
            manual = self.api("POST", "/api/v1/managed-services/scan", None, purpose="ServiceShell scan again", timeout=300)
            checks["manual_scan_http"] = manual.status
            files = choose_config_files(self.api("GET", "/api/v1/managed-services",
                                                 purpose="useManagedServices (after scan again)").json())
        checks["config_files"] = files
        self.state["config_files"] = files
        # S7: owner, group and mode of every file the Panel will write, before any section.
        before = {}
        paths = [files["pg_conf"], files["pg_hba"], files["mariadb_conf"]] + ([MAIN_CF] if self.settings.mail else [])
        for path in [p for p in paths if p]:
            state = self.snap("meta-before-" + path.strip("/").replace("/", "_"), "read-file", path=path)["file"]
            before[path] = {k: state.get(k) for k in ("exists", "type", "uid", "gid", "owner", "group", "mode", "inode", "sha256")}
        self.state["meta_before"] = before
        checks["metadata_before"] = before
        return "passed"

    # -- S1 -----------------------------------------------------------------------------

    def cron_list(self, label: str) -> dict:
        return self.call(label, "GET", f"/api/v1/domains/{self.state['domain_id']}/cron", kind="cron")

    def crontab(self, label: str) -> dict:
        return self.snap(label, "read-crontab", user=self.state["site_user"])

    def s1_cron(self) -> None:
        user, path = self.state["site_user"], f"/api/v1/domains/{self.state['domain_id']}/cron"
        stale = dict(kind="cron")

        # (a) no crontab
        n0 = self.crontab("a-crontab-none")
        listed = n0["list"]
        self.current["no_crontab_answer"] = {"crontab_binary": n0.get("crontab_binary"), "package": n0.get("crontab_package"),
                                             "returncode": listed.get("returncode"), "stdout": listed.get("stdout"),
                                             "stderr": listed.get("stderr")}
        self.check("a: the site user has no crontab (native `crontab -u <user> -l` did not print one)",
                   crontab_native_kind(listed, user) != "crontab", self.current["no_crontab_answer"])
        self.check("a: this platform's answer is the one the product recognises (exit 1, no output, exactly "
                   "`no crontab for <user>` on standard error)", crontab_native_kind(listed, user) == "no-crontab",
                   self.current["no_crontab_answer"])
        a = self.cron_list("S1a list with no crontab")
        body = a["_parsed"] if isinstance(a["_parsed"], dict) else {}
        version = body.get("version")
        self.check("a: the list answers 200 with a known empty list and a version (not an error)",
                   a["status"] == 200 and body.get("jobs") == [] and isinstance(version, str) and version.startswith("ct1-"),
                   {"status": a["status"], "body": {k: body.get(k) for k in ("jobs", "version", "code", "reason")}})
        self.check("a: the version is that of an empty crontab",
                   version == "ct1-" + hashlib.sha256(b"").hexdigest(), version)
        if not version:
            self.note("the list gave no version, so no change could be sent; sub-steps b-f were not run")
            self.check("b-f: run", None, "not run: no version to carry")
            return

        # (b) add
        line = PANEL_JOB["schedule"] + " " + PANEL_JOB["command"] + "\n"
        b = self.call("S1b add a task", "POST", path, dict(PANEL_JOB, version=version))
        n1 = self.crontab("b-crontab-after-add")
        self.check("b: add answers 200", b["status"] == 200, b["status"])
        self.check("b: the crontab holds exactly the one line", n1["list"].get("stdout") == line, n1["list"].get("stdout"))
        v1 = (self.cron_list("S1b list after add")["_parsed"] or {}).get("version")

        # (c) duplicate
        c = self.call("S1c add the identical task", "POST", path, dict(PANEL_JOB, version=v1), **stale)
        self.refused("c: the identical task is refused 409 CRON_JOB_DUPLICATE", c, 409, "CRON_JOB_DUPLICATE")
        n2 = self.crontab("c-crontab-after-duplicate")
        self.check("c: the crontab is unchanged", n2["list"].get("stdout") == line, n2["list"].get("stdout"))

        # (d) the owner edits the crontab by hand
        owner_content = line + OWNER_CRON_LINES
        installed = self.owner("d-crontab-edit", "owner-crontab", user=user, content_b64=b64(owner_content))
        self.check("d: the owner's `crontab -u <user> <file>` succeeded", installed["install"].get("returncode") == 0,
                   installed["install"])
        n3 = self.crontab("d-crontab-after-owner-edit")
        self.check("d: `crontab -l` prints the owner's crontab byte for byte", n3["list"].get("stdout") == owner_content,
                   n3["list"].get("stdout"))
        d = self.call("S1d add with the version from before the owner's edit", "POST", path,
                      {"schedule": "5 4 * * *", "command": "/usr/bin/true set1-stale-job", "version": v1}, **stale)
        self.refused("d: a write with the old version is refused 409 SETTINGS_CHANGED (scheduled_tasks)", d, 409,
                     "SETTINGS_CHANGED", "scheduled_tasks")
        n4 = self.crontab("d-crontab-after-stale-write")
        self.check("d: nothing changed", n4["list"].get("stdout") == owner_content, n4["list"].get("stdout"))
        reloaded = self.cron_list("S1d reload")["_parsed"] or {}
        jobs = reloaded.get("jobs") or []
        mine = next((j for j in jobs if j.get("command") == PANEL_JOB["command"]), None)
        theirs = next((j for j in jobs if str(j.get("command", "")).endswith("set1-owner-job")), None)
        self.check("d: after a reload both the Panel's task and the owner's task are listed, with a new version",
                   len(jobs) == 2 and mine is not None and theirs is not None and reloaded.get("version") not in (None, v1),
                   {"jobs": jobs, "version": reloaded.get("version")})
        self.check("d: the owner's comment is shown with the owner's task",
                   bool(theirs) and theirs.get("comment") == OWNER_CRON_LINES.split("\n")[0][2:], theirs)
        if not mine:
            self.check("e-f: run", None, "not run: the Panel's task is not listed")
            return

        # (e) disable, edit, enable, delete the Panel's task
        def step(label: str, name: str, method: str, body: Any, expected: str, query: str = "") -> dict:
            answer = self.call(label, method, path + query, body, **stale)
            native = self.crontab("e-crontab-after-" + name)
            self.check(f"e: {name} answers 200", answer["status"] == 200, answer.get("answer") or answer["status"])
            got = native["list"].get("stdout")
            self.check(f"e: after {name} only the Panel's line differs; the owner's comment and task are byte-identical",
                       got == expected, {"expected": expected, "got": got})
            return self.cron_list("S1e list after " + name)["_parsed"] or {}

        def task(view: dict) -> dict:
            return next((j for j in view.get("jobs") or [] if j.get("command") == PANEL_JOB["command"]), {})

        view = step("S1e disable", "disable", "PUT",
                    dict(PANEL_JOB, id=mine["id"], enabled=False, version=reloaded["version"]),
                    "# DISABLED: " + line + OWNER_CRON_LINES)
        self.check("e: the disabled task is listed as disabled with the same id",
                   task(view).get("enabled") is False and task(view).get("id") == mine["id"], task(view))
        edited = PANEL_JOB_EDITED + " " + PANEL_JOB["command"] + "\n"
        view = step("S1e edit (disabled task)", "edit", "PUT",
                    {"id": task(view).get("id"), "schedule": PANEL_JOB_EDITED, "command": PANEL_JOB["command"],
                     "enabled": False, "version": view.get("version")}, "# DISABLED: " + edited + OWNER_CRON_LINES)
        view = step("S1e enable", "enable", "PUT",
                    {"id": task(view).get("id"), "schedule": PANEL_JOB_EDITED, "command": PANEL_JOB["command"],
                     "enabled": True, "version": view.get("version")}, edited + OWNER_CRON_LINES)
        view = step("S1e delete", "delete", "DELETE", None, OWNER_CRON_LINES,
                    f"?id={quote(str(task(view).get('id')))}&version={quote(str(view.get('version')))}")
        final_version = view.get("version")
        self.check("e: after the delete only the owner's task is listed", len(view.get("jobs") or []) == 1,
                   view.get("jobs"))

        # (f) the crontab cannot be read. set2: every cause is applied in turn, and each is judged with the cause
        # the answer carries (cron.allow is a verified cause; a relocated spool is not one the Agent names).
        used = []
        for kind in CRON_FAULTS:
            applied = self.owner("f-fault-" + kind, "owner-cron-fault", action="apply", kind=kind)
            try:
                probe = self.crontab("f-crontab-under-" + kind)
                native_kind = crontab_native_kind(probe["list"], user)
                record = {"kind": kind, "applied": applied.get("applied"), "native": native_kind,
                          "returncode": probe["list"].get("returncode"), "stderr": probe["list"].get("stderr")}
                self.current.setdefault("cron_faults", []).append(record)
                listing = self.cron_list(f"S1f list under the owner's {kind}")
                lbody = listing["_parsed"] if isinstance(listing["_parsed"], dict) else {}
                record["list_status"] = listing["status"]
                record["list_code"] = lbody.get("code")
                record["answer_cause"] = lbody.get("detail")
                record["answer_said"] = (lbody.get("vars") or {}).get("detail")
                add = None
                if native_kind == "failed":
                    add = self.call(f"S1f add under the owner's {kind}", "POST", path,
                                    {"schedule": "7 7 * * *", "command": "/usr/bin/true set1-fault-job",
                                     "version": final_version}, **stale)
                    record["add_status"] = add["status"]
                # The native read is taken again after the Panel's calls: a cause the platform's own crontab
                # repairs by itself between two reads (cronie recreates a missing spool directory) is not a
                # stable unreadable state, and the Panel's answers are then recorded, not judged.
                after = self.crontab("f-crontab-under-" + kind + "-again")
                record["native_again"] = crontab_native_kind(after["list"], user)
                record["stderr_again"] = after["list"].get("stderr")
                if native_kind != "failed" or record["native_again"] != "failed":
                    self.note(f"{kind} does not keep root's `crontab -u <user> -l` failing on this platform (native "
                              f"answers: {native_kind}, then {record['native_again']}); the list answered HTTP "
                              f"{listing['status']}", record)
                    continue
                used.append(kind)
                self.refused(f"f ({kind}): the list answers 502 CURRENT_SETTINGS_UNREADABLE (scheduled_tasks), not an "
                             "empty list", listing, 502, "CURRENT_SETTINGS_UNREADABLE", "scheduled_tasks")
                self.check(f"f ({kind}): Add is refused", add["status"] != 200,
                           add.get("answer") or add["status"])
                self.refused(f"f ({kind}): Add answers 502 CURRENT_SETTINGS_UNREADABLE (scheduled_tasks)", add, 502,
                             "CURRENT_SETTINGS_UNREADABLE", "scheduled_tasks")
                said = str(record["answer_said"] or "")
                tool_line = next((l.strip() for l in str(record["stderr"] or "").splitlines() if l.strip()), "")
                shown = {"detail": record["answer_cause"], "vars.detail": said, "crontab_printed": tool_line}
                if kind == "cron-allow":
                    self.check("f (cron-allow): the answer carries the recognised cause (detail = cron_allow) and "
                               "crontab's own line", record["answer_cause"] == "cron_allow" and bool(said), shown)
                else:
                    self.check(f"f ({kind}): the answer names no cause (neutral) and carries the tool's own line",
                               not record["answer_cause"] and bool(said)
                               and (said in tool_line or tool_line in said or said[:40] in str(record["stderr"])), shown)
                self.current.setdefault("cron_texts", {})[kind] = {
                    key: {language: self.translator.text(key, {"detail": said, "user": user}, language=language)
                          for language in ("en", "tr")}
                    for key in ("cron.unknown", "cron.unknown." + str(record["answer_cause"] or "none"), "cron.unknown.said")
                    if key in self.translator.catalog["en"]}
            finally:
                self.owner("f-restore-" + kind, "owner-cron-fault", action="restore")
        self.current["cron_faults_judged"] = used
        self.check("f: one of the owner's realistic causes made the native crontab read fail", True if used else None,
                   self.current.get("cron_faults"))
        n9 = self.crontab("f-crontab-after-restore")
        self.check("f: after the restore the crontab is the owner's, unchanged", n9["list"].get("stdout") == OWNER_CRON_LINES,
                   n9["list"].get("stdout"))
        back = self.cron_list("S1f list after restore")["_parsed"] or {}
        self.check("f: the list is known again with the version from before the fault",
                   back.get("version") == final_version and len(back.get("jobs") or []) == 1, back)

    # -- S2 -----------------------------------------------------------------------------

    def postfix(self, label: str, **arguments: Any) -> dict:
        value = self.snap(label, "read-postfix", **arguments)
        value["_values"] = {name: answer.get("stdout", "").strip() for name, answer in value["values"].items()}
        return value

    def smtp(self, label: str) -> dict:
        value: dict = {}
        for attempt in range(1, 7):   # a just-started Postfix needs a moment before it listens
            value = self.snap(label + ("" if attempt == 1 else f"-try{attempt}"), "read-smtp",
                              sender=f"probe@{SITE_DOMAIN}", recipient=self.state.get("mailbox"))
            if value["25"].get("ok") and value["587"].get("ok"):
                break
            time.sleep(3)
        return value

    def owner_main_cf(self, label: str, text: str, reload: bool = True) -> dict:
        edited = self.owner(label + "-main-cf", "owner-edit", path=MAIN_CF, content_b64=b64(text))
        result = {"edit": edited}
        if reload:
            result["reload"] = self.owner(label + "-reload-postfix", "owner-systemctl", action="reload", unit="postfix")
        return result

    def policy_get(self, label: str) -> dict:
        return self.call(label, "GET", "/api/v1/mail/policy", kind="mailpolicy")

    def journal(self, label: str, units: list, since: float, identifiers: tuple = ()) -> str:
        value = self.snap(label, "read-journal", units=units, identifiers=list(identifiers), since_epoch=int(since), lines=400)
        return value["journal"].get("stdout", "")

    def guest_clock(self) -> float:
        return float(self.native("read-clock")["epoch"])

    def s2_mail_policy(self) -> None:
        url = "/api/v1/mail/policy"
        refusal = dict(kind="mailpolicy")
        postfix_log = ("postfix/master", "postfix/postfix-script")   # whichever unit the platform runs Postfix in

        # (a) read
        p0 = self.postfix("a-postfix-before", unit_text=True)
        self.keep_text("native-text/postconf-n-before.txt", p0["postconf_n"].get("stdout", ""))
        self.keep_text("native-text/main.cf-before.txt", p0["main_cf"].get("text", ""))
        self.keep_text("native-text/postfix-unit.txt", json.dumps(p0.get("unit_text"), indent=1))
        s0 = self.smtp("a-smtp-before")
        self.check("a: before any change mail is accepted on 25 (to the site's mailbox) and 587 answers",
                   s0["25"].get("ok") and s0["587"].get("ok"), s0)
        a = self.policy_get("S2a read")
        policy = a["_parsed"] if isinstance(a["_parsed"], dict) else {}
        values = p0["_values"]
        self.check("a: the read answers the server's values with a version",
                   a["status"] == 200 and str(policy.get("version", "")).startswith("mp1-")
                   and policy.get("message_size_mb") == int(values["message_size_limit"] or 0) // MEBIBYTE
                   and policy.get("outbound_rate_limit") == int(values["smtpd_client_message_rate_limit"] or 0),
                   {"api": policy, "native": values})
        v0 = policy.get("version")

        # (b) the owner adds restrictions by hand
        owner_elements = owner_restriction_elements(values["smtpd_recipient_restrictions"])
        block = restriction_block("smtpd_recipient_restrictions", owner_elements)
        owner_text = set_main_cf_parameter(p0["main_cf"]["text"], "smtpd_recipient_restrictions", block)
        done = self.owner_main_cf("b-owner-restrictions", owner_text)
        p1 = self.postfix("b-postfix-after-owner-edit")
        self.diff("b-main.cf-owner-edit", p0["main_cf"]["text"], p1["main_cf"]["text"])
        self.check("b: the owner's edit is in place: `postfix check` and the reload succeeded and postconf reads the "
                   "owner's list", p1["check"].get("returncode") == 0 and done["reload"]["result"].get("returncode") == 0
                   and split_restrictions(p1["_values"]["smtpd_recipient_restrictions"]) == owner_elements,
                   {"check": p1["check"], "reload": done["reload"]["result"], "value": p1["_values"]["smtpd_recipient_restrictions"]})
        b1 = self.policy_get("S2b read after the owner's edit")
        seen = b1["_parsed"] if isinstance(b1["_parsed"], dict) else {}
        v1 = seen.get("version")
        self.check("b: the Panel read shows a new version", b1["status"] == 200 and v1 not in (None, v0),
                   {"before": v0, "after": v1, "dnsbl_locked": seen.get("dnsbl_locked"), "zones": seen.get("dnsbl_zones")})
        wanted = {"message_size_mb": seen.get("message_size_mb"), "dnsbl_zones": [DNSBL_ZONE], "outbound_rate_limit": 30}
        b2 = self.call("S2b save with the old version", "PUT", url, dict(wanted, version=v0), **refusal)
        self.refused("b: a Save with the old version is refused 409 SETTINGS_CHANGED (mail_policy)", b2, 409,
                     "SETTINGS_CHANGED", "mail_policy")
        p2 = self.postfix("b-postfix-after-stale-save")
        self.check("b: main.cf is unchanged by the refused save", p2["main_cf"].get("sha256") == p1["main_cf"].get("sha256"),
                   [p1["main_cf"].get("sha256"), p2["main_cf"].get("sha256")])

        # (c) DNSBL zone and rate limit with the new version
        t0 = self.guest_clock()
        c = self.call("S2c save a DNSBL zone and a rate limit", "PUT", url, dict(wanted, version=v1), **refusal)
        saved = (c["_parsed"] or {}).get("policy") if isinstance(c["_parsed"], dict) else None
        p3 = self.postfix("c-postfix-after-save")
        self.diff("c-main.cf-panel-save", p1["main_cf"]["text"], p3["main_cf"]["text"])
        self.keep_text("native-text/postconf-n-after-c.txt", p3["postconf_n"].get("stdout", ""))
        self.check("c: the save answers 200 with the saved policy",
                   c["status"] == 200 and isinstance(saved, dict) and saved.get("dnsbl_zones") == [DNSBL_ZONE]
                   and saved.get("outbound_rate_limit") == 30, c.get("answer") or saved)
        self.check("c: the answer says what Postfix did with it (applied = reloaded)",
                   isinstance(c["_parsed"], dict) and c["_parsed"].get("applied") == "reloaded",
                   (c["_parsed"] or {}).get("applied") if isinstance(c["_parsed"], dict) else None)
        got = split_restrictions(p3["_values"]["smtpd_recipient_restrictions"])
        self.check("c: the owner's restrictions are all there in the same order and only `reject_rbl_client <zone>` was added",
                   got == owner_elements + ["reject_rbl_client", DNSBL_ZONE], {"expected_prefix": owner_elements, "got": got})
        self.check("c: the rate limit and its window are written; the message size was not touched",
                   p3["_values"]["smtpd_client_message_rate_limit"] == "30" and p3["_values"]["anvil_rate_time_unit"] == "60s"
                   and p3["_values"]["message_size_limit"] == p1["_values"]["message_size_limit"], p3["_values"])
        layout = main_cf_logical_line(p3["main_cf"]["text"], "smtpd_recipient_restrictions")
        self.current["restrictions_layout"] = {"owner_wrote": "\n".join(block), "after_panel_save": layout}
        if layout != "\n".join(block[:-1] + [block[-1] + ","] + ["    reject_rbl_client " + DNSBL_ZONE]):
            self.note("the owner's one-restriction-per-line layout in main.cf was rewritten as a single line by the save "
                      "(`postconf -e`); the elements and their order are the owner's", self.current["restrictions_layout"])
        self.check("c: `postfix check` is clean after the save (exit 0, same output as after the owner's own edit)",
                   p3["check"].get("returncode") == 0 and p3["check"].get("stderr") == p1["check"].get("stderr"), p3["check"])
        log = self.journal("c-postfix-journal", [], t0, postfix_log)
        self.keep_text("native-text/postfix-journal-c.txt", log or "(no lines)")
        self.check("c: Postfix was reloaded (its journal has the reload line; the master process is the same one)",
                   ("reload -- version" in log or "refreshing the Postfix mail system" in log)
                   and p3.get("master_pid") == p1.get("master_pid") and p3.get("master_pid") is not None,
                   {"master_pid": [p1.get("master_pid"), p3.get("master_pid")],
                    "reload_lines": [l for l in log.splitlines() if "reload" in l or "refreshing" in l][-4:]})
        master_lines = [l for l in log.splitlines() if "postfix/master" in l and "reload" in l]
        self.check("c: the journal has a `postfix/master ... reload` line from the same master process",
                   bool(master_lines) and f"postfix/master[{p3.get('master_pid')}]" in master_lines[-1], master_lines[-3:])
        s1 = self.smtp("c-smtp-after-save")
        self.check("c: mail is still accepted on 25 and 587 answers", s1["25"].get("ok") and s1["587"].get("ok"), s1)

        # (d) remove the zone
        v2 = (self.policy_get("S2d read")["_parsed"] or {}).get("version")
        d = self.call("S2d remove the DNSBL zone", "PUT", url,
                      {"message_size_mb": seen.get("message_size_mb"), "dnsbl_zones": [], "outbound_rate_limit": 30,
                       "version": v2}, **refusal)
        p4 = self.postfix("d-postfix-after-remove")
        self.diff("d-main.cf-zone-removed", p3["main_cf"]["text"], p4["main_cf"]["text"])
        got = split_restrictions(p4["_values"]["smtpd_recipient_restrictions"])
        self.check("d: removing the zone answers 200 and the list is the owner's list exactly (same elements, same order)",
                   d["status"] == 200 and got == owner_elements, {"expected": owner_elements, "got": got})
        self.current["restrictions_layout"]["after_zone_removed"] = main_cf_logical_line(p4["main_cf"]["text"],
                                                                                      "smtpd_recipient_restrictions")

        # (e) a list the Panel cannot split with certainty
        macro_text = set_main_cf_parameter(p4["main_cf"]["text"], "smtpd_recipient_restrictions",
                                           [OWNER_MACRO + " = " + ", ".join(POLICY_BASELINE),
                                            "smtpd_recipient_restrictions = ${" + OWNER_MACRO + "}, reject_unknown_sender_domain"])
        done = self.owner_main_cf("e-owner-macro", macro_text)
        p5 = self.postfix("e-postfix-after-owner-macro")
        self.check("e: the owner's macro form is valid for Postfix (`postfix check` and reload succeeded)",
                   p5["check"].get("returncode") == 0 and done["reload"]["result"].get("returncode") == 0,
                   {"check": p5["check"], "value": p5["_values"]["smtpd_recipient_restrictions"]})
        e0 = self.policy_get("S2e read with a ${variable} in the list")
        locked = e0["_parsed"] if isinstance(e0["_parsed"], dict) else {}
        self.check("e: the read reports the DNSBL setting as locked (variable)",
                   e0["status"] == 200 and locked.get("dnsbl_locked") == "variable", locked)
        e1 = self.call("S2e save a DNSBL zone over the ${variable} list", "PUT", url,
                       {"message_size_mb": locked.get("message_size_mb"), "dnsbl_zones": [DNSBL_ZONE],
                        "outbound_rate_limit": 31, "version": locked.get("version")}, **refusal)
        self.refused("e: the DNSBL change is refused 409 MAIL_POLICY_RESTRICTIONS_UNMANAGED (variable)", e1, 409,
                     "MAIL_POLICY_RESTRICTIONS_UNMANAGED", "variable")
        p6 = self.postfix("e-postfix-after-refused-dnsbl")
        self.check("e: main.cf is unchanged by the refused DNSBL save",
                   p6["main_cf"].get("sha256") == p5["main_cf"].get("sha256"), [p5["main_cf"].get("sha256"), p6["main_cf"].get("sha256")])
        e2 = self.call("S2e save size and rate only", "PUT", url,
                       {"message_size_mb": 30, "dnsbl_zones": locked.get("dnsbl_zones") or [], "outbound_rate_limit": 45,
                        "version": locked.get("version")}, **refusal)
        p7 = self.postfix("e-postfix-after-size-rate")
        self.diff("e-main.cf-size-rate", p5["main_cf"]["text"], p7["main_cf"]["text"])
        self.check("e: message size and rate still save (200; 31457280 and 45 in main.cf) and the owner's list is untouched",
                   e2["status"] == 200 and p7["_values"]["message_size_limit"] == str(30 * MEBIBYTE)
                   and p7["_values"]["smtpd_client_message_rate_limit"] == "45"
                   and p7["_values"]["smtpd_recipient_restrictions"] == p5["_values"]["smtpd_recipient_restrictions"]
                   and main_cf_logical_line(p7["main_cf"]["text"], "smtpd_recipient_restrictions")
                   == main_cf_logical_line(p5["main_cf"]["text"], "smtpd_recipient_restrictions"),
                   {"answer": e2.get("answer") or e2["status"], "values": p7["_values"]})
        plain = set_main_cf_parameter(set_main_cf_parameter(p7["main_cf"]["text"], OWNER_MACRO, None),
                                      "smtpd_recipient_restrictions", block)
        self.owner_main_cf("e-owner-plain-list-again", plain)

        # (f) Postfix's own check refuses the configuration (the owner's unfinished edit)
        p8 = self.postfix("f-postfix-before-typo")
        typo_text = add_line(p8["main_cf"]["text"], MAIN_CF_TYPO)
        fixed = False
        try:
            self.owner_main_cf("f-owner-typo", typo_text, reload=False)
            p9 = self.postfix("f-postfix-with-typo")
            effective = p9["check"].get("returncode") not in (0, None)
            self.check("f: the owner's unfinished edit makes Postfix refuse its configuration (`postfix check` fails)",
                       True if effective else None, p9["check"])
            f0 = self.policy_get("S2f read with the owner's unfinished edit")
            current = f0["_parsed"] if isinstance(f0["_parsed"], dict) else {}
            self.current["read_under_typo"] = {"status": f0["status"], "answer": f0.get("answer"), "version": current.get("version")}
            if not effective:
                self.note("the owner's typo did not make Postfix refuse its configuration on this platform, so no "
                          "reload failure was caused and no save was sent")
            elif f0["status"] != 200:
                self.note("with the owner's typo in main.cf the policy read itself is refused, so the reload-failure "
                          "answer could not be reached this way", f0.get("answer"))
                self.check("f: MAIL_POLICY_NOT_RELOADED reached", None, f0.get("answer"))
            else:
                t0 = self.guest_clock()
                f1 = self.call("S2f save while Postfix cannot reload", "PUT", url,
                               {"message_size_mb": current.get("message_size_mb"), "dnsbl_zones": current.get("dnsbl_zones") or [],
                                "outbound_rate_limit": 46, "version": current.get("version")}, **refusal)
                p10 = self.postfix("f-postfix-after-save")
                flog = self.journal("f-postfix-journal", [], t0, postfix_log)
                self.keep_text("native-text/postfix-journal-f.txt", flog or "(no lines)")
                answer = f1["_parsed"] if isinstance(f1["_parsed"], dict) else {}
                self.refused("f: the save answers 502 MAIL_POLICY_NOT_RELOADED with the reason `check`", f1, 502,
                             "MAIL_POLICY_NOT_RELOADED", "check")
                self.check("f: the answer says the change was applied (mutation_applied)", answer.get("mutation_applied") is True,
                           f1.get("answer"))
                written = answer.get("policy") if isinstance(answer.get("policy"), dict) else None
                self.check("f: the body carries the written policy (rate 46) with its new version",
                           bool(written) and written.get("outbound_rate_limit") == 46
                           and str(written.get("version", "")).startswith("mp1-") and written.get("version") != current.get("version"),
                           written)
                said = str((answer.get("vars") or {}).get("detail") or "")
                self.check("f: the answer carries Postfix's own line", "bad numerical configuration" in said
                           or (bool(said) and said[:30] in (p9["check"].get("stderr", "") + p9["check"].get("stdout", ""))),
                           {"vars.detail": said, "postfix_check": (p9["check"].get("stderr") or "")[-300:]})
                self.check("f: the value is written in main.cf (46) and Postfix keeps running with its previous process",
                           p10["_values"]["smtpd_client_message_rate_limit"] == "46" and p10.get("master_pid") == p8.get("master_pid")
                           and p10.get("master_pid") is not None and p10.get("master_alive") is True,
                           {"rate": p10["_values"]["smtpd_client_message_rate_limit"], "master_pid": [p8.get("master_pid"), p10.get("master_pid")],
                            "master_alive": p10.get("master_alive"), "postfix_status": p10["status"], "units": p10.get("units")})
                if p10["status"].get("returncode") not in (0, None):
                    # H27 (set2 run-a): with a refused main.cf `postfix status` reads the configuration first and refuses
                    # to answer, so it cannot say whether the master runs; the PID file and /proc do.
                    self.note("while main.cf is refused, `postfix status` itself exits non-zero with Postfix's fatal line "
                              "and does not say whether the master runs", p10["status"])
                self.check("f: nothing was reloaded (the journal has no reload line since the save)",
                           not [l for l in flog.splitlines() if "postfix/master" in l and "reload" in l], flog[-600:])
                self.current["not_reloaded_answer_carries_policy"] = "policy" in answer
                f2 = self.policy_get("S2f read after the save")
                shown = f2["_parsed"] if isinstance(f2["_parsed"], dict) else {}
                self.check("f: the saved values are the ones a reload of the page shows, with the version the answer carried",
                           f2["status"] == 200 and shown.get("outbound_rate_limit") == 46
                           and (not written or shown.get("version") == written.get("version")), shown)
        finally:
            # set2: the owner corrects the line and does NOT reload; sub-step g is the page's save without a change.
            now = self.postfix("f-postfix-before-restore")
            self.owner_main_cf("f-owner-removes-typo", remove_line(now["main_cf"]["text"], MAIN_CF_TYPO), reload=False)
            p11 = self.postfix("f-postfix-after-owner-correction")
            fixed = p11["check"].get("returncode") == 0
            self.check("f: after the owner's correction `postfix check` succeeds (Postfix was not reloaded by the owner)",
                       fixed, p11["check"])

        # (g) a save without a change after the owner corrected main.cf: every accepted save ends with the reload
        if fixed:
            g0 = self.policy_get("S2g read after the owner's correction")
            seen_g = g0["_parsed"] if isinstance(g0["_parsed"], dict) else {}
            t0 = self.guest_clock()
            g1 = self.call("S2g save without a change", "PUT", url,
                           {"message_size_mb": seen_g.get("message_size_mb"), "dnsbl_zones": seen_g.get("dnsbl_zones") or [],
                            "outbound_rate_limit": seen_g.get("outbound_rate_limit"), "version": seen_g.get("version")}, **refusal)
            p12 = self.postfix("g-postfix-after-unchanged-save")
            glog = self.journal("g-postfix-journal", [], t0, postfix_log)
            self.keep_text("native-text/postfix-journal-g.txt", glog or "(no lines)")
            gbody = g1["_parsed"] if isinstance(g1["_parsed"], dict) else {}
            self.check("g: a save without a change answers 200 with applied = unchanged_reloaded",
                       g1["status"] == 200 and gbody.get("applied") == "unchanged_reloaded",
                       g1.get("answer") or {k: gbody.get(k) for k in ("success", "applied")})
            self.check("g: main.cf is byte-identical (nothing was written)",
                       p12["main_cf"].get("sha256") == p11["main_cf"].get("sha256"),
                       [p11["main_cf"].get("sha256"), p12["main_cf"].get("sha256")])
            glines = [l for l in glog.splitlines() if "postfix/master" in l and "reload" in l]
            self.check("g: Postfix was reloaded (a `postfix/master ... reload` line; the same master process)",
                       bool(glines) and p12.get("master_pid") == p11.get("master_pid") and p12.get("master_pid") is not None,
                       {"reload_lines": glines[-3:], "master_pid": [p11.get("master_pid"), p12.get("master_pid")]})
        else:
            self.check("g: run", None, "not run: main.cf is not accepted by `postfix check` after the correction")

        # (h) Postfix is stopped: a save leaves it stopped and says so
        self.owner("h-stop-postfix", "owner-systemctl", action="stop", unit="postfix")
        try:
            p13 = self.postfix("h-postfix-stopped")
            stopped = p13["status"].get("returncode") not in (0, None)
            self.check("h: the owner stopped Postfix (`postfix status` says it is not running)", True if stopped else None,
                       {"status": p13["status"], "units": p13.get("units")})
            h0 = self.policy_get("S2h read while Postfix is stopped")
            seen_h = h0["_parsed"] if isinstance(h0["_parsed"], dict) else {}
            if stopped and h0["status"] == 200:
                wanted_h = {"message_size_mb": seen_h.get("message_size_mb"), "dnsbl_zones": seen_h.get("dnsbl_zones") or [],
                            "outbound_rate_limit": 47}
                h1 = self.call("S2h save while Postfix is stopped", "PUT", url, dict(wanted_h, version=seen_h.get("version")), **refusal)
                p14 = self.postfix("h-postfix-after-save")
                hbody = h1["_parsed"] if isinstance(h1["_parsed"], dict) else {}
                self.check("h: the save answers 200 with applied = not_running",
                           h1["status"] == 200 and hbody.get("applied") == "not_running",
                           h1.get("answer") or {k: hbody.get(k) for k in ("success", "applied")})
                self.check("h: the value is written (47) and Postfix stays stopped (not started by the save)",
                           p14["_values"]["smtpd_client_message_rate_limit"] == "47"
                           and p14["status"].get("returncode") not in (0, None),
                           {"rate": p14["_values"]["smtpd_client_message_rate_limit"], "status": p14["status"], "units": p14.get("units")})
                h2v = (self.policy_get("S2h read after the save")["_parsed"] or {}).get("version")
                h2 = self.call("S2h save without a change while Postfix is stopped", "PUT", url, dict(wanted_h, version=h2v), **refusal)
                p15 = self.postfix("h-postfix-after-unchanged-save")
                h2body = h2["_parsed"] if isinstance(h2["_parsed"], dict) else {}
                self.check("h: a save without a change while stopped answers 200 with applied = unchanged, still stopped",
                           h2["status"] == 200 and h2body.get("applied") == "unchanged"
                           and p15["status"].get("returncode") not in (0, None),
                           {"answer": h2.get("answer") or h2body.get("applied"), "status": p15["status"]})
            else:
                self.check("h: the save was sent", None, {"stopped": stopped, "read": h0.get("answer") or h0["status"]})
        finally:
            started = self.owner("h-start-postfix", "owner-systemctl", action="start", unit="postfix")
        restored = self.owner("h-owner-reload-postfix", "owner-systemctl", action="reload", unit="postfix")
        p16 = self.postfix("h-postfix-end")
        self.keep_text("native-text/postconf-n-end.txt", p16["postconf_n"].get("stdout", ""))
        self.check("h: the owner starts Postfix again; `postfix check`, `postfix status` and `systemctl reload postfix` succeed",
                   started["result"].get("returncode") == 0 and p16["check"].get("returncode") == 0
                   and p16["status"].get("returncode") == 0 and restored["result"].get("returncode") == 0,
                   {"start": started["result"], "check": p16["check"], "status": p16["status"], "reload": restored["result"]})
        s2 = self.smtp("h-smtp-end")
        self.check("h: mail is accepted on 25 and 587 answers at the end", s2["25"].get("ok") and s2["587"].get("ok"), s2)

    # -- S3 -----------------------------------------------------------------------------

    def rows(self, label: str) -> dict:
        return self.snap(label, "read-panel-rows", domain_id=self.state["domain_id"])["rows"]

    def s3_backup_schedule(self) -> None:
        url = f"/api/v1/domains/{self.state['domain_id']}/backups/schedule"
        hints = dict(kind="backup")

        def row(label: str) -> dict | None:
            found = self.rows(label).get("backup_schedules")
            return found[0] if isinstance(found, list) and found else None

        def settings(value: dict | None) -> Any:
            return None if value is None else [value.get("frequency"), value.get("backup_type"), value.get("retention"), value.get("enabled")]

        self.check("the domain has no schedule row before the section", row("a-rows-before") is None)
        a = self.call("S3 read (no schedule)", "GET", url, **hints)
        v0 = (a["_parsed"] or {}).get("version")
        self.check("read answers 200, off, with a version", a["status"] == 200 and (a["_parsed"] or {}).get("enabled") is False
                   and str(v0).startswith("bs1-"), a["_parsed"])
        wanted = {"frequency": "weekly", "backup_type": "full", "retention": 30}
        b = self.call("S3 create without a version", "PUT", url, wanted, **hints)
        self.refused("a write without a version is refused 409 SETTINGS_VERSION_REQUIRED (backup_schedule)", b, 409,
                     "SETTINGS_VERSION_REQUIRED", "backup_schedule")
        self.check("no row was written by the refused write", row("b-rows-after-versionless") is None)
        c = self.call("S3 create weekly/full/30", "PUT", url, dict(wanted, version=v0), **hints)
        v1 = (c["_parsed"] or {}).get("version")
        created = row("c-rows-after-create")
        self.check("the schedule is created (200) and the database row is weekly/full/30/on",
                   c["status"] == 200 and settings(created) == ["weekly", "full", 30, 1], {"answer": c["_parsed"], "row": settings(created)})
        d = self.call("S3 stale write (the version from before the create)", "PUT", url,
                      {"frequency": "daily", "backup_type": "files", "retention": 7, "version": v0}, **hints)
        self.refused("a stale write is refused 409 SETTINGS_CHANGED (backup_schedule)", d, 409, "SETTINGS_CHANGED",
                     "backup_schedule")
        self.check("the row is unchanged by the stale write", settings(row("d-rows-after-stale")) == ["weekly", "full", 30, 1])
        e = self.call("S3 change retention to 14 with the current version", "PUT", url,
                      {"frequency": "weekly", "backup_type": "full", "retention": 14, "version": v1}, **hints)
        changed = row("e-rows-after-change")
        self.check("a valid change is applied (200; row weekly/full/14/on)",
                   e["status"] == 200 and settings(changed) == ["weekly", "full", 14, 1], {"answer": e["_parsed"], "row": settings(changed)})
        f = self.call("S3 read after the change", "GET", url, **hints)
        shown = f["_parsed"] or {}
        self.check("the read shows the saved settings and the version the change answered",
                   f["status"] == 200 and [shown.get("frequency"), shown.get("backup_type"), shown.get("retention"), shown.get("enabled")]
                   == ["weekly", "full", 14, True] and shown.get("version") == (e["_parsed"] or {}).get("version"), shown)
        listing = self.call("S3 backups list (read-only)", "GET", f"/api/v1/domains/{self.state['domain_id']}/backups")
        prune = would_prune(listing["_parsed"], int((changed or {}).get("retention") or 0))
        self.current["next_run_prune"] = prune
        self.check("what the next scheduled run would prune follows the saved retention (the product's rule over the "
                   "listed scheduled copies; the row holds 14)",
                   listing["status"] == 200 and (changed or {}).get("retention") == 14
                   and len(prune["would_prune"]) == max(0, prune["scheduled_files_or_full"] - 14), prune)
        if prune["scheduled_files_or_full"] <= 14:
            self.note(f"only {prune['scheduled_files_or_full']} scheduled copies exist, so no copy would be pruned; the "
                      "prune itself was not exercised")

    # -- S4 -----------------------------------------------------------------------------

    def config_get(self, label: str, path: str) -> dict:
        return self.call(label, "GET", "/api/v1/config?path=" + quote(path, safe="/"), kind="config",
                         values={"file": path.rsplit("/", 1)[-1]})

    def config_post(self, label: str, path: str, content: str, version: str | None, service: str) -> dict:
        body = {"path": path, "content": content}
        if version is not None:
            body["version"] = version
        return self.call(label, "POST", "/api/v1/config", body, timeout=180, kind="config",
                         values={"file": path.rsplit("/", 1)[-1], "service": service})

    def file(self, label: str, path: str) -> dict:
        return self.snap(label, "read-file", path=path, backup_text=False)

    def backup_kept(self, name: str, answer: dict, before: dict, after: dict) -> None:
        body = answer["_parsed"] if isinstance(answer["_parsed"], dict) else {}
        kept = next((b for b in after["backups"] if b.get("path") == body.get("backup")), None)
        self.check(f"{name}: a timestamped backup of the previous file exists next to it, with the previous bytes and "
                   "the file's owner, group and mode",
                   bool(kept) and kept.get("sha256") == before["file"].get("sha256")
                   and same_metadata(before["file"], kept)["equal"]
                   and bool(re.search(r"\.celikpanel-backup-[0-9]{8}T[0-9]{6}Z(-[0-9]+)?$", str(body.get("backup")))),
                   {"backup": body.get("backup"), "kept": {k: (kept or {}).get(k) for k in ("owner", "group", "mode", "sha256")},
                    "previous": {k: before["file"].get(k) for k in ("owner", "group", "mode", "sha256")}})
        self.check(f"{name}: no validation copy is left next to the file", after.get("candidates_left") == [],
                   after.get("candidates_left"))
        self.check(f"{name}: the file keeps its owner, group and mode", same_metadata(before["file"], after["file"])["equal"],
                   same_metadata(before["file"], after["file"]))

    def unchanged(self, name: str, before: dict, after: dict) -> None:
        self.check(f"{name}: the file is unchanged, no backup was added and no validation copy is left",
                   after["file"].get("sha256") == before["file"].get("sha256")
                   and len(after["backups"]) == len(before["backups"]) and after.get("candidates_left") == [],
                   {"sha256": [before["file"].get("sha256"), after["file"].get("sha256")],
                    "backups": [len(before["backups"]), len(after["backups"])], "candidates_left": after.get("candidates_left")})

    def pg(self, label: str, unit: str) -> dict:
        value = self.snap(label, "read-postgres", unit=unit, settings=["work_mem"])
        answers = value["answers"]
        value["_work_mem"] = answers.get("show_work_mem", {}).get("stdout", "").strip()
        value["_times"] = (answers["load"].get("stdout", "").strip().split("\t") + ["", ""])[:2]
        value["_main_pid"] = value["units"].get(unit, {}).get("MainPID")
        rules = []
        for line in answers["hba"].get("stdout", "").splitlines():
            parts = line.split("\t")
            if len(parts) >= 8:
                rules.append(dict(zip(("line", "type", "database", "user", "address", "netmask", "method", "error"), parts)))
        value["_hba"] = rules
        return value

    def s4_postgresql(self) -> None:
        files = self.state["config_files"]
        conf, hba = files.get("pg_conf"), files.get("pg_hba")
        if not conf or not hba:
            self.check("the component scan names postgresql.conf and pg_hba.conf", False, files)
            return
        unit, service = pg_unit(conf), "PostgreSQL"
        self.current["unit"] = unit

        # (a) read
        n0 = self.file("a-conf-before", conf)
        h0 = self.file("a-hba-before", hba)
        s0 = self.pg("a-server-before", unit)
        self.keep_text("native-text/postgresql.conf-before.txt", n0["file"].get("text", ""))
        self.keep_text("native-text/pg_hba.conf-before.txt", h0["file"].get("text", ""))
        a1 = self.config_get("S4a read postgresql.conf", conf)
        a2 = self.config_get("S4a read pg_hba.conf", hba)
        text0, v0 = (a1["_parsed"] or {}).get("Content"), (a1["_parsed"] or {}).get("Version")
        htext0, hv0 = (a2["_parsed"] or {}).get("Content"), (a2["_parsed"] or {}).get("Version")
        self.check("a: both reads answer the file's own bytes with the version of those bytes",
                   a1["status"] == 200 and a2["status"] == 200 and text0 == n0["file"].get("text") and htext0 == h0["file"].get("text")
                   and v0 == "cf1-" + str(n0["file"].get("sha256")) and hv0 == "cf1-" + str(h0["file"].get("sha256")),
                   {"conf": [a1["status"], v0, n0["file"].get("sha256")], "hba": [a2["status"], hv0, h0["file"].get("sha256")]})
        self.check("a: PostgreSQL is running and answers over the local socket", bool(s0["_work_mem"]) and bool(s0.get("postmaster_pid")),
                   {"work_mem": s0["_work_mem"], "postmaster_pid": s0.get("postmaster_pid"), "unit": s0["units"]})
        if not isinstance(text0, str) or not isinstance(htext0, str):
            self.check("b-g: run", None, "not run: the files could not be read through the Panel")
            return

        # (b) one setting
        text1, how = set_pg_setting(text0, "work_mem", "8MB")
        self.current["work_mem_edit"] = how
        t0 = self.guest_clock()
        b = self.config_post("S4b set work_mem = 8MB", conf, text1, v0, service)
        body = b["_parsed"] if isinstance(b["_parsed"], dict) else {}
        n1 = self.file("b-conf-after", conf)
        s1 = self.pg("b-server-after", unit)
        self.keep_text("native-text/postgresql-journal-b.txt", self.journal("b-unit-journal", [unit], t0) or "(no lines)")
        changes = self.diff("b-postgresql.conf", n0["file"]["text"], n1["file"]["text"])
        self.check("b: the save answers 200, applied = reloaded", b["status"] == 200 and body.get("applied") == "reloaded",
                   b.get("answer") or {k: body.get(k) for k in ("success", "applied", "daemon_check", "backup", "restart_required", "unchanged")})
        self.check("b: the file differs only on the setting's line",
                   len(changes["added"]) == 1 and len(changes["removed"]) <= 1 and "work_mem" in changes["added"][0]
                   and n1["file"].get("text") == text1, changes)
        self.backup_kept("b", b, n0, n1)
        self.check("b: the running server shows the new value (`SHOW work_mem` = 8MB)", s1["_work_mem"] == "8MB",
                   [s0["_work_mem"], s1["_work_mem"]])
        self.check("b: the server re-read its files (configuration load time moved) and was not restarted (same postmaster "
                   "PID, same start time)",
                   s1["_times"][1] != s0["_times"][1] and s1["_times"][0] == s0["_times"][0]
                   and s1.get("postmaster_pid") == s0.get("postmaster_pid") and s1["_main_pid"] == s0["_main_pid"],
                   {"start_time": [s0["_times"][0], s1["_times"][0]], "load_time": [s0["_times"][1], s1["_times"][1]],
                    "postmaster_pid": [s0.get("postmaster_pid"), s1.get("postmaster_pid")], "unit_main_pid": [s0["_main_pid"], s1["_main_pid"]]})
        def fresh(label: str, path: str) -> Any:
            return (self.config_get(label, path)["_parsed"] or {}).get("Version")

        v1 = fresh("S4c read", conf)
        self.check("b: the version the save answered is the version of the file now on the server",
                   body.get("version") == v1 == "cf1-" + str(n1["file"].get("sha256")), [body.get("version"), v1])

        # (c) a value the server refuses
        bad, _ = set_pg_setting(text1, "work_mem", "eight-megabytes")
        c = self.config_post("S4c set work_mem to a value PostgreSQL refuses", conf, bad, v1, service)
        self.refused("c: refused 422 CONFIG_INVALID (daemon)", c, 422, "CONFIG_INVALID", "daemon")
        detail = ((c["_parsed"] or {}).get("vars") or {}).get("detail") if isinstance(c["_parsed"], dict) else None
        self.check("c: the answer carries the server's own message", bool(detail) and "work_mem" in str(detail), (c["_parsed"] or {}).get("vars"))
        n2 = self.file("c-conf-after-invalid", conf)
        self.unchanged("c", n1, n2)
        self.check("c: the server still runs with 8MB", self.pg("c-server-after-invalid", unit)["_work_mem"] == "8MB")

        # (d) empty content
        v1 = fresh("S4d read", conf)
        d = self.config_post("S4d save empty content", conf, "", v1, service)
        self.refused("d: empty content is refused 422 CONFIG_INVALID (empty)", d, 422, "CONFIG_INVALID", "empty")
        self.unchanged("d", n2, self.file("d-conf-after-empty", conf))

        # (e) pg_hba.conf
        htext1 = add_line(htext0, HBA_GOOD_LINE)
        e1 = self.config_post("S4e add a host rule to pg_hba.conf", hba, htext1, hv0, service)
        hbody = e1["_parsed"] if isinstance(e1["_parsed"], dict) else {}
        h1 = self.file("e-hba-after-add", hba)
        s2 = self.pg("e-server-after-hba", unit)
        hchanges = self.diff("e-pg_hba.conf", h0["file"]["text"], h1["file"]["text"])
        self.check("e: the valid rule is applied (200, reloaded) and the file differs by that one line",
                   e1["status"] == 200 and hbody.get("applied") == "reloaded" and hchanges["added"] == [HBA_GOOD_LINE]
                   and not hchanges["removed"], {"answer": e1.get("answer") or hbody, "changes": hchanges})
        self.backup_kept("e", e1, h0, h1)
        mine = [r for r in s2["_hba"] if r["address"] == "10.99.0.0"]
        self.check("e: the running server lists the rule in pg_hba_file_rules and reports no error in the file",
                   len(mine) == 1 and mine[0]["method"] == "scram-sha-256" and not any(r["error"] for r in s2["_hba"]),
                   {"rule": mine, "errors": [r for r in s2["_hba"] if r["error"]]})
        hv1 = fresh("S4e read pg_hba.conf", hba)
        e2 = self.config_post("S4e add a rule without a method", hba, add_line(htext1, HBA_BAD_LINE), hv1, service)
        self.refused("e: a syntactically bad rule is refused 422 CONFIG_INVALID (syntax)", e2, 422, "CONFIG_INVALID", "syntax")
        h2 = self.file("e-hba-after-bad-rule", hba)
        self.unchanged("e (bad rule)", h1, h2)
        without, removed = remove_local_rules(htext1)
        self.current["hba_local_rules_removed"] = removed
        hv1 = fresh("S4e read pg_hba.conf again", hba)
        e3 = self.config_post("S4e remove the local rules (local administrator access)", hba, without, hv1, service)
        self.refused("e: a file without the local postgres access is refused 422 CONFIG_INVALID (lockout)", e3, 422,
                     "CONFIG_INVALID", "lockout")
        self.unchanged("e (lockout)", h2, self.file("e-hba-after-lockout", hba))

        # (f) stale after an owner hand edit
        v1 = fresh("S4f read before the owner's edit", conf)
        owner_text = add_line(n1["file"]["text"], OWNER_NOTE)
        self.owner("f-postgresql.conf-note", "owner-edit", path=conf, content_b64=b64(owner_text))
        n3 = self.file("f-conf-after-owner-edit", conf)
        stale, _ = set_pg_setting(text1, "work_mem", "12MB")
        f = self.config_post("S4f save with the version from before the owner's edit", conf, stale, v1, service)
        self.refused("f: the stale write is refused 409 SETTINGS_CHANGED (config_file)", f, 409, "SETTINGS_CHANGED", "config_file")
        n4 = self.file("f-conf-after-stale", conf)
        self.unchanged("f", n3, n4)
        self.check("f: the file is the owner's", n4["file"].get("text") == owner_text)
        f2 = self.config_get("S4f read again", conf)
        v2 = (f2["_parsed"] or {}).get("Version")

        # (g) the owner's own reload hook fails
        hook = self.owner("g-reload-hook", "owner-reload-hook", action="apply", unit=unit)
        self.current["reload_hook"] = {"applied": hook.get("applied"), "exec_reload": hook.get("exec_reload"),
                                       "realism": "an owner's own unit drop-in; no failure of the packaged unit's reload "
                                                  "was found that an owner causes"}
        try:
            wanted, _ = set_pg_setting(owner_text, "work_mem", "16MB")
            t0 = self.guest_clock()
            g = self.config_post("S4g save while the unit's reload fails", conf, wanted, v2, service)
            n5 = self.file("g-conf-after-failed-reload", conf)
            s3 = self.pg("g-server-after-failed-reload", unit)
            self.keep_text("native-text/postgresql-journal-g.txt", self.journal("g-unit-journal", [unit], t0) or "(no lines)")
            gbody = g["_parsed"] if isinstance(g["_parsed"], dict) else {}
            gvars = gbody.get("vars") if isinstance(gbody.get("vars"), dict) else {}
            self.refused("g: the save answers 502 CONFIG_RELOAD_FAILED with the reason restored_unit_reload_failed", g, 502,
                         "CONFIG_RELOAD_FAILED", "restored_unit_reload_failed")
            self.check("g: the previous file is back in place, byte for byte", n5["file"].get("text") == owner_text
                       and n5["file"].get("sha256") == n3["file"].get("sha256"),
                       {"sha256": [n3["file"].get("sha256"), n5["file"].get("sha256")]})
            self.check("g: the file keeps its owner, group and mode", same_metadata(n3["file"], n5["file"])["equal"],
                       same_metadata(n3["file"], n5["file"]))
            named = gvars.get("name")
            kept = next((x for x in n5["backups"] if x.get("path") == named), None)
            self.current["reload_failed"] = {
                "reason": gbody.get("reason"), "vars": gvars, "error": gbody.get("error"),
                "work_mem_after": s3["_work_mem"], "postmaster_pid": [s0.get("postmaster_pid"), s3.get("postmaster_pid")],
                "file_is_previous": n5["file"].get("text") == owner_text,
                "named_copy": named, "named_copy_exists": bool(kept),
                "backups_before": len(n4["backups"]), "backups_after": len(n5["backups"]),
                "candidates_left": n5.get("candidates_left"),
                "postgresql_version": (s3["answers"]["files"].get("stdout", "").strip().split("\t") + [""] * 4)[3]}
            self.check("g: the answer names no copy", not named, gvars)
            self.check("g: no copy was left next to the file (as many kept copies as before this save; no validation copy)",
                       len(n5["backups"]) == len(n4["backups"]) and n5.get("candidates_left") == [],
                       {"backups": [len(n4["backups"]), len(n5["backups"])], "candidates_left": n5.get("candidates_left")})
            self.check("g: the answer names the unit whose reload fails", str(gvars.get("unit") or "").startswith("postgresql"),
                       gvars)
            self.check("g: `SHOW work_mem` is the previous value (8MB) and the server was not restarted (same postmaster PID)",
                       s3["_work_mem"] == "8MB" and s3.get("postmaster_pid") == s0.get("postmaster_pid"),
                       self.current["reload_failed"])
            # The queries the Agent uses for this answer, run the same way, with their raw output.
            reread = self.owner("g-agent-reread-batch", "owner-pg-reread")
            raw = reread["answer"]
            self.current["agent_queries"] = {"postgresql_version": reread.get("version"), "returncode": raw.get("returncode"),
                                             "stdout": raw.get("stdout"), "stderr": raw.get("stderr"), "batch": reread.get("batch")}
            self.keep_text("native-text/postgresql-agent-queries-g.txt",
                           "-- " + str(reread.get("version")) + "\n-- batch (cmd/agent/db_config.go dbConfigPostgreSQLRereadVerified):\n"
                           + str(reread.get("batch")) + "\n-- raw stdout:\n" + str(raw.get("stdout")) + "\n-- stderr:\n" + str(raw.get("stderr")))
            lines = dict(l.split("=", 1) for l in str(raw.get("stdout") or "").splitlines() if "=" in l)
            self.check("g: asked the same way, PostgreSQL confirms it (this file, signal sent, load time after the signal, "
                       "no error in the files)", lines.get("file") == conf and lines.get("signal") in ("true", "t")
                       and "error" not in lines and float(lines.get("loaded") or 0) >= float(lines.get("before") or 1e18),
                       lines)
        finally:
            self.owner("g-reload-hook-removed", "owner-reload-hook", action="restore", unit=unit)
        again = self.owner("g-owner-reload", "owner-systemctl", action="reload", unit=unit)
        s4 = self.pg("g-server-end", unit)
        self.check("g: with the hook removed `systemctl reload` of the unit succeeds and the server answers",
                   again["result"].get("returncode") == 0 and bool(s4["_work_mem"]), {"reload": again["result"], "work_mem": s4["_work_mem"]})
        self.keep_text("native-text/postgresql.conf-end.txt", self.file("g-conf-end", conf)["file"].get("text", ""))

    # -- S5 -----------------------------------------------------------------------------

    def maria(self, label: str) -> dict:
        value = self.snap(label, "read-mariadb", variables=["max_connections"])
        value["_max_connections"] = value["answers"].get("max_connections", {}).get("stdout", "").strip()
        units = value["units"]
        active = next((u for u in ("mariadb.service", "mysql.service") if units.get(u, {}).get("ActiveState") == "active"), "mariadb.service")
        value["_identity"] = [units.get(active, {}).get("MainPID"), units.get(active, {}).get("ActiveEnterTimestamp"),
                              units.get(active, {}).get("NRestarts"), value.get("server_pid")]
        return value

    def s5_mariadb(self) -> None:
        path = self.state["config_files"].get("mariadb_conf")
        if not path:
            self.check("the component scan names a MariaDB option file", False, self.state["config_files"])
            return
        service = "MariaDB"
        self.current["file"] = path
        n0 = self.file("a-file-before", path)
        m0 = self.maria("a-server-before")
        self.keep_text("native-text/mariadb-option-file-before.txt", n0["file"].get("text", ""))
        a = self.config_get("S5a read the option file", path)
        text0, v0 = (a["_parsed"] or {}).get("Content"), (a["_parsed"] or {}).get("Version")
        self.check("a: the read answers the file's own bytes with their version",
                   a["status"] == 200 and text0 == n0["file"].get("text") and v0 == "cf1-" + str(n0["file"].get("sha256")),
                   [a["status"], v0, n0["file"].get("sha256")])
        self.check("a: MariaDB is running and answers", bool(m0["_max_connections"]) and m0["_identity"][0] not in (None, "0"),
                   {"max_connections": m0["_max_connections"], "identity": m0["_identity"]})
        if not isinstance(text0, str):
            self.check("b-e: run", None, "not run: the file could not be read through the Panel")
            return
        value = "173" if m0["_max_connections"] != "173" else "174"
        text1, how, index = set_mariadb_option(text0, "max_connections", value)
        self.current["option_edit"] = {"how": how, "value": value}

        # (b) one option
        b = self.config_post(f"S5b set max_connections = {value}", path, text1, v0, service)
        body = b["_parsed"] if isinstance(b["_parsed"], dict) else {}
        n1 = self.file("b-file-after", path)
        m1 = self.maria("b-server-after")
        changes = self.diff("b-mariadb-option-file", n0["file"]["text"], n1["file"]["text"])
        self.check("b: the save answers 200 and says a restart is required (applied = restart_required)",
                   b["status"] == 200 and body.get("applied") == "restart_required",
                   b.get("answer") or {k: body.get(k) for k in ("success", "applied", "daemon_check", "backup", "restart_required", "unchanged")})
        self.check("b: the file differs only on the option's line", n1["file"].get("text") == text1
                   and all("max_connections" in line or line.strip() in ("", "[mysqld]") for line in changes["added"])
                   and len(changes["removed"]) <= 1 and any("max_connections" in line for line in changes["added"]), changes)
        self.backup_kept("b", b, n0, n1)
        self.check("b: the Panel did not restart MariaDB (same process, same start, no restart counted) and the running "
                   "value is the old one", m1["_identity"] == m0["_identity"] and m1["_max_connections"] == m0["_max_connections"],
                   {"identity": [m0["_identity"], m1["_identity"]], "max_connections": [m0["_max_connections"], m1["_max_connections"]]})
        def fresh(label: str) -> Any:
            return (self.config_get(label, path)["_parsed"] or {}).get("Version")

        v1 = fresh("S5c read")
        self.check("b: the version the save answered is the version of the file now on the server",
                   body.get("version") == v1 == "cf1-" + str(n1["file"].get("sha256")), [body.get("version"), v1])

        # (c) refused by mariadbd
        c1 = self.config_post("S5c add an unknown variable", path, insert_after(text1, index, "set1_unknown_option = 1"), v1, service)
        self.refused("c: an unknown variable is refused 422 CONFIG_INVALID (daemon)", c1, 422, "CONFIG_INVALID", "daemon")
        n2 = self.file("c-file-after-unknown", path)
        self.unchanged("c (unknown variable)", n1, n2)
        bad, _, _ = set_mariadb_option(text1, "max_connections", MARIADB_UNUSABLE_VALUE)
        v1 = fresh("S5c read again")
        c2 = self.config_post("S5c set max_connections to a value MariaDB refuses", path, bad, v1, service)
        self.refused("c: an unusable value is refused 422 CONFIG_INVALID (daemon)", c2, 422, "CONFIG_INVALID", "daemon")
        n2b = self.file("c-file-after-bad-value", path)
        self.unchanged("c (unusable value)", n2, n2b)

        # (c, observation) a value MariaDB itself reads as a number with a size suffix and adjusts
        adjusted, _, _ = set_mariadb_option(text1, "max_connections", MARIADB_ADJUSTED_VALUE)
        cx = self.config_post(f"S5c set max_connections = {MARIADB_ADJUSTED_VALUE}", path, adjusted, fresh("S5c read (adjusted value)"), service)
        own = self.snap("c-mariadbd-own-reading", "read-mariadb-check", path=path, variables=["max_connections"],
                        content_b64=b64(adjusted))
        xbody = cx["_parsed"] if isinstance(cx["_parsed"], dict) else {}
        xvars = xbody.get("vars") if isinstance(xbody.get("vars"), dict) else {}
        after_x = self.file("c-file-after-adjusted-value", path)
        self.current["adjusted_value"] = {
            "value": MARIADB_ADJUSTED_VALUE, "panel_status": cx["status"], "panel_applied": xbody.get("applied"),
            "panel_daemon_check": xbody.get("daemon_check"), "panel_answer": cx.get("answer"),
            "written": after_x["file"].get("text") == adjusted,
            "mariadbd_returncode_on_that_text": own.get("returncode"),
            "mariadbd_resulting_max_connections": (own.get("resulting_variables") or {}).get("max_connections"),
            "mariadbd_stderr_lines": [l for l in own.get("stderr", "").splitlines() if "max" in l.lower()][-4:]}
        self.refused(f"c (adjusted value): `max_connections = {MARIADB_ADJUSTED_VALUE}` is refused 422 CONFIG_INVALID (daemon)",
                     cx, 422, "CONFIG_INVALID", "daemon")
        said = str(xvars.get("detail") or "")
        self.check("c (adjusted value): the answer carries MariaDB's own line (the adjustment) and the option's name",
                   "adjusted" in said and "max_connections" in (said + " " + str(xvars.get("name") or "")),
                   {"vars": xvars, "mariadbd_printed": self.current["adjusted_value"]["mariadbd_stderr_lines"]})
        if cx["status"] == 200:
            back = self.config_post("S5c correct max_connections on the page again", path, text1,
                                    fresh("S5c read (before the correction)"), service)
            n2b = self.file("c-file-after-correction", path)
            self.note("the adjusted value was written; the page saved the earlier value again so that the following "
                      "sub-steps compare against a known file", back.get("answer") or back["status"])
        else:
            self.unchanged("c (adjusted value)", n2b, after_x)
            n2b = after_x

        # (d) empty
        v1 = fresh("S5d read")
        d = self.config_post("S5d save empty content", path, "", v1, service)
        self.refused("d: empty content is refused 422 CONFIG_INVALID (empty)", d, 422, "CONFIG_INVALID", "empty")
        self.unchanged("d", n2b, self.file("d-file-after-empty", path))

        # (e) stale after an owner hand edit
        v1 = fresh("S5e read before the owner's edit")
        owner_text = add_line(n1["file"]["text"], OWNER_NOTE)
        self.owner("e-option-file-note", "owner-edit", path=path, content_b64=b64(owner_text))
        n3 = self.file("e-file-after-owner-edit", path)
        other, _, _ = set_mariadb_option(text1, "max_connections", "181")
        e = self.config_post("S5e save with the version from before the owner's edit", path, other, v1, service)
        self.refused("e: the stale write is refused 409 SETTINGS_CHANGED (config_file)", e, 409, "SETTINGS_CHANGED", "config_file")
        n4 = self.file("e-file-after-stale", path)
        self.unchanged("e", n3, n4)
        m2 = self.maria("e-server-end")
        self.check("end: MariaDB is still the same process", m2["_identity"] == m0["_identity"], [m0["_identity"], m2["_identity"]])
        self.keep_text("native-text/mariadb-option-file-end.txt", n4["file"].get("text", ""))

    # -- S6 -----------------------------------------------------------------------------

    def s6_catchall_queue(self) -> None:
        domain_id, mailbox = self.state["domain_id"], self.state["mailbox"]
        url = f"/api/v1/domains/{domain_id}/mail/catch-all"
        hints = dict(kind="catchall")

        def native(label: str) -> dict:
            value = self.snap(label, "read-catchall", domain=SITE_DOMAIN)
            hits = [l["query"].get("stdout", "").strip() for l in value["lookups"] if l["query"].get("returncode") == 0]
            rows = self.rows(label + "-rows").get("mail_catch_all")
            return {"postfix": hits, "row": rows[0].get("destination") if isinstance(rows, list) and rows else None}

        before = native("a-catchall-before")
        a = self.call("S6 catch-all read", "GET", url, **hints)
        v0 = (a["_parsed"] or {}).get("version")
        self.check("catch-all: the read answers 200, off, with a version; Postfix has no catch-all for the domain",
                   a["status"] == 200 and (a["_parsed"] or {}).get("enabled") is False and str(v0).startswith("ca1-")
                   and before == {"postfix": [], "row": None}, {"answer": a["_parsed"], "native": before})
        b = self.call("S6 catch-all set without a version", "PUT", url, {"destination": mailbox}, **hints)
        self.refused("catch-all: a write without a version is refused 409 SETTINGS_VERSION_REQUIRED (mail_catch_all)", b, 409,
                     "SETTINGS_VERSION_REQUIRED", "mail_catch_all")
        c = self.call("S6 catch-all set", "PUT", url, {"destination": mailbox, "version": v0}, **hints)
        v1 = (c["_parsed"] or {}).get("version")
        after = native("c-catchall-after-set")
        self.check("catch-all: set answers 200 and Postfix resolves @domain to the mailbox",
                   c["status"] == 200 and after["postfix"] == [mailbox] and after["row"] == mailbox, {"answer": c["_parsed"], "native": after})
        d = self.call("S6 catch-all stale set (the version from before)", "PUT", url,
                      {"destination": f"postmaster@{SITE_DOMAIN}", "version": v0}, **hints)
        self.refused("catch-all: a stale set is refused 409 SETTINGS_CHANGED (mail_catch_all)", d, 409, "SETTINGS_CHANGED",
                     "mail_catch_all")
        e = self.call("S6 catch-all stale disable", "DELETE", url + "?version=" + quote(str(v0)), None, **hints)
        self.refused("catch-all: a stale disable is refused 409 SETTINGS_CHANGED (mail_catch_all)", e, 409, "SETTINGS_CHANGED",
                     "mail_catch_all")
        self.check("catch-all: nothing changed by the stale writes", native("e-catchall-after-stale") == after)
        f = self.call("S6 catch-all disable", "DELETE", url + "?version=" + quote(str(v1)), None, **hints)
        gone = native("f-catchall-after-disable")
        self.check("catch-all: disable answers 200 and Postfix has no catch-all for the domain again",
                   f["status"] == 200 and (f["_parsed"] or {}).get("enabled") is False and gone == {"postfix": [], "row": None},
                   {"answer": f["_parsed"], "native": gone})

        # mail queue
        queue_url = "/api/v1/postfix/queue"
        qhints = dict(kind="queue")

        def read(label: str) -> tuple:
            native_queue = self.snap(label, "read-queue")["postqueue"]
            answer = self.call("S6 mail queue: " + label, "GET", queue_url, **qhints)
            return native_queue, answer

        q0, qa = read("queue-idle")
        self.check("queue: with Postfix running the native queue is empty and the Panel answers a known empty list",
                   q0.get("returncode") == 0 and not q0.get("stdout", "").strip() and qa["status"] == 200 and qa["_parsed"] == [],
                   {"native": q0, "status": qa["status"], "json": qa["_parsed"]})
        self.owner("queue-stop-postfix", "owner-systemctl", action="stop", unit="postfix")
        try:
            q1, qb = read("queue-postfix-stopped")
            failed = q1.get("returncode") != 0
            self.current["queue_postfix_stopped"] = {"native_returncode": q1.get("returncode"), "native_stderr": q1.get("stderr"),
                                                     "native_stdout": q1.get("stdout"), "status": qb["status"], "answer": qb.get("answer")}
            if failed:
                self.refused("queue (Postfix stopped): the native read fails and the Panel answers 502 MAIL_QUEUE_UNREADABLE",
                             qb, 502, "MAIL_QUEUE_UNREADABLE")
                self.check("queue (Postfix stopped): no cause is named that was not verified (not postfix_config)",
                           (qb["_parsed"] or {}).get("reason") != "postfix_config", qb.get("answer"))
            else:
                self.note("with Postfix stopped root's `postqueue -j` still reads the queue directly, so the queue is "
                          "known; the Panel's answer is judged against that", self.current["queue_postfix_stopped"])
                self.check("queue (Postfix stopped): the native read still succeeds for root and the Panel answers the "
                           "known list", qb["status"] == 200 and qb["_parsed"] == [], self.current["queue_postfix_stopped"])
        finally:
            started = self.owner("queue-start-postfix", "owner-systemctl", action="start", unit="postfix")
        self.check("queue: Postfix is started again", started["result"].get("returncode") == 0, started["result"])
        base_text = self.snap("queue-main.cf-before-typo", "read-file", path=MAIN_CF)["file"]["text"]
        try:
            self.owner("queue-typo-main-cf", "owner-edit", path=MAIN_CF, content_b64=b64(add_line(base_text, MAIN_CF_TYPO)))
            q2, qc = read("queue-main.cf-typo")
            self.current["queue_typo"] = {"native_returncode": q2.get("returncode"), "native_stderr": q2.get("stderr"),
                                          "status": qc["status"], "answer": qc.get("answer")}
            self.check("queue (owner's typo in main.cf): the native `postqueue -j` fails",
                       True if q2.get("returncode") not in (0, None) else None, q2)
            if q2.get("returncode") not in (0, None):
                self.refused("queue (owner's typo): the Panel answers 502 MAIL_QUEUE_UNREADABLE with the verified cause "
                             "postfix_config, not an empty queue", qc, 502, "MAIL_QUEUE_UNREADABLE", "postfix_config")
                qsaid = str(((qc["_parsed"] or {}).get("vars") or {}).get("detail") or "") if isinstance(qc["_parsed"], dict) else ""
                self.check("queue (owner's typo): the answer carries postqueue's own line",
                           bool(qsaid) and ("bad numerical configuration" in qsaid or qsaid[:30] in str(q2.get("stderr"))),
                           {"vars.detail": qsaid, "postqueue_printed": q2.get("stderr")})
        finally:
            self.owner("queue-typo-removed", "owner-edit", path=MAIN_CF, content_b64=b64(base_text))
        q3, qd = read("queue-after-restore")
        self.check("queue: after the owner's correction the queue is known and empty again",
                   q3.get("returncode") == 0 and qd["status"] == 200 and qd["_parsed"] == [], {"native": q3, "status": qd["status"]})
        s = self.smtp("smtp-end")
        self.check("end: mail is accepted on 25 and 587 answers", s["25"].get("ok") and s["587"].get("ok"), s)

    # -- S7 -----------------------------------------------------------------------------

    def s7_metadata(self) -> None:
        table = {}
        for path, before in (self.state.get("meta_before") or {}).items():
            now = self.snap("meta-after-" + path.strip("/").replace("/", "_"), "read-file", path=path)
            after = now["file"]
            verdict = same_metadata(before, after)
            written = before.get("sha256") != after.get("sha256") or before.get("inode") != after.get("inode")
            table[path] = {"before": {k: before.get(k) for k in ("owner", "group", "mode", "uid", "gid", "inode")},
                           "after": {k: after.get(k) for k in ("owner", "group", "mode", "uid", "gid", "inode")},
                           "content_or_inode_changed": written, "equal": verdict["equal"],
                           "backups": [{k: b.get(k) for k in ("path", "owner", "group", "mode")} for b in now["backups"]]}
            self.check(f"{path}: owner, group and mode are what they were before the sections", verdict["equal"], table[path])
            self.check(f"{path}: every kept backup has the file's owner, group and mode",
                       all(same_metadata(after, b)["equal"] for b in now["backups"]), table[path]["backups"])
        self.current["files"] = table
        if not table:
            self.check("files were recorded before the sections", None)
        # set2 (O5): the component scan lists each configuration file once.
        listed = (self.state.get("config_files") or {}).get("listed") or {}
        duplicates = {service: sorted({p for p in paths if paths.count(p) > 1}) for service, paths in listed.items()}
        self.current["config_files_listed"] = listed
        self.check("O5: the component scan lists each configuration file once (PostgreSQL and MariaDB)",
                   bool(listed.get("postgresql")) and bool(listed.get("mariadb")) and not any(duplicates.values()),
                   {"listed": listed, "listed_more_than_once": duplicates})

    # -- S8 (set2): service actions ---------------------------------------------------------------------------------

    def service_units(self) -> dict:
        pg = pg_unit((self.state.get("config_files") or {}).get("pg_conf") or "")
        units = {"nginx": ["nginx.service"], "mariadb": ["mariadb.service"],
                 "postgresql": ["postgresql.service"] + ([pg] if pg != "postgresql.service" else [])}
        if self.settings.mail:
            units["dovecot"] = ["dovecot.service"]
            units["postfix"] = ["postfix.service", "postfix@-.service"]
        return units

    def service_state(self, label: str, service: str, cat: bool = False) -> dict:
        return self.snap(label, "read-service", units=self.service_units()[service], cat=cat,
                         postfix=service == "postfix", postgres=service == "postgresql", mariadb=service == "mariadb")

    def daemon(self, service: str, state: dict) -> dict:
        """The unit that runs the daemon (the last listed unit that is loaded and is not a oneshot), and whether the
        daemon runs as the daemon itself says where it can be asked."""
        units = state["units"]
        real = None
        for name in self.service_units()[service]:
            properties = units[name]["properties"]
            if properties.get("LoadState") == "loaded" and properties.get("Type") != "oneshot":
                real = name
        properties = units[real]["properties"] if real else {}
        pid = int(properties.get("MainPID") or 0) if str(properties.get("MainPID") or "0").isdigit() else 0
        running = properties.get("ActiveState") == "active" and pid > 0
        asked = "systemd: the unit is active with a main process"
        if service == "postfix":
            # H27: the master's PID file and /proc; `postfix status` refuses to answer while main.cf is refused.
            running = bool(state["postfix"]["master_alive"])
            pid = state["postfix"]["master_pid"] if state["postfix"]["master_alive"] else 0
            asked = "the master's PID file and /proc (`postfix status` exit %s)" % state["postfix"]["status"].get("returncode")
        elif service == "postgresql":
            running = bool(state["postgres"]["answers"])
            pid = state["postgres"]["postmaster_pid"] if state["postgres"]["postmaster_alive"] else 0
            asked = "a query over the local socket and postmaster.pid"
        elif service == "mariadb":
            running = bool(state["mariadb"]["answers"])
            pid = state["mariadb"]["server_pid"] if state["mariadb"]["server_alive"] else 0
            asked = "a query over the local socket and the server's PID file"
        return {"real_unit": real, "pid": pid or 0, "running": bool(running), "asked": asked,
                "exec_reload": properties.get("ExecReload"), "reload_result": properties.get("ReloadResult"),
                "conf_load_time": (state.get("postgres") or {}).get("conf_load_time"),
                "units": {name: {k: units[name]["properties"].get(k) for k in ("LoadState", "ActiveState", "SubState", "MainPID", "Result", "ReloadResult")}
                          for name in units}}

    def service_action(self, service: str, action: str, situation: str = "", expect: tuple | None = None) -> dict:
        tag = re.sub(r"[^a-z0-9]+", "-", f"{service}-{action}-{situation}".lower()).strip("-")[:60]
        before = self.service_state(tag + "-before", service)
        d0 = self.daemon(service, before)
        t0 = self.guest_clock()
        label = f"S8 {service} {action}" + (f" ({situation})" if situation else "")
        busy = []
        for attempt in range(1, 10):
            answer = self.call(label + ("" if attempt == 1 else f", pressed again ({attempt})"), "POST", "/api/v1/service/action",
                               {"name": service, "action": action}, timeout=240, kind="service")
            refused = answer["_parsed"] if isinstance(answer["_parsed"], dict) else {}
            if answer["status"] != 409 or refused.get("code") not in BUSY_CODES:
                break
            # H31: refused before anything was sent; the owner waits and presses again.
            busy.append({"at": answer["at"], "code": refused.get("code"), "error": refused.get("error")})
            time.sleep(6)
        after = self.service_state(tag + "-after", service)
        d1 = self.daemon(service, after)
        units = [u for u in self.service_units()[service] if before["units"][u]["properties"].get("LoadState") == "loaded"]
        log = self.journal(tag + "-journal", units, t0)
        if service == "postfix":
            log += self.journal(tag + "-journal-postfix", [], t0, ("postfix/master", "postfix/postfix-script"))
        body = answer["_parsed"] if isinstance(answer["_parsed"], dict) else {}
        reloaded_by = []
        if action == "reload":     # H30: a restart also changes these; they are signs of a reload only after a Reload
            reloaded_by = [m for m in RELOAD_MARKERS.get(service, ()) if m in log]
            if d0["exec_reload"] != d1["exec_reload"] and d1["exec_reload"]:
                reloaded_by.append("the unit's ExecReload ran")
            if service == "postgresql" and d0["conf_load_time"] and d1["conf_load_time"] and d0["conf_load_time"] != d1["conf_load_time"]:
                reloaded_by.append("pg_conf_load_time() moved")
        unit_reload_ok = d1["reload_result"] in (None, "", "success")
        truth = {"start": d1["running"], "stop": not d1["running"],
                 "restart": d1["running"] and (not d0["running"] or d1["pid"] != d0["pid"]),
                 "reload": d1["running"] and d0["running"] and d1["pid"] == d0["pid"] and bool(reloaded_by) and unit_reload_ok}[action]
        code = body.get("code")
        said = "success" if answer["status"] == 200 and body.get("success") else \
            "unknown" if code == "SERVICE_ACTION_UNKNOWN" else "failed"
        matches = None if said == "unknown" else (said == "success") == bool(truth)
        record = {"service": service, "action": action, "situation": situation, "at": answer["at"],
                  "status": answer["status"], "code": code, "reason": body.get("reason"), "vars": body.get("vars"),
                  "answer": {k: body.get(k) for k in ("success", "outcome", "applied", "unit", "error") if body.get(k) is not None},
                  "said": said, "truth_action_took_effect": bool(truth), "matches": matches, "refused_while_busy": busy,
                  "daemon_before": d0, "daemon_after": d1, "reload_evidence": reloaded_by,
                  "journal_lines": [l for l in log.splitlines() if l.strip()][-12:]}
        self.current.setdefault("actions", []).append(record)
        self.keep_text(f"journal/{tag}.txt", log or "(no lines)")
        self.check(f"{label}: the answer ({said}{', ' + str(code) + '/' + str(body.get('reason')) if code else ''}) "
                   "matches what the service shows", matches,
                   {k: record[k] for k in ("status", "code", "reason", "vars", "answer", "truth_action_took_effect", "reload_evidence")}
                   | {"before": {k: d0[k] for k in ("running", "pid", "units")}, "after": {k: d1[k] for k in ("running", "pid", "units")}})
        if expect is not None:
            self.refused(f"{label}: answers {expect[0]} {expect[1]}" + (f" ({expect[2]})" if len(expect) > 2 else ""), answer,
                         expect[0], expect[1], expect[2] if len(expect) > 2 else None)
        return record

    def s8_service_actions(self) -> None:
        units = self.service_units()
        order = [name for name in ("nginx", "mariadb", "dovecot", "postfix", "postgresql") if name in units]
        texts = {}
        for service in order:
            state = self.service_state(service + "-unit-text", service, cat=True)
            texts[service] = {name: {"cat": value.get("cat"), "show": value.get("properties"), "is_active": value.get("is_active")}
                              for name, value in state["units"].items()}
            self.keep_text(f"native-text/units-{service}.txt", "\n".join(
                f"##### systemctl cat {name}\n{value.get('cat') or '(not loaded)'}\n##### systemctl show {name}\n"
                + json.dumps(value.get("properties"), indent=1, sort_keys=True) for name, value in state["units"].items()))
        self.current["unit_facts"] = {service: {name: {k: value["show"].get(k) for k in (
            "LoadState", "Type", "RemainAfterExit", "ExecStart", "ExecReload", "ReloadResult", "ConsistsOf", "PartOf",
            "PropagatesReloadTo", "ReloadPropagatedFrom", "Wants", "FragmentPath", "DropInPaths")}
            for name, value in items.items()} for service, items in texts.items()}

        # healthy: every action on every service, in the order an owner would press them
        for service in order:
            for action, situation in (("start", "already running"), ("reload", ""), ("restart", ""), ("stop", ""),
                                      ("reload", "while stopped"), ("start", "")):
                self.service_action(service, action, situation)
            end = self.daemon(service, self.service_state(service + "-healthy-end", service))
            self.check(f"{service}: runs again after the healthy sequence", end["running"], end)

        # Postfix with a main.cf line its own check refuses
        if "postfix" in units:
            base_text = self.snap("postfix-main.cf-before-typo", "read-file", path=MAIN_CF)["file"]["text"]
            situation = "main.cf holds a line Postfix refuses"
            try:
                self.owner("postfix-typo-main-cf", "owner-edit", path=MAIN_CF, content_b64=b64(add_line(base_text, MAIN_CF_TYPO)))
                refused = self.postfix("postfix-with-typo")
                self.check("postfix (refused configuration): `postfix check` refuses main.cf",
                           True if refused["check"].get("returncode") not in (0, None) else None, refused["check"])
                self.service_action("postfix", "reload", situation, (502, "SERVICE_ACTION_FAILED", "check"))
                self.service_action("postfix", "restart", situation, (502, "SERVICE_ACTION_FAILED", "check"))
                self.service_action("postfix", "stop", situation)
                self.service_action("postfix", "start", situation, (502, "SERVICE_ACTION_FAILED", "check"))
            finally:
                self.owner("postfix-typo-removed", "owner-edit", path=MAIN_CF, content_b64=b64(base_text))
            self.service_action("postfix", "start", "after the owner's correction")
            mail = self.smtp("postfix-smtp-end")
            self.check("postfix: mail is accepted on 25 and 587 answers at the end", mail["25"].get("ok") and mail["587"].get("ok"), mail)

        # PostgreSQL with the owner's reload hook that fails after it signalled the server
        instance = units["postgresql"][-1]
        situation = "the owner's reload hook fails"
        hook = self.owner("postgresql-reload-hook", "owner-reload-hook", action="apply", unit=instance)
        self.current["reload_hook"] = {"unit": instance, "exec_reload": hook.get("exec_reload")}
        try:
            failed = self.service_action("postgresql", "reload", situation)
            moved = "pg_conf_load_time() moved" in failed["reload_evidence"]
            detail = str((failed.get("vars") or {}).get("detail") or "") + " " + str(failed["answer"].get("error") or "")
            self.current["reload_hook"].update(server_reread_its_files=moved, answer_detail=detail.strip(),
                                               reload_result=failed["daemon_after"]["reload_result"])
            self.check("postgresql (reload hook): the answer is a failure of the reload, not success",
                       failed["said"] == "failed", {k: failed[k] for k in ("status", "code", "reason", "vars")})
            self.check("postgresql (reload hook): the answer does not say the server keeps its previous settings when the "
                       "server did read its files again", not (moved and "previous settings" in detail),
                       {"server_reread_its_files": moved, "answer_detail": detail.strip()})
            self.service_action("postgresql", "restart", situation)
        finally:
            self.owner("postgresql-reload-hook-removed", "owner-reload-hook", action="restore", unit=instance)
        self.service_action("postgresql", "reload", "after the hook was removed")

        # PostgreSQL with a postgresql.conf the server refuses at start
        conf = (self.state.get("config_files") or {}).get("pg_conf")
        if conf:
            good_text = self.snap("postgresql.conf-before-refused-value", "read-file", path=conf)["file"]["text"]
            bad_text, _ = set_pg_setting(good_text, "work_mem", PG_REFUSED_VALUE)
            situation = "postgresql.conf holds a value the server refuses"
            try:
                self.owner("postgresql.conf-refused-value", "owner-edit", path=conf, content_b64=b64(bad_text))
                self.service_action("postgresql", "restart", situation)
                self.service_action("postgresql", "start", situation + ", after the failed restart")
            finally:
                self.owner("postgresql.conf-corrected", "owner-edit", path=conf, content_b64=b64(good_text))
            self.service_action("postgresql", "start", "after the owner's correction")
            now = self.daemon("postgresql", self.service_state("postgresql-after-correction", "postgresql"))
            if not now["running"]:
                self.service_action("postgresql", "restart", "after the owner's correction (Start left it stopped)")
                now = self.daemon("postgresql", self.service_state("postgresql-after-correction-restart", "postgresql"))
            if not now["running"]:
                self.owner("postgresql-owner-start-instance", "owner-systemctl", action="start", unit=instance)
                now = self.daemon("postgresql", self.service_state("postgresql-after-owner-start", "postgresql"))
                self.note("PostgreSQL was started by the owner on the server (`systemctl start` of the instance unit): "
                          "neither Start nor Restart on the Services page brought it back", now)
            self.check("postgresql: answers again at the end", now["running"], now)

    # -- collect and result -------------------------------------------------------------

    def collect(self, checks: dict) -> str:
        if not self.state.get("helpers_uploaded"):
            checks["reason"] = "the guest was never prepared"
            return "skipped"
        unit = pg_unit((self.state.get("config_files") or {}).get("pg_conf") or "")
        groups = {"product": ["celikpanel-panel.service", "celikpanel-agent.service"],
                  "services": ["postfix.service", "postfix@-.service", unit, "postgresql.service", "mariadb.service",
                               "cron.service", "cronie.service", "dovecot.service", "nginx.service"],
                  "install": [base.BASELINE_INSTALL_UNIT]}
        unavailable = {}
        for label, units in groups.items():
            try:
                value = self.workload("journal", "--since=-6h", "--lines", "20000", *sum((["--unit", u] for u in units), []),
                                      timeout=180)
                self.ev.write_text(f"{self.step_dir}/journal-{label}.txt", value.get("stdout", ""))
            except Exception as exc:  # noqa: BLE001 - one missing part never loses the others
                unavailable[label] = self.redactor.text(f"{type(exc).__name__}: {exc}")[:300]
        if self.state.get("domain_id"):
            try:
                self.current = {}
                checks["panel_rows_at_end"] = self.snap("rows-at-end", "read-panel-rows", domain_id=self.state["domain_id"])["rows"]
            except Exception as exc:  # noqa: BLE001
                unavailable["rows"] = type(exc).__name__
        checks.update(journals=sorted(groups), unavailable=unavailable)
        return "passed" if not unavailable else "inconclusive"

    def execute(self) -> dict:
        self.step("preflight", self.preflight)
        self.step("origin", self.origin, needs=("preflight",))
        self.step("baseline-install", self.baseline_install, needs=("origin",))
        self.step("owner-login", self.owner_login, needs=("baseline-install",))
        self.step("license", self.license, needs=("owner-login",))
        self.step("setup", self.setup, needs=("license",))
        self.step("site", self.site, needs=("setup",))
        functions = {"S1-cron": self.s1_cron, "S2-mail-policy": self.s2_mail_policy, "S3-backup-schedule": self.s3_backup_schedule,
                     "S4-postgresql": self.s4_postgresql, "S5-mariadb": self.s5_mariadb,
                     "S6-catchall-queue": self.s6_catchall_queue, "S7-file-metadata": self.s7_metadata,
                     "S8-service-actions": self.s8_service_actions}
        for key, title, mail_only in SECTIONS:
            if mail_only and not self.settings.mail:
                self.step(key, lambda checks: checks.update(reason="mail is not supported on this platform") or "skipped")
                self.sections[key] = {"title": title, "verdict": "not-run", "reason": "mail is not supported on this platform"}
                continue
            self.section(key, title, functions[key], needs=("site",))
            self.sections.setdefault(key, {"title": title, "verdict": "not-run", "reason": "the site step did not pass"})
        self.step("collect", self.collect)
        self.tunnel.close()
        verdicts = [s["verdict"] for s in self.steps]
        result = {"schema": base.RESULT_SCHEMA, "cell_kind": CELL_KIND, "native_evidence": False,
                  "cell": dataclasses.asdict(self.settings),
                  "identity": {k: self.identity[k] for k in ("cell_id", "node", "vm_uuid")},
                  "request_id": None, "provenance": base.provenance_for("good"),
                  "artifacts": {"baseline": {k: self.artifacts["baseline"][k] for k in ("version", "commit", "sha256")}},
                  "outcome": {"classification": "settings-writes-measured", "final_status": None},
                  "setup": {"purpose": self.settings.purpose, "components": sorted(self.settings.components),
                            "waiting": self.state.get("setup_waiting"),
                            "step": next(({k: s.get(k) for k in ("verdict", "reason", "started_at", "finished_at")}
                                          for s in self.steps if s["name"] == "setup"), None)},
                  "site": {k: self.state.get(k) for k in ("domain_id", "site_user", "mailbox", "config_files")},
                  "sections": self.sections, "findings": self.state["findings"],
                  "steps": [{k: s.get(k) for k in ("name", "verdict", "reason", "started_at", "finished_at")} for s in self.steps],
                  "overall": base.overall(verdicts),
                  "note": "settings-writes: observations for the owner's review; no update is started and no P0 row is judged."}
        self.step_dir = "result"
        return self.ev.finalize_upd1(result)


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

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
    settings = CELLS[args.cell]
    base.validate_work_root(args.work_root)
    if not 1024 < args.local_port < 65536:
        parser.error("--local-port must be an unprivileged loopback port")
    document = json.loads(args.artifacts.read_text())
    base.configure_labels(document)
    if args.command == "plan":
        base.validate_cell_artifacts(document, settings.cell, check_files=not args.dry_run)
        print(json.dumps(build_plan(settings, document, args.work_root, args.local_port), indent=2, sort_keys=True))
        return 0
    if not args.execute:
        parser.error("run mutates one registered disposable guest and requires --execute")
    base.validate_cell_artifacts(document, settings.cell)
    result = SettingsTrial(settings, document, args.work_root, args.local_port).execute()
    print(json.dumps({"overall": result["overall"], "cell_kind": CELL_KIND,
                      "sections": {k: v.get("verdict") for k, v in result["sections"].items()}}, sort_keys=True))
    return 0 if result["overall"] != "failed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
