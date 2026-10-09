#!/usr/bin/env python3
"""set6: the release candidate (commit 72b879eea) installed FRESH and measured, the Panel's own
``Strict-Transport-Security`` header read on the guest, and one good update per platform from the published
v0.1.0-alpha.81, on disposable QEMU guests (lab.py).

Two cell kinds, built on the drivers of set4, set4b and set5 and changing none of them:

``set6-arch`` | ``set6-debian13`` | ``set6-ubuntu`` (``Set6Trial`` over ``set4b_trial.Set4bTrial``, itself
``set4_trial.Set4Trial``): the candidate is installed fresh and the owner's setup is run as in set4; then set4's
sections in set4's order, with set4b's form of the import section (what the answer lists):

  M0  the platform, the name pinning as set4 reads it, the nginx package's PHP snippet
  B   the header reading (below), once after M0 and once after the last section
  M1  a PHP site: created, ``nginx -t``, executed as the site's account, a missing script, PATH_INFO, the recorded
      PHP version and socket, deleted with the site's home really checked
  M6  a new site whose configuration the web server refuses
  M2  the cPanel-archive import, with ``imported`` / ``not_imported`` / ``left_out`` and each step's ``state``
  M5  an archive with a member named by an absolute path (set4's section, its two lists judged by the rule of the
      candidate, which lists a step that imported nothing on purpose under `left_out`)
  M10 Postfix stopped through the Panel while ``postfix check`` refuses ``main.cf`` (Debian, Ubuntu)
  M4  Reload of a stopped nginx, MariaDB and PostgreSQL

``upd1-arch-good`` | ``upd1-debian13-good`` | ``upd1-ubuntu-good`` (``Set6UpdateTrial`` over
``set5_trial.Set5UpdateTrial``): set5's good-update cells with the header reading added before the update (the
published alpha.81 answers) and after it (the candidate answers). The other seven cells of set5 are not offered.

Added to every cell of both kinds:

  set6-name-pinning (the first step, before ``preflight``)
      Lab preparation, not an owner action. The release origin's name (``celikpanel.net``) and the four
      certificate-authority directory names set5 pinned are written to this guest's hosts file as its own
      loopback before anything of the product is on the guest, and read back twice: with ``getent -s files``
      (the hosts file only) and, after that reading showed loopback only, with plain ``getent`` (the default
      lookup path of the guest's nsswitch.conf, which on Arch asks ``resolve`` before ``files``). Where
      systemd-resolved runs, its transaction counter is read before and after the default-path lookups.
      ``preflight`` and everything after it run only if every reading shows this guest's loopback only.
      The fixture origin's own provisioning (worker_fixture_origin.py) keeps the origin line instead of writing
      its own; ``preflight`` is told that this one line is the lab's.
  set6-name-pinning-at-the-end (the last measuring step)
      Read only: the same names by both paths, the boot id, certbot's directories, kernel and package versions.

B, the header reading (``header_reading``): on the guest, as root, from the running Panel on its own loopback
port. Over HTTPS: the SPA root, an unknown page (the SPA's fallback), one fingerprinted asset, a public API
route, two API routes with the owner's session, two without it (401), a POST without an Origin (403, refused by
the cross-origin rule before any handler), an unknown API route with the session (404), an OPTIONS preflight.
Over plain HTTP: every TCP port the Panel's process listens on, and port 80 (which is the web server's, not the
Panel's). Every answer is recorded with its status line and all its headers; cookie values are replaced on the
guest. The owner's session cookie reaches the guest inside the script text on the SSH channel's input, never in
an argument list, and is never printed.

Collection: ``set6_redact`` (set5's token-digest rules, and the same rules inside base64 text) is put in front of
the driver's redactor; before the result is written every file of the run is passed through it once more.

The driver calls only the Panel's HTTP API; every native fact is read over SSH. No certificate authority and no
licence service is contacted by the driver. Every result carries ``native_evidence: false``.

  set6_trial.py plan --cell set6-arch --artifacts A.json --work-root /var/tmp/cp-release-drill-X [--dry-run]
  set6_trial.py run  --cell set6-arch --artifacts A.json --work-root /var/tmp/cp-release-drill-X --execute
"""
from __future__ import annotations

import argparse
import dataclasses
import json
from pathlib import Path
import shlex
import sys
from typing import Any

HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    sys.path.insert(0, str(HERE))
import set4b_trial as s4b  # noqa: E402
import set5_trial as s5  # noqa: E402
import set6_redact  # noqa: E402

s4 = s4b.s4
sw, base = s4.sw, s4.base
CELL_KIND = "set6-fresh-install"
UPDATE_CELL_KIND = "set6-good-update"
ORIGIN_NAME = "celikpanel.net"
CA_NAMES = s5.CA_NAMES
PINNED_NAMES = (ORIGIN_NAME,) + tuple(CA_NAMES)
# worker_fixture_origin.LAB_PRE_PIN_LINE (test_set6_trial pins the equality): IPv4 only, as the fixture's own line,
# because the fixture origin listens on 127.0.0.1:443 and the origin step's verdict accepts 127.0.0.1 only.
ORIGIN_PIN_LINE = "127.0.0.1 celikpanel.net # disposable CelikPanel lab pin before provisioning"
PIN_MARK = ("# set6 lab pin (disposable guest): the release origin's name and the certificate-authority directory "
            "names answer on this guest's own loopback")
PIN_STEP, PIN_END_STEP = "set6-name-pinning", "set6-name-pinning-at-the-end"
HEADERS_BEFORE_STEP, HEADERS_AFTER_STEP = "set6-headers-before-the-update", "set6-headers-after-the-update"
LOOPBACK = ("127.0.0.1", "::1")
HSTS = "Strict-Transport-Security"
HSTS_CANDIDATE = "max-age=31536000"                       # cmd/panel/security.go at 72b879eea (D-030)
HSTS_ALPHA81 = "max-age=31536000; includeSubDomains"      # cmd/panel/security.go at v0.1.0-alpha.81
SESSION_COOKIE = "celikpanel_session"
PANEL_PORT = 2083

