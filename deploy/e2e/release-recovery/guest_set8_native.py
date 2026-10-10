#!/usr/bin/env python3
"""set8: native readers and the owner's hand edits for the baseline measurement of what the Panel does with a site's
nginx vhost (and PHP-FPM pool) that the server owner edited by hand, inside a marked disposable QEMU guest.

Readers (``read-*``) only inspect: a site's vhost file with its bytes, inode, mode, owner, attributes and enabled link,
its PHP-FPM pool file, the owner's include file, ``nginx -t`` and ``nginx -T``, the units of nginx, PHP-FPM, the Panel
and the Agent with nginx's worker processes, and the journal of those units since a moment. Owner actions
(``owner-*``) are what a server owner does by hand: edit, replace, remove or lock (``chattr +i``) a vhost, edit a pool,
test and reload nginx or PHP-FPM, stop, start or restart the Panel's unit. ``lab-restore`` is lab preparation between
scenarios (the recorded bytes the Panel wrote are put back), recorded as such.

No mode prints a password, a private key or a hash of one, and no mode contacts another host.

set8: Sunucu sahibinin elle düzenlediği site yapılandırmasına Panel'in ne yaptığının ölçümü için okuyucular ve
sahibin elle yaptığı işlemler. Hiçbir kip parola, özel anahtar ya da özet yazdırmaz; başka bir sunucuya bağlanmaz.
"""
from __future__ import annotations

import argparse
import base64
import fnmatch
import glob
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import sys
import time

HERE = Path(__file__).resolve().parent
SCHEMA = "celikpanel/set8-native/v1"
NGINX = "/etc/nginx"
AVAILABLE = NGINX + "/sites-available"
ENABLED = NGINX + "/sites-enabled"
OWNER_DIR = NGINX + "/owner"
POOL_GLOBS = ("/etc/php/*/fpm/pool.d/site*.conf", "/etc/php/php-fpm.d/site*.conf")
PANEL_UNIT = "celikpanel-panel.service"
AGENT_UNIT = "celikpanel-agent.service"
RECONCILE_LINES = ("certificate startup reconcile: restored", "certificate startup reconcile: restore hosted vhost batch")
OWNER_INDEX = "set8-owner-index.html"
OWNER_INDEX_TEXT = b"set8 owner index page\n"
INI_PROBE = "set8-ini.php"
INI_PROBE_TEXT = b"<?php\nheader('Content-Type: text/plain');\necho 'set8-ini:memory_limit=' . ini_get('memory_limit') . \"\\n\";\n"


def _load(name: str):
    spec = importlib.util.spec_from_file_location("set8_" + name.replace(".", "_"), HERE / name)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


s4 = _load("guest_set4_native.py")
rid = s4.rid
run, utc, Refused = rid.run, rid.utc, rid.Refused


# -- pure text rules (covered offline by test_set8_trial) ---------------------------------------------------------

def server_block_end(text: str, domain: str) -> int | None:
    """Index of the line holding the closing brace of the ``server`` block whose ``server_name`` names ``domain`` as a
    whole word; None when there is no such block. Braces are counted on the part of each line before a ``#``."""
    lines = text.split("\n")
    depth, start, names_it = 0, None, False
    for index, line in enumerate(lines):
        code = line.split("#", 1)[0]
        stripped = code.strip()
        if depth == 0 and re.match(r"server\s*\{", stripped):
            start, names_it = index, False
        if depth == 1 and start is not None and stripped.startswith("server_name"):
            names_it = domain in stripped.rstrip(";").split()[1:]
        depth += code.count("{") - code.count("}")
        if depth == 0 and start is not None and "}" in code:
            if names_it:
                return index
            start = None
    return None


def insert_before_block_end(text: str, domain: str, lines: list) -> str:
    end = server_block_end(text, domain)
    if end is None:
        raise Refused("no server block names the domain")
    parts = text.split("\n")
    return "\n".join(parts[:end] + lines + parts[end:])


