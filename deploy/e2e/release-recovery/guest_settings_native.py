#!/usr/bin/env python3
"""set1: native state readers and the lab owner's actions inside a marked disposable QEMU guest.

Readers (``read-*`` modes) only inspect: files with owner, group and mode, the user's crontab as ``crontab -l``
answers it, Postfix values, the PostgreSQL and MariaDB servers through their own local clients, the Panel's
database opened read-only. Owner actions (``owner-*`` modes) are what a server owner does by hand on their own
server: edit a file in place, install a crontab, reload or stop a service, harden cron. They change the guest
and are refused outside the fixed path and unit lists below. Nothing here installs, updates or repairs
CelikPanel, and no mode contacts another host.

set1: işaretli geçici QEMU konuğunda yerel durum okuyucuları ve laboratuvar sahibinin elle yaptığı işlemler.
"""
from __future__ import annotations

import argparse
import base64
import grp
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import pwd
import re
import shutil
import socket
import sqlite3
import stat
import subprocess
import sys
import time

HERE = Path(__file__).resolve().parent
PRIVATE_ROOT = Path("/root/celikpanel-release-recovery-lab")
PANEL_DB = Path("/var/lib/celikpanel/celikpanel.db")
ENV = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL": "C", "LANGUAGE": "C"}
SCHEMA = "celikpanel/set1-native/v1"
TEXT_LIMIT = 1 << 20
USER_RE = re.compile(r"[a-z_][a-z0-9_-]{0,31}\Z")
DOMAIN_RE = re.compile(r"(?=.{1,253}\Z)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}\Z")
SETTING_RE = re.compile(r"[a-z][a-z0-9_.]{0,63}\Z")
# Files an owner edits by hand in this cell, and files the readers return with their text.
EDITABLE = (
    re.compile(r"/etc/postfix/main\.cf\Z"),
    re.compile(r"/etc/postgresql/[0-9]+(?:\.[0-9]+)?/[A-Za-z0-9][A-Za-z0-9_.-]*/(?:postgresql|pg_hba)\.conf\Z"),
    re.compile(r"/var/lib/(?:postgres|pgsql)/data/(?:postgresql|pg_hba)\.conf\Z"),
    re.compile(r"/etc/mysql/(?:[A-Za-z0-9_.-]+/)?[A-Za-z0-9_.-]+\.cnf\Z"),
    re.compile(r"/etc/my\.cnf\Z"),
)
UNITS = re.compile(r"(?:postfix(?:@-)?|postgresql(?:@[0-9]+(?:\.[0-9]+)?-[A-Za-z0-9_.-]+)?|mariadb|mysql|cron|cronie)\.service\Z")
SPOOLS = ("/var/spool/cron/crontabs", "/var/spool/cron")
CRON_ALLOW = Path("/etc/cron.allow")
FAULT_RECORD = "set1-cron-fault.json"
RELOCATED_TARGET = "/mnt/set1-data-volume/cron-spool"   # a volume that is not mounted: the path does not exist
HOOK_RECORD = "set1-reload-hook.json"
HOOK_SCRIPT = Path("/usr/local/sbin/set1-owner-reload-hook")
HOOK_POOLER = "set1-owner-pooler.service"
POLICY_PARAMETERS = ("message_size_limit", "smtpd_recipient_restrictions", "smtpd_client_message_rate_limit",
                     "anvil_rate_time_unit")


class Refused(ValueError):
    """The request is outside what this helper does."""


def _load_probe():
    spec = importlib.util.spec_from_file_location("set1_native_probe", HERE / "guest_probe.py")
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


def utc() -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


