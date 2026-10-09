# set3: guest_request_identity_native.py - fixtures with a real mailbox password, hostile archives, the readers of S1,
# P4, P5 and O14.
ROOT = r'C:\CELIKBROS PROJECTS\celikpanel\deploy\e2e\release-recovery' + '\\'


class Patch:
    def __init__(self, name):
        self.path = ROOT + name
        self.s = open(self.path, encoding='utf-8', newline='').read()
        assert '\r\n' not in self.s, name

    def rep(self, a, b, n=1):
        assert self.s.count(a) == n, (a[:90], self.s.count(a))
        self.s = self.s.replace(a, b)

    def save(self):
        open(self.path, 'w', encoding='utf-8', newline='').write(self.s)


h = Patch('guest_request_identity_native.py')
h.rep('''import importlib.util
import io
''', '''import http.client
import imaplib
import importlib.util
import io
''')
h.rep('''import shutil
import sqlite3
''', '''import secrets as secrets_module
import shutil
import sqlite3
import ssl
''')
h.rep('''SEED_TABLE = "set2_rows"
''', '''SEED_TABLE = "set2_rows"
# set3: the same shapes owner_update_trial.HASH_SHAPED redacts at collection time (pinned equal by the offline test).
HASH_SHAPED = (
    r"(?:\\{[A-Z][A-Z0-9.-]{1,24}\\})?\\$(?:1|2[abxy]?|5|6|7|y|gy|sha1|argon2(?:id|i|d)|scrypt|pbkdf2(?:-sha(?:1|256|512))?)"
    r"\\$[./A-Za-z0-9$=,+-]{8,}"
    r"|\\{(?:SSHA(?:256|512)?|SHA(?:256|512)?|SMD5|PLAIN|CRYPT|CRAM-MD5|[A-Z0-9]+-CRYPT|ARGON2ID?|PBKDF2)\\}[^\\s\\"'<>\\\\]{4,}"
    r"|SCRAM-SHA-256\\$\\d+:[A-Za-z0-9+/=]+\\$[A-Za-z0-9+/=]+:[A-Za-z0-9+/=]+"
    r"|(?<![0-9A-Za-z])\\*[0-9A-F]{40}(?![0-9A-Fa-f])")
HASH_SHAPED_BYTES = re.compile(HASH_SHAPED.encode())
ESCAPE_PREFIX = "set3-escape"
ESCAPE_ROOTS = ("/var/www", "/var/lib", "/var/tmp", "/var/backups", "/etc", "/tmp", "/home", "/root", "/srv", "/opt", "/usr/local")
HOSTILE_KINDS = ("dotdot", "absolute", "symlink")
PHP_PROBE = "set3-probe.php"
PHP_PROBE_MARK = "set3-php-executed"
ADDRESS_RE = re.compile(r"[a-z0-9][a-z0-9._-]{0,63}@(?=.{1,253}\\Z)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\\.)+[a-z]{2,63}\\Z")
''')
h.rep('''def cpmove_members(domain: str, user: str, database: str, megabytes: int, sleep_seconds: int, rows: int,
                   random_bytes=os.urandom) -> list:''', '''def cpmove_members(domain: str, user: str, database: str, megabytes: int, sleep_seconds: int, rows: int,
                   random_bytes=os.urandom, shadow_hash: bytes | None = None) -> list:''')
h.rep('''        (f"{top}/homedir/etc/{domain}/shadow",
         b"info:$6$set2fixture$" + hashlib.sha512((domain + user).encode()).hexdigest()[:86].encode() + b":19000::::::\\n"),''',
      '''        # set3: with ``shadow_hash`` the mailbox carries the crypt hash of a password the lab knows, so that the
        # imported mailbox can be asked to authenticate with it; without it the fabricated value of set2.
        (f"{top}/homedir/etc/{domain}/shadow",
         b"info:" + (shadow_hash or b"$6$set2fixture$" + hashlib.sha512((domain + user).encode()).hexdigest()[:86].encode())
         + b":19000::::::\\n"),''')
