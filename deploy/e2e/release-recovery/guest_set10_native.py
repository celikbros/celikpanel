#!/usr/bin/env python3
"""set10: native readers and the owner's hand actions for the measurement of D-031 (a site configuration file the
owner changed is kept and named) on a marked disposable QEMU guest.

Built on ``guest_set8_native.py`` (its readers, its owner actions and its lab restore are reused unchanged). Added:

Readers (``read-*``) only inspect: a site's vhost with its first line read as the render header
(``# celikpanel-render v2 sha256=<hex>``: present, declared digest, digest of the body under it, equal or not), the
files the Panel keeps beside it (``<file>.celikpanel-pending``, ``<file>.celikpanel-backup-*``), the owner include
directory ``/etc/nginx/celikpanel-sites.d/<domain>/`` (mode, owner, entries with their bytes), the Panel database's
migration ledger and its ``managed_site_files`` rows (file digests, states, decisions; no secret is stored there),
the installed PHP-FPM versions.

Owner actions (``owner-*``) are what a server owner does by hand: edit, replace, remove, lock or replace by a symlink a
site's vhost; change its owner, group and mode; put a ``.conf`` file of their own into the site's include directory
(and take back only such a file this harness wrote); back up the Panel's database while it runs and put that backup
back while the Panel is stopped. ``lab-*`` is lab preparation, recorded as such: set8's restore of the Panel's text,
and ``lab-stale-managed``, which gives a site's vhost a valid header over a body that differs from the Panel's text
(as an older CelikPanel text would be), so that the next start has to write it.

No mode prints a password, a private key or a hash of one, and no mode contacts another host.

set10: D-031 ölçümü için okuyucular ve sahibin elle yaptığı işlemler. Hiçbir kip parola, özel anahtar ya da parola
özeti yazdırmaz; başka bir sunucuya bağlanmaz. Dosya özetleri (sha256) gizli değildir ve kaydedilir.
"""
from __future__ import annotations

import argparse
import base64
import glob
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import sqlite3
import sys
import time

HERE = Path(__file__).resolve().parent
SCHEMA = "celikpanel/set10-native/v1"


def _load(name: str):
    spec = importlib.util.spec_from_file_location("set10_" + name.replace(".", "_"), HERE / name)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


g8 = _load("guest_set8_native.py")
s4, rid = g8.s4, g8.rid
run, utc, Refused = rid.run, rid.utc, rid.Refused
# The start line of the D-031 release (cmd/panel/cert_startup_reconcile.go logHostedVhostStartup) besides set8's two
# lines of the earlier releases: owner-panel waits for one of them after a start or restart.
g8.RECONCILE_LINES = g8.RECONCILE_LINES + ("site configuration files at start:",)

NGINX = g8.NGINX
AVAILABLE, ENABLED, OWNER_DIR = g8.AVAILABLE, g8.ENABLED, g8.OWNER_DIR
INCLUDE_BASE = NGINX + "/celikpanel-sites.d"
HEADER_PREFIX = b"# celikpanel-render v2 sha256="
PENDING_SUFFIX = ".celikpanel-pending"
BACKUP_MARKER = ".celikpanel-backup-"
PANEL_DB = "/var/lib/celikpanel/celikpanel.db"
DB_BACKUP = "/root/set10-panel-db-before-the-update.db"
OWNER_FILE_RE = re.compile(r"set10-[a-z0-9-]{1,40}\.conf\Z")
STALE_LINE = b"# set10 lab preparation: an older CelikPanel text of this site (a line the current render does not have)\n"


# -- pure rules (covered offline by test_set10_trial) ---------------------------------------------------------------

def header_of(data: bytes | None) -> dict:
    """The render header as cmd/panel's classifier reads it: the first line (a trailing CR tolerated) must be the
    prefix and 64 lowercase hex; the body is every byte after that line."""
    if data is None:
        return {"present": False}
    newline = data.find(b"\n")
    if newline < 0:
        return {"present": False}
    line = data[:newline]
    if line.endswith(b"\r"):
        line = line[:-1]
    if not line.startswith(HEADER_PREFIX):
        return {"present": False, "first_line": line[:120].decode("utf-8", "replace")}
    declared = line[len(HEADER_PREFIX):].decode("ascii", "replace")
    if len(declared) != 64 or declared.strip("0123456789abcdef"):
        return {"present": False, "first_line": line[:120].decode("utf-8", "replace"), "malformed": True}
    body = data[newline + 1:]
    actual = hashlib.sha256(body).hexdigest()
    return {"present": True, "declared": declared, "body_sha256": actual, "valid": declared == actual,
            "body_bytes": len(body)}


def seal(body: bytes) -> bytes:
    return HEADER_PREFIX + hashlib.sha256(body).hexdigest().encode() + b"\n" + body