def run(argv, timeout=30, input_bytes=None, limit=TEXT_LIMIT) -> dict:
    started = time.time()
    try:
        done = subprocess.run(argv, input=input_bytes, capture_output=True, timeout=timeout, env=ENV)
    except FileNotFoundError:
        return {"argv": argv, "status": "unavailable"}
    except subprocess.TimeoutExpired:
        return {"argv": argv, "status": "timeout"}
    return {"argv": argv, "status": "ok", "returncode": done.returncode, "at": utc(),
            "seconds": round(time.time() - started, 3),
            "stdout": done.stdout[:limit].decode("utf-8", "replace"),
            "stderr": done.stderr[:16384].decode("utf-8", "replace")}


# -- pure rules (covered offline) -----------------------------------------------------

def editable_path(path: str) -> str:
    if not isinstance(path, str) or os.path.normpath(path) != path or not any(rule.fullmatch(path) for rule in EDITABLE):
        raise Refused("not a file an owner action or reader of this cell touches: " + repr(path))
    return path


def unit_name(unit: str) -> str:
    if not isinstance(unit, str):
        raise Refused("unit must be a string")
    if not unit.endswith(".service"):
        unit += ".service"
    if not UNITS.fullmatch(unit):
        raise Refused("not a unit an owner action of this cell touches: " + repr(unit))
    return unit


def user_name(user: str) -> str:
    if not isinstance(user, str) or not USER_RE.fullmatch(user) or user == "root":
        raise Refused("not a site account name")
    return user


def mode_text(mode: int) -> str:
    return "%04o" % stat.S_IMODE(mode)


def backup_names(names, base: str) -> list:
    """The Panel's kept copies of one file, oldest first (``<name>.celikpanel-backup-<UTC time>``)."""
    prefix = base + ".celikpanel-backup-"
    return sorted(n for n in names if n.startswith(prefix))


def candidate_names(names, base: str) -> list:
    """Validation copies the Agent places next to the file; none may remain after a write."""
    prefix = "." + base + ".celikpanel-candidate-"
    return sorted(n for n in names if n.startswith(prefix))


def parse_show(text: str) -> dict:
    return dict(line.split("=", 1) for line in text.splitlines() if "=" in line)


# -- readers ------------------------------------------------------------------

def _name(lookup, key):
    try:
        return lookup(key)[0]
    except KeyError:
        return None


def file_state(path: str, text: bool = False) -> dict:
    try:
        info = os.lstat(path)
    except OSError as exc:
        return {"path": path, "exists": False, "error": type(exc).__name__}
    state = {"path": path, "exists": True, "type": "regular" if stat.S_ISREG(info.st_mode) else
             "symlink" if stat.S_ISLNK(info.st_mode) else "directory" if stat.S_ISDIR(info.st_mode) else "other",
             "uid": info.st_uid, "gid": info.st_gid, "owner": _name(pwd.getpwuid, info.st_uid),
             "group": _name(grp.getgrgid, info.st_gid), "mode": mode_text(info.st_mode), "size": info.st_size,
             "inode": info.st_ino, "nlink": info.st_nlink, "mtime_ns": info.st_mtime_ns}
    if state["type"] == "symlink":
        state["target"] = os.readlink(path)
    if state["type"] == "regular" and info.st_size <= TEXT_LIMIT:
        data = Path(path).read_bytes()
        state["sha256"] = hashlib.sha256(data).hexdigest()
        if text:
            state["text"] = data.decode("utf-8", "replace")
    return state


def read_file(args: dict) -> dict:
    path = editable_path(args["path"])
    directory, base = os.path.dirname(path), os.path.basename(path)
    names = sorted(os.listdir(directory))
    return {"file": file_state(path, text=True),
            "backups": [file_state(os.path.join(directory, n), text=bool(args.get("backup_text")))
                        for n in backup_names(names, base)],
            "candidates_left": candidate_names(names, base)}


def spool_files(user: str) -> list:
    return [file_state(os.path.join(directory, user), text=True) for directory in SPOOLS
            if os.path.isdir(directory) and os.path.lexists(os.path.join(directory, user))]


