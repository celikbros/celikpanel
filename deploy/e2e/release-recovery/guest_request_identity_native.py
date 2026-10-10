#!/usr/bin/env python3
"""set2: native state readers and the lab owner's actions for the ``request-identity`` cell, inside a marked
disposable QEMU guest.

Readers (``read-*``) only inspect: the domain's backup directory with each archive's manifest, the document root as
a checksum, the database engines through their own local clients, WireGuard's peer list (public keys only), the
Panel's database opened read-only (``request_identities`` without its stored bodies: length, digest and whether a
given secret occurs in them), the journal of the Panel and the Agent. Owner actions (``owner-*``) are what a server
owner does by hand: put files into the site, change them, change rows of the site's database, place a cPanel
archive under the import directory, restart the Panel's service. ``lab-*`` modes are lab isolation only.

No mode prints a password, a private key or a client configuration, and no mode contacts another host.

set2: ``request-identity`` hücresi için işaretli geçici QEMU konuğunda yerel durum okuyucuları ve sunucu sahibinin
elle yaptığı işlemler. Hiçbir kip parola, özel anahtar ya da istemci yapılandırması yazdırmaz.
"""
from __future__ import annotations

import argparse
import base64
import gzip
import hashlib
import imaplib
import importlib.util
import io
import json
import os
from pathlib import Path
import pwd
import re
import secrets as secrets_module
import shutil
import socket
import sqlite3
import ssl
import stat
import subprocess
import sys
import tarfile
import time

HERE = Path(__file__).resolve().parent
PRIVATE_ROOT = Path("/root/celikpanel-release-recovery-lab")
PANEL_DB = Path("/var/lib/celikpanel/celikpanel.db")
BACKUP_BASE = Path("/var/backups/celikpanel")
IMPORT_ROOT = Path("/var/lib/celikpanel-imports")
SITES_ROOT = "/var/www/celikpanel/subscriptions"
ENV = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL": "C", "LANGUAGE": "C"}
SCHEMA = "celikpanel/set2-request-identity-native/v1"
USER_RE = re.compile(r"[a-z_][a-z0-9_-]{0,31}\Z")
NAME_RE = re.compile(r"[A-Za-z_][A-Za-z0-9_]{0,62}\Z")
DOMAIN_RE = re.compile(r"(?=.{1,253}\Z)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}\Z")
JOURNAL_UNITS = ("celikpanel-panel.service", "celikpanel-agent.service", "nginx.service", "mariadb.service",
                 "wg-quick@wg0.service", "dovecot.service", "php-fpm.service")
ACME_HOSTS = ("acme-v02.api.letsencrypt.org", "acme-staging-v02.api.letsencrypt.org")
ACME_MARK = "# set2 lab isolation: no ACME directory is reachable from this guest"
HOSTS = Path("/etc/hosts")
SEED_TABLE = "set2_rows"
# set3: the same shapes owner_update_trial.HASH_SHAPED redacts at collection time (pinned equal by the offline test).
HASH_SHAPED = (
    r"(?:\{[A-Z][A-Z0-9.-]{1,24}\})?\$(?:1|2[abxy]?|5|6|7|y|gy|sha1|argon2(?:id|i|d)|scrypt|pbkdf2(?:-sha(?:1|256|512))?)"
    r"\$[./A-Za-z0-9$=,+-]{8,}"
    r"|\{(?:SSHA(?:256|512)?|SHA(?:256|512)?|SMD5|PLAIN|CRYPT|CRAM-MD5|[A-Z0-9]+-CRYPT|ARGON2ID?|PBKDF2)\}[^\s\"'<>\\]{4,}"
    r"|SCRAM-SHA-256\$\d+:[A-Za-z0-9+/=]+\$[A-Za-z0-9+/=]+:[A-Za-z0-9+/=]+"
    r"|(?<![0-9A-Za-z])\*[0-9A-F]{40}(?![0-9A-Fa-f])")
HASH_SHAPED_BYTES = re.compile(HASH_SHAPED.encode())
ESCAPE_PREFIX = "set3-escape"
ESCAPE_ROOTS = ("/var/www", "/var/lib", "/var/tmp", "/var/backups", "/etc", "/tmp", "/home", "/root", "/srv", "/opt", "/usr/local")
HOSTILE_KINDS = ("dotdot", "absolute", "symlink")
PHP_PROBE = "set3-probe.php"
PHP_PROBE_MARK = "set3-php-executed"
ADDRESS_RE = re.compile(r"[a-z0-9][a-z0-9._-]{0,63}@(?=.{1,253}\Z)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}\Z")


class Refused(ValueError):
    """The request is outside what this helper does."""


def _load_probe():
    spec = importlib.util.spec_from_file_location("set2_native_probe", HERE / "guest_probe.py")
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


def utc() -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


def run(argv, timeout=60, input_bytes=None, limit=1 << 20, env=None) -> dict:
    started = time.time()
    try:
        done = subprocess.run(argv, input=input_bytes, capture_output=True, timeout=timeout, env=dict(ENV, **(env or {})))
    except FileNotFoundError:
        return {"argv": argv, "status": "unavailable"}
    except subprocess.TimeoutExpired:
        return {"argv": argv, "status": "timeout"}
    return {"argv": argv, "status": "ok", "returncode": done.returncode, "at": utc(),
            "seconds": round(time.time() - started, 3),
            "stdout": done.stdout[:limit].decode("utf-8", "replace"),
            "stderr": done.stderr[:8192].decode("utf-8", "replace")}


# -- pure rules (covered offline) -----------------------------------------------------

def user_name(user) -> str:
    if not isinstance(user, str) or not USER_RE.fullmatch(user) or user == "root":
        raise Refused("not a site account name")
    return user


def sql_name(name) -> str:
    if not isinstance(name, str) or not NAME_RE.fullmatch(name):
        raise Refused("not a plain SQL identifier")
    return name


def domain_name(name) -> str:
    if not isinstance(name, str) or not DOMAIN_RE.fullmatch(name):
        raise Refused("not a plain domain")
    return name


def site_username(domain: str) -> str:
    """internal/services.SiteUsername: '.' and '-' become '_', cut at 32."""
    return domain.replace(".", "_").replace("-", "_")[:32]


def backup_directory(subscription_id: int, domain_id: int) -> Path:
    if not (isinstance(subscription_id, int) and isinstance(domain_id, int) and subscription_id > 0 and domain_id > 0):
        raise Refused("not a subscription and domain identity")
    return BACKUP_BASE / "subscriptions" / str(subscription_id) / "domains" / str(domain_id)


def document_root(subscription_id: int, domain_id: int) -> str:
    if not (isinstance(subscription_id, int) and isinstance(domain_id, int) and subscription_id > 0 and domain_id > 0):
        raise Refused("not a subscription and domain identity")
    return f"{SITES_ROOT}/{subscription_id}/sites/{domain_id}/public_html"


def secret_occurrences(body: bytes, needles: list) -> int:
    """How many of the given secrets occur in a stored body (the count only; never the text)."""
    return sum(1 for needle in needles if needle and needle in body)


def body_shape(body: bytes | None) -> dict:
    """What a stored answer is, without its text: length, digest, and for a JSON object its key names."""
    if body is None:
        return {"stored": False}
    shape = {"stored": True, "bytes": len(body), "sha256": hashlib.sha256(body).hexdigest()}
    try:
        value = json.loads(body)
    except ValueError:
        return shape
    if isinstance(value, dict):
        shape["json_keys"] = sorted(value)
        shape["code"] = value.get("code") if isinstance(value.get("code"), str) else None
    return shape


