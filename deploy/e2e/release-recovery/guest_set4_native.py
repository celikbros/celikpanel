#!/usr/bin/env python3
"""set4: native readers and the lab owner's actions for the re-measurement of commit 557b554eb, inside a marked
disposable QEMU guest.

Readers (``read-*``) only inspect: the platform and its packages, the nginx package's PHP snippet (digest and text),
what ``nginx -t`` says, the files of nginx's configuration that name a domain, one GET to this guest's own web server
on loopback, what exists on the server for a domain name (rows of the Panel's database opened read-only, the system
account, the site's directories, PHP-FPM pool files and sockets, the certificate-validation directories), systemd's
own view of a unit with its journal since a moment. Owner actions (``owner-*``) are what a server owner does by hand:
upload one PHP page and one text file to a site, move one of nginx's own files away and back.

No mode prints a password, a private key or a hash, and no mode contacts another host.

set4: 557b554eb düzeltmelerinin yeniden ölçümü için işaretli geçici QEMU konuğunda yerel durum okuyucuları ve sunucu
sahibinin elle yaptığı işlemler. Hiçbir kip parola, özel anahtar ya da özet yazdırmaz; başka bir sunucuya bağlanmaz.
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
import pwd
import re
import socket
import sqlite3
import sys

HERE = Path(__file__).resolve().parent
SCHEMA = "celikpanel/set4-native/v1"
PROBE = "set4-probe.php"
PROBE_MARK = "set4-php-executed"
STATIC = "set4-static.txt"
STATIC_TEXT = b"set4 static file served by the web server\n"
NGINX = "/etc/nginx"
SNIPPET = NGINX + "/snippets/fastcgi-php.conf"
OWNER_MOVED = NGINX + "/fastcgi.conf"
OWNER_MOVED_TO = NGINX + "/fastcgi.conf.set4-owner-moved"
SITES_ROOT = "/var/www/celikpanel/subscriptions"
ACME_ROOT = "/var/lib/celikpanel-agent/acme-http-01"
POOL_GLOBS = ("/etc/php/*/fpm/pool.d/*.conf", "/etc/php/php-fpm.d/*.conf")
SOCKET_DIRS = ("/run/php", "/run/php-fpm")
PATH_RE = re.compile(r"/[A-Za-z0-9._/?=&%-]{0,200}\Z")
UNIT_RE = re.compile(r"[A-Za-z0-9@_.:-]{1,80}\.service\Z")
SECRET_COLUMN = re.compile(r"pass|secret|token|key|hash|salt|credential", re.I)
UNIT_PROPERTIES = ("LoadState", "ActiveState", "SubState", "Result", "MainPID", "ExecMainPID", "ExecMainStatus",
                   "InvocationID", "NRestarts", "StateChangeTimestamp", "ActiveEnterTimestamp", "ActiveExitTimestamp",
                   "InactiveEnterTimestamp", "InactiveExitTimestamp", "ExecMainStartTimestamp", "ExecReload",
                   "ReloadResult", "Type", "FragmentPath")
PACKAGES = ("nginx", "nginx-common", "nginx-core", "nginx-mainline", "php", "php-fpm", "php8.3-fpm", "php8.4-fpm",
            "php8.5-fpm", "php-cgi", "mariadb", "mariadb-server", "postgresql", "postgresql-17", "postgresql-16",
            "postfix", "dovecot-core", "dovecot", "certbot", "systemd")


def _load(name: str):
    spec = importlib.util.spec_from_file_location("set4_" + name.replace(".", "_"), HERE / name)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


rid = _load("guest_request_identity_native.py")
run, utc, Refused = rid.run, rid.utc, rid.Refused


def sha256_file(path: str):
    try:
        return hashlib.sha256(Path(path).read_bytes()).hexdigest()
    except OSError:
        return None


def text_of(path: str, limit: int = 12000):
    try:
        return Path(path).read_bytes()[:limit].decode("utf-8", "replace")
    except OSError:
        return None


def file_facts(path: str, text: bool = False) -> dict:
    value = dict(rid._stat(path), sha256=sha256_file(path) if os.path.isfile(path) else None)
    if text and os.path.isfile(path):
        value["text"] = text_of(path)
        value["bytes"] = os.path.getsize(path)
    return value


def nginx_test() -> dict:
    done = run(["nginx", "-t"], timeout=60)
    return {"returncode": done.get("returncode"), "status": done.get("status"), "output": (done.get("stderr") or "") + (done.get("stdout") or ""),
            "at": done.get("at")}


def package_owner(path: str) -> dict:
    if not os.path.exists(path):
        return {"path": path, "exists": False}
    for argv in (["dpkg", "-S", path], ["pacman", "-Qo", path]):
        done = run(argv, timeout=30)
        if done.get("status") == "ok":
            return {"path": path, "exists": True, "asked": " ".join(argv), "returncode": done.get("returncode"),
                    "answer": ((done.get("stdout") or "") + (done.get("stderr") or "")).strip()[:300]}
    return {"path": path, "exists": True, "asked": None}


def read_platform(args: dict) -> dict:
    """The platform as it stands: the distribution, the packages' own versions, nginx's and PHP-FPM's own version
    lines, and the nginx package's PHP snippet with its digest, text and owning package."""
    release = {}
    for line in (text_of("/etc/os-release") or "").splitlines():
        key, _, value = line.partition("=")
        if key in ("ID", "VERSION_ID", "PRETTY_NAME", "VERSION_CODENAME", "BUILD_ID", "IMAGE_ID", "IMAGE_VERSION"):
            release[key] = value.strip('"')
    packages = {}
    dpkg = run(["dpkg-query", "-W", "-f", "${Package} ${Version} ${db:Status-Abbrev}\n", *PACKAGES], timeout=60)
    if dpkg.get("status") == "ok":
        for line in (dpkg.get("stdout") or "").splitlines():
            parts = line.split()
            if len(parts) >= 3 and parts[2].startswith("ii"):
                packages[parts[0]] = parts[1]
    pacman = run(["pacman", "-Q", *PACKAGES], timeout=60)
    if pacman.get("status") == "ok":
        for line in (pacman.get("stdout") or "").splitlines():
            parts = line.split()
            if len(parts) == 2:
                packages[parts[0]] = parts[1]
    programs = {}
    for name in ("php-fpm", "php-fpm8.5", "php-fpm8.4", "php-fpm8.3", "php-fpm8.2"):
        done = run([name, "-v"], timeout=30)
        if done.get("status") == "ok":
            programs[name] = (done.get("stdout") or "").splitlines()[:1]
    nginx_v = run(["nginx", "-v"], timeout=30)
    php_dirs = sorted(p for p in glob.glob("/etc/php/*") if os.path.isdir(p))
    return {"os_release": release, "kernel": os.uname().release, "packages": packages,
            "package_manager": "dpkg" if dpkg.get("status") == "ok" else "pacman" if pacman.get("status") == "ok" else None,
            "nginx_version": ((nginx_v.get("stderr") or "") + (nginx_v.get("stdout") or "")).strip(),
            "php_fpm_programs": programs, "etc_php_directories": php_dirs,
            "nginx_php_snippet": dict(file_facts(SNIPPET, text=True), owner_package=package_owner(SNIPPET)),
            "nginx_snippets_directory": rid._stat(NGINX + "/snippets"),
            "nginx_fastcgi_conf": dict(file_facts(NGINX + "/fastcgi.conf"), owner_package=package_owner(NGINX + "/fastcgi.conf")),
            "nginx_fastcgi_params": file_facts(NGINX + "/fastcgi_params"),
            "nginx_directory": sorted(os.listdir(NGINX)) if os.path.isdir(NGINX) else None,
            "nginx_test": nginx_test(), "at": utc()}