def stale_body(data: bytes) -> bytes:
    """The body of a header-valid file with one line added after the header's comment block: still CelikPanel's
    format (a valid header), not CelikPanel's current text."""
    head = header_of(data)
    if not head.get("present") or not head.get("valid"):
        raise Refused("the file does not carry a valid render header")
    body = data[data.find(b"\n") + 1:]
    return STALE_LINE + body


def owner_include_text(kind: str, domain: str) -> str:
    if kind == "location":
        return ("# set10: the server owner's own addition for %s, in the site's include directory\n"
                "location /owner-dir/ {\n    return 200 \"owner-dir\";\n}\n") % domain
    if kind == "duplicate-root":
        return ("# set10: an owner's file that conflicts with CelikPanel's text (a second root in the same server block)\n"
                "root /srv/set10-owner-root;\n")
    raise Refused("unknown include kind")


# -- readers ------------------------------------------------------------------------------------------------------

def raw(path: str) -> bytes | None:
    try:
        if os.path.islink(path) or not os.path.isfile(path):
            return None
        return Path(path).read_bytes()
    except OSError:
        return None


def record(path: str, text: bool = True) -> dict:
    value = g8.file_record(path, text=text)
    data = raw(path)
    if data is not None:
        value["header"] = header_of(data)
        value["first_line"] = data.split(b"\n", 1)[0][:160].decode("utf-8", "replace")
    return value


def side_files(path: str) -> dict:
    return {"pending": record(path + PENDING_SUFFIX),
            "backups": [record(p) for p in sorted(glob.glob(glob.escape(path) + BACKUP_MARKER + "*"))]}


def include_dir(domain: str) -> dict:
    directory = f"{INCLUDE_BASE}/{domain}"
    value = dict(rid._stat(directory))
    try:
        info = os.lstat(directory)
        value.update(inode=info.st_ino, uid=info.st_uid, gid=info.st_gid, mode_octal=oct(info.st_mode & 0o7777),
                     mtime_ns=info.st_mtime_ns)
    except OSError:
        return value
    if os.path.isdir(directory) and not os.path.islink(directory):
        value["entries"] = [record(os.path.join(directory, n)) for n in sorted(os.listdir(directory))]
    return value


def ledger() -> dict:
    """The Panel database read-only: the migration ledger's last version and every managed_site_files row."""
    value = {"database": PANEL_DB}
    try:
        connection = sqlite3.connect("file:" + PANEL_DB + "?mode=ro", uri=True, timeout=20, isolation_level=None)
    except sqlite3.Error as exc:
        return dict(value, error=type(exc).__name__ + ": " + str(exc)[:200])
    try:
        connection.execute("PRAGMA query_only=ON")
        connection.execute("BEGIN")
        versions = [r[0] for r in connection.execute("SELECT version FROM schema_migrations ORDER BY version")]
        value.update(schema_version=max(versions) if versions else None, ledger_rows=len(versions),
                     ledger_last=versions[-3:])
        tables = {r[0] for r in connection.execute("SELECT name FROM sqlite_schema WHERE type='table'")}
        value["managed_site_files_table"] = "managed_site_files" in tables
        if value["managed_site_files_table"]:
            cursor = connection.execute("SELECT * FROM managed_site_files ORDER BY id")
            names = [d[0] for d in cursor.description]
            value["managed_site_files"] = [dict(zip(names, row)) for row in cursor.fetchall()]
        value["domains"] = [{"id": r[0], "name": r[1]} for r in connection.execute("SELECT id, name FROM domains ORDER BY id")]
        value["sites"] = [{"id": r[0], "domain_id": r[1], "project_type": r[2]} for r in
                          connection.execute("SELECT id, domain_id, project_type FROM sites ORDER BY id")]
        connection.execute("ROLLBACK")
    except sqlite3.Error as exc:
        value["error"] = type(exc).__name__ + ": " + str(exc)[:200]
    finally:
        connection.close()
    return value


def php_versions() -> list:
    found = set()
    for pattern in ("/usr/sbin/php-fpm*", "/usr/bin/php-fpm*"):
        for path in glob.glob(pattern):
            found.add(os.path.basename(path))
    return sorted(found)


def read_site10(args: dict) -> dict:
    domains = [rid.domain_name(d) for d in args.get("domains", [])]
    value = g8.read_site({"domains": domains, "dump": bool(args.get("dump")), "pool": False})
    for domain in domains:
        path = f"{AVAILABLE}/{domain}.conf"
        site = value["sites"][domain]
        site["vhost"] = record(path)
        site["side_files"] = side_files(path)
        site["include_dir"] = include_dir(domain)
        if os.path.islink(path):
            target = os.path.realpath(path)
            site["symlink_target"] = record(target)
    value["include_base"] = dict(rid._stat(INCLUDE_BASE))
    value["ledger"] = ledger()
    value["php_fpm_programs"] = php_versions()
    return value