PUBLIC_HTML_MEMBER = "homedir/public_html"


def cpmove_members(domain: str, user: str, database: str, megabytes: int, sleep_seconds: int, rows: int,
                   random_bytes=os.urandom, shadow_hash: bytes | None = None) -> list:
    """The members of a minimal cPanel account archive that ``cmd/agent/cpmove_rpc.go`` reads: the account file
    (``cp/<user>``, its ``DNS=`` line names the domain), the site (``homedir/public_html``), one mailbox
    (``homedir/etc/<domain>/shadow`` and ``quota``), one forwarder (``va/<domain>``) and one database
    (``mysql/<name>.create`` and ``.sql``). Built from the parser's own rules; it is not a real cPanel backup.
    ``sleep_seconds`` puts ``DO SLEEP(n)`` at the end of the dump, which makes the import last long enough to
    interrupt; it changes no data."""
    top = "cpmove-" + user
    dump = [f"CREATE TABLE {SEED_TABLE} (id INT PRIMARY KEY, note VARCHAR(64));"]
    for start in range(1, rows + 1, 500):
        values = ",".join(f"({i},'imported row {i}')" for i in range(start, min(start + 500, rows + 1)))
        dump.append(f"INSERT INTO {SEED_TABLE} (id, note) VALUES {values};")
    if sleep_seconds:
        dump.append(f"DO SLEEP({int(sleep_seconds)});")
    members = [
        (f"{top}/cp/{user}", f"DNS={domain}\nUSER={user}\nPLAN=set2-fixture\n".encode()),
        (f"{top}/homedir/public_html/index.html", f"<h1>{domain}</h1>\n<p>imported by the set2 fixture</p>\n".encode()),
        (f"{top}/homedir/public_html/assets/site.css", b"body { font-family: sans-serif; }\n"),
        # set3: with ``shadow_hash`` the mailbox carries the crypt hash of a password the lab knows, so that the
        # imported mailbox can be asked to authenticate with it; without it the fabricated value of set2.
        (f"{top}/homedir/etc/{domain}/shadow",
         b"info:" + (shadow_hash or b"$6$set2fixture$" + hashlib.sha512((domain + user).encode()).hexdigest()[:86].encode())
         + b":19000::::::\n"),
        (f"{top}/homedir/etc/{domain}/quota", b"info:268435456\n"),
        (f"{top}/va/{domain}", f"sales@{domain}: info@{domain}\n".encode()),
        (f"{top}/mysql/{database}.create", f"CREATE DATABASE `{database}`;\n".encode()),
        (f"{top}/mysql/{database}.sql", ("\n".join(dump) + "\n").encode()),
    ]
    if megabytes:
        members.append((f"{top}/homedir/public_html/assets/blob.bin", random_bytes(megabytes * 1024 * 1024)))
    return members


def loopback_only(getent_answer: str) -> bool:
    """A ``getent hosts`` answer whose every address is this host's own loopback (and that is not empty)."""
    addresses = [line.split()[0] for line in getent_answer.splitlines() if line.split()]
    return bool(addresses) and all(address in ("127.0.0.1", "::1") for address in addresses)


def hostile_members(top: str, kind: str) -> list:
    """set3: members an import must never extract: a ``..`` path out of the document root, an absolute path, a
    symbolic link out of the document root with a file named through it. (name, data or link target, tar type)."""
    payload = f"{top}/homedir/public_html"
    note = ("hostile member of the set3 fixture: " + kind + "\n").encode()
    if kind == "dotdot":
        return [(f"{payload}/../../{ESCAPE_PREFIX}-dotdot.txt", note, tarfile.REGTYPE)]
    if kind == "absolute":
        return [(f"/etc/{ESCAPE_PREFIX}-absolute.txt", note, tarfile.REGTYPE)]
    if kind == "symlink":
        return [(f"{payload}/{ESCAPE_PREFIX}-link", "/etc", tarfile.SYMTYPE),
                (f"{payload}/{ESCAPE_PREFIX}-link/{ESCAPE_PREFIX}-symlink.txt", note, tarfile.REGTYPE)]
    raise Refused("unknown hostile member kind")


def payload_files(members: list) -> dict:
    """What the document root must hold after a complete import: {relative path: [bytes, sha256]}."""
    expected = {}
    for name, data in members:
        relative = name.split("/", 1)[-1]
        if relative.startswith(PUBLIC_HTML_MEMBER + "/"):
            expected[relative[len(PUBLIC_HTML_MEMBER) + 1:]] = [len(data), hashlib.sha256(data).hexdigest()]
    return expected


def cpmove_archive(members: list, public_html_directory_member: bool = True, hostile: str | None = None) -> bytes:
    """A gzip tar of the members with a directory member for every parent, as tar writes an archive of a directory
    (a directory member's name ends with "/"). With ``public_html_directory_member`` false the one directory member
    ``<top>/homedir/public_html/`` is left out (set2 H34: the import's files step refuses an archive that holds it)."""
    buffer = io.BytesIO()
    with gzip.GzipFile(fileobj=buffer, mode="wb", compresslevel=1, mtime=0) as zipped:
        with tarfile.open(fileobj=zipped, mode="w") as archive:
            seen = set()
            for name, data in members:
                parts = name.split("/")
                for depth in range(1, len(parts)):
                    directory = "/".join(parts[:depth])
                    if directory not in seen:
                        seen.add(directory)
                        if not public_html_directory_member and directory.split("/", 1)[-1] == PUBLIC_HTML_MEMBER:
                            continue
                        info = tarfile.TarInfo(directory)
                        info.type, info.mode, info.mtime = tarfile.DIRTYPE, 0o755, 1760000000
                        archive.addfile(info)
                info = tarfile.TarInfo(name)
                info.size, info.mode, info.mtime = len(data), 0o644, 1760000000
                archive.addfile(info, io.BytesIO(data))
            if hostile:
                top = members[0][0].split("/", 1)[0]
                for name, data, kind in hostile_members(top, hostile):
                    info = tarfile.TarInfo(name)
                    info.type, info.mode, info.mtime = kind, 0o644, 1760000000
                    if kind == tarfile.SYMTYPE:
                        info.linkname = data
                        archive.addfile(info)
                    else:
                        info.size = len(data)
                        archive.addfile(info, io.BytesIO(data))
    return buffer.getvalue()


# -- the Panel's database, read-only ------------------------------------------------------

def panel_db():
    connection = sqlite3.connect("file:" + str(PANEL_DB) + "?mode=ro", uri=True, timeout=15)
    connection.row_factory = sqlite3.Row
    return connection


def query(connection, statement: str, arguments: tuple = ()) -> list:
    try:
        return [dict(row) for row in connection.execute(statement, arguments).fetchall()]
    except sqlite3.Error as exc:
        return [{"error": type(exc).__name__ + ": " + str(exc)[:160]}]


def domain_identity(domain_id: int) -> dict:
    connection = panel_db()
    try:
        rows = query(connection, "SELECT id, name, subscription_id, status FROM domains WHERE id = ?", (int(domain_id),))
    finally:
        connection.close()
    if not rows or "error" in rows[0]:
        raise Refused("the Panel's database has no such domain")
    return rows[0]