def nginx_files_naming(domain: str) -> list:
    found = []
    for root, _dirs, files in os.walk(NGINX, followlinks=False):
        for name in sorted(files):
            path = os.path.join(root, name)
            if os.path.islink(path):
                target = os.path.realpath(path)
                if domain in name or domain in os.path.basename(target):
                    found.append({"path": path, "kind": "symlink", "target": os.readlink(path), "target_exists": os.path.exists(target)})
                continue
            try:
                data = Path(path).read_bytes()
            except OSError:
                continue
            if domain.encode() in data or domain in name:
                text = data[:16000].decode("utf-8", "replace")
                info = os.stat(path)
                found.append({"path": path, "kind": "file", "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest(),
                              "inode": info.st_ino, "mtime_ns": info.st_mtime_ns, "ctime_ns": info.st_ctime_ns,
                              "text": text,
                              "includes": re.findall(r"(?m)^\s*include\s+([^;]+);", text),
                              "names_the_php_snippet": "snippets/fastcgi-php.conf" in text,
                              "fastcgi_pass": re.findall(r"(?m)^\s*fastcgi_pass\s+([^;]+);", text),
                              "php_location_lines": php_location(text)})
    return found


def php_location(text: str) -> list:
    """The directive lines of the first ``location ~ \\.php$`` block of a vhost text, in their order."""
    match = re.search(r"location\s+~\s+\\\.php\$\s*\{(.*?)\n\s*\}", text, re.S)
    if not match:
        return []
    return [line.strip() for line in match.group(1).splitlines() if line.strip() and not line.strip().startswith("#")]