SECTIONS = (
    ("M0-prepare", "the platform, the name pinning as set4 reads it, the nginx package's PHP snippet", None),
    ("B-headers-after-setup", "the Panel's own response headers, read on the guest (Strict-Transport-Security)", None),
    ("M1-php-site", "a PHP site: created, nginx -t, executed as the site's account, PATH_INFO, a missing script, the "
                    "recorded PHP version and socket, deleted", None),
    ("M6-site-refused", "a new site whose configuration the web server refuses", None),
    ("M2-import", "the cPanel-archive import of set3's fixture: the imported site served, and what the answer lists", None),
    ("M5-import-absolute", "an archive with a member named by an absolute path", None),
    ("M10-postfix-stop", "Postfix stopped through the Panel while `postfix check` refuses main.cf", "mail"),
    ("M4-reload-stopped", "Reload of a stopped nginx, MariaDB and PostgreSQL", None),
    ("B-headers-at-the-end", "the Panel's own response headers again, after every section", None),
)
CELLS = {
    "set6-arch": sw.SettingsCell("set6-arch", "arch", "web", sw.WEB_PRESET + ("postgresql",), False),
    "set6-debian13": sw.SettingsCell("set6-debian13", "debian13", "web_mail", sw.MAIL_PRESET + ("postgresql",), True),
    "set6-ubuntu": sw.SettingsCell("set6-ubuntu", "ubuntu", "web_mail", sw.MAIL_PRESET + ("postgresql",), True),
}
UPDATE_CELLS = ("upd1-arch-good", "upd1-debian13-good", "upd1-ubuntu-good")

# One guest script for both pin readings; `apply` appends the lines first. It prints one JSON object and no secret.
PIN_SCRIPT = r'''
import hashlib, json, os, subprocess, sys, time
apply = sys.argv[1] == "apply"
names = json.loads(sys.argv[2]); mark = sys.argv[3]; origin = sys.argv[4]; origin_line = sys.argv[5]
settle = float(sys.argv[6])
hosts = "/etc/hosts"
LOOP = ("127.0.0.1", "::1")
def mapped(text, name):
    return [l.strip() for l in text.splitlines() if name in l.split("#", 1)[0].split()[1:]]
def run(argv, timeout=20):
    try:
        done = subprocess.run(argv, capture_output=True, text=True, timeout=timeout)
        return {"returncode": done.returncode, "stdout": done.stdout.strip(), "stderr": done.stderr.strip()[:400]}
    except (OSError, subprocess.SubprocessError) as exc:
        return {"returncode": None, "stdout": "", "error": type(exc).__name__}
def addresses(answer):
    return [l.split()[0] for l in (answer.get("stdout") or "").splitlines() if l.split()]
def only_loopback(reading):
    found = addresses(reading["ahosts"]) + addresses(reading["hosts"])
    return bool(addresses(reading["ahosts"])) and bool(addresses(reading["hosts"])) and all(a in LOOP for a in found)
before = open(hosts, "rb").read()
every = [origin] + names
out = {"mode": sys.argv[1], "hosts_sha256_before": hashlib.sha256(before).hexdigest(),
       "mapped_before": {n: mapped(before.decode("utf-8", "replace"), n) for n in every}}
out["product_paths_present"] = [p for p in ("/opt/celikpanel", "/etc/celikpanel", "/var/lib/celikpanel", "/usr/libexec/celikpanel") if os.path.exists(p)]
if apply:
    if any(out["mapped_before"][n] for n in every):
        out["refused"] = "a name is already mapped; the hosts file is not written twice"
    elif out["product_paths_present"]:
        out["refused"] = "a product path exists already; the names are pinned before the product or not at all"
    else:
        with open(hosts, "ab") as stream:
            stream.write(("\n" + mark + "\n" + origin_line + "\n" + "".join("127.0.0.1 %s\n::1 %s\n" % (n, n) for n in names)).encode())
            stream.flush(); os.fsync(stream.fileno())
        out["appended_lines"] = 2 + 2 * len(names)
        out["written_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
text = open(hosts, "rb").read()
out["hosts_sha256_after"] = hashlib.sha256(text).hexdigest()
out["hosts_lines"] = [l for l in text.decode("utf-8", "replace").splitlines() if l.strip() and not l.lstrip().startswith("#")]
out["hosts_mode"] = oct(os.stat(hosts).st_mode & 0o7777)
out["asked_with"] = "getent -s files ahosts NAME and getent -s files hosts NAME (the hosts file only; no DNS question is sent)"
out["resolves"] = {n: {"ahosts": run(["getent", "-s", "files", "ahosts", n]), "hosts": run(["getent", "-s", "files", "hosts", n])} for n in every}
out["files_loopback_only"] = {n: only_loopback(out["resolves"][n]) for n in every}
try:
    out["nsswitch_hosts"] = [l.strip() for l in open("/etc/nsswitch.conf") if l.startswith("hosts:")]
except OSError as exc:
    out["nsswitch_hosts"] = type(exc).__name__
try:
    out["resolv_conf"] = [l.strip() for l in open("/etc/resolv.conf") if l.strip() and not l.lstrip().startswith("#")][:12]
    out["resolv_conf_target"] = os.path.realpath("/etc/resolv.conf")
except OSError as exc:
    out["resolv_conf"] = type(exc).__name__
out["systemd_resolved"] = run(["systemctl", "is-active", "systemd-resolved.service"])["stdout"]
def statistics():
    if not os.path.exists("/usr/bin/resolvectl"):
        return None
    return run(["resolvectl", "statistics"])
# The default lookup path is asked only after the hosts file alone answered loopback for every name, and (after a
# write) only after systemd-resolved's own re-check interval for /etc/hosts has passed.
if all(out["files_loopback_only"].values()):
    if apply and settle > 0:
        time.sleep(settle)
    out["default_path_settle_seconds"] = settle if apply else 0
    out["resolved_statistics_before_the_default_path"] = statistics()
    out["default_path_asked_with"] = "getent ahosts NAME and getent hosts NAME (the lookup order of this guest's nsswitch.conf)"
    out["default_path"] = {n: {"ahosts": run(["getent", "ahosts", n]), "hosts": run(["getent", "hosts", n])} for n in every}
    out["default_path_loopback_only"] = {n: only_loopback(out["default_path"][n]) for n in every}
    out["resolved_statistics_after_the_default_path"] = statistics()
else:
    out["default_path"] = None
    out["default_path_not_asked"] = "the hosts file alone does not answer this guest's loopback for every name"
out["certbot_program"] = next((p for p in ("/usr/bin/certbot", "/usr/local/bin/certbot", "/snap/bin/certbot") if os.path.exists(p)), None)
logs = []
for directory in ("/var/log/letsencrypt", "/etc/letsencrypt/accounts", "/etc/letsencrypt/live", "/etc/letsencrypt/renewal"):
    try:
        logs.append({"path": directory, "entries": sorted(os.listdir(directory))[:20]})
    except OSError as exc:
        logs.append({"path": directory, "entries": None, "reason": type(exc).__name__})
out["certbot_directories"] = logs
wanted = ["nginx", "nginx-common", "nginx-core", "php-fpm", "php8.4-fpm", "php8.3-fpm", "php", "mariadb-server", "mariadb",
          "postgresql", "postgresql-17", "postgresql-16", "postfix", "dovecot-core", "dovecot", "rspamd", "roundcube",
          "roundcube-core", "roundcubemail", "certbot", "systemd", "openssl", "cronie", "cron"]
found = {}
for name in wanted:
    if os.path.exists("/usr/bin/dpkg-query"):
        done = subprocess.run(["dpkg-query", "-W", "-f", "${db:Status-Abbrev}|${Version}", name], capture_output=True, text=True, timeout=20)
        if done.returncode == 0 and done.stdout.startswith("ii"):
            found[name] = done.stdout.split("|", 1)[1]
    elif os.path.exists("/usr/bin/pacman"):
        done = subprocess.run(["pacman", "-Q", name], capture_output=True, text=True, timeout=20)
        if done.returncode == 0 and len(done.stdout.split()) == 2:
            found[name] = done.stdout.split()[1]
out["packages_installed"] = found
out["kernel"] = os.uname().release
try:
    out["os_release"] = {k: v.strip().strip('"') for k, v in (l.split("=", 1) for l in open("/etc/os-release") if "=" in l)
                         if k in ("PRETTY_NAME", "VERSION_ID", "ID", "BUILD_ID", "VERSION_CODENAME")}
except OSError as exc:
    out["os_release"] = type(exc).__name__
out["boot_id"] = open("/proc/sys/kernel/random/boot_id").read().strip()
out["uptime_seconds"] = float(open("/proc/uptime").read().split()[0])
out["at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
print(json.dumps(out, sort_keys=True))
'''