def read_panel_state(args: dict) -> dict:
    """Rows the eight routes write, without any secret column."""
    connection = panel_db()
    try:
        state = {
            "domains": query(connection, "SELECT id, name, subscription_id, status FROM domains ORDER BY id"),
            "databases_v2": query(connection, "SELECT id, server_id, subscription_id, domain_id, name FROM databases_v2 ORDER BY id"),
            "database_users": query(connection, "SELECT id, server_id, subscription_id, username, length(password) AS sealed_bytes "
                                                "FROM database_users ORDER BY id"),
            "database_user_grants": query(connection, "SELECT id, database_id, user_id, privileges FROM database_user_grants ORDER BY id"),
            "database_servers": query(connection, "SELECT ds.id, dst.name AS type, ds.host, ds.port, ds.status, ds.admin_username, "
                                                  "length(ds.root_password_encrypted) AS sealed_bytes FROM database_servers ds "
                                                  "JOIN database_server_types dst ON dst.id = ds.type_id ORDER BY ds.id"),
            "vpn_peers": query(connection, "SELECT id, subscription_id, name, public_key, ip, desired_state, sync_state, "
                                           "provisioning_state FROM vpn_peers ORDER BY id"),
            "email_accounts": query(connection, "SELECT id, domain_id, address, quota_mb FROM email_accounts ORDER BY id"),
            "email_forwardings": query(connection, "SELECT id, domain_id, source, destination FROM email_forwardings ORDER BY id"),
            "ssl_certificates": query(connection, "SELECT id, domain_id, type, status FROM ssl_certificates ORDER BY id"),
            "subscription_entitlements": query(connection, "SELECT subscription_id, product_id, status FROM subscription_entitlements"),
        }
        since = args.get("audit_since_id")
        state["audit_logs"] = query(connection, "SELECT id, action, resource_type, resource_id, created_at FROM audit_logs "
                                                "WHERE id > ? ORDER BY id LIMIT 400", (int(since or 0),))
        state["audit_max_id"] = (query(connection, "SELECT coalesce(max(id), 0) AS id FROM audit_logs") or [{}])[0].get("id")
        counts = query(connection, "SELECT action, COUNT(*) AS n FROM audit_logs GROUP BY action ORDER BY action")
        state["audit_counts"] = {row["action"][:120]: row["n"] for row in counts if "action" in row}
    finally:
        connection.close()
    return {"state": state, "at": utc()}


def read_identities(args: dict) -> dict:
    """``request_identities`` without the stored bodies: per row its fields, the stored answer's length, digest and
    JSON key names, and how many of the given secrets occur in it. ``needles_b64`` are the secrets this run minted
    or was shown (VPN keys, passwords); they are compared and never printed."""
    needles = [base64.b64decode(item, validate=True) for item in args.get("needles_b64", [])]
    generic = (b"PrivateKey", b"PresharedKey", b"[Interface]", b"client_config", b"delivery_token", b"\"password\"",
               b"new_password", b"BEGIN ")
    only = args.get("ids")
    connection = panel_db()
    rows = []
    try:
        found = connection.execute(
            "SELECT id, actor_user_id, method, route, request_sha256, status, response_status, response_retained, "
            "response_content_type, response_body, created_at, finished_at, expires_at FROM request_identities "
            "ORDER BY created_at, id").fetchall()
        for row in found:
            if only is not None and row["id"] not in only:
                continue
            body = row["response_body"]
            if isinstance(body, str):
                body = body.encode()
            shape = body_shape(body)
            shape["sensitive_values_of_this_run_found"] = secret_occurrences(body or b"", needles)
            shape["sensitive_words_found"] = sorted(word.decode() for word in generic if body and word in body)
            # set3 (S1): hash-shaped values in the stored answer and in the row's other text columns (counts only).
            shape["hash_shaped_values_found"] = len(HASH_SHAPED_BYTES.findall(body or b""))
            shape["hash_shaped_values_in_other_columns"] = sum(
                len(HASH_SHAPED_BYTES.findall(str(row[column]).encode())) for column in ("id", "method", "route", "request_sha256",
                                                                                      "status", "response_content_type"))
            rows.append({"id": row["id"], "actor_user_id": row["actor_user_id"], "method": row["method"], "route": row["route"],
                         "request_sha256": row["request_sha256"], "status": row["status"],
                         "response_status": row["response_status"], "response_retained": row["response_retained"],
                         "response_content_type": row["response_content_type"], "body": shape,
                         "created_at": row["created_at"], "finished_at": row["finished_at"], "expires_at": row["expires_at"],
                         "lifetime_seconds": row["expires_at"] - row["created_at"]})
        columns = [r[1] for r in connection.execute("PRAGMA table_info(request_identities)").fetchall()]
    except sqlite3.Error as exc:
        return {"error": type(exc).__name__ + ": " + str(exc)[:200], "at": utc()}
    finally:
        connection.close()
    states: dict = {}
    for row in rows:
        states[row["status"]] = states.get(row["status"], 0) + 1
    return {"rows": rows, "count": len(rows), "states": states, "columns": columns, "needles_compared": len(needles),
            "rows_holding_a_sensitive_value_of_this_run": [r["id"] for r in rows if r["body"]["sensitive_values_of_this_run_found"]],
            "rows_holding_a_hash_shaped_value": [r["id"] for r in rows if r["body"]["hash_shaped_values_found"]
                                                 or r["body"]["hash_shaped_values_in_other_columns"]],
            "now_epoch": int(time.time()), "at": utc()}


# -- backups, document root, engines, WireGuard --------------------------------------------

def _manifest(path: Path) -> dict:
    try:
        with tarfile.open(path, mode="r:gz") as archive:
            member = archive.next()
            if member is None or member.name != "manifest.json" or member.size > (1 << 20):
                return {"error": "the first member is not manifest.json"}
            value = json.loads(archive.extractfile(member).read())
    except (OSError, tarfile.TarError, ValueError, EOFError) as exc:
        return {"error": type(exc).__name__}
    kept = {key: value.get(key) for key in ("type", "origin", "job_key", "subscription_id", "domain_id", "created_at",
                                            "format_version", "version") if key in value}
    kept["databases"] = len(value.get("databases") or [])
    kept["keys"] = sorted(value)
    return kept


def read_backups(args: dict) -> dict:
    identity = domain_identity(args["domain_id"])
    directory = backup_directory(identity["subscription_id"], identity["id"])
    entries = []
    if directory.is_dir():
        for name in sorted(os.listdir(directory)):
            path = directory / name
            info = path.lstat()
            entry = {"name": name, "bytes": info.st_size, "mode": "%04o" % stat.S_IMODE(info.st_mode),
                     "kind": "directory" if stat.S_ISDIR(info.st_mode) else "file", "mtime": int(info.st_mtime)}
            if name.endswith(".cpbak") and not name.startswith(".") and stat.S_ISREG(info.st_mode):
                entry["manifest"] = _manifest(path)
            entries.append(entry)
    archives = [e for e in entries if e["name"].endswith(".cpbak") and not e["name"].startswith(".")]
    origins: dict = {}
    for entry in archives:
        origin = str((entry.get("manifest") or {}).get("origin"))
        origins[origin] = origins.get(origin, 0) + 1
    tree = run(["find", str(BACKUP_BASE), "-maxdepth", "7", "-printf", "%y %s %p\n"], timeout=60)
    return {"directory": str(directory), "exists": directory.is_dir(), "entries": entries, "archives": len(archives),
            "manifests_read": sum(1 for e in archives if "error" not in (e.get("manifest") or {"error": 1})),
            "origins": origins, "partial_files": [e["name"] for e in entries if e["name"].startswith(".")],
            "tree": tree.get("stdout", "").splitlines()[:300], "at": utc(), "epoch": time.time()}