def change_index(text: str) -> tuple:
    """The owner puts their own page first in the ``index`` directive the Panel's template sets."""
    pattern = re.compile(r"^(\s*)index (index\.php index\.html index\.htm;)\s*$", re.M)
    match = pattern.search(text)
    if not match:
        raise Refused("the template's index directive is not in the file")
    old = match.group(0)
    new = match.group(1) + "index " + OWNER_INDEX + " index.php index.html index.htm;"
    return text.replace(old, new, 1), old.strip(), new.strip()


# -- readers ------------------------------------------------------------------------------------------------------

def file_record(path: str, text: bool = True) -> dict:
    value = dict(rid._stat(path))
    try:
        info = os.lstat(path)
        value.update(inode=info.st_ino, uid=info.st_uid, gid=info.st_gid, nlink=info.st_nlink, size=info.st_size,
                     mtime_ns=info.st_mtime_ns, ctime_ns=info.st_ctime_ns)
    except OSError:
        return value
    if os.path.islink(path):
        value["link_target"] = os.readlink(path)
        value["resolves_to"] = os.path.realpath(path)
    if os.path.isfile(path) and not os.path.islink(path):
        data = Path(path).read_bytes()
        value["sha256"] = hashlib.sha256(data).hexdigest()
        if text:
            value["text"] = data[:65536].decode("utf-8", "replace")
        attributes = run(["lsattr", "-d", path], timeout=20)
        value["lsattr"] = ((attributes.get("stdout") or "").split() or [None])[0]
        value["immutable"] = "i" in (value["lsattr"] or "")      # lower-case i is the immutable flag only
    return value


def directory_listing(path: str) -> list:
    try:
        names = sorted(os.listdir(path))
    except OSError as exc:
        return [{"error": type(exc).__name__}]
    return [{"name": n, "kind": rid._stat(os.path.join(path, n)).get("kind")} for n in names]


def site_id_of(text: str | None):
    match = re.search(r"^# Site ID: (\d+)", text or "", re.M)
    return int(match.group(1)) if match else None


def pool_of(site_id) -> dict | None:
    if not isinstance(site_id, int):
        return None
    for pattern in POOL_GLOBS:
        for path in sorted(glob.glob(pattern)):
            if os.path.basename(path) == "site%d.conf" % site_id:
                return file_record(path)
    return {"exists": False, "looked_in": list(POOL_GLOBS), "site_id": site_id}


def nginx_workers() -> dict:
    unit = s4.unit_show("nginx.service")
    master = unit.get("MainPID")
    children = run(["pgrep", "-P", str(master)], timeout=20) if master and master != "0" else {}
    return {"master": master, "workers": sorted((children.get("stdout") or "").split()), "invocation": unit.get("InvocationID"),
            "active": unit.get("ActiveState"), "started": unit.get("ExecMainStartTimestamp")}


def php_units() -> list:
    listed = run(["systemctl", "list-units", "--type=service", "--all", "--no-legend", "--plain", "php*"], timeout=30)
    return sorted({line.split()[0] for line in (listed.get("stdout") or "").splitlines()
                   if line.split() and "fpm" in line.split()[0]})


def nginx_dump() -> dict:
    done = run(["nginx", "-T"], timeout=60, limit=4 << 20)
    out = done.get("stdout") or ""
    return {"returncode": done.get("returncode"), "stderr": (done.get("stderr") or "")[:4000],
            "sha256": hashlib.sha256(out.encode()).hexdigest(), "bytes": len(out.encode()), "text": out}