h.rep('''def cpmove_archive(members: list, public_html_directory_member: bool = True) -> bytes:''',
      '''def hostile_members(top: str, kind: str) -> list:
    """set3: members an import must never extract: a ``..`` path out of the document root, an absolute path, a
    symbolic link out of the document root with a file named through it. (name, data or link target, tar type)."""
    payload = f"{top}/homedir/public_html"
    note = ("hostile member of the set3 fixture: " + kind + "\\n").encode()
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


def cpmove_archive(members: list, public_html_directory_member: bool = True, hostile: str | None = None) -> bytes:''')
h.rep('''                info = tarfile.TarInfo(name)
                info.size, info.mode, info.mtime = len(data), 0o644, 1760000000
                archive.addfile(info, io.BytesIO(data))
    return buffer.getvalue()''', '''                info = tarfile.TarInfo(name)
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
    return buffer.getvalue()''')
# read_identities: hash-shaped values per row
h.rep('''            shape["sensitive_words_found"] = sorted(word.decode() for word in generic if body and word in body)''',
      '''            shape["sensitive_words_found"] = sorted(word.decode() for word in generic if body and word in body)
            # set3 (S1): hash-shaped values in the stored answer and in the row's other text columns (counts only).
            shape["hash_shaped_values_found"] = len(HASH_SHAPED_BYTES.findall(body or b""))
            shape["hash_shaped_values_in_other_columns"] = sum(
                len(HASH_SHAPED_BYTES.findall(str(row[column]).encode())) for column in ("id", "method", "route", "request_sha256",
                                                                                      "status", "response_content_type"))''')
h.rep('''            "rows_holding_a_sensitive_value_of_this_run": [r["id"] for r in rows if r["body"]["sensitive_values_of_this_run_found"]],''',
      '''            "rows_holding_a_sensitive_value_of_this_run": [r["id"] for r in rows if r["body"]["sensitive_values_of_this_run_found"]],
            "rows_holding_a_hash_shaped_value": [r["id"] for r in rows if r["body"]["hash_shaped_values_found"]
                                                 or r["body"]["hash_shaped_values_in_other_columns"]],''')
# _tree_digest: per-file list and non-regular entries
h.rep('''    digest, files, total, newest = hashlib.sha256(), 0, 0, 0
    names = []''', '''    digest, files, total, newest = hashlib.sha256(), 0, 0, 0
    names, file_list, non_regular = [], {}, []''')
h.rep('''                if not stat.S_ISREG(info.st_mode):
                    digest.update(("L " + relative + "\\n").encode())
                    continue''', '''                if not stat.S_ISREG(info.st_mode):
                    digest.update(("L " + relative + "\\n").encode())
                    non_regular.append(relative)
                    continue''')
h.rep('''            if len(names) < 40:
                names.append(relative)
    info = os.lstat(root)
    return {"root": root, "exists": True, "files": files, "bytes": total, "sha256": digest.hexdigest(), "names": names,''',
      '''            if len(names) < 40:
                names.append(relative)
            if len(file_list) < 300:
                file_list[relative] = [info.st_size, inner.hexdigest()]
        for name in subdirectories:
            if os.path.islink(os.path.join(directory, name)):
                non_regular.append(os.path.relpath(os.path.join(directory, name), root))
    info = os.lstat(root)
    return {"root": root, "exists": True, "files": files, "bytes": total, "sha256": digest.hexdigest(), "names": names,
            "file_list": file_list, "non_regular": non_regular,''')
# read_imported: site home entries and escapes
h.rep('''    if domain_id:
        result["docroot"] = _tree_digest(document_root(rows[0]["subscription_id"], domain_id))''',
      '''    if domain_id:
        root = document_root(rows[0]["subscription_id"], domain_id)
        result["docroot"] = _tree_digest(root)
        home = os.path.dirname(root)
        result["site_home_entries"] = sorted(os.listdir(home)) if os.path.isdir(home) else []
    result["escapes"] = find_escapes()''')