def read_nginx(args: dict) -> dict:
    domain = rid.domain_name(args["domain"])
    return {"domain": domain, "nginx_test": nginx_test(), "files": nginx_files_naming(domain),
            "unit": unit_show("nginx.service"), "at": utc()}


def read_http(args: dict) -> dict:
    """One GET to this guest's own web server on loopback with the site's Host header (never another host)."""
    host = rid.domain_name(args["domain"])
    path = args.get("path", "/")
    if not isinstance(path, str) or not PATH_RE.fullmatch(path):
        raise Refused("not a path this reader asks for")
    try:
        with socket.create_connection(("127.0.0.1", 80), timeout=20) as connection:
            connection.sendall(("GET " + path + " HTTP/1.0\r\nHost: " + host + "\r\nConnection: close\r\n\r\n").encode())
            raw = b""
            while len(raw) < 262144:
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
        if name.lower() in ("content-type", "server", "x-powered-by", "location", "content-length"):
            headers[name.lower()] = value.strip()
    return {"host": host, "path": path, "status": status, "status_line": lines[0], "headers": headers, "bytes": len(body),
            "body_sha256": hashlib.sha256(body).hexdigest(), "body": body[:2000].decode("utf-8", "replace"), "at": utc()}


def owner_php_probe(args: dict) -> dict:
    """The owner uploads one PHP page and one text file to the site. The page prints a marker, a product only PHP
    can compute, the PHP version, the server API, the account it runs as and the request values PHP-FPM was handed
    (PATH_INFO, SCRIPT_NAME, SCRIPT_FILENAME, DOCUMENT_ROOT, REQUEST_URI, QUERY_STRING), one per line."""
    identity, root, uid, gid = rid._site(args)
    source = ("<?php\nheader('Content-Type: text/plain');\n"
              "echo '" + PROBE_MARK + ":' . (6 * 7) . ':' . PHP_VERSION . ':' . php_sapi_name() . ':' . "
              "(function_exists('posix_geteuid') ? posix_getpwuid(posix_geteuid())['name'] : get_current_user()) . \"\\n\";\n"
              "foreach (['PATH_INFO', 'SCRIPT_NAME', 'SCRIPT_FILENAME', 'DOCUMENT_ROOT', 'REQUEST_URI', 'QUERY_STRING', "
              "'DOCUMENT_URI', 'PATH_TRANSLATED'] as $k) {\n"
              "    echo $k . '=[' . (isset($_SERVER[$k]) ? $_SERVER[$k] : '(unset)') . \"]\\n\";\n}\n")
    rid._write(os.path.join(root, PROBE), source.encode(), uid, gid)
    rid._write(os.path.join(root, STATIC), STATIC_TEXT, uid, gid)
    return {"action": "owner-php-probe", "domain": identity, "path": os.path.join(root, PROBE), "document_root": root,
            "probe_sha256": hashlib.sha256(source.encode()).hexdigest(),
            "docroot_entries": sorted(os.listdir(root)), "site_account": pwd.getpwuid(uid).pw_name, "at": utc()}


def _panel_tables(connection) -> list:
    return [row["name"] for row in connection.execute("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")]


def _visible(row: sqlite3.Row) -> dict:
    return {key: ("[not read: secret-shaped column]" if SECRET_COLUMN.search(key) else row[key]) for key in row.keys()}