def read_crontab(args: dict) -> dict:
    user = user_name(args["user"])
    crontab = shutil.which("crontab", path=ENV["PATH"])
    listed = run([crontab or "crontab", "-u", user, "-l"])
    owner = None
    if crontab:
        for query in (["dpkg", "-S", crontab], ["pacman", "-Qo", crontab]):
            answer = run(query)
            if answer.get("status") == "ok" and answer.get("returncode") == 0:
                owner = answer["stdout"].strip()
                break
    account = None
    try:
        entry = pwd.getpwnam(user)
        account = {"uid": entry.pw_uid, "gid": entry.pw_gid, "home": entry.pw_dir}
    except KeyError:
        pass
    return {"user": user, "account": account, "crontab_binary": crontab, "crontab_package": owner,
            "list": listed, "list_sha256": hashlib.sha256(listed.get("stdout", "").encode()).hexdigest(),
            "spool": spool_files(user), "spool_directories": [file_state(d) for d in SPOOLS],
            "cron_allow": file_state(str(CRON_ALLOW), text=True),
            "cron_deny": file_state("/etc/cron.deny", text=True)}


def unit_facts(units) -> dict:
    facts = {}
    for unit in units:
        shown = run(["systemctl", "show", unit, "-p", "LoadState", "-p", "ActiveState", "-p", "SubState",
                     "-p", "MainPID", "-p", "ActiveEnterTimestamp", "-p", "ExecMainStartTimestamp",
                     "-p", "NRestarts", "-p", "Result", "-p", "FragmentPath", "-p", "DropInPaths"])
        facts[unit] = parse_show(shown.get("stdout", ""))
    return facts


def _pid_file(path: str):
    try:
        return int(Path(path).read_text().split()[0])
    except (OSError, ValueError, IndexError):
        return None


def read_postfix(args: dict) -> dict:
    values = {name: run(["postconf", "-h", name]) for name in POLICY_PARAMETERS}
    result = {"postconf_n": run(["postconf", "-n"]), "values": values, "check": run(["postfix", "check"]),
              "main_cf": file_state("/etc/postfix/main.cf", text=True),
              "master_cf": file_state("/etc/postfix/master.cf"),
              "master_pid": _pid_file("/var/spool/postfix/pid/master.pid"),
              "units": unit_facts(("postfix.service", "postfix@-.service")),
              "mail_version": run(["postconf", "-h", "mail_version"]).get("stdout", "").strip()}
    if args.get("unit_text"):
        result["unit_text"] = {u: run(["systemctl", "cat", u]).get("stdout", "")
                               for u in ("postfix.service", "postfix@-.service")}
    return result


def _smtp_lines(sock) -> str:
    data = b""
    while True:
        chunk = sock.recv(4096)
        if not chunk:
            break
        data += chunk
        lines = data.split(b"\r\n")
        if len(lines) >= 2 and len(lines[-2]) >= 4 and lines[-2][3:4] == b" ":
            break
    return data.decode("ascii", "replace")


def smtp_dialogue(port: int, sender: str | None, recipient: str | None) -> dict:
    """Banner and EHLO; on request MAIL FROM and RCPT TO, then RSET and QUIT (no DATA: nothing is queued)."""
    result = {"port": port}
    try:
        with socket.create_connection(("127.0.0.1", port), timeout=8) as sock:
            sock.settimeout(8)
            result["banner"] = _smtp_lines(sock).strip()[:200]
            steps = [("ehlo", b"EHLO set1-probe.test\r\n")]
            if sender and recipient:
                steps += [("mail", ("MAIL FROM:<%s>\r\n" % sender).encode()),
                          ("rcpt", ("RCPT TO:<%s>\r\n" % recipient).encode()), ("rset", b"RSET\r\n")]
            for name, line in steps:
                sock.sendall(line)
                answer = _smtp_lines(sock).strip()
                result[name] = answer[:3]
                result[name + "_last_line"] = answer.splitlines()[-1][:200] if answer else ""
            sock.sendall(b"QUIT\r\n")
    except OSError as exc:
        result["error"] = type(exc).__name__
    result["ok"] = (str(result.get("banner", "")).startswith("220") and result.get("ehlo") == "250"
                    and (not (sender and recipient) or (result.get("mail") == "250" and result.get("rcpt") == "250")))
    return result