# The header reading. `COOKIE` is replaced by a string literal on the host; nothing prints it. Set-Cookie values
# are replaced here, on the guest, before anything leaves it.
HEADER_SCRIPT = r'''
import hashlib, http.client, json, os, re, socket, ssl, subprocess, sys, time
COOKIE_NAME = sys.argv[1]; PORT = int(sys.argv[2]); PANEL_HOST = sys.argv[3]
COOKIE = @@COOKIE@@
def run(argv, timeout=20):
    try:
        done = subprocess.run(argv, capture_output=True, text=True, timeout=timeout)
        return done.stdout
    except (OSError, subprocess.SubprocessError) as exc:
        return "(%s)" % type(exc).__name__
def shown(headers):
    return [[n, "[REDACTED]" if n.lower() in ("set-cookie", "cookie", "authorization") else v] for n, v in headers]
def named(headers, name):
    return [v for n, v in headers if n.lower() == name]
leaves = set()
def https(name, method, path, session=False, body=None, extra=None):
    out = {"name": name, "scheme": "https", "host": "127.0.0.1", "port": PORT, "method": method, "path": path,
           "as_logged_in_owner": False, "request_headers": []}
    if session and not COOKIE:
        out["not_sent"] = "the driver holds no session"
        return out
    try:
        context = ssl.create_default_context(); context.check_hostname = False; context.verify_mode = ssl.CERT_NONE
        connection = http.client.HTTPSConnection("127.0.0.1", PORT, timeout=20, context=context)
        headers = {"Host": "127.0.0.1:%d" % PORT, "Accept": "*/*", "User-Agent": "set6-header-reading"}
        headers.update(extra or {})
        out["request_headers"] = [[k, v] for k, v in headers.items()]
        if session:
            headers["Cookie"] = COOKIE_NAME + "=" + COOKIE
            out["as_logged_in_owner"] = True
            out["request_headers"].append(["Cookie", "[REDACTED]"])
        connection.request(method, path, body=body, headers=headers)
        leaves.add(hashlib.sha256(connection.sock.getpeercert(binary_form=True)).hexdigest())
        out["tls_version"] = connection.sock.version()
        response = connection.getresponse()
        data = response.read()
        got = response.getheaders()
        out.update(status=response.status, reason=response.reason, http_version=response.version, headers=shown(got),
                   strict_transport_security=named(got, "strict-transport-security"),
                   body_bytes=len(data), body_sha256=hashlib.sha256(data).hexdigest())
        if response.status >= 400 or name == "api-public":
            out["body_start"] = data[:400].decode("utf-8", "replace")
        if name == "api-version-as-owner" and response.status == 200:
            try:
                body_json = json.loads(data)
                out["panel"] = {k: body_json.get(k) for k in ("version", "commit", "agent_commit", "agent_matches", "schema_version")}
            except ValueError:
                out["panel"] = None
        out["_body"] = data if name == "spa-root" else None
        connection.close()
    except Exception as exc:
        out["error"] = "%s: %s" % (type(exc).__name__, str(exc)[:200])
    return out
def plain(name, host, port, path, host_header):
    """One plain-HTTP request over a raw socket; the answer is kept as the bytes that came back."""
    out = {"name": name, "scheme": "http", "host": host, "port": port, "method": "GET", "path": path,
           "request_headers": [["Host", host_header], ["Accept", "*/*"], ["User-Agent", "set6-header-reading"], ["Connection", "close"]],
           "as_logged_in_owner": False}
    try:
        with socket.create_connection((host, port), timeout=10) as sock:
            sock.sendall(("GET %s HTTP/1.1\r\nHost: %s\r\nAccept: */*\r\nUser-Agent: set6-header-reading\r\nConnection: close\r\n\r\n" % (path, host_header)).encode())
            sock.settimeout(10)
            chunks = []
            while sum(map(len, chunks)) < 65536:
                try:
                    piece = sock.recv(8192)
                except socket.timeout:
                    out["read_timed_out"] = True
                    break
                if not piece:
                    break
                chunks.append(piece)
        raw = b"".join(chunks)
        head, _, body = raw.partition(b"\r\n\r\n")
        lines = head.decode("latin-1").split("\r\n")
        out["status_line"] = lines[0] if raw else None
        match = re.match(r"HTTP/\d\.\d (\d{3})", lines[0]) if raw else None
        out["status"] = int(match.group(1)) if match else None
        got = [[l.split(":", 1)[0].strip(), l.split(":", 1)[1].strip()] for l in lines[1:] if ":" in l]
        out["headers"] = shown(got)
        out["strict_transport_security"] = named(got, "strict-transport-security")
        out["raw_bytes"] = len(raw)
        out["raw_head"] = head.decode("latin-1")[:2000] if not any(n.lower() == "set-cookie" for n, _ in got) else "(not kept raw: a Set-Cookie line)"
        out["body_start"] = body[:300].decode("utf-8", "replace")
        out["header_name_anywhere_in_the_answer"] = b"strict-transport-security" in raw.lower()
        out["answered"] = bool(raw)
    except Exception as exc:
        out["answered"] = False
        out["error"] = "%s: %s" % (type(exc).__name__, str(exc)[:200])
    return out
result = {"at_start": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "boot_id": open("/proc/sys/kernel/random/boot_id").read().strip(),
          "owner_login_held": bool(COOKIE)}
unit = dict(l.split("=", 1) for l in run(["systemctl", "show", "celikpanel-panel.service", "-p", "MainPID", "-p", "ActiveState",
                                         "-p", "SubState", "-p", "ExecMainStartTimestamp"]).splitlines() if "=" in l)
result["panel_unit"] = unit
pid = unit.get("MainPID") or "0"
try:
    result["panel_program"] = os.readlink("/proc/%s/exe" % pid)
except OSError as exc:
    result["panel_program"] = type(exc).__name__
listening = [l for l in run(["ss", "-H", "-ltnp"]).splitlines() if l.strip()]
panel_ports = sorted({int(m.group(1)) for l in listening if ("pid=%s," % pid) in l for m in [re.search(r":(\d+)\s", l)] if m})
result["listening_tcp_of_the_panel_process"] = [l.split()[3] + " " + l.split()[-1] for l in listening if ("pid=%s," % pid) in l]
result["listening_tcp_on_80_443_and_the_panel_port"] = [l.split()[3] + " " + l.split()[-1] for l in listening
                                                        if re.search(r":(80|443|%d)\s" % PORT, l)]
result["panel_ports"] = panel_ports
requests = []
root = https("spa-root", "GET", "/")
body = root.pop("_body", None) or b""
requests.append(root)
requests.append(https("spa-unknown-page", "GET", "/set6-no-such-page"))
asset = re.search(rb'(?:src|href)="(/assets/[A-Za-z0-9._-]+)"', body)
if asset:
    requests.append(https("spa-asset", "GET", asset.group(1).decode()))
requests.append(https("api-public", "GET", "/api/v1/panel/access-address"))
requests.append(https("api-version-as-owner", "GET", "/api/v1/panel/version", session=True))
requests.append(https("api-domains-as-owner", "GET", "/api/v1/domains", session=True))
requests.append(https("api-domains-no-login", "GET", "/api/v1/domains"))
requests.append(https("api-unknown-no-login", "GET", "/api/v1/set6-no-such-route"))
requests.append(https("api-post-without-origin", "POST", "/api/v1/set6-no-such-route", body=b"", extra={"Content-Length": "0"}))
requests.append(https("api-unknown-as-owner", "GET", "/api/v1/set6-no-such-route", session=True))
requests.append(https("api-options-preflight", "OPTIONS", "/api/v1/domains"))
for entry in requests:
    entry.pop("_body", None)
for port in panel_ports or [PORT]:
    requests.append(plain("plain-http-to-the-panel-port-%d" % port, "127.0.0.1", port, "/", "127.0.0.1:%d" % port))
    requests.append(plain("plain-http-to-the-panel-port-%d-api" % port, "127.0.0.1", port, "/api/v1/panel/access-address", "127.0.0.1:%d" % port))
requests.append(plain("plain-http-port-80-panel-host-name", "127.0.0.1", 80, "/", PANEL_HOST))
requests.append(plain("plain-http-port-80-address", "127.0.0.1", 80, "/", "127.0.0.1"))
result["requests"] = requests
result["tls_leaf_sha256"] = sorted(leaves)
result["at_end"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
print(json.dumps(result, sort_keys=True))
'''