h.rep('''def read_clock(args: dict) -> dict:
    return {"epoch": time.time(), "at": utc()}
''', '''def read_clock(args: dict) -> dict:
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
    kind = "socket" if stat.S_ISSOCK(info.st_mode) else "directory" if stat.S_ISDIR(info.st_mode) else \\
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
                    if re.match(re.escape(key) + r"\\s*=", text):
                        keep[key] = text.split("=", 1)[1].strip()
            pools.append(dict(_stat(path), directory=directory, settings=keep))
            if str(keep.get("listen", "")).startswith("/"):
                sockets[keep["listen"]] = _stat(keep["listen"])
    program = shutil.which("php-fpm", path=ENV["PATH"]) or next(
        (shutil.which(name, path=ENV["PATH"]) for name in ("php-fpm8.4", "php-fpm8.3", "php-fpm8.2", "php-fpm8.5") if shutil.which(name, path=ENV["PATH"])), None)
    return {"units": shown, "pools": pools, "sockets": sockets,
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
    try:
        connection = http.client.HTTPConnection("127.0.0.1", 80, timeout=20)
        connection.request("GET", path, headers={"Host": host, "Connection": "close"})
        response = connection.getresponse()
        body = response.read(65536)
        headers = {k.lower(): v for k, v in response.getheaders() if k.lower() in ("content-type", "server", "x-powered-by")}
        connection.close()
    except Exception as exc:  # noqa: BLE001 - reported as data
        return {"host": host, "path": path, "error": type(exc).__name__ + ": " + str(exc)[:200], "at": utc()}
    return {"host": host, "path": path, "status": response.status, "headers": headers, "bytes": len(body),
            "body": body[:600].decode("utf-8", "replace"), "at": utc()}


def owner_php_probe(args: dict) -> dict:
    """The owner uploads one PHP page to the site: it prints a marker, a product only PHP can compute, the PHP
    version, the server API and the account it runs as. Served as text it would show its source instead."""
    identity, root, uid, gid = _site(args)
    source = ("<?php\\nheader('Content-Type: text/plain');\\n"
              "echo '" + PHP_PROBE_MARK + ":' . (6 * 7) . ':' . PHP_VERSION . ':' . php_sapi_name() . ':' . "
              "(function_exists('posix_geteuid') ? posix_getpwuid(posix_geteuid())['name'] : get_current_user()) . \\"\\\\n\\";\\n")
    _write(os.path.join(root, PHP_PROBE), source.encode(), uid, gid)
    return {"action": "owner-php-probe", "domain": identity, "path": os.path.join(root, PHP_PROBE),
            "docroot": _tree_digest(root), "site_account": pwd.getpwuid(uid).pw_name, "at": utc()}
''')
h.rep('''    members = cpmove_members(domain, user, database, max(0, min(int(args.get("megabytes", 0)), 512)),
                             max(0, min(int(args.get("sleep_seconds", 0)), 120)), max(1, min(int(args.get("rows", 200)), 50000)))
    with_member = bool(args.get("public_html_directory_member", True))
    data = cpmove_archive(members, with_member)''', '''    shadow_hash, mailbox = None, "the fabricated value of set2 (the hash of no password)"
    if args.get("password_b64"):
        # set3 (S1): the crypt hash of a password the lab chose, made by the guest's own openssl; neither is recorded.
        made = subprocess.run(["openssl", "passwd", "-6", "-stdin"], input=base64.b64decode(args["password_b64"], validate=True) + b"\\n",
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
    data = cpmove_archive(members, with_member, hostile)''')
h.rep('''            "members": [{"name": name, "bytes": len(content)} for name, content in members],
            "directory": {"mode": "%04o" % stat.S_IMODE(info.st_mode), "uid": info.st_uid},''',
      '''            "members": [{"name": name, "bytes": len(content)} for name, content in members],
            "hostile": hostile, "hostile_members": [{"name": name, "type": "symlink -> " + data if kind == tarfile.SYMTYPE else "file"}
                                                    for name, data, kind in (hostile_members("cpmove-" + user, hostile) if hostile else [])],
            "docroot_expected": payload_files(members), "mailbox_shadow": mailbox,
            "directory": {"mode": "%04o" % stat.S_IMODE(info.st_mode), "uid": info.st_uid},''')
h.rep('''    "read-imported": read_imported, "read-clock": read_clock,''',
      '''    "read-imported": read_imported, "read-clock": read_clock,
    "read-mail-login": read_mail_login, "read-engine-versions": read_engine_versions, "read-php": read_php,
    "read-http": read_http, "owner-php-probe": owner_php_probe,''')
h.rep('''JOURNAL_UNITS = ("celikpanel-panel.service", "celikpanel-agent.service", "nginx.service", "mariadb.service",
                 "wg-quick@wg0.service")''', '''JOURNAL_UNITS = ("celikpanel-panel.service", "celikpanel-agent.service", "nginx.service", "mariadb.service",
                 "wg-quick@wg0.service", "dovecot.service", "php-fpm.service")''')
h.save()
print('ok')