def panel_rows_for(domain: str) -> dict:
    """Every row of the Panel's database (opened read-only) that names the domain in a text column, and the rows of
    ``domains`` and ``sites`` for it. Secret-shaped columns are never read out."""
    connection = rid.panel_db()
    connection.row_factory = sqlite3.Row
    try:
        tables = _panel_tables(connection)
        domains = [_visible(r) for r in connection.execute("SELECT * FROM domains WHERE name = ?", (domain,))]
        ids = [r.get("id") for r in domains]
        sites = []
        if "sites" in tables:
            for domain_id in ids:
                sites += [_visible(r) for r in connection.execute("SELECT * FROM sites WHERE domain_id = ?", (domain_id,))]
        naming = {}
        for table in tables:
            columns = [r["name"] for r in connection.execute("PRAGMA table_info(" + '"' + table.replace('"', '') + '"' + ")")
                       if not SECRET_COLUMN.search(r["name"])]
            total = 0
            for column in columns:
                try:
                    found = connection.execute(
                        "SELECT COUNT(*) AS n FROM \"" + table + "\" WHERE typeof(\"" + column + "\") = 'text' AND instr(\"" + column + "\", ?) > 0",
                        (domain,)).fetchone()["n"]
                except sqlite3.Error:
                    continue
                if found:
                    naming.setdefault(table, {})[column] = found
                    total += found
        counts = {}
        for table in ("domains", "sites", "subscriptions", "databases_v2", "email_accounts"):
            if table in tables:
                counts[table] = connection.execute("SELECT COUNT(*) AS n FROM \"" + table + "\"").fetchone()["n"]
        sequence = {}
        try:
            for row in connection.execute("SELECT name, seq FROM sqlite_sequence WHERE name IN ('domains', 'sites')"):
                sequence[row["name"]] = row["seq"]
        except sqlite3.Error:
            pass
        return {"domains": domains, "sites": sites, "tables_naming_the_domain": naming, "row_counts": counts,
                "autoincrement": sequence}
    finally:
        connection.close()


def _listing(root: str, pattern: str) -> list:
    return sorted(p for p in glob.glob(os.path.join(root, pattern)))


def read_leftovers(args: dict) -> dict:
    """What exists on this server for one domain name: the Panel's rows, nginx's files, the system account, its
    processes, the site directories and the certificate-validation directories (all of them, so that two readings can
    be compared when the domain has no row any more), PHP-FPM pool files and sockets."""
    domain = rid.domain_name(args["domain"])
    user = rid.site_username(domain)
    account = None
    try:
        entry = pwd.getpwnam(user)
        account = {"name": entry.pw_name, "uid": entry.pw_uid, "gid": entry.pw_gid, "home": entry.pw_dir, "shell": entry.pw_shell,
                   "home_exists": os.path.isdir(entry.pw_dir)}
    except KeyError:
        pass
    group = run(["getent", "group", user], timeout=20)
    pools = []
    for pattern in POOL_GLOBS:
        for path in sorted(glob.glob(pattern)):
            text = text_of(path, 8000) or ""
            keep = {}
            for line in text.splitlines():
                line = line.strip()
                if line.startswith("[") and line.endswith("]"):
                    keep["pool"] = line[1:-1]
                for key in ("listen", "user", "group", "listen.owner", "listen.group", "listen.mode"):
                    if re.match(re.escape(key) + r"\s*=", line):
                        keep[key] = line.split("=", 1)[1].strip()
            pools.append({"path": path, "settings": keep, "names_the_account": keep.get("user") == user or user in os.path.basename(path),
                          "names_the_domain": domain in text})
    sockets = []
    for directory in SOCKET_DIRS:
        if os.path.isdir(directory):
            sockets += [rid._stat(os.path.join(directory, name)) for name in sorted(os.listdir(directory))]
    processes = run(["pgrep", "-a", "-u", user], timeout=20) if account else {"status": "no-account"}
    rows = panel_rows_for(domain)
    site_dirs = _listing(SITES_ROOT, "*/sites/*")       # internal/hostingpath.SiteHome
    acme_dirs = _listing(ACME_ROOT, "subscriptions/*/domains/*")
    mine = {}
    for row in rows["domains"]:
        sub, dom = row.get("subscription_id"), row.get("id")
        mine = {"site_home": f"{SITES_ROOT}/{sub}/sites/{dom}", "acme_challenge_root": f"{ACME_ROOT}/subscriptions/{sub}/domains/{dom}"}
        mine["site_home_stat"] = rid._stat(mine["site_home"])
        mine["site_home_entries"] = sorted(os.listdir(mine["site_home"])) if os.path.isdir(mine["site_home"]) else None
        mine["acme_challenge_root_stat"] = rid._stat(mine["acme_challenge_root"])
    crontab = [p for p in ("/var/spool/cron/crontabs/" + user, "/var/spool/cron/" + user) if os.path.exists(p)]
    dumped = run(["nginx", "-T"], timeout=60, limit=4 << 20)
    dumped_lines = [line.strip()[:200] for line in (dumped.get("stdout") or "").splitlines() if domain in line]
    return {"domain": domain, "account_name": user, "account": account,
            "group": (group.get("stdout") or "").strip().split(":")[0] or None,
            "processes_of_the_account": [l.split(None, 1)[-1][:120] for l in (processes.get("stdout") or "").splitlines()] if account else [],
            "panel": rows, "this_domain": mine,
            "nginx_files_naming_the_domain": [{k: f.get(k) for k in ("path", "kind", "target", "target_exists", "bytes", "sha256")}
                                              for f in nginx_files_naming(domain)],
            "nginx_dump_lines_naming_the_domain": dumped_lines[:20], "nginx_dump_returncode": dumped.get("returncode"),
            "nginx_test": nginx_test(),
            "pool_files": pools, "pool_files_of_the_account": [p["path"] for p in pools if p["names_the_account"] or p["names_the_domain"]],
            "sockets": sockets, "site_directories": site_dirs, "acme_challenge_directories": acme_dirs,
            "site_directories_root": rid._stat(SITES_ROOT),
            "subscription_directories": {d: sorted(os.listdir(d)) for d in _listing(SITES_ROOT, "*") if os.path.isdir(d)},
            "acme_root": rid._stat(ACME_ROOT), "crontab_spool": crontab, "at": utc()}