def read_smtp(args: dict) -> dict:
    sender, recipient = args.get("sender"), args.get("recipient")
    for value in (sender, recipient):
        if value is not None and not re.fullmatch(r"[a-z0-9._-]{1,64}@[a-z0-9.-]{1,253}", value):
            raise Refused("not a plain probe address")
    return {"25": smtp_dialogue(25, sender, recipient), "587": smtp_dialogue(587, None, None), "at": utc()}


def psql(statement: str) -> dict:
    return run(["sudo", "-u", "postgres", "psql", "--no-psqlrc", "--set", "ON_ERROR_STOP=on", "--no-align",
                "--tuples-only", "--quiet", "--field-separator", "\t"], input_bytes=(statement + "\n").encode())


def read_postgres(args: dict) -> dict:
    settings = [s for s in args.get("settings", []) if SETTING_RE.fullmatch(s)]
    unit = unit_name(args["unit"])
    queries = {
        "files": "SELECT current_setting('config_file'), current_setting('hba_file'), current_setting('data_directory'), "
                 "current_setting('server_version');",
        "load": "SELECT pg_postmaster_start_time(), pg_conf_load_time();",
        "hba": "SELECT line_number, type, array_to_string(database, ','), array_to_string(user_name, ','), "
               "coalesce(address, ''), coalesce(netmask, ''), coalesce(auth_method, ''), coalesce(error, '') "
               "FROM pg_hba_file_rules ORDER BY line_number;",
        "file_errors": "SELECT sourcefile, sourceline, coalesce(name, ''), error FROM pg_file_settings WHERE error IS NOT NULL;",
        "pending_restart": "SELECT name FROM pg_settings WHERE pending_restart ORDER BY name;",
    }
    if settings:
        names = ",".join("'" + s + "'" for s in settings)
        queries["settings"] = ("SELECT name, setting, coalesce(unit, ''), source, coalesce(sourcefile, ''), "
                               "coalesce(sourceline::text, '') FROM pg_settings WHERE name IN (" + names + ") ORDER BY name;")
        for setting in settings:
            queries["show_" + setting] = "SHOW " + setting + ";"
    answers = {name: psql(statement) for name, statement in queries.items()}
    data_directory = (answers["files"].get("stdout", "").strip().split("\t") + ["", "", ""])[2]
    postmaster = _pid_file(os.path.join(data_directory, "postmaster.pid")) if data_directory else None
    return {"unit": unit, "units": unit_facts((unit,)), "postmaster_pid": postmaster, "answers": answers, "at": utc()}


def read_mariadb(args: dict) -> dict:
    variables = [v for v in args.get("variables", []) if SETTING_RE.fullmatch(v)]
    client = shutil.which("mariadb", path=ENV["PATH"]) or shutil.which("mysql", path=ENV["PATH"])
    answers = {}
    if client:
        answers["version"] = run([client, "-N", "-B", "-e", "SELECT VERSION(), @@pid_file"])
        for variable in variables:
            answers[variable] = run([client, "-N", "-B", "-e", "SELECT @@global." + variable])
    units = unit_facts(("mariadb.service", "mysql.service"))
    pid_file = (answers.get("version", {}).get("stdout", "").strip().split("\t") + [""])[1] if client else ""
    return {"client": client, "units": units, "pid_file": pid_file, "server_pid": _pid_file(pid_file) if pid_file else None,
            "answers": answers, "at": utc()}