def _tree_digest(root: str) -> dict:
    digest, files, total, newest = hashlib.sha256(), 0, 0, 0
    names, file_list, non_regular = [], {}, []
    if not os.path.isdir(root):
        return {"root": root, "exists": False}
    for directory, subdirectories, filenames in os.walk(root):
        subdirectories.sort()
        for name in sorted(filenames):
            path = os.path.join(directory, name)
            relative = os.path.relpath(path, root)
            try:
                info = os.lstat(path)
                if not stat.S_ISREG(info.st_mode):
                    digest.update(("L " + relative + "\n").encode())
                    non_regular.append(relative)
                    continue
                inner = hashlib.sha256()
                with open(path, "rb") as stream:
                    for chunk in iter(lambda: stream.read(1 << 20), b""):
                        inner.update(chunk)
            except OSError as exc:
                digest.update(("E " + relative + " " + type(exc).__name__ + "\n").encode())
                continue
            digest.update((relative + "\0" + str(info.st_size) + "\0" + inner.hexdigest() + "\n").encode())
            files += 1
            total += info.st_size
            newest = max(newest, int(info.st_mtime))
            if len(names) < 40:
                names.append(relative)
            if len(file_list) < 300:
                file_list[relative] = [info.st_size, inner.hexdigest()]
        for name in subdirectories:
            if os.path.islink(os.path.join(directory, name)):
                non_regular.append(os.path.relpath(os.path.join(directory, name), root))
    info = os.lstat(root)
    return {"root": root, "exists": True, "files": files, "bytes": total, "sha256": digest.hexdigest(), "names": names,
            "file_list": file_list, "non_regular": non_regular,
            "newest_mtime": newest, "owner_uid": info.st_uid, "mode": "%04o" % stat.S_IMODE(info.st_mode)}


def read_docroot(args: dict) -> dict:
    identity = domain_identity(args["domain_id"])
    root = document_root(identity["subscription_id"], identity["id"])
    home = os.path.dirname(root)
    siblings = sorted(os.listdir(home)) if os.path.isdir(home) else []
    return {"domain": identity, "docroot": _tree_digest(root), "site_home_entries": siblings, "at": utc()}


def _mysql(statement: str, database: str | None = None, timeout=120) -> dict:
    client = shutil.which("mariadb", path=ENV["PATH"]) or shutil.which("mysql", path=ENV["PATH"]) or "mariadb"
    argv = [client, "-N", "-B"] + ([database] if database else []) + ["-e", statement]
    return run(argv, timeout=timeout)


def _psql(statement: str, database: str = "postgres", timeout=120) -> dict:
    return run(["sudo", "-u", "postgres", "psql", "--no-psqlrc", "--set", "ON_ERROR_STOP=on", "--no-align",
                "--tuples-only", "--quiet", "--field-separator", "\t", "-d", database],
               input_bytes=(statement + "\n").encode(), timeout=timeout)


def read_engines(args: dict) -> dict:
    """The databases and accounts each engine holds (names only), and the row counts of the seed table in the
    named MariaDB databases."""
    result: dict = {"at": utc()}
    listed = _mysql("SHOW DATABASES")
    result["mariadb"] = {
        "returncode": listed.get("returncode"), "stderr": listed.get("stderr", "")[-300:],
        "databases": sorted(n for n in listed.get("stdout", "").split() if n not in (
            "information_schema", "mysql", "performance_schema", "sys")),
        "users": sorted(set(_mysql("SELECT CONCAT(User, '@', Host) FROM mysql.user").get("stdout", "").split()))}
    listed = _psql("SELECT datname FROM pg_database WHERE NOT datistemplate ORDER BY 1;")
    result["postgresql"] = {
        "returncode": listed.get("returncode"), "stderr": listed.get("stderr", "")[-300:],
        "databases": sorted(n for n in listed.get("stdout", "").split() if n != "postgres"),
        "roles": sorted(_psql("SELECT rolname FROM pg_roles WHERE rolname !~ '^pg_' ORDER BY 1;").get("stdout", "").split())}
    tables = {}
    for name in args.get("mariadb_tables_in", []):
        database = sql_name(name)
        exists = _mysql("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = '%s' AND table_name = '%s'"
                        % (database, SEED_TABLE))
        entry = {"table_exists": exists.get("stdout", "").strip() == "1"}
        if entry["table_exists"]:
            counted = _mysql("SELECT COUNT(*), COALESCE(SUM(id), 0), COALESCE(MAX(id), 0) FROM `%s`" % SEED_TABLE, database)
            parts = (counted.get("stdout", "").split() + ["", "", ""])[:3]
            entry.update(rows=parts[0], sum_id=parts[1], max_id=parts[2],
                         checksum=_mysql("CHECKSUM TABLE `%s`" % SEED_TABLE, database).get("stdout", "").split()[-1:])
        tables[database] = entry
    result["seed_tables"] = tables
    return result


def read_login(args: dict) -> dict:
    """Whether an account can log in to its engine over TCP with a password. The password arrives over the lab's
    SSH channel and is given to the client through its environment; it is never printed."""
    engine, username = args["engine"], sql_name(args["username"])
    password = base64.b64decode(args["password_b64"], validate=True).decode()
    if engine in ("mysql", "mariadb"):
        # The Panel's account is <name>@localhost: the local socket first, then TCP to localhost as the Panel connects.
        client = shutil.which("mariadb", path=ENV["PATH"]) or shutil.which("mysql", path=ENV["PATH"]) or "mariadb"
        answer = run([client, "-N", "-B", "-u", username, "-e", "SELECT CURRENT_USER()"], env={"MYSQL_PWD": password}, timeout=30)
        if answer.get("returncode") != 0:
            answer = run([client, "-N", "-B", "--protocol=TCP", "-h", "localhost", "-P", "3306", "-u", username, "-e",
                          "SELECT CURRENT_USER()"], env={"MYSQL_PWD": password}, timeout=30)
    elif engine == "postgresql":
        answer = run(["psql", "--no-psqlrc", "-h", "127.0.0.1", "-p", "5432", "-U", username, "-d", "postgres", "--no-align",
                      "--tuples-only", "-c", "SELECT current_user"], env={"PGPASSWORD": password, "PGCONNECT_TIMEOUT": "10"},
                     timeout=30)
    else:
        raise Refused("unknown engine")
    scrub = lambda text: text.replace(password, "[REDACTED]") if password else text  # noqa: E731
    return {"engine": engine, "username": username, "returncode": answer.get("returncode"), "status": answer.get("status"),
            "stdout": scrub(answer.get("stdout", ""))[:200], "stderr": scrub(answer.get("stderr", ""))[-300:],
            "logged_in": answer.get("returncode") == 0 and username in answer.get("stdout", ""), "at": utc()}