def read_ledger(args: dict) -> dict:
    return {"at": utc(), "epoch": time.time(), "ledger": ledger()}


# -- owner actions ------------------------------------------------------------------------------------------------

def owner_vhost10(args: dict) -> dict:
    domain = rid.domain_name(args["domain"])
    action = args["action"]
    path, link = f"{AVAILABLE}/{domain}.conf", f"{ENABLED}/{domain}.conf"
    value = {"action": "owner-vhost10", "what": action, "domain": domain,
             "before": {"vhost": record(path), "enabled": g8.file_record(link, text=False)}}
    if action in ("append-location", "change-index", "replace", "add-include", "append-comment"):
        text = Path(path).read_text()
        if action == "append-location":
            added = ["", "    # set10: added by the server owner by hand", "    location /owner-extra/ {",
                     "        return 200 \"owner\";", "    }"]
            new = g8.insert_before_block_end(text, domain, added)
            value["added_lines"] = added
        elif action == "change-index":
            new, old_line, new_line = g8.change_index(text)
            value["changed"] = {"from": old_line, "to": new_line}
        elif action == "replace":
            new = ("# set10: the server owner's own vhost for %s, written by hand\n"
                   "server {\n    listen 80;\n    listen [::]:80;\n    server_name %s;\n"
                   "    location / {\n        return 200 \"owner-replaced\\n\";\n    }\n}\n") % (domain, domain)
        elif action == "append-comment":
            new = text.rstrip("\n") + "\n# set10: the server owner edited this file again (" + utc() + ")\n"
        else:
            os.makedirs(OWNER_DIR, exist_ok=True)
            include = f"{OWNER_DIR}/{domain}.conf"
            with open(include, "w") as stream:
                stream.write("# set10: the server owner's own additions for %s\nlocation /owner-include/ {\n"
                             "    return 200 \"owner-include\";\n}\n" % domain)
            os.chmod(include, 0o644)
            added = ["", "    # set10: the server owner's own include", f"    include {include};"]
            new = g8.insert_before_block_end(text, domain, added)
            value["added_lines"] = added
            value["include_file"] = record(include)
        g8.write_in_place(path, new.encode())
    elif action == "remove":
        for target in (link, path):
            if os.path.lexists(target):
                os.unlink(target)
    elif action in ("chattr-plus-i", "chattr-minus-i"):
        value["chattr"] = run(["chattr", "+i" if action == "chattr-plus-i" else "-i", path], timeout=20)
    elif action == "chown":
        os.chown(path, int(args["uid"]), int(args["gid"]))
        os.chmod(path, int(str(args["file_mode"]), 8))
    elif action == "symlink":
        # The owner keeps their own file elsewhere and points the vhost path at it with a symlink.
        os.makedirs(OWNER_DIR, exist_ok=True)
        target = f"{OWNER_DIR}/{domain}-own-vhost.conf"
        text = Path(path).read_text()
        added = ["", "    # set10: the owner's own file, reached through a symlink", "    location /owner-link/ {",
                 "        return 200 \"owner-link\";", "    }"]
        with open(target, "w") as stream:
            stream.write(g8.insert_before_block_end(text, domain, added))
        os.chmod(target, 0o644)
        os.unlink(path)
        os.symlink(target, path)
        value["symlink_target"] = record(target)
    else:
        raise Refused("unknown owner action")
    value["after"] = {"vhost": record(path), "enabled": g8.file_record(link, text=False)}
    value.update(g8.nginx_reload(bool(args.get("reload"))))
    value["at"] = utc()
    value["epoch"] = time.time()
    return value


def owner_include(args: dict) -> dict:
    """The owner puts a .conf file of their own into the site's include directory, or takes back one this harness
    wrote there (only a name set10-*.conf)."""
    domain = rid.domain_name(args["domain"])
    name = args["name"]
    if not OWNER_FILE_RE.fullmatch(name):
        raise Refused("not a file name this harness writes")
    directory = f"{INCLUDE_BASE}/{domain}"
    path = os.path.join(directory, name)
    value = {"action": "owner-include", "domain": domain, "path": path, "directory_before": include_dir(domain)}
    if args.get("remove"):
        if os.path.isfile(path) and not os.path.islink(path):
            os.unlink(path)
            value["removed"] = True
    else:
        if not os.path.isdir(directory):
            raise Refused("the site's include directory is not there")
        with open(path, "w") as stream:
            stream.write(owner_include_text(args["kind"], domain))
        os.chmod(path, 0o644)
        value["file"] = record(path)
    value["directory_after"] = include_dir(domain)
    value.update(g8.nginx_reload(bool(args.get("reload"))))
    value["at"] = utc()
    return value