def read_site(args: dict) -> dict:
    domains = [rid.domain_name(d) for d in args.get("domains", [])]
    value = {"at": utc(), "epoch": time.time(), "sites": {}, "nginx_test": s4.nginx_test(), "nginx": nginx_workers(),
             "units": {u: s4.unit_show(u) for u in [PANEL_UNIT, AGENT_UNIT, "nginx.service"] + php_units()},
             "sites_available": directory_listing(AVAILABLE), "sites_enabled": directory_listing(ENABLED),
             "owner_dir": directory_listing(OWNER_DIR) if os.path.isdir(OWNER_DIR) else None}
    for domain in domains:
        vhost = file_record(f"{AVAILABLE}/{domain}.conf")
        site_id = site_id_of(vhost.get("text"))
        value["sites"][domain] = {"vhost": vhost, "enabled": file_record(f"{ENABLED}/{domain}.conf", text=False),
                                  "site_id": site_id, "pool": pool_of(site_id) if args.get("pool") else None,
                                  "owner_include": file_record(f"{OWNER_DIR}/{domain}.conf")}
    if args.get("dump"):
        value["nginx_dump"] = nginx_dump()
    return value


def journal(units: list, since: str, limit: int = 3000) -> list:
    argv = ["journalctl", "--no-pager", "-o", "short-iso-precise", "--since", since, "-n", str(limit)]
    for unit in units:
        argv += ["-u", unit]
    done = run(argv, timeout=90, limit=8 << 20)
    return [l[:1200] for l in (done.get("stdout") or "").splitlines() if l.strip() and not l.startswith("-- ")]


def read_window(args: dict) -> dict:
    since = "@" + str(int(args["since_epoch"]))
    units = [PANEL_UNIT, AGENT_UNIT, "nginx.service"] + php_units()
    value = {"since_epoch": int(args["since_epoch"]), "at": utc(), "units": units,
             "by_unit": {u: journal([u], since) for u in units}}
    manager = run(["journalctl", "--no-pager", "-o", "short-iso-precise", "--since", since, "_PID=1", "-n", "2000"], timeout=60)
    value["systemd_manager"] = [l[:600] for l in (manager.get("stdout") or "").splitlines()
                                if l.strip() and not l.startswith("-- ") and any(u.split(".service")[0] in l for u in units)]
    return value


def read_http(args: dict) -> dict:
    return s4.read_http(args)


# -- owner actions ------------------------------------------------------------------------------------------------

def write_in_place(path: str, data: bytes) -> None:
    """As an editor that saves into the same file (same inode): truncate and write, then fsync."""
    with open(path, "r+b") as stream:
        stream.seek(0)
        stream.write(data)
        stream.truncate()
        stream.flush()
        os.fsync(stream.fileno())


def nginx_reload(reload: bool) -> dict:
    test = s4.nginx_test()
    value = {"nginx_test": test}
    if reload and test.get("returncode") == 0:
        value["reload"] = run(["systemctl", "reload", "nginx.service"], timeout=90)
    elif reload:
        value["reload"] = {"not_run": "nginx -t refused the configuration"}
    return value