def read_wireguard(args: dict) -> dict:
    """Peers by public key only (``wg show <interface> peers``); the configuration file is counted, never printed."""
    interfaces = run(["wg", "show", "interfaces"])
    names = interfaces.get("stdout", "").split()
    peers = {}
    for name in names:
        if re.fullmatch(r"[a-z0-9]{1,15}", name):
            peers[name] = sorted(run(["wg", "show", name, "peers"]).get("stdout", "").split())
    config = Path("/etc/wireguard/wg0.conf")
    blocks = None
    if config.is_file():
        blocks = sum(1 for line in config.read_text(errors="replace").splitlines() if line.strip() == "[Peer]")
    unit = run(["systemctl", "show", "wg-quick@wg0.service", "-p", "ActiveState", "-p", "SubState", "-p", "LoadState"])
    return {"wg": shutil.which("wg", path=ENV["PATH"]), "interfaces": names, "interfaces_rc": interfaces.get("returncode"),
            "interfaces_stderr": interfaces.get("stderr", "")[-200:], "peers": peers,
            "peer_count": sum(len(v) for v in peers.values()), "config_peer_blocks": blocks,
            "unit": unit.get("stdout", "").split(), "at": utc()}


def read_journal(args: dict) -> dict:
    units = [u for u in args.get("units", []) if u in JOURNAL_UNITS]
    if not units:
        raise Refused("the journal reader needs one of this cell's units")
    argv = ["journalctl", "--no-pager", "-o", "short-iso-precise", "--since", "@" + str(int(args["since_epoch"])),
            "-n", str(max(1, min(int(args.get("lines", 400)), 3000)))]
    for unit in units:
        argv += ["-u", unit]
    return {"units": units, "journal": run(argv, limit=1 << 19), "at": utc()}


def read_units(args: dict) -> dict:
    facts = {}
    for unit in ("celikpanel-panel.service", "celikpanel-agent.service"):
        shown = run(["systemctl", "show", unit, "-p", "ActiveState", "-p", "SubState", "-p", "MainPID", "-p", "NRestarts",
                     "-p", "ActiveEnterTimestamp", "-p", "ExecMainStartTimestamp"])
        facts[unit] = dict(line.split("=", 1) for line in shown.get("stdout", "").splitlines() if "=" in line)
    return {"units": facts, "epoch": time.time(), "at": utc()}


def read_acme(args: dict) -> dict:
    """Signs that certbot ran: its log files (names, sizes, times) wherever the product keeps them."""
    found = run(["find", "/var/log/letsencrypt", "/var/log/celikpanel", "/var/lib/celikpanel", "/etc/letsencrypt", "-maxdepth", "6", "-name",
                 "letsencrypt.log*", "-printf", "%T@ %s %p\n"], timeout=30)
    logs = sorted(found.get("stdout", "").splitlines())
    hosts = HOSTS.read_text(errors="replace")
    resolves = {host: run(["getent", "hosts", host]).get("stdout", "").strip() for host in ACME_HOSTS}
    return {"certbot": shutil.which("certbot", path=ENV["PATH"]), "certbot_logs": logs, "certbot_log_count": len(logs),
            "acme_isolated": ACME_MARK in hosts and all(loopback_only(answer) for answer in resolves.values()),
            "resolves": resolves,
            "at": utc(), "epoch": time.time()}


def read_imported(args: dict) -> dict:
    """What an import left for one domain name: the Panel's rows, the site's files, the mailbox Postfix knows."""
    domain = domain_name(args["domain"])
    connection = panel_db()
    try:
        rows = query(connection, "SELECT id, name, subscription_id, status FROM domains WHERE name = ?", (domain,))
        domain_id = rows[0]["id"] if rows and "id" in rows[0] else None
        result = {"domain": domain, "rows": rows,
                  "email_accounts": query(connection, "SELECT address, quota_mb FROM email_accounts WHERE domain_id = ?", (domain_id,)),
                  "email_forwardings": query(connection, "SELECT source, destination FROM email_forwardings WHERE domain_id = ?", (domain_id,)),
                  "databases_v2": query(connection, "SELECT name, server_id FROM databases_v2 WHERE domain_id = ?", (domain_id,))}
    finally:
        connection.close()
    if domain_id:
        root = document_root(rows[0]["subscription_id"], domain_id)
        result["docroot"] = _tree_digest(root)
        home = os.path.dirname(root)
        result["site_home_entries"] = sorted(os.listdir(home)) if os.path.isdir(home) else []
    result["escapes"] = find_escapes()
    account = None
    try:
        entry = pwd.getpwnam(site_username(domain))
        account = {"uid": entry.pw_uid, "home": entry.pw_dir}
    except KeyError:
        pass
    result["site_account"] = account
    result["at"] = utc()
    return result


def read_clock(args: dict) -> dict:
    return {"epoch": time.time(), "at": utc()}


# -- set3 readers ---------------------------------------------------------------------------

def find_escapes() -> list:
    """Every file system entry whose name starts with the hostile members' prefix, outside the import directory
    (where the archives themselves live). An import that refuses them leaves this list empty."""
    found = run(["find", *[root for root in ESCAPE_ROOTS if os.path.isdir(root)], "-xdev", "-name", ESCAPE_PREFIX + "*",
                 "-not", "-path", str(IMPORT_ROOT) + "/*"], timeout=120)
    return sorted(line for line in found.get("stdout", "").splitlines() if line)


def _mail_address(address) -> str:
    if not isinstance(address, str) or not ADDRESS_RE.fullmatch(address):
        raise Refused("not a plain mailbox address")
    return address


def _imap_login(address: str, password: str) -> dict:
    """A real IMAP LOGIN against this guest's own Dovecot on loopback (TLS on 993; the certificate is not judged)."""
    context = ssl.create_default_context()
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    scrub = lambda text: str(text).replace(password, "[REDACTED]")[:200]  # noqa: E731
    try:
        client = imaplib.IMAP4_SSL("127.0.0.1", 993, ssl_context=context, timeout=20)
    except Exception as exc:  # noqa: BLE001 - reported as data
        return {"transport": "imaps 127.0.0.1:993", "connected": False, "error": type(exc).__name__ + ": " + scrub(exc)}
    try:
        kind, data = client.login(address, password)
        answer = {"transport": "imaps 127.0.0.1:993", "connected": True, "logged_in": kind == "OK", "answer": kind}
        try:
            kind, data = client.select("INBOX", readonly=True)
            answer["inbox_selected"] = kind == "OK"
        except Exception as exc:  # noqa: BLE001
            answer["inbox_selected"] = False
            answer["select_error"] = type(exc).__name__ + ": " + scrub(exc)
        return answer
    except imaplib.IMAP4.error as exc:
        return {"transport": "imaps 127.0.0.1:993", "connected": True, "logged_in": False, "answer": scrub(exc)}
    finally:
        try:
            client.logout()
        except Exception:  # noqa: BLE001
            pass


def read_mail_login(args: dict) -> dict:
    """Whether a mailbox authenticates with a given password: an IMAP LOGIN on loopback and Dovecot's own
    ``doveadm auth test``. A password that is certainly wrong is tried beside it, so that a login that accepts
    anything cannot pass. The password arrives over the lab's SSH channel and is never printed."""
    address = _mail_address(args["address"])
    password = base64.b64decode(args["password_b64"], validate=True).decode()
    readings = {}
    for label, value in (("the_original_password", password), ("a_wrong_password", secrets_module.token_urlsafe(18))):
        tested = run(["doveadm", "auth", "test", "-x", "service=imap", "-x", "rip=127.0.0.1", address, value], timeout=30)
        scrub = lambda text, value=value: str(text).replace(value, "[REDACTED]")  # noqa: E731
        readings[label] = {"imap": _imap_login(address, value),
                           "doveadm_auth_test": {"status": tested.get("status"), "returncode": tested.get("returncode"),
                                                 "stdout": scrub(tested.get("stdout", ""))[:300],
                                                 "stderr": scrub(tested.get("stderr", ""))[-300:]}}
    looked_up = run(["doveadm", "user", address], timeout=30)
    return {"address": address, "readings": readings,
            "doveadm_user": {"returncode": looked_up.get("returncode"),
                             "fields": sorted(line.split()[0] for line in looked_up.get("stdout", "").splitlines()[1:] if line.split())},
            "dovecot_version": run(["dovecot", "--version"]).get("stdout", "").strip(), "at": utc()}