def read_mariadb_check(args: dict) -> dict:
    """What the installed server program makes of the option file as it is on disk now: the same read-only run the
    Agent uses before it installs a file (``--help --verbose`` with a private, empty data directory: it reads the
    option files, prints the resulting variables and exits; it starts nothing)."""
    import tempfile
    path = editable_path(args["path"])
    variables = [v for v in args.get("variables", []) if SETTING_RE.fullmatch(v)]
    program = shutil.which("mariadbd", path=ENV["PATH"]) or shutil.which("mysqld", path=ENV["PATH"])
    if not program:
        return {"program": None, "at": utc()}
    with tempfile.TemporaryDirectory(prefix="set1-mariadb-check-") as private:
        done = run([program, "--defaults-file=" + path, "--datadir=" + private, "--help", "--verbose"], timeout=60)
    table = {}
    for line in done.get("stdout", "").splitlines():
        parts = line.split()
        if len(parts) == 2 and parts[0].replace("-", "_") in variables:
            table[parts[0].replace("-", "_")] = parts[1]
    return {"program": program, "path": path, "returncode": done.get("returncode"), "status": done.get("status"),
            "stderr": done.get("stderr", "")[-3000:], "resulting_variables": table, "at": utc()}


def read_queue(args: dict) -> dict:
    return {"postqueue": run(["postqueue", "-j"]), "units": unit_facts(("postfix.service", "postfix@-.service")),
            "master_pid": _pid_file("/var/spool/postfix/pid/master.pid"), "at": utc()}


def read_catchall(args: dict) -> dict:
    domain = args["domain"]
    if not DOMAIN_RE.fullmatch(domain):
        raise Refused("not a plain domain")
    maps = run(["postconf", "-h", "virtual_alias_maps"])
    lookups = []
    for entry in re.split(r"[,\s]+", maps.get("stdout", "").strip()):
        if not re.fullmatch(r"[a-z]+:/[A-Za-z0-9_./-]+", entry or ""):
            continue
        source = entry.split(":", 1)[1]
        lookups.append({"map": entry, "query": run(["postmap", "-q", "@" + domain, entry]),
                        "source": file_state(source, text=True)})
    return {"domain": domain, "virtual_alias_maps": maps.get("stdout", "").strip(), "lookups": lookups, "at": utc()}


def read_panel_rows(args: dict) -> dict:
    domain_id = int(args["domain_id"])
    connection = sqlite3.connect("file:" + str(PANEL_DB) + "?mode=ro", uri=True, timeout=10)
    try:
        connection.row_factory = sqlite3.Row
        rows = {}
        for table in ("backup_schedules", "mail_catch_all"):
            try:
                found = connection.execute("SELECT * FROM " + table + " WHERE domain_id = ?", (domain_id,)).fetchall()
                rows[table] = [dict(row) for row in found]
            except sqlite3.Error as exc:
                rows[table] = {"error": type(exc).__name__ + ": " + str(exc)[:120]}
    finally:
        connection.close()
    return {"domain_id": domain_id, "rows": rows, "at": utc()}


def read_journal(args: dict) -> dict:
    units = [unit_name(u) for u in args.get("units", [])]
    since = int(args["since_epoch"])
    argv = ["journalctl", "--no-pager", "-o", "short-iso-precise", "--since", "@" + str(since),
            "-n", str(max(1, min(int(args.get("lines", 200)), 2000)))]
    for unit in units:
        argv += ["-u", unit]
    identifiers = [tag for tag in args.get("identifiers", []) if re.fullmatch(r"[A-Za-z0-9_./-]{1,40}", tag)]
    for tag in identifiers:
        argv += ["-t", tag]
    if not units and not identifiers:
        raise Refused("the journal reader needs a unit or an identifier")
    return {"units": units, "identifiers": identifiers, "since_epoch": since, "journal": run(argv), "at": utc()}


def read_clock(args: dict) -> dict:
    return {"epoch": time.time(), "at": utc()}


# -- owner actions ----------------------------------------------------------------