def unit_show(unit: str) -> dict:
    argv = ["systemctl", "show", unit]
    for name in UNIT_PROPERTIES:
        argv += ["-p", name]
    done = run(argv, timeout=30)
    return dict(line.split("=", 1) for line in (done.get("stdout") or "").splitlines() if "=" in line)


def read_units(args: dict) -> dict:
    """systemd's own view of each unit and, with ``since_epoch``, every journal line of the unit since that moment
    (the unit's own messages and systemd's messages about it), and the Agent's and the Panel's lines of the window."""
    units = []
    for unit in args.get("units", []):
        if not isinstance(unit, str) or not UNIT_RE.fullmatch(unit):
            raise Refused("not a service unit name")
        units.append(unit)
    result = {"units": {unit: unit_show(unit) for unit in units}, "is_active": {}, "at": utc()}
    for unit in units:
        done = run(["systemctl", "is-active", unit], timeout=20)
        result["is_active"][unit] = (done.get("stdout") or "").strip()
        failed = run(["systemctl", "is-failed", unit], timeout=20)
        result.setdefault("is_failed", {})[unit] = (failed.get("stdout") or "").strip()
    if args.get("since_epoch") is not None:
        since = "@" + str(int(args["since_epoch"]))
        result["since_epoch"] = int(args["since_epoch"])
        result["journal"] = {}
        for unit in units:
            done = run(["journalctl", "--no-pager", "-o", "short-iso-precise", "--since", since, "-u", unit, "-n", "400"], timeout=60)
            lines = [l for l in (done.get("stdout") or "").splitlines() if l.strip() and not l.startswith("-- ")]
            result["journal"][unit] = lines
        product = run(["journalctl", "--no-pager", "-o", "short-iso-precise", "--since", since, "-u", "celikpanel-agent.service",
                       "-u", "celikpanel-panel.service", "-n", "400"], timeout=60)
        result["journal_product"] = [l[:600] for l in (product.get("stdout") or "").splitlines() if l.strip() and not l.startswith("-- ")]
        manager = run(["journalctl", "--no-pager", "-o", "short-iso-precise", "--since", since, "_PID=1", "-n", "400"], timeout=60)
        result["journal_systemd_manager"] = [l[:400] for l in (manager.get("stdout") or "").splitlines()
                                             if l.strip() and not l.startswith("-- ") and any(u.split(".service")[0] in l for u in units)]
    return result


def owner_nginx_file(args: dict) -> dict:
    """The owner, tidying nginx's directory by hand, renames one of nginx's own files (``fastcgi.conf``) and later
    renames it back. Nothing else is touched; nginx is neither tested into service nor reloaded by this action."""
    action = args.get("action")
    before = {"file": rid._stat(OWNER_MOVED), "moved": rid._stat(OWNER_MOVED_TO), "nginx_test": nginx_test()}
    if action == "move-away":
        if not os.path.isfile(OWNER_MOVED) or os.path.exists(OWNER_MOVED_TO):
            raise Refused("the file is not there to move, or was moved already")
        os.rename(OWNER_MOVED, OWNER_MOVED_TO)
    elif action == "restore":
        if not os.path.isfile(OWNER_MOVED_TO) or os.path.exists(OWNER_MOVED):
            raise Refused("nothing to restore")
        os.rename(OWNER_MOVED_TO, OWNER_MOVED)
    else:
        raise Refused("unknown owner action")
    return {"action": "owner-nginx-file", "what": action, "path": OWNER_MOVED, "moved_to": OWNER_MOVED_TO, "before": before,
            "after": {"file": rid._stat(OWNER_MOVED), "moved": rid._stat(OWNER_MOVED_TO), "nginx_test": nginx_test()}, "at": utc()}