def read_engine_versions(args: dict) -> dict:
    """The version each engine itself reports (O14): the running servers through their local clients, and the
    server and client programs' own ``--version`` lines."""
    server = shutil.which("mariadbd", path=ENV["PATH"]) or shutil.which("mysqld", path=ENV["PATH"])
    client = shutil.which("mariadb", path=ENV["PATH"]) or shutil.which("mysql", path=ENV["PATH"])
    return {"mariadb": {"select_version": _mysql("SELECT VERSION()").get("stdout", "").strip(),
                        "server_program": server, "server_program_version": run([server, "--version"]).get("stdout", "").strip() if server else None,
                        "client_program": client, "client_program_version": run([client, "--version"]).get("stdout", "").strip() if client else None},
            "postgresql": {"show_server_version": _psql("SHOW server_version;").get("stdout", "").strip(),
                           "select_version": _psql("SELECT version();").get("stdout", "").strip()},
            "os_release": {k: v.strip('"') for k, v in (line.split("=", 1) for line in Path("/etc/os-release").read_text().splitlines()
                                                         if "=" in line) if k in ("ID", "VERSION_ID", "PRETTY_NAME")},
            "at": utc()}


def _stat(path: str) -> dict:
    try:
        info = os.lstat(path)
    except OSError as exc:
        return {"path": path, "exists": False, "error": type(exc).__name__}
    try:
        owner = pwd.getpwuid(info.st_uid).pw_name
    except KeyError:
        owner = str(info.st_uid)
    try:
        import grp
        group = grp.getgrgid(info.st_gid).gr_name
    except (KeyError, ImportError):
        group = str(info.st_gid)
    kind = "socket" if stat.S_ISSOCK(info.st_mode) else "directory" if stat.S_ISDIR(info.st_mode) else \
        "symlink" if stat.S_ISLNK(info.st_mode) else "file" if stat.S_ISREG(info.st_mode) else "other"
    return {"path": path, "exists": True, "kind": kind, "owner": owner, "group": group, "mode": "%04o" % stat.S_IMODE(info.st_mode)}


def read_php(args: dict) -> dict:
    """The native PHP-FPM facts of this host (P5): the units, the pool directories and files with their ``listen``,
    ``user`` and ``listen.owner`` lines, each socket's owner and mode, and the program's own version."""
    units = run(["systemctl", "list-units", "--type=service", "--all", "--no-legend", "--plain", "php*"]).get("stdout", "")
    unit_names = sorted({line.split()[0] for line in units.splitlines() if line.split()})
    shown = {}
    for unit in unit_names:
        answer = run(["systemctl", "show", unit, "-p", "LoadState", "-p", "ActiveState", "-p", "SubState", "-p", "MainPID",
                      "-p", "FragmentPath", "-p", "ExecStart", "-p", "ProtectHome", "-p", "ProtectSystem", "-p", "PrivateTmp",
                      "-p", "ReadWritePaths", "-p", "NoNewPrivileges", "-p", "DropInPaths"])
        shown[unit] = dict(line.split("=", 1) for line in answer.get("stdout", "").splitlines() if "=" in line)
    pools, sockets = [], {}
    for directory in sorted(set(str(p) for p in list(Path("/etc/php").glob("*/fpm/pool.d")) + [Path("/etc/php/php-fpm.d")] if p.is_dir())):
        for name in sorted(os.listdir(directory)):
            path = os.path.join(directory, name)
            if not name.endswith(".conf") or not os.path.isfile(path):
                continue
            keep = {}
            for line in Path(path).read_text(errors="replace").splitlines():
                text = line.strip()
                if text.startswith("[") and text.endswith("]"):
                    keep["pool"] = text[1:-1]
                for key in ("listen", "user", "group", "listen.owner", "listen.group", "listen.mode"):
                    if re.match(re.escape(key) + r"\s*=", text):
                        keep[key] = text.split("=", 1)[1].strip()
            pools.append(dict(_stat(path), directory=directory, settings=keep))
            if str(keep.get("listen", "")).startswith("/"):
                sockets[keep["listen"]] = _stat(keep["listen"])
    program = shutil.which("php-fpm", path=ENV["PATH"]) or next(
        (shutil.which(name, path=ENV["PATH"]) for name in ("php-fpm8.4", "php-fpm8.3", "php-fpm8.2", "php-fpm8.5") if shutil.which(name, path=ENV["PATH"])), None)
    return {"units": shown, "pools": pools, "sockets": sockets,
            "nginx_php_snippet": _stat("/etc/nginx/snippets/fastcgi-php.conf"),
            "pool_directories": sorted({p["directory"] for p in pools}),
            "run_directories": [_stat(path) for path in ("/run/php-fpm", "/run/php") if os.path.exists(path)],
            "program": program, "program_version": run([program, "-v"]).get("stdout", "").splitlines()[:1] if program else None,
            "web_server_accounts": [name for name in ("www-data", "nginx", "http") if _exists_user(name)], "at": utc()}


def _exists_user(name: str) -> bool:
    try:
        pwd.getpwnam(name)
        return True
    except KeyError:
        return False


def read_http(args: dict) -> dict:
    """One GET to this guest's own web server on loopback with the site's Host header (never another host)."""
    host = domain_name(args["domain"])
    path = args.get("path", "/")
    if path not in ("/", "/" + PHP_PROBE, "/index.html", "/index.php"):
        raise Refused("not a path this reader asks for")
    # A plain socket to 127.0.0.1:80 and nothing else: this helper holds no client that could name another host.
    try:
        with socket.create_connection(("127.0.0.1", 80), timeout=20) as connection:
            connection.sendall(("GET " + path + " HTTP/1.0\r\nHost: " + host + "\r\nConnection: close\r\n\r\n").encode())
            raw = b""
            while len(raw) < 131072:
                chunk = connection.recv(65536)
                if not chunk:
                    break
                raw += chunk
    except Exception as exc:  # noqa: BLE001 - reported as data
        return {"host": host, "path": path, "error": type(exc).__name__ + ": " + str(exc)[:200], "at": utc()}
    head, _, body = raw.partition(b"\r\n\r\n")
    lines = head.decode("latin-1").split("\r\n")
    status = int(lines[0].split()[1]) if len(lines[0].split()) > 1 and lines[0].split()[1].isdigit() else None
    headers = {}
    for line in lines[1:]:
        name, _, value = line.partition(":")
        if name.lower() in ("content-type", "server", "x-powered-by", "transfer-encoding"):
            headers[name.lower()] = value.strip()
    return {"host": host, "path": path, "status": status, "headers": headers, "bytes": len(body),
            "body": body[:600].decode("utf-8", "replace"), "at": utc()}