def owner_edit(args: dict) -> dict:
    """An owner's editor saving in place: the same inode, owner, group and mode; only the bytes change."""
    path = editable_path(args["path"])
    content = base64.b64decode(args["content_b64"], validate=True)
    before = file_state(path)
    if not before.get("exists") or before.get("type") != "regular":
        raise Refused("the owner edits only an existing regular file")
    fd = os.open(path, os.O_WRONLY | os.O_TRUNC | os.O_NOFOLLOW)
    with os.fdopen(fd, "wb") as stream:
        stream.write(content)
        stream.flush()
        os.fsync(stream.fileno())
    return {"action": "owner-edit", "path": path, "before": before, "after": file_state(path), "at": utc()}


def owner_crontab(args: dict) -> dict:
    """``crontab -u <user> <file>``, as an owner installs a crontab they edited."""
    user = user_name(args["user"])
    content = base64.b64decode(args["content_b64"], validate=True)
    installed = run(["crontab", "-u", user, "-"], input_bytes=content)
    return {"action": "owner-crontab", "user": user, "install": installed, "at": utc()}


def owner_systemctl(args: dict) -> dict:
    action, unit = args["action"], unit_name(args["unit"])
    if action not in ("reload", "stop", "start", "restart"):
        raise Refused("not an owner service action of this cell")
    return {"action": "owner-systemctl", "unit": unit, "verb": action,
            "result": run(["systemctl", action, unit], timeout=120), "units": unit_facts((unit,)), "at": utc()}


def _record(name: str) -> Path:
    info = PRIVATE_ROOT.lstat()
    if not (stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o700):
        raise Refused("unsafe private lab root")
    return PRIVATE_ROOT / name


def owner_cron_fault(args: dict) -> dict:
    """Two things an owner can do that stop ``crontab -u <user> -l``: restrict cron to root with /etc/cron.allow
    (a common hardening step), or relocate the spool directory to another volume behind a symlink and have that
    volume not mounted (the directory is moved aside and a dangling symlink stands in its place). ``restore``
    undoes exactly what ``apply`` did."""
    record = _record(FAULT_RECORD)
    if args["action"] == "apply":
        kind = args["kind"]
        if record.exists():
            raise Refused("a cron fault is already applied")
        if kind == "cron-allow":
            if CRON_ALLOW.exists() or CRON_ALLOW.is_symlink():
                raise Refused("/etc/cron.allow already exists; it is the owner's")
            CRON_ALLOW.write_text("root\n")
            os.chmod(CRON_ALLOW, 0o640)
            done = {"kind": kind, "created": str(CRON_ALLOW)}
        elif kind == "spool-relocated":
            source = next((d for d in SPOOLS if os.path.isdir(d) and not os.path.islink(d)), None)
            if source is None:
                raise Refused("no cron spool directory")
            target = source + ".set1-moved-aside"
            if os.path.lexists(target) or os.path.lexists(RELOCATED_TARGET):
                raise Refused("the moved-aside name or the unmounted volume path exists")
            os.rename(source, target)
            os.symlink(RELOCATED_TARGET, source)
            done = {"kind": kind, "moved": [source, target], "symlink": [source, RELOCATED_TARGET]}
        else:
            raise Refused("unknown cron fault")
        record.write_text(json.dumps(done))
        return {"action": "owner-cron-fault", "applied": done, "at": utc()}
    done = json.loads(record.read_text())
    if done["kind"] == "cron-allow":
        os.unlink(done["created"])
    else:
        if not os.path.islink(done["symlink"][0]):
            raise Refused("the relocated spool's symlink is no longer a symlink; the owner restores it by hand")
        os.unlink(done["symlink"][0])
        os.rename(done["moved"][1], done["moved"][0])
    record.unlink()
    return {"action": "owner-cron-fault", "restored": done, "cron_allow": file_state(str(CRON_ALLOW)),
            "spool_directories": [file_state(d) for d in SPOOLS], "at": utc()}