def lab_stale_managed(args: dict) -> dict:
    """Lab preparation (not an owner action): each named site's vhost gets a valid render header over its body with
    one line added, written through a new file renamed into place with the file's owner, group and mode."""
    out = {"action": "lab-stale-managed", "sites": {}}
    for domain in [rid.domain_name(d) for d in args["domains"]]:
        path = f"{AVAILABLE}/{domain}.conf"
        data = Path(path).read_bytes()
        info = os.lstat(path)
        new = seal(stale_body(data))
        temporary = path + ".set10-stale"
        with open(temporary, "wb") as stream:
            stream.write(new)
            stream.flush()
            os.fsync(stream.fileno())
        os.chown(temporary, info.st_uid, info.st_gid)
        os.chmod(temporary, info.st_mode & 0o7777)
        os.rename(temporary, path)
        out["sites"][domain] = {"before": {"sha256": hashlib.sha256(data).hexdigest(), "inode": info.st_ino},
                                "after": record(path, text=False)}
    out.update(g8.nginx_reload(True))
    out["at"] = utc()
    return out


def panel_active() -> str:
    return (run(["systemctl", "is-active", g8.PANEL_UNIT], timeout=20).get("stdout") or "").strip()


def owner_db_backup(args: dict) -> dict:
    """The owner backs up the Panel's database while the Panel runs (SQLite's online backup), to a file of their
    own under /root."""
    source = sqlite3.connect("file:" + PANEL_DB + "?mode=ro", uri=True, timeout=30)
    target = sqlite3.connect(DB_BACKUP)
    try:
        source.backup(target)
    finally:
        target.close()
        source.close()
    os.chmod(DB_BACKUP, 0o600)
    check = sqlite3.connect("file:" + DB_BACKUP + "?mode=ro", uri=True)
    try:
        versions = [r[0] for r in check.execute("SELECT version FROM schema_migrations ORDER BY version")]
        tables = {r[0] for r in check.execute("SELECT name FROM sqlite_schema WHERE type='table'")}
    finally:
        check.close()
    return {"action": "owner-db-backup", "path": DB_BACKUP, "panel": panel_active(),
            "backup": dict(rid._stat(DB_BACKUP), size=os.path.getsize(DB_BACKUP)),
            "backup_schema_version": max(versions) if versions else None,
            "backup_has_managed_site_files": "managed_site_files" in tables, "at": utc()}


def owner_db_restore(args: dict) -> dict:
    """With the Panel stopped, the owner puts the backup back over the Panel's database (owner, group and mode of
    the database file kept; the write-ahead files of the replaced database removed, as SQLite requires)."""
    state = panel_active()
    if state == "active":
        raise Refused("the Panel is running; the owner stops it before putting a database back")
    if not os.path.isfile(DB_BACKUP):
        raise Refused("there is no backup")
    info = os.lstat(PANEL_DB)
    value = {"action": "owner-db-restore", "panel": state, "database_before": dict(rid._stat(PANEL_DB)),
             "ledger_before": ledger(),
             "sidecars_before": {s: rid._stat(PANEL_DB + s) for s in ("-wal", "-shm")}}
    temporary = PANEL_DB + ".set10-restore"
    with open(DB_BACKUP, "rb") as source, open(temporary, "wb") as stream:
        stream.write(source.read())
        stream.flush()
        os.fsync(stream.fileno())
    os.chown(temporary, info.st_uid, info.st_gid)
    os.chmod(temporary, info.st_mode & 0o7777)
    for suffix in ("-wal", "-shm"):
        if os.path.isfile(PANEL_DB + suffix):
            os.unlink(PANEL_DB + suffix)
    os.rename(temporary, PANEL_DB)
    value.update(database_after=dict(rid._stat(PANEL_DB)), ledger_after=ledger(), at=utc())
    return value


MODES = dict(g8.MODES)
MODES.update({"read-site10": read_site10, "read-ledger": read_ledger, "owner-vhost10": owner_vhost10,
              "owner-include": owner_include, "lab-stale-managed": lab_stale_managed,
              "owner-db-backup": owner_db_backup, "owner-db-restore": owner_db_restore})


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("mode", choices=sorted(MODES))
    for name in ("lab-nonce", "vm-uuid", "cell-id", "node"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--args-b64", default="e30=")
    args = parser.parse_args(argv)
    rid._load_probe().guard_guest(args)
    request = json.loads(base64.b64decode(args.args_b64, validate=True))
    if not isinstance(request, dict):
        parser.error("--args-b64 must hold a JSON object")
    value = MODES[args.mode](request)
    value.update(schema=SCHEMA, mode=args.mode, owner_action=args.mode.startswith("owner-"))
    print(json.dumps(value, sort_keys=True, default=str))
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