# ---------------------------------------------------------------------------
# Pure rules
# ---------------------------------------------------------------------------

def addresses_of(answer: dict | None) -> list:
    return [line.split()[0] for line in str((answer or {}).get("stdout") or "").splitlines() if line.split()]


def loopback_only(reading: dict | None) -> bool:
    """Both databases answered, and every address is this guest's own loopback."""
    reading = reading or {}
    found = addresses_of(reading.get("ahosts")) + addresses_of(reading.get("hosts"))
    return (bool(addresses_of(reading.get("ahosts"))) and bool(addresses_of(reading.get("hosts")))
            and all(address in LOOPBACK for address in found))


def pin_verdict(reading: dict, names: tuple = PINNED_NAMES) -> dict:
    """Per name: the hosts file alone answers loopback only, and so does the guest's default lookup path."""
    files, default = reading.get("resolves") or {}, reading.get("default_path") or {}
    return {name: {"hosts_file": loopback_only(files.get(name)),
                   "default_path": loopback_only(default.get(name)) if reading.get("default_path") is not None else None}
            for name in names}


def pin_holds(verdict: dict) -> bool:
    return bool(verdict) and all(item["hosts_file"] is True and item["default_path"] is True for item in verdict.values())


def resolved_transactions(statistics: dict | None) -> int | None:
    """``Total Transactions`` of ``resolvectl statistics`` (the number of lookups systemd-resolved sent to a DNS
    server or a multicast scope so far), or None when it was not read."""
    import re
    match = re.search(r"Total Transactions:\s*(\d+)", str((statistics or {}).get("stdout") or ""))
    return int(match.group(1)) if match else None


def tolerant_mappings(original, pin_line: str = ORIGIN_PIN_LINE):
    """``owner_update_trial.hosts_mappings`` for the preflight of a guest whose first step pinned the origin name:
    the lab's own one line is not a sign of an earlier install; anything else still is. The lines seen are kept."""
    seen: list = []

    def mappings(hosts_text: str, name: str = ORIGIN_NAME) -> list:
        found = original(hosts_text, name)
        seen[:] = found
        return [] if found and all(line == pin_line for line in found) else found
    mappings.seen = seen
    return mappings


def partial_lists(steps: list) -> tuple:
    """The `imported` and `not_imported` lists an import answer must carry for its steps, by the rule the candidate
    documents (cmd/panel/import_handlers.go at 72b879eea: a step that ended without an error and carries a `state`
    imported nothing and is listed under `left_out`, never under `imported`; `finalize` is not a part).
    ``request_identity_trial.partial_lists`` (set3) predates `left_out` and counts every `ok` step as imported."""
    parts = [s for s in steps or [] if isinstance(s, dict) and s.get("step") != "finalize"]
    return ([s.get("step") for s in parts if s.get("ok") and not s.get("state")],
            [s.get("step") for s in parts if not s.get("ok")])