def owner_vhost(args: dict) -> dict:
    domain = rid.domain_name(args["domain"])
    action = args["action"]
    path, link = f"{AVAILABLE}/{domain}.conf", f"{ENABLED}/{domain}.conf"
    before = {"vhost": file_record(path), "enabled": file_record(link, text=False)}
    value = {"action": "owner-vhost", "what": action, "domain": domain, "before": before}
    if action in ("append-location", "change-index", "replace", "add-include"):
        text = Path(path).read_text()
        if action == "append-location":
            added = ["", "    # set8: added by the server owner by hand", "    location /owner-extra/ {",
                     "        return 200 \"owner\";", "    }"]
            new = insert_before_block_end(text, domain, added)
            value["added_lines"] = added
        elif action == "change-index":
            new, old_line, new_line = change_index(text)
            value["changed"] = {"from": old_line, "to": new_line}
        elif action == "replace":
            new = ("# set8: the server owner's own vhost for %s, written by hand\n"
                   "server {\n    listen 80;\n    listen [::]:80;\n    server_name %s;\n"
                   "    location / {\n        return 200 \"owner-replaced\\n\";\n    }\n}\n") % (domain, domain)
        else:
            os.makedirs(OWNER_DIR, exist_ok=True)
            include = f"{OWNER_DIR}/{domain}.conf"
            with open(include, "w") as stream:
                stream.write("# set8: the server owner's own additions for %s\nlocation /owner-include/ {\n"
                             "    return 200 \"owner-include\";\n}\n" % domain)
            os.chmod(include, 0o644)
            added = ["", "    # set8: the server owner's own include", f"    include {include};"]
            new = insert_before_block_end(text, domain, added)
            value["added_lines"] = added
            value["include_file"] = file_record(include)
        write_in_place(path, new.encode())
    elif action == "remove":
        for target in (link, path):
            if os.path.lexists(target):
                os.unlink(target)
    elif action == "chattr-plus-i":
        value["chattr"] = run(["chattr", "+i", path], timeout=20)
    elif action == "chattr-minus-i":
        value["chattr"] = run(["chattr", "-i", path], timeout=20)
    else:
        raise Refused("unknown owner action")
    value["after"] = {"vhost": file_record(path), "enabled": file_record(link, text=False)}
    value.update(nginx_reload(bool(args.get("reload"))))
    value["at"] = utc()
    value["epoch"] = time.time()
    return value


def owner_nginx_reload(args: dict) -> dict:
    """The owner (or a package's logrotate hook) reloads nginx: ``nginx -t`` first, then ``systemctl reload``."""
    value = {"action": "owner-nginx-reload", "before": nginx_workers()}
    value.update(nginx_reload(True))
    time.sleep(1.5)
    value["after"] = nginx_workers()
    value["at"] = utc()
    return value


def owner_files(args: dict) -> dict:
    """The owner uploads set4's PHP probe and static file, an index page of their own and a page that prints PHP's
    memory_limit, into the site's document root as the site's account."""
    probe = s4.owner_php_probe(args)
    identity, root, uid, gid = rid._site(args)
    rid._write(os.path.join(root, OWNER_INDEX), OWNER_INDEX_TEXT, uid, gid)
    rid._write(os.path.join(root, INI_PROBE), INI_PROBE_TEXT, uid, gid)
    return {"action": "owner-files", "domain": identity, "document_root": root, "site_account": probe.get("site_account"),
            "docroot_entries": sorted(os.listdir(root)), "at": utc()}


def owner_pool(args: dict) -> dict:
    """The owner adds one directive and one comment to the site's PHP-FPM pool file, tests PHP-FPM's configuration
    and reloads the PHP-FPM unit."""
    site_id = int(args["site_id"])
    pool = pool_of(site_id)
    if not pool or not pool.get("exists"):
        raise Refused("the site's pool file is not there")
    path = pool["path"]
    value = {"action": "owner-pool", "path": path, "before": pool}
    lines = ["; set8: added by the server owner by hand", "php_admin_value[memory_limit] = 193M"]
    text = Path(path).read_text()
    write_in_place(path, (text.rstrip("\n") + "\n" + "\n".join(lines) + "\n").encode())
    value["added_lines"] = lines
    value["after"] = pool_of(site_id)
    value.update(php_reload())
    value["at"] = utc()
    return value


def php_reload() -> dict:
    units = [u for u in php_units() if s4.unit_show(u).get("ActiveState") == "active"]
    programs = sorted(set(glob.glob("/usr/sbin/php-fpm*") + glob.glob("/usr/bin/php-fpm*")))
    test = run([programs[0], "-t"], timeout=60) if programs else {"status": "unavailable"}
    value = {"php_fpm_program": programs[0] if programs else None, "php_fpm_test": test, "php_units": units}
    if test.get("returncode") == 0 and units:
        value["reload"] = run(["systemctl", "reload", units[0]], timeout=90)
    return value