def owner_reload_hook(args: dict) -> dict:
    """An owner's own reload hook for the database unit (a systemd drop-in): it signals the server as the packaged
    unit does and then reloads a connection pooler unit that does not exist, so ``systemctl reload`` fails although
    the server itself re-read its files. ``restore`` removes the drop-in and the script."""
    record = _record(HOOK_RECORD)
    unit = unit_name(args["unit"])
    if not unit.startswith("postgresql"):
        raise Refused("the reload hook is for the PostgreSQL unit")
    directory = Path("/etc/systemd/system") / (unit + ".d")
    dropin = directory / "set1-owner-reload-hook.conf"
    if args["action"] == "apply":
        if record.exists() or HOOK_SCRIPT.exists() or dropin.exists():
            raise Refused("a reload hook is already applied")
        made_directory = not directory.exists()
        directory.mkdir(mode=0o755, exist_ok=True)
        HOOK_SCRIPT.write_text("#!/bin/sh\n# owner hook: tell PostgreSQL to re-read its files, then reload the pooler\n"
                               "kill -HUP \"$MAINPID\" || exit 1\nexec /usr/bin/systemctl reload " + HOOK_POOLER + "\n")
        os.chmod(HOOK_SCRIPT, 0o755)
        dropin.write_text("[Service]\nExecReload=\nExecReload=" + str(HOOK_SCRIPT) + "\n")
        record.write_text(json.dumps({"unit": unit, "dropin": str(dropin), "made_directory": made_directory}))
        reloaded = run(["systemctl", "daemon-reload"], timeout=60)
        return {"action": "owner-reload-hook", "applied": {"unit": unit, "dropin": str(dropin), "script": str(HOOK_SCRIPT)},
                "daemon_reload": reloaded, "exec_reload": run(["systemctl", "show", unit, "-p", "ExecReload"]).get("stdout", ""),
                "at": utc()}
    done = json.loads(record.read_text())
    os.unlink(done["dropin"])
    if done.get("made_directory"):
        os.rmdir(os.path.dirname(done["dropin"]))
    HOOK_SCRIPT.unlink()
    record.unlink()
    reloaded = run(["systemctl", "daemon-reload"], timeout=60)
    return {"action": "owner-reload-hook", "restored": done, "daemon_reload": reloaded,
            "exec_reload": run(["systemctl", "show", unit, "-p", "ExecReload"]).get("stdout", ""), "at": utc()}


MODES = {
    "read-file": read_file, "read-crontab": read_crontab, "read-postfix": read_postfix, "read-smtp": read_smtp,
    "read-postgres": read_postgres, "read-mariadb": read_mariadb, "read-mariadb-check": read_mariadb_check,
    "read-queue": read_queue,
    "read-catchall": read_catchall, "read-panel-rows": read_panel_rows, "read-journal": read_journal,
    "read-clock": read_clock,
    "owner-edit": owner_edit, "owner-crontab": owner_crontab, "owner-systemctl": owner_systemctl,
    "owner-cron-fault": owner_cron_fault, "owner-reload-hook": owner_reload_hook,
}


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("mode", choices=sorted(MODES))
    for name in ("lab-nonce", "vm-uuid", "cell-id", "node"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--args-b64", default="e30=")
    args = parser.parse_args(argv)
    _load_probe().guard_guest(args)
    request = json.loads(base64.b64decode(args.args_b64, validate=True))
    if not isinstance(request, dict):
        parser.error("--args-b64 must hold a JSON object")
    value = MODES[args.mode](request)
    value.update(schema=SCHEMA, mode=args.mode, owner_action=args.mode.startswith("owner-"))
    print(json.dumps(value, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Refused as exc:
        print(json.dumps({"refused": "Refused", "reason": str(exc)[:300]}), file=sys.stderr)
        raise SystemExit(3)
    except Exception as exc:  # noqa: BLE001 - reported as data; the driver decides
        print(json.dumps({"refused": type(exc).__name__, "reason": str(exc)[:300]}), file=sys.stderr)
        raise SystemExit(2)