def left_out_list(steps: list) -> list:
    return [s.get("step") for s in steps or [] if isinstance(s, dict) and s.get("step") != "finalize" and s.get("ok") and s.get("state")]


def header_judgement(reading: dict, expected: str) -> list:
    """The checks of one header reading, as (name, ok, detail). ``ok`` is None where nothing was measured."""
    requests = [r for r in reading.get("requests") or [] if isinstance(r, dict)]
    https = [r for r in requests if r.get("scheme") == "https" and isinstance(r.get("status"), int)]
    not_read = [r.get("name") for r in requests if r.get("scheme") == "https" and not isinstance(r.get("status"), int)]
    plain = [r for r in requests if r.get("scheme") == "http"]
    table = {r["name"]: {"request": f"{r.get('method')} {r.get('path')}", "as_logged_in_owner": r.get("as_logged_in_owner"),
                         "status": r.get("status"), HSTS: r.get("strict_transport_security")} for r in https}
    api = [r for r in https if str(r.get("path", "")).startswith("/api/")]
    covered = {"the SPA root answered 200": any(r["name"] == "spa-root" and r["status"] == 200 for r in https),
               "an API route answered 200": any(r["status"] == 200 for r in api),
               "an API route answered 401 or 403": any(r["status"] in (401, 403) for r in api),
               "a 404": any(r["status"] == 404 for r in https)}
    exact = [r["name"] for r in https if r.get("strict_transport_security") == [expected]]
    other = {r["name"]: r.get("strict_transport_security") for r in https if r.get("strict_transport_security") != [expected]}
    wider = {r["name"]: r.get("strict_transport_security") for r in https
             if any(word in " ".join(r.get("strict_transport_security") or []).lower() for word in ("includesubdomains", "preload"))}
    checks = [
        (f"B: every HTTPS answer of the running Panel carries {HSTS} exactly `{expected}` (one header line, that value)",
         (not other and not not_read) if https else None,
         {"answers_read": len(https), "with_exactly_the_expected_value": len(exact), "other": other, "not_read": not_read,
          "by_request": table}),
        ("B: the readings cover the SPA root (200), an API route answered 200, an API route answered 401 or 403, and a 404",
         True if all(covered.values()) else None, covered),
    ]
    if expected == HSTS_CANDIDATE:
        checks.append((f"B: no HTTPS answer names includeSubDomains or preload in {HSTS}", (not wider) if https else None, wider))
    # A refusal is an outcome too: a plain request counts as measured when it was sent and either bytes came back
    # or the failure to get any is recorded. At least one of them must have gone to a port of the Panel's process.
    attempted = [r for r in plain if r.get("answered") is not None]
    to_the_panel = [r for r in attempted if str(r.get("name", "")).startswith("plain-http-to-the-panel-port")]
    with_header = {r["name"]: r.get("strict_transport_security") for r in plain
                   if r.get("strict_transport_security") or r.get("header_name_anywhere_in_the_answer")}
    checks.append((f"B: no answer to plain HTTP carries {HSTS} (what each plain-HTTP request got is recorded)",
                   (not with_header) if to_the_panel else None,
                   {"plain_requests": {r["name"]: {"to": f"{r.get('host')}:{r.get('port')}", "answered": r.get("answered"),
                                                   "status_line": r.get("status_line"), "error": r.get("error"),
                                                   "server": [v for n, v in r.get("headers") or [] if n.lower() == "server"],
                                                   "location": [v for n, v in r.get("headers") or [] if n.lower() == "location"],
                                                   "body_start": (r.get("body_start") or "")[:120]} for r in plain},
                    "with_the_header": with_header}))
    return checks


def headers_text(reading: dict) -> str:
    """Every answer of a reading as a text: the request line, the status line, every header line."""
    lines = [f"# header reading on the guest, {reading.get('at_start')} to {reading.get('at_end')}; Panel unit "
             f"{json.dumps(reading.get('panel_unit'), sort_keys=True)}; program {reading.get('panel_program')}",
             f"# TCP listeners of the Panel's process: {reading.get('listening_tcp_of_the_panel_process')}",
             f"# TCP listeners on 80, 443 and the Panel's port: {reading.get('listening_tcp_on_80_443_and_the_panel_port')}",
             f"# TLS leaf SHA-256 seen: {reading.get('tls_leaf_sha256')}", ""]
    for r in reading.get("requests") or []:
        lines.append(f"### {r.get('name')}: {r.get('method')} {r.get('scheme')}://{r.get('host')}:{r.get('port')}{r.get('path')}"
                     f" (session cookie sent: {'yes' if r.get('as_logged_in_owner') else 'no'})")
        for name, value in r.get("request_headers") or []:
            lines.append(f"> {name}: {value}")
        if r.get("not_sent"):
            lines.append(f"(not sent: {r['not_sent']})")
        elif r.get("error"):
            lines.append(f"(no answer: {r['error']})")
        elif r.get("scheme") == "https":
            lines.append(f"< HTTP/{'1.1' if r.get('http_version') == 11 else r.get('http_version')} {r.get('status')} {r.get('reason')}")
            lines += [f"< {name}: {value}" for name, value in r.get("headers") or []]
            lines.append(f"(body: {r.get('body_bytes')} bytes, sha256 {r.get('body_sha256')})")
            if r.get("body_start") is not None:
                lines.append("(body starts: " + json.dumps(r["body_start"][:300]) + ")")
        else:
            lines.append(f"< {r.get('status_line')}")
            lines += [f"< {name}: {value}" for name, value in r.get("headers") or []]
            lines.append(f"({r.get('raw_bytes')} bytes came back; body starts: " + json.dumps((r.get("body_start") or "")[:200]) + ")")
        lines.append("")
    return "\n".join(lines) + "\n"


# ---------------------------------------------------------------------------
# What both trial kinds add
# ---------------------------------------------------------------------------