NGINX_PHP_SNIPPET = Path("/etc/nginx/snippets/fastcgi-php.conf")
# The file Debian's and Ubuntu's nginx packages ship under that name (nginx-common), which the product's PHP vhost
# template includes (internal/services/templates/nginx/vhost.conf.tmpl). Arch's nginx package has no snippets directory.
NGINX_PHP_SNIPPET_TEXT = (
    "# regex to split $uri to $fastcgi_script_name and $fastcgi_path\n"
    "fastcgi_split_path_info ^(.+?\\.php)(/.*)$;\n\n"
    "# Check that the PHP script exists before passing it\n"
    "try_files $fastcgi_script_name =404;\n\n"
    "# Bypass the fact that try_files resets $fastcgi_path_info\n"
    "# see: http://trac.nginx.org/nginx/ticket/321\n"
    "set $path_info $fastcgi_path_info;\n"
    "fastcgi_param PATH_INFO $path_info;\n\n"
    "fastcgi_index index.php;\n"
    "include fastcgi.conf;\n")


def owner_nginx_php_snippet(args: dict) -> dict:
    """set3, second Arch reading only: the server owner places by hand the nginx snippet the product's PHP vhost
    includes, with the text Debian's nginx package ships. Nothing is written when the file already exists."""
    existed = NGINX_PHP_SNIPPET.exists()
    made_directory = False
    if not existed:
        if not NGINX_PHP_SNIPPET.parent.exists():
            NGINX_PHP_SNIPPET.parent.mkdir(mode=0o755)
            made_directory = True
        NGINX_PHP_SNIPPET.write_text(NGINX_PHP_SNIPPET_TEXT)
        os.chmod(NGINX_PHP_SNIPPET, 0o644)
    return {"action": "owner-nginx-php-snippet", "path": str(NGINX_PHP_SNIPPET), "existed_before": existed,
            "made_directory": made_directory, "file": _stat(str(NGINX_PHP_SNIPPET)),
            "sha256": hashlib.sha256(NGINX_PHP_SNIPPET.read_bytes()).hexdigest(),
            "fastcgi_conf": _stat("/etc/nginx/fastcgi.conf"), "nginx_test": run(["nginx", "-t"], timeout=30), "at": utc()}


def owner_php_probe(args: dict) -> dict:
    """The owner uploads one PHP page to the site: it prints a marker, a product only PHP can compute, the PHP
    version, the server API and the account it runs as. Served as text it would show its source instead."""
    identity, root, uid, gid = _site(args)
    source = ("<?php\nheader('Content-Type: text/plain');\n"
              "echo '" + PHP_PROBE_MARK + ":' . (6 * 7) . ':' . PHP_VERSION . ':' . php_sapi_name() . ':' . "
              "(function_exists('posix_geteuid') ? posix_getpwuid(posix_geteuid())['name'] : get_current_user()) . \"\\n\";\n")
    _write(os.path.join(root, PHP_PROBE), source.encode(), uid, gid)
    return {"action": "owner-php-probe", "domain": identity, "path": os.path.join(root, PHP_PROBE),
            "docroot": _tree_digest(root), "site_account": pwd.getpwuid(uid).pw_name, "at": utc()}


# -- owner actions --------------------------------------------------------------------------

def _site(args: dict) -> tuple:
    identity = domain_identity(args["domain_id"])
    root = document_root(identity["subscription_id"], identity["id"])
    info = os.lstat(root)
    if not stat.S_ISDIR(info.st_mode) or info.st_uid == 0:
        raise Refused("the document root is not a site account's directory")
    return identity, root, info.st_uid, info.st_gid


def _write(path: str, data: bytes, uid: int, gid: int) -> None:
    os.makedirs(os.path.dirname(path), exist_ok=True)
    os.chown(os.path.dirname(path), uid, gid)
    with open(path, "wb") as stream:
        stream.write(data)
    os.chown(path, uid, gid)
    os.chmod(path, 0o644)