def read_isolation(args: dict) -> dict:
    """Lab isolation as this guest's own hosts file answers it: the product's name and the certificate authority's
    two directory names. Asked with ``getent -s files``, which reads /etc/hosts only: no DNS question is sent for
    any of these names, and nothing is connected to."""
    names = ("celikpanel.net", "acme-v02.api.letsencrypt.org", "acme-staging-v02.api.letsencrypt.org")
    resolves = {name: (run(["getent", "-s", "files", "hosts", name], timeout=20).get("stdout") or "").strip() for name in names}
    return {"asked_with": "getent -s files hosts NAME", "resolves": resolves,
            "loopback_only": {name: rid.loopback_only(answer) for name, answer in resolves.items()},
            "hosts_lines": [l for l in (text_of("/etc/hosts") or "").splitlines() if l.strip() and not l.startswith("#")],
            "at": utc()}


def read_spa(args: dict) -> dict:
    """What the installed web build holds: for every ``web/dist`` on this guest, how many of its files contain each
    of the given sentences; and where the running Panel's process lives."""
    needles = [n for n in args.get("needles", []) if isinstance(n, str) and 8 <= len(n) <= 200][:12]
    found = run(["find", "/opt", "/usr/local", "/var/lib", "/srv", "/root", "/home", "-xdev", "-type", "f", "(", "-path",
                 "*/web/dist/index.html", "-o", "-path", "*/web/index.html", ")"], timeout=180)
    builds = []
    for index in sorted((found.get("stdout") or "").splitlines()):
        root = os.path.dirname(index)
        counts, files = {needle: 0 for needle in needles}, 0
        for directory, _dirs, names in os.walk(root):
            for name in names:
                if not name.endswith((".js", ".html", ".json", ".mjs")):
                    continue
                files += 1
                try:
                    data = Path(os.path.join(directory, name)).read_bytes()
                except OSError:
                    continue
                for needle in needles:
                    if needle.encode() in data:
                        counts[needle] += 1
        builds.append({"dist": root, "real_path": os.path.realpath(root), "files_read": files, "files_containing": counts,
                       "index_sha256": sha256_file(index)})
    unit = run(["systemctl", "show", "celikpanel-panel.service", "-p", "MainPID", "-p", "ExecStart", "-p", "WorkingDirectory",
                "-p", "ActiveState"], timeout=30)
    shown = dict(line.split("=", 1) for line in (unit.get("stdout") or "").splitlines() if "=" in line)
    process = {}
    pid = shown.get("MainPID")
    if pid and pid.isdigit() and int(pid) > 1:
        for name in ("exe", "cwd"):
            try:
                process[name] = os.readlink(f"/proc/{pid}/{name}")
            except OSError as exc:
                process[name] = type(exc).__name__
    served = None
    if pid and pid.isdigit() and int(pid) > 1:
        try:
            for entry in Path(f"/proc/{pid}/environ").read_bytes().split(bytes([0])):
                if entry.startswith(b"CELIKPANEL_WEB_DIR="):
                    served = entry.split(b"=", 1)[1].decode("utf-8", "replace")
        except OSError:
            pass
        if served is None and isinstance(process.get("cwd"), str) and process["cwd"].startswith("/"):
            served = os.path.join(process["cwd"], "web", "dist")
        if served is not None and not os.path.isabs(served) and isinstance(process.get("cwd"), str):
            served = os.path.join(process["cwd"], served)
    for build in builds:
        build["served_by_the_running_panel"] = served is not None and os.path.realpath(served) == build["real_path"]
    return {"needles": needles, "web_builds": builds, "panel_unit": shown, "panel_process": process,
            "web_directory_of_the_running_panel": served, "at": utc()}


MODES = {"read-platform": read_platform, "read-nginx": read_nginx, "read-http": read_http, "read-leftovers": read_leftovers,
         "read-units": read_units, "read-isolation": read_isolation, "read-spa": read_spa,
         "owner-php-probe": owner_php_probe, "owner-nginx-file": owner_nginx_file}


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