class Lab6:
    """The name pinning of the first step, the preflight that knows it, and the header reading."""

    def pin_reading6(self, mode: str) -> dict:
        script = ("python3 -I - " + shlex.join([mode, json.dumps(list(CA_NAMES)), PIN_MARK, ORIGIN_NAME, ORIGIN_PIN_LINE, "4"])
                  + " <<'CP_SET6_PIN'\n" + PIN_SCRIPT + "\nCP_SET6_PIN\n")
        return json.loads(self.guest(script, timeout=180).stdout)

    def pin_checks(self, reading: dict) -> dict:
        verdict = pin_verdict(reading)
        before, after = (resolved_transactions(reading.get("resolved_statistics_before_the_default_path")),
                         resolved_transactions(reading.get("resolved_statistics_after_the_default_path")))
        return {"names": list(PINNED_NAMES), "loopback_only": verdict,
                "default_path_answers": {name: {k: (v or {}).get("stdout") for k, v in ((reading.get("default_path") or {}).get(name) or {}).items()}
                                         for name in PINNED_NAMES},
                "nsswitch_hosts": reading.get("nsswitch_hosts"), "systemd_resolved": reading.get("systemd_resolved"),
                "resolved_total_transactions": {"before_the_default_path_lookups": before, "after_them": after,
                                                "difference": (after - before) if isinstance(before, int) and isinstance(after, int) else None,
                                                "what_it_is": "systemd-resolved's own counter of lookups it sent to a DNS server; "
                                                              "not a capture of traffic, and None where resolvectl is absent"},
                "product_paths_present": reading.get("product_paths_present"), "boot_id": reading.get("boot_id"),
                "uptime_seconds": reading.get("uptime_seconds"), "at": reading.get("at"),
                "asked_with": [reading.get("asked_with"), reading.get("default_path_asked_with")]}

    def pin_names6(self, checks: dict) -> str:
        """Before anything else is done on the guest: the five names answer on its own loopback."""
        self.lab.process_guard(self.node)
        reading = self.pin_reading6("apply")
        self.record_json("name-pinning.json", reading)
        checks.update(self.pin_checks(reading), what="lab preparation, not an owner action",
                      appended_lines=reading.get("appended_lines"), refused=reading.get("refused"),
                      written_at=reading.get("written_at"), certbot_program=reading.get("certbot_program"),
                      default_path_settle_seconds=reading.get("default_path_settle_seconds"), file="name-pinning.json")
        if reading.get("refused"):
            raise base.StepFailed("the names were not pinned: " + str(reading["refused"]))
        if reading.get("product_paths_present"):
            raise base.StepFailed("the product was on the guest before the names were pinned")
        if not pin_holds(checks["loopback_only"]):
            raise base.StepFailed("a pinned name does not answer on this guest's loopback only: "
                                  + json.dumps(checks["loopback_only"], sort_keys=True))
        return "passed"

    def pin_at_the_end6(self, checks: dict) -> str:
        if self.verdict_of(PIN_STEP) != "passed":
            checks["reason"] = "the names were not pinned at the start; there is nothing to read again"
            return "skipped"
        reading = self.pin_reading6("read")
        self.record_json("name-pinning-at-the-end.json", reading)
        first = next((s for s in self.steps if s["name"] == PIN_STEP), {})
        checks.update(self.pin_checks(reading), boot_id_at_the_pinning=(first.get("checks") or {}).get("boot_id"),
                      certbot_program=reading.get("certbot_program"), certbot_directories=reading.get("certbot_directories"),
                      packages_installed=reading.get("packages_installed"), kernel=reading.get("kernel"),
                      os_release=reading.get("os_release"), hosts_lines=reading.get("hosts_lines"),
                      file="name-pinning-at-the-end.json")
        if not pin_holds(checks["loopback_only"]):
            raise base.StepFailed("a pinned name no longer answers on this guest's loopback only: "
                                  + json.dumps(checks["loopback_only"], sort_keys=True))
        return "passed"

    def preflight_after_the_pin(self, function):
        """The base driver's preflight, unchanged, told that the origin line of the first step is the lab's own."""
        def run(checks: dict) -> str | None:
            mappings = tolerant_mappings(base.hosts_mappings)
            with base.patched(base, "hosts_mappings", mappings):
                try:
                    return function(checks)
                finally:
                    checks["set6_origin_name_lines_in_the_hosts_file"] = list(mappings.seen)
                    checks["set6_note"] = ("`origin_name_before.hosts_mappings` is empty when the only line that maps the name is "
                                           "the lab's own pin of the first step; the lines as read are in the field above")
        return run

    # -- the header reading -------------------------------------------------------------------------------------

    def session_cookie(self) -> str | None:
        """The owner's session as the driver's client holds it; a fresh login when the Panel no longer accepts it."""
        try:
            probe = self.api("GET", "/api/v1/panel/version", purpose="set6: is the owner's session still accepted")
            if probe.status == 401 and getattr(self, "_password", None):
                self.panel_client().login(self.state["username"], self._password)
        except Exception as exc:  # noqa: BLE001 - the reading goes on without a session and says so
            self.state.setdefault("set6_session_notes", []).append(self.redactor.text(f"{type(exc).__name__}: {exc}")[:300])
        cookie = getattr(self.client, "cookie", None)
        if cookie:
            self.redactor.register(cookie)
        return cookie

    def header_reading(self, label: str, expected: str, expected_commit: str | None) -> dict:
        cookie = self.session_cookie()
        script = ("python3 -I - " + shlex.join([SESSION_COOKIE, str(PANEL_PORT), f"panel-{self.node_name}.upd1-infra.test"])
                  + " <<'CP_SET6_HEADERS'\n" + HEADER_SCRIPT.replace("@@COOKIE@@", json.dumps(cookie or ""))
                  + "\nCP_SET6_HEADERS\n")
        reading = json.loads(self.guest(script, timeout=300).stdout)
        for entry in reading.get("requests") or []:
            entry["headers"] = self.redactor.headers([tuple(pair) for pair in entry.get("headers") or []])
        reading["label"] = label
        reading["expected_strict_transport_security"] = expected
        self.record_json(f"headers-{label}.json", reading)
        self.ev.write_text(f"{self.step_dir}/headers-{label}.txt", headers_text(reading))
        panel = next((r.get("panel") for r in reading.get("requests") or [] if r.get("name") == "api-version-as-owner"), None)
        checks = header_judgement(reading, expected)
        checks.append(("B: the Panel that answered is the build this reading is about (the commit its own version route names)",
                       (panel.get("commit") == expected_commit) if isinstance(panel, dict) and expected_commit else None,
                       {"panel": panel, "expected_commit": expected_commit}))
        return {"label": label, "expected": expected, "panel": panel, "panel_unit": reading.get("panel_unit"),
                "panel_ports": reading.get("panel_ports"),
                "listening_tcp_of_the_panel_process": reading.get("listening_tcp_of_the_panel_process"),
                "listening_tcp_on_80_443_and_the_panel_port": reading.get("listening_tcp_on_80_443_and_the_panel_port"),
                "files": [f"headers-{label}.json", f"headers-{label}.txt"], "checks": checks}

    def collect_swept(self) -> None:
        """Before the result is written: every file of the run through the token-digest rules once more, plain and
        inside base64 text."""
        finalize = self.ev.finalize_upd1

        def finalize_swept(result: dict) -> dict:
            report = set6_redact.sweep([self.ev.directory], self.token_digests)
            self.ev.write_json("set6-redaction-sweep.json", report)
            return finalize(result)
        self.ev.finalize_upd1 = finalize_swept