def owner_panel(args: dict) -> dict:
    """The owner stops, starts or restarts the Panel's unit by hand. After a start or restart: wait until the unit is
    active and the Panel's journal has its start-time vhost line since the action (or ``wait`` seconds pass)."""
    action = args["action"]
    if action not in ("stop", "start", "restart"):
        raise Refused("unknown action")
    started = time.time()
    since = "@" + str(int(started) - 1)
    value = {"action": "owner-panel", "what": action, "epoch": started, "at": utc(), "before": s4.unit_show(PANEL_UNIT)}
    value["systemctl"] = run(["systemctl", action, PANEL_UNIT], timeout=180)
    lines = []
    if action != "stop":
        deadline = started + float(args.get("wait", 150))
        while time.time() < deadline:
            lines = [l for l in journal([PANEL_UNIT], since) if any(m in l for m in RECONCILE_LINES)]
            if lines and s4.unit_show(PANEL_UNIT).get("ActiveState") == "active":
                break
            time.sleep(2)
        value["waited_seconds"] = round(time.time() - started, 1)
    value["reconcile_lines_seen"] = lines
    value["after"] = s4.unit_show(PANEL_UNIT)
    value["is_active"] = (run(["systemctl", "is-active", PANEL_UNIT], timeout=20).get("stdout") or "").strip()
    value["finished_at"] = utc()
    return value


def lab_restore(args: dict) -> dict:
    """Lab preparation between scenarios, not an owner action: the vhost is put back to the bytes recorded right after
    the Panel last wrote with no owner edit (with that file's recorded owner, group and mode; a new file renamed into
    place as the Panel writes it), the
    enabled link to it is put back, any ``chattr +i`` is cleared, then ``nginx -t`` and a reload. Optionally the pool
    file gets its recorded bytes back (in place, with PHP-FPM tested and reloaded)."""
    domain = rid.domain_name(args["domain"])
    path, link = f"{AVAILABLE}/{domain}.conf", f"{ENABLED}/{domain}.conf"
    data = base64.b64decode(args["content_b64"])
    value = {"action": "lab-restore", "domain": domain, "before": {"vhost": file_record(path, text=False),
                                                                   "enabled": file_record(link, text=False)}}
    if os.path.exists(path):
        value["chattr"] = run(["chattr", "-i", path], timeout=20)
    temporary = path + ".set8-restore"
    with open(temporary, "wb") as stream:
        stream.write(data)
        stream.flush()
        os.fsync(stream.fileno())
    os.chown(temporary, int(args.get("uid", 0)), int(args.get("gid", 0)))
    os.chmod(temporary, int(str(args.get("file_mode") or "0644"), 8))
    os.rename(temporary, path)
    if os.path.lexists(link) and not (os.path.islink(link) and os.readlink(link) == path):
        os.unlink(link)
    if not os.path.lexists(link):
        os.symlink(path, link)
    if args.get("pool_path") and args.get("pool_b64"):
        pool_path = args["pool_path"]
        if not any(fnmatch.fnmatch(pool_path, pattern) for pattern in POOL_GLOBS) or not os.path.isfile(pool_path):
            raise Refused("not a site pool path")
        write_in_place(pool_path, base64.b64decode(args["pool_b64"]))
        value["pool"] = file_record(pool_path)
        value.update(php_reload())
    value["after"] = {"vhost": file_record(path, text=False), "enabled": file_record(link, text=False)}
    value["sha256_matches"] = value["after"]["vhost"].get("sha256") == hashlib.sha256(data).hexdigest()
    value.update(nginx_reload(True))
    value["at"] = utc()
    return value


MODES = {"read-site": read_site, "read-window": read_window, "read-http": read_http, "owner-vhost": owner_vhost,
         "owner-nginx-reload": owner_nginx_reload, "owner-files": owner_files, "owner-pool": owner_pool,
         "owner-panel": owner_panel, "lab-restore": lab_restore}


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