def owner_seed_site(args: dict) -> dict:
    """The owner uploads a site: an index page, a few small files and ``megabytes`` of data that does not compress."""
    identity, root, uid, gid = _site(args)
    megabytes = max(0, min(int(args.get("megabytes", 0)), 1024))
    _write(os.path.join(root, "index.html"), b"<h1>set2 owner site</h1>\n<p>generation 0</p>\n", uid, gid)
    for index in range(1, 6):
        _write(os.path.join(root, "pages", f"page-{index}.html"), (f"<p>page {index}</p>\n" * 50).encode(), uid, gid)
    for index in range(megabytes // 64 + (1 if megabytes % 64 else 0)):
        size = min(64, megabytes - index * 64)
        _write(os.path.join(root, "data", f"blob-{index:02d}.bin"), os.urandom(size * 1024 * 1024), uid, gid)
    return {"action": "owner-seed-site", "domain": identity, "docroot": _tree_digest(root), "megabytes": megabytes, "at": utc()}


def owner_change_site(args: dict) -> dict:
    """The owner changes the site after the backup was taken: the index page is rewritten and a file is added."""
    identity, root, uid, gid = _site(args)
    label = re.sub(r"[^a-z0-9-]", "", str(args.get("tag", "x")))[:40] or "x"
    _write(os.path.join(root, "index.html"), f"<h1>set2 owner site</h1>\n<p>changed after the backup: {label}</p>\n".encode(), uid, gid)
    _write(os.path.join(root, "added-after-backup-" + label + ".txt"), (label + "\n").encode(), uid, gid)
    return {"action": "owner-change-site", "label": label, "docroot": _tree_digest(root), "at": utc()}


def owner_seed_rows(args: dict) -> dict:
    """The owner's application data in a MariaDB database of the site: a table with ``rows`` rows (``seed``), or
    rows deleted and one added after the backup was taken (``drift``)."""
    database = sql_name(args["database"])
    if args["action"] == "seed":
        rows = max(1, min(int(args.get("rows", 1000)), 200000))
        statements = [f"CREATE TABLE IF NOT EXISTS `{SEED_TABLE}` (id INT PRIMARY KEY, note VARCHAR(64));",
                      f"DELETE FROM `{SEED_TABLE}`;"]
        for start in range(1, rows + 1, 1000):
            values = ",".join(f"({i},'owner row {i}')" for i in range(start, min(start + 1000, rows + 1)))
            statements.append(f"INSERT INTO `{SEED_TABLE}` (id, note) VALUES {values};")
    elif args["action"] == "drift":
        statements = [f"DELETE FROM `{SEED_TABLE}` WHERE id % 2 = 0;",
                      f"INSERT INTO `{SEED_TABLE}` (id, note) VALUES (9000000 + FLOOR(RAND() * 900000), 'added after the backup');"]
    else:
        raise Refused("unknown row action")
    client = shutil.which("mariadb", path=ENV["PATH"]) or shutil.which("mysql", path=ENV["PATH"]) or "mariadb"
    done = run([client, database], input_bytes="\n".join(statements).encode(), timeout=300)
    counted = _mysql("SELECT COUNT(*), COALESCE(SUM(id), 0) FROM `%s`" % SEED_TABLE, database)
    return {"action": "owner-seed-rows", "database": database, "kind": args["action"], "returncode": done.get("returncode"),
            "stderr": done.get("stderr", "")[-300:], "rows_and_sum": counted.get("stdout", "").split(), "at": utc()}


def owner_cpmove_fixture(args: dict) -> dict:
    """The owner uploads a cPanel account archive to the import directory, as the import page asks: a root-owned
    0600 file under a root-owned directory. The archive is the minimal one of ``cpmove_members``."""
    domain, user, database = domain_name(args["domain"]), user_name(args["user"]), sql_name(args["database"])
    shadow_hash, mailbox = None, "the fabricated value of set2 (the hash of no password)"
    if args.get("password_b64"):
        # set3 (S1): the crypt hash of a password the lab chose, made by the guest's own openssl; neither is recorded.
        made = subprocess.run(["openssl", "passwd", "-6", "-stdin"], input=base64.b64decode(args["password_b64"], validate=True) + b"\n",
                              capture_output=True, timeout=30, env=ENV)
        shadow_hash = made.stdout.strip()
        if made.returncode != 0 or not shadow_hash.startswith(b"$6$") or b":" in shadow_hash:
            raise Refused("openssl did not produce a sha512-crypt hash")
        mailbox = "the sha512-crypt hash (openssl passwd -6) of a password the lab chose; neither is recorded"
    members = cpmove_members(domain, user, database, max(0, min(int(args.get("megabytes", 0)), 512)),
                             max(0, min(int(args.get("sleep_seconds", 0)), 120)), max(1, min(int(args.get("rows", 200)), 50000)),
                             shadow_hash=shadow_hash)
    with_member = bool(args.get("public_html_directory_member", True))
    hostile = args.get("hostile") or None
    if hostile is not None and hostile not in HOSTILE_KINDS:
        raise Refused("unknown hostile member kind")
    data = cpmove_archive(members, with_member, hostile)
    IMPORT_ROOT.mkdir(mode=0o700, exist_ok=True)
    os.chown(IMPORT_ROOT, 0, 0)
    target = IMPORT_ROOT / f"cpmove-{user}.tar.gz"
    if target.exists():
        raise Refused("the archive already exists")
    fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(data)
        os.fsync(stream.fileno())
    info = IMPORT_ROOT.lstat()
    return {"action": "owner-cpmove-fixture", "path": str(target), "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest(),
            "members": [{"name": name, "bytes": len(content)} for name, content in members],
            "hostile": hostile, "hostile_members": [{"name": name, "type": "symlink -> " + data if kind == tarfile.SYMTYPE else "file"}
                                                    for name, data, kind in (hostile_members("cpmove-" + user, hostile) if hostile else [])],
            "docroot_expected": payload_files(members), "mailbox_shadow": mailbox,
            "directory": {"mode": "%04o" % stat.S_IMODE(info.st_mode), "uid": info.st_uid},
            "public_html_directory_member": with_member, "domain": domain, "user": user, "database": database, "at": utc()}


def owner_restart_panel(args: dict) -> dict:
    """``systemctl restart celikpanel-panel``, as an owner does it; waits until the unit is active again."""
    before = read_units({})
    started = time.time()
    done = run(["systemctl", "restart", "celikpanel-panel.service"], timeout=120)
    after = read_units({})
    for _ in range(60):
        if after["units"]["celikpanel-panel.service"].get("ActiveState") == "active":
            break
        time.sleep(0.5)
        after = read_units({})
    policy = run(["systemctl", "show", "celikpanel-panel.service", "-p", "Restart", "-p", "RestartUSec", "-p", "TimeoutStopUSec",
                  "-p", "KillMode", "-p", "NRestarts"])
    return {"action": "owner-restart-panel", "issued_epoch": started, "returned_epoch": time.time(), "result": done,
            "restart_took_seconds": round(time.time() - started, 1), "unit_policy": policy.get("stdout", "").split(),
            "before": before["units"], "after": after["units"], "at": utc()}


def lab_kill_panel(args: dict) -> dict:
    """A lab fault, not an owner action: the Panel's main process is killed outright (SIGKILL), as a crash, the
    kernel's out-of-memory killer or a power loss would end it. systemd's own restart policy of the unit
    (``Restart=on-failure``) is left to bring it back; this helper only waits for that and reports it."""
    before = read_units({})
    started = time.time()
    done = run(["systemctl", "kill", "--signal=SIGKILL", "--kill-whom=main", "celikpanel-panel.service"], timeout=60)
    after = read_units({})
    for _ in range(120):
        unit = after["units"]["celikpanel-panel.service"]
        if unit.get("ActiveState") == "active" and unit.get("MainPID") not in (None, "0", before["units"]["celikpanel-panel.service"].get("MainPID")):
            break
        time.sleep(0.5)
        after = read_units({})
    policy = run(["systemctl", "show", "celikpanel-panel.service", "-p", "Restart", "-p", "RestartUSec", "-p", "TimeoutStopUSec",
                  "-p", "KillMode", "-p", "NRestarts"])
    return {"action": "lab-kill-panel", "issued_epoch": started, "returned_epoch": time.time(), "result": done,
            "before": before["units"], "after": after["units"], "unit_policy": policy.get("stdout", "").split(), "at": utc()}


def lab_isolate_acme(args: dict) -> dict:
    """Lab isolation, not an owner action: this guest must not reach a certificate authority. The two Let's Encrypt
    directory names resolve to the guest's own loopback, where nothing answers for them."""
    text = HOSTS.read_text()
    if args["action"] == "apply":
        if ACME_MARK not in text:
            with open(HOSTS, "a") as stream:
                stream.write("\n" + ACME_MARK + "\n" + "".join(f"127.0.0.1 {host}\n::1 {host}\n" for host in ACME_HOSTS))
    else:
        raise Refused("the isolation stays for the life of the guest")
    resolves, waited = {}, 0.0
    started = time.time()
    while True:     # H38: a caching resolver takes the new /etc/hosts lines a moment later
        resolves = {host: run(["getent", "hosts", host]).get("stdout", "").strip() for host in ACME_HOSTS}
        waited = round(time.time() - started, 1)
        if all(loopback_only(answer) for answer in resolves.values()) or waited > 30:
            break
        time.sleep(1)
    return {"action": "lab-isolate-acme", "hosts_lines": [l for l in HOSTS.read_text().splitlines() if "acme" in l],
            "resolves": resolves, "confirmed": all(loopback_only(answer) for answer in resolves.values()),
            "confirmed_after_seconds": waited, "at": utc()}


MODES = {
    "read-panel-state": read_panel_state, "read-identities": read_identities, "read-backups": read_backups,
    "read-docroot": read_docroot, "read-engines": read_engines, "read-login": read_login,
    "read-wireguard": read_wireguard, "read-journal": read_journal, "read-units": read_units, "read-acme": read_acme,
    "read-imported": read_imported, "read-clock": read_clock,
    "read-mail-login": read_mail_login, "read-engine-versions": read_engine_versions, "read-php": read_php,
    "read-http": read_http, "owner-php-probe": owner_php_probe, "owner-nginx-php-snippet": owner_nginx_php_snippet,
    "owner-seed-site": owner_seed_site, "owner-change-site": owner_change_site, "owner-seed-rows": owner_seed_rows,
    "owner-cpmove-fixture": owner_cpmove_fixture, "owner-restart-panel": owner_restart_panel,
    "lab-isolate-acme": lab_isolate_acme, "lab-kill-panel": lab_kill_panel,
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
    value.update(schema=SCHEMA, mode=args.mode, owner_action=args.mode.startswith("owner-"),
                 lab_action=args.mode.startswith("lab-"))
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