# ---------------------------------------------------------------------------
# Fresh-install cells
# ---------------------------------------------------------------------------

class Set6Trial(Lab6, s4b.Set4bTrial):
    def __init__(self, settings: Any, artifacts: dict, work_root: str, local_port: int) -> None:
        super().__init__(settings, artifacts, work_root, local_port)
        self.redactor, self.token_digests = set6_redact.wrap(self.redactor)
        self.collect_swept()

    def step(self, name: str, function, *, needs: tuple = ()) -> str:
        plain = base.Trial.step
        if name == "preflight":
            plain(self, PIN_STEP, self.pin_names6)
            return plain(self, name, self.preflight_after_the_pin(function), needs=tuple(needs) + (PIN_STEP,))
        return plain(self, name, function, needs=needs)

    def b_headers(self, label: str) -> None:
        record = self.header_reading(label, HSTS_CANDIDATE, self.artifacts["baseline"]["commit"])
        self.current["header_reading"] = {k: v for k, v in record.items() if k != "checks"}
        for name, ok, detail in record["checks"]:
            self.check(name, ok, detail)

    def m5_import_absolute_lists(self) -> None:
        """set4's M5 section, unchanged, judged with the list rule of the candidate (S6-H1: set3's rule counted the
        `dns` step, which ends `ok` with `state: left_to_owner`, as imported), and what the answer lists."""
        with base.patched(s4.rid, "partial_lists", partial_lists):
            self.m5_import_absolute()
        record = self.current.get("absolute") or {}
        applied = [c for c in self.current["calls"] if (c.get("request") or {}).get("path") == "/api/v1/import/cpanel/apply"]
        answer = (applied[-1].get("json") if applied else None) or {}
        steps = [s for s in record.get("steps") or [] if isinstance(s, dict)]
        parts = [s.get("step") for s in steps if s.get("step") != "finalize"]
        lists = [answer.get("imported") or [], answer.get("not_imported") or [], answer.get("left_out") or []]
        self.current["lists"] = {"imported": answer.get("imported"), "not_imported": answer.get("not_imported"),
                                 "left_out": answer.get("left_out"),
                                 "steps": [{k: s.get(k) for k in ("step", "ok", "state", "code")} for s in steps],
                                 "list_rule": "cmd/panel/import_handlers.go at the candidate: not ok -> not_imported; ok with a "
                                              "state -> left_out; ok without a state -> imported; finalize is not a part"}
        self.check("M5 lists: `left_out` names exactly the steps that ended without an error and carry a `state`",
                   isinstance(answer.get("left_out"), list) and answer.get("left_out") == left_out_list(steps),
                   {"left_out": answer.get("left_out"), "from_the_steps": left_out_list(steps)})
        self.check("M5 lists: every step of the answer is in exactly one of `imported`, `not_imported` and `left_out`",
                   bool(parts) and all(sum(part in one for one in lists) == 1 for part in parts),
                   {"steps": parts, "imported": lists[0], "not_imported": lists[1], "left_out": lists[2]})

    def execute(self) -> dict:
        self.step("preflight", self.preflight)
        self.step("origin", self.origin, needs=("preflight",))
        self.step("baseline-install", self.baseline_install, needs=("origin",))
        self.step("owner-login", self.owner_login, needs=("baseline-install",))
        self.step("license", self.license, needs=("owner-login",))
        self.step("setup", self.setup, needs=("license",))
        self.step("site", self.site, needs=("setup",))
        functions = {"M0-prepare": self.m0_prepare, "M1-php-site": self.m1_php_site, "M2-import": self.m2_import_lists,
                     "M5-import-absolute": self.m5_import_absolute_lists, "M10-postfix-stop": self.m10_postfix_stop,
                     "M4-reload-stopped": self.m4_reload_stopped, "M6-site-refused": self.m6_site_refused,
                     "B-headers-after-setup": lambda: self.b_headers("after-setup"),
                     "B-headers-at-the-end": lambda: self.b_headers("at-the-end")}
        for key, title, only in SECTIONS:
            if only == "mail" and not self.settings.mail:
                self.step(key, lambda checks: checks.update(reason="mail is not supported on this platform") or "skipped")
                self.sections[key] = {"title": title, "verdict": "not-run", "reason": "mail is not supported on this platform"}
                continue
            if key == "M0-prepare" or key.startswith("B-headers"):
                # the header reading needs the logged-in owner only; it is read whatever M0 established
                self.section(key, title, functions[key], needs=("site",) if key == "M0-prepare" else ("owner-login",))
            elif self.state.get("subscription_id") is None:
                self.step(key, lambda checks: checks.update(reason="M0 did not establish the subscription") or "not-run")
            else:
                self.section(key, title, functions[key], needs=("site",))
            self.sections.setdefault(key, {"title": title, "verdict": "not-run", "reason": "an earlier step did not pass"})
        self.step(PIN_END_STEP, self.pin_at_the_end6)
        self.step("collect", self.collect)
        self.tunnel.close()
        verdicts = [s["verdict"] for s in self.steps]
        result = {"schema": base.RESULT_SCHEMA, "cell_kind": CELL_KIND, "native_evidence": False,
                  "cell": dataclasses.asdict(self.settings),
                  "identity": {k: self.identity[k] for k in ("cell_id", "node", "vm_uuid")},
                  "request_id": None, "provenance": base.provenance_for("good"),
                  "artifacts": {"baseline": {k: self.artifacts["baseline"][k] for k in ("version", "commit", "sha256")}},
                  "outcome": {"classification": "set6-measured", "final_status": None},
                  "setup": {"purpose": self.settings.purpose, "components": sorted(self.settings.components),
                            "waiting": self.state.get("setup_waiting")},
                  "site": {k: self.state.get(k) for k in ("domain_id", "site_user", "mailbox", "subscription_id")},
                  "sections": self.sections, "findings": self.state["findings"],
                  "set6": {"cell_kind": CELL_KIND, "pinned_names": list(PINNED_NAMES),
                           "expected_strict_transport_security": HSTS_CANDIDATE},
                  "steps": [{k: s.get(k) for k in ("name", "verdict", "reason", "started_at", "finished_at")} for s in self.steps],
                  "overall": base.overall(verdicts),
                  "note": "set6: observations for the owner's review; no update is started and no P0 row is judged."}
        self.step_dir = "result"
        return self.ev.finalize_upd1(result)


# ---------------------------------------------------------------------------
# Good-update cells
# ---------------------------------------------------------------------------

class Set6UpdateTrial(Lab6, s5.Set5UpdateTrial):
    def __init__(self, cell: Any, artifacts: dict, work_root: str, local_port: int) -> None:
        super().__init__(cell, artifacts, work_root, local_port)
        # set5's wrap is in place (self.token_digests); the sweep before the result also looks inside base64 text
        finalize = self.ev.finalize_upd1

        def finalize_swept(result: dict) -> dict:
            result["set6"] = {"cell_kind": UPDATE_CELL_KIND, "pinned_names": list(PINNED_NAMES),
                              "expected_strict_transport_security": {"before_the_update": HSTS_ALPHA81,
                                                                     "after_the_update": HSTS_CANDIDATE}}
            self.ev.write_json("set6-redaction-sweep.json", set6_redact.sweep([self.ev.directory], self.token_digests))
            return finalize(result)
        self.ev.finalize_upd1 = finalize_swept

    def step(self, name: str, function, *, needs: tuple = ()) -> str:
        """set5's added steps in set5's places, with set6's pinning instead of set5's and the two header readings."""
        plain = base.Trial.step
        if name == "preflight":
            plain(self, PIN_STEP, self.pin_names6)
            function, needs = self.preflight_after_the_pin(function), tuple(needs) + (PIN_STEP,)
        if name == "pre-state":
            if self.additions["item9"]:
                plain(self, "set4-php-site-before-the-update", self.php_before, needs=("seed",))
            plain(self, HEADERS_BEFORE_STEP, self.headers_before, needs=("seed",))
        if name == "collect":
            if self.additions["item9"]:
                plain(self, "set4-php-site-after-the-update", self.php_after,
                      needs=("terminal", "set4-php-site-before-the-update"))
            plain(self, HEADERS_AFTER_STEP, self.headers_after)
        verdict = plain(self, name, function, needs=needs)
        if name == "verdicts":
            if self.additions["m10"]:
                self.m10_after_the_update()
            plain(self, PIN_END_STEP, self.pin_at_the_end6)
        return verdict

    def headers_step(self, checks: dict, label: str, expected: str, role: str) -> str:
        record = self.header_reading(label, expected, self.artifacts[role]["commit"])
        judged = [{"name": name, "ok": ok, "detail": detail} for name, ok, detail in record.pop("checks")]
        checks.update(record, checks=judged, expected_build=role)
        failed = [c["name"] for c in judged if c["ok"] is False]
        unknown = [c["name"] for c in judged if c["ok"] is None]
        if failed:
            raise base.StepFailed("; ".join(failed)[:1500])
        if unknown:
            raise base.StepInconclusive("not established: " + "; ".join(unknown)[:1200])
        return "passed"

    def headers_before(self, checks: dict) -> str:
        """The published alpha.81 as installed and set up, before the owner starts the update."""
        return self.headers_step(checks, "before-the-update", HSTS_ALPHA81, "baseline")

    def headers_after(self, checks: dict) -> str:
        """Whatever Panel runs after the update ended; the expected one is the candidate."""
        checks["terminal_verdict"] = self.verdict_of("terminal")
        return self.headers_step(checks, "after-the-update", HSTS_CANDIDATE, self.role)


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def build_plan(name: str, artifacts: dict, work_root: str, local_port: int) -> dict:
    plan = {"schema": "celikpanel/set6-plan/v1", "native_evidence": False, "cell": name, "work_root": work_root,
            "local_port": local_port, "pinned_names": list(PINNED_NAMES),
            "rule": "the driver calls only the Panel's HTTP API as the logged-in owner; every native fact is a read-only SSH "
                    "inspection; owner actions and lab preparation on the guest are recorded as such; no certificate "
                    "authority and no licence service is contacted by the driver"}
    if name in CELLS:
        settings = CELLS[name]
        plan.update(cell_kind=CELL_KIND, candidate={k: artifacts["baseline"][k] for k in ("version", "commit", "sha256")},
                    setup={"purpose": settings.purpose, "components": sorted(settings.components), "dns_mode": "external"},
                    steps=[PIN_STEP, "preflight", "origin", "baseline-install", "owner-login", "license", "setup", "site"]
                          + [key for key, _title, only in SECTIONS if not (only == "mail" and not settings.mail)]
                          + [PIN_END_STEP, "collect"])
    else:
        cell = base.validate_cell(name)
        additions = s5.cell_additions(name)
        added = [PIN_STEP]
        if additions["item9"]:
            added.append("set4-php-site-before-the-update")
        added.append(HEADERS_BEFORE_STEP)
        if additions["item9"]:
            added.append("set4-php-site-after-the-update")
        added.append(HEADERS_AFTER_STEP)
        if additions["m10"]:
            added.append(s5.M10_STEP)
        added.append(PIN_END_STEP)
        plan.update(cell_kind=UPDATE_CELL_KIND,
                    update=base.build_plan(cell, artifacts, work_root, local_port, "external").get("steps"), added_steps=added)
    return plan


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="command", required=True)
    for name in ("plan", "run"):
        cmd = sub.add_parser(name)
        cmd.add_argument("--cell", required=True, choices=sorted(CELLS) + sorted(UPDATE_CELLS))
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
    cell = CELLS[args.cell].cell if args.cell in CELLS else base.validate_cell(args.cell)
    if args.command == "plan":
        base.validate_cell_artifacts(document, cell, check_files=not args.dry_run)
        print(json.dumps(build_plan(args.cell, document, args.work_root, args.local_port), indent=2, sort_keys=True))
        return 0
    if not args.execute:
        parser.error("run mutates one registered disposable guest and requires --execute")
    base.validate_cell_artifacts(document, cell)
    if args.cell in CELLS:
        result = Set6Trial(CELLS[args.cell], document, args.work_root, args.local_port).execute()
        print(json.dumps({"overall": result["overall"], "cell_kind": CELL_KIND,
                          "sections": {k: v.get("verdict") for k, v in result["sections"].items()}}, sort_keys=True))
    else:
        result = Set6UpdateTrial(cell, document, args.work_root, args.local_port).execute()
        print(json.dumps({"overall": result["overall"], "outcome": result["outcome"]["classification"],
                          "request_id": result["request_id"],
                          "steps": {s["name"]: s["verdict"] for s in result["steps"]}}, sort_keys=True))
    return 0 if result["overall"] != "failed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
