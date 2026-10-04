#!/usr/bin/env python3
"""Guest-side workload sampler and read-only state reader for the upd1 trial.

Disposable registered QEMU guest only (nonce/DMI/marker guard from guest_probe).
This helper has no product authority. It never edits CelikPanel state, never
starts an update and never repairs a workload. Its only writes are its own
evidence files under the private lab root and two enabled lab units:
``cp-lab-upd1-sampler.service`` (appends samples) and
``cp-lab-upd1-origin.service`` (serves the already provisioned guest-loopback
``celikpanel.net`` fixture origin, so it returns after a restart). Lab units
are named ``cp-lab-*``: a ``celikpanel-*`` unit file would make the real
installer treat the guest as an already-started install.

The one exception is ``owner-retry``: after the product itself reported
``automatic_recovery=paused_retry_limit``, it executes, at most once, the exact
one-time retry command the product printed in the recovery journal, exactly
as the product text instructs the server owner. A durable attempt record is
written before that command runs; an existing record refuses a second run.

``db-seed`` writes one table with one row into the owner's database that the
Panel API created, through the native client as the server owner (root over
the local socket), exactly as an owner's application would; nothing else.

Modes: ``install-sampler``, ``sample-loop``, ``samples``, ``snapshot``,
``cli-status``, ``budget``, ``journal``, ``tls-leaf``, ``credentials``,
``owner-retry``, ``install-origin``, ``origin-check`` (read-only),
``cron-availability`` (read-only), ``inspect`` (read-only; the observer
sidecar's inspection folded into the driver), ``db-seed``, ``db-query``
(read-only).

``getent`` exit code 2 means "not found" (a valid answer), never a probe
failure (upd3 sidecar v1 lost every light probe on it).
"""
from __future__ import annotations

import argparse
import base64
import datetime as dt
import hashlib
import http.client
import ipaddress
import importlib.util
import json
import os
from pathlib import Path
import pwd
import re
import shlex
import shutil
import socket
import ssl
import stat
import struct
import subprocess
import sys
import time

HERE = Path(__file__).resolve().parent
PRIVATE_ROOT = Path("/root/celikpanel-release-recovery-lab")
SAMPLES = PRIVATE_ROOT / "upd1-samples.jsonl"
SAMPLER_UNIT = "cp-lab-upd1-sampler.service"
SAMPLER_UNIT_PATH = Path("/etc/systemd/system") / SAMPLER_UNIT
ORIGIN_UNIT = "cp-lab-upd1-origin.service"
ORIGIN_UNIT_PATH = Path("/etc/systemd/system") / ORIGIN_UNIT
ORIGIN_HELPER = PRIVATE_ROOT / "worker-fixture-origin.py"
ORIGIN_PROVISIONED = PRIVATE_ROOT / "worker-origin-provisioned.json"
ORIGIN_PROBE_URL = "https://celikpanel.net/releases/latest.txt"
SAMPLE_SCHEMA = "celikpanel/upd1-workload-sample/v1"
SNAPSHOT_SCHEMA = "celikpanel/upd1-workload-snapshot/v1"
RETRY_SCHEMA = "celikpanel/upd1-owner-retry/v1"
CRON_STAMP = "upd1-cron-stamp.txt"
RECOVERY_CLI = "/usr/libexec/celikpanel/recovery"
BUDGET_ROOT = Path("/var/lib/celikpanel-release-state/recovery-dispatch/v1")
FLOOR = Path("/var/lib/celikpanel-release-state/sequence.floor")
FOUNDATION = Path("/var/lib/celikpanel-release-state/recovery-foundation.v1")
ENV = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL": "C"}
HEX32 = re.compile(r"[0-9a-f]{32}\Z")
SNAPSHOT_RE = re.compile(r"[0-9]{8}T[0-9]{6}Z-from-[A-Za-z0-9._-]+-to-[0-9a-f]{40}-[0-9a-f]{32}\Z")
DOMAIN_RE = re.compile(r"(?=.{1,253}\Z)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}\Z")
MARKER_RE = re.compile(r"upd1-marker-[0-9a-f]{32}\Z")
# The product's own paused text (deploy/release-recovery-runner.sh) prints this
# exact command for the owner; nothing else is ever executed by owner-retry.
RETRY_LINE_RE = re.compile(
    r"sudo (/usr/libexec/celikpanel/recovery recover --retry --snapshot "
    r"([0-9]{8}T[0-9]{6}Z-from-unknown-to-[0-9a-f]{40}-[0-9a-f]{32}))(?=\s|$)")
BUDGET_ROW_RE = re.compile(
    r"schema=celikpanel-recovery-dispatch/v1\nsnapshot=[A-Za-z0-9._-]{1,200}\nattempt=(?:[1-3]|owner)\n"
    r"token_sha256=[0-9a-f]{64}\noperation=(?:update|rollback)\n"
    r"phase=(?:quiesce|active|completion|completion-scheduler|scheduler)\n\Z")
DB_NAME_RE = re.compile(r"[A-Za-z][A-Za-z0-9_]{0,63}\Z")
DB_TABLE = "upd1_owner"
DB_CLIENTS = ("mariadb", "mysql")
HOSTING_PARENTS = ("/var", "/var/www", "/var/www/celikpanel", "/var/www/celikpanel/subscriptions")
HOSTING_RECEIPT = Path("/var/lib/celikpanel-agent-private/hosting-root-v1.json")
WEB_ACCOUNTS = ("http", "www-data", "nginx")
INSPECT_PORTS = (80, 443, 2083, 587, 25, 53)
TIMER_PATTERNS = ("certbot", "renew", "acme", "celikpanel", "logrotate", "cron")
SERVICE_UNITS = ("nginx.service", "named.service", "bind9.service", "pdns.service", "postfix.service",
                 "dovecot.service", "cron.service", "cronie.service", "celikpanel-agent.service",
                 "celikpanel-panel.service", "celikpanel-firewall-restore.service", "nftables.service")


def _load_probe():
    spec = importlib.util.spec_from_file_location("upd1_workload_probe", HERE / "guest_probe.py")
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


def utc(ts: float | None = None) -> str:
    return dt.datetime.fromtimestamp(time.time() if ts is None else ts, dt.timezone.utc).strftime(
        "%Y-%m-%dT%H:%M:%S.%fZ")


def boot_id() -> str:
    return Path("/proc/sys/kernel/random/boot_id").read_text().strip()


# -- pure protocol helpers (covered by offline tests) --------------------------

def dns_query_packet(name: str, qtype: int = 6, query_id: int = 0x5550) -> bytes:
    """Minimal RFC 1035 query (SOA by default), recursion not desired."""
    if not DOMAIN_RE.fullmatch(name):
        raise ValueError("invalid DNS name")
    header = struct.pack(">HHHHHH", query_id, 0x0000, 1, 0, 0, 0)
    labels = b"".join(bytes([len(part)]) + part.encode("ascii") for part in name.split(".")) + b"\0"
    return header + labels + struct.pack(">HH", qtype, 1)


def _skip_name(data: bytes, offset: int) -> int:
    for _ in range(128):
        if offset >= len(data):
            raise ValueError("truncated DNS name")
        length = data[offset]
        if length == 0:
            return offset + 1
        if length & 0xC0 == 0xC0:
            return offset + 2
        offset += 1 + length
    raise ValueError("DNS name loop")


def parse_dns_response(data: bytes, query_id: int = 0x5550) -> dict:
    """rcode, AA flag, answer count and the first SOA serial when present."""
    if len(data) < 12:
        raise ValueError("short DNS response")
    ident, flags, qd, an, _ns, _ar = struct.unpack(">HHHHHH", data[:12])
    if ident != query_id or not flags & 0x8000:
        raise ValueError("DNS response identity differs")
    offset = 12
    for _ in range(qd):
        offset = _skip_name(data, offset) + 4
    serial = None
    for _ in range(an):
        offset = _skip_name(data, offset)
        rtype, _rclass, _ttl, rdlength = struct.unpack(">HHIH", data[offset:offset + 10])
        offset += 10
        rdata_start = offset
        if rtype == 6 and serial is None:
            inner = _skip_name(data, rdata_start)
            inner = _skip_name(data, inner)
            serial = struct.unpack(">I", data[inner:inner + 4])[0]
        offset = rdata_start + rdlength
    return {"rcode": flags & 0x000F, "authoritative": bool(flags & 0x0400), "answers": an, "soa_serial": serial}


def extract_retry_command(journal_text: str, expected_snapshot: str) -> str:
    """The product's printed owner retry command for exactly this snapshot.

    The last matching line wins (the runner prints it on every paused tick).
    Any other snapshot, a missing line or two different commands refuse.
    """
    if not SNAPSHOT_RE.fullmatch(expected_snapshot):
        raise ValueError("expected snapshot is not canonical")
    found = RETRY_LINE_RE.findall(journal_text)
    if not found:
        raise ValueError("the recovery journal does not show the one-time retry command")
    snapshots = {snapshot for _, snapshot in found}
    if snapshots != {expected_snapshot}:
        raise ValueError("the recovery journal names a different snapshot")
    return found[-1][0]


def budget_receipt_safe(raw: bytes) -> bool:
    try:
        return BUDGET_ROW_RE.fullmatch(raw.decode("ascii")) is not None
    except UnicodeDecodeError:
        return False


def getent_outcome(result: dict | None) -> str:
    """getent(1): 0 found, 2 key not found (a valid answer), anything else a probe failure."""
    if not isinstance(result, dict) or result.get("status") != "ok":
        return "probe-failed"
    return {0: "found", 2: "not-found"}.get(result.get("returncode"), "probe-failed")


def db_statements(marker: str) -> str:
    """The owner's one table and one row (the site marker), as an application would create them."""
    if not MARKER_RE.fullmatch(marker or ""):
        raise ValueError("invalid marker")
    return (f"CREATE TABLE IF NOT EXISTS {DB_TABLE} (id INT PRIMARY KEY, marker VARCHAR(64) NOT NULL); "
            f"INSERT INTO {DB_TABLE} (id, marker) VALUES (1, '{marker}') "
            "ON DUPLICATE KEY UPDATE marker = VALUES(marker);")


def db_query_statement() -> str:
    return f"SELECT marker FROM {DB_TABLE} WHERE id = 1;"


# -- guarded I/O ---------------------------------------------------------------

def private_root() -> Path:
    info = PRIVATE_ROOT.lstat()
    if not (stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_gid == 0
            and stat.S_IMODE(info.st_mode) == 0o700):
        raise ValueError("unsafe private lab root")
    return PRIVATE_ROOT


def save_once(path: Path, value) -> str:
    raw = (json.dumps(value, sort_keys=True) + "\n").encode()
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)
    return hashlib.sha256(raw).hexdigest()


def run(argv, timeout=20, limit=262144, input_bytes=None) -> dict:
    try:
        completed = subprocess.run(argv, input=input_bytes, capture_output=True, timeout=timeout, env=ENV)
    except FileNotFoundError:
        return {"argv": argv, "status": "unavailable"}
    except subprocess.TimeoutExpired:
        return {"argv": argv, "status": "timeout"}
    return {"argv": argv, "status": "ok", "returncode": completed.returncode,
            "stdout": completed.stdout[:limit].decode("utf-8", "replace"),
            "stderr": completed.stderr[:16384].decode("utf-8", "replace")}


# -- workload probes -------------------------------------------------------------

def probe_http(domain: str, marker: str) -> dict:
    started = time.monotonic()
    try:
        connection = http.client.HTTPConnection("127.0.0.1", 80, timeout=4)
        try:
            connection.request("GET", "/", headers={"Host": domain, "User-Agent": "celikpanel-upd1-sampler"})
            response = connection.getresponse()
            body = response.read(65536)
        finally:
            connection.close()
        return {"ok": response.status == 200 and marker.encode() in body, "status": response.status,
                "marker": marker.encode() in body, "ms": round((time.monotonic() - started) * 1000)}
    except (OSError, http.client.HTTPException) as exc:
        return {"ok": False, "error": type(exc).__name__, "ms": round((time.monotonic() - started) * 1000)}


def probe_dns(domain: str, tcp: bool = False, server: str = "127.0.0.1") -> dict:
    packet = dns_query_packet(domain)
    try:
        if tcp:
            with socket.create_connection((server, 53), timeout=3) as sock:
                sock.sendall(struct.pack(">H", len(packet)) + packet)
                size = struct.unpack(">H", _recv_exact(sock, 2))[0]
                data = _recv_exact(sock, size)
        else:
            with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
                sock.settimeout(3)
                sock.sendto(packet, (server, 53))
                data, _ = sock.recvfrom(4096)
        parsed = parse_dns_response(data)
        return dict(parsed, ok=parsed["rcode"] == 0 and parsed["answers"] > 0 and parsed["soa_serial"] is not None)
    except (OSError, ValueError, struct.error) as exc:
        return {"ok": False, "error": type(exc).__name__}


def _recv_exact(sock, size):
    data = b""
    while len(data) < size:
        chunk = sock.recv(size - len(data))
        if not chunk:
            raise ValueError("short TCP DNS response")
        data += chunk
    return data


def probe_smtp(port: int = 587) -> dict:
    try:
        with socket.create_connection(("127.0.0.1", port), timeout=4) as sock:
            sock.settimeout(4)
            banner = sock.recv(512).decode("ascii", "replace")
            try:
                sock.sendall(b"QUIT\r\n")
            except OSError:
                pass
        return {"ok": banner.startswith("220"), "banner_code": banner[:3]}
    except OSError as exc:
        return {"ok": False, "error": type(exc).__name__}


def probe_panel() -> dict:
    context = ssl.create_default_context()
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    try:
        connection = http.client.HTTPSConnection("127.0.0.1", 2083, timeout=4, context=context)
        try:
            connection.request("GET", "/api/v1/panel/availability")
            response = connection.getresponse()
            response.read(8192)
        finally:
            connection.close()
        return {"ok": response.status in (200, 401), "status": response.status}
    except (OSError, http.client.HTTPException) as exc:
        return {"ok": False, "error": type(exc).__name__}


def cron_stamp_path() -> Path | None:
    """The upd1 cron job's stamp file, found through the tenant crontab spool."""
    for spool in (Path("/var/spool/cron/crontabs"), Path("/var/spool/cron")):
        try:
            entries = sorted(spool.iterdir())
        except OSError:
            continue
        for entry in entries[:512]:
            try:
                if not entry.is_file() or entry.is_symlink() or entry.stat().st_size > 65536:
                    continue
                if CRON_STAMP in entry.read_text(errors="replace"):
                    home = Path(pwd.getpwnam(entry.name).pw_dir)
                    return home / CRON_STAMP
            except (OSError, KeyError):
                continue
    return None


def probe_cron(path: Path | None) -> dict:
    if path is None:
        return {"ok": False, "error": "crontab-entry-not-found"}
    try:
        info = path.lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_size > 256:
            return {"ok": False, "error": "unsafe-stamp"}
        return {"ok": True, "path": str(path), "mtime": info.st_mtime,
                "content": path.read_text(errors="replace").strip()[:64]}
    except OSError as exc:
        return {"ok": False, "path": str(path), "error": type(exc).__name__}


def db_client() -> str | None:
    for name in DB_CLIENTS:
        found = shutil.which(name, path=ENV["PATH"])
        if found:
            return found
    return None


def probe_db(name: str | None, marker: str) -> dict:
    """Read-only: the owner's row through the native client, as the server owner (root, local socket)."""
    if not name:
        return {"ok": None, "skipped": "no-owner-database"}
    if not DB_NAME_RE.fullmatch(name):
        return {"ok": False, "error": "invalid-database-name"}
    client = db_client()
    if client is None:
        return {"ok": False, "error": "no-native-client"}
    result = run([client, "--batch", "--skip-column-names", "--connect-timeout=3", name, "-e", db_query_statement()],
                 timeout=6, limit=1024)
    value = (result.get("stdout") or "").strip()
    return {"ok": result.get("status") == "ok" and result.get("returncode") == 0 and value == marker,
            "status": result.get("status"), "returncode": result.get("returncode"), "row_matches": value == marker,
            "client": Path(client).name, "error": (result.get("stderr") or "").strip()[:200] or None}


def db_seed(name: str, marker: str) -> dict:
    """The owner's application step: one table, one row, in the database the Panel API created."""
    if not DB_NAME_RE.fullmatch(name or ""):
        raise ValueError("invalid database name")
    client = db_client()
    if client is None:
        raise ValueError("no native MariaDB/MySQL client on this server")
    result = run([client, "--batch", "--connect-timeout=5", name, "-e", db_statements(marker)], timeout=30, limit=4096)
    return {"database": name, "table": DB_TABLE, "client": Path(client).name, "status": result.get("status"),
            "returncode": result.get("returncode"), "stderr": (result.get("stderr") or "")[:400],
            "query": probe_db(name, marker)}


def sample(domain: str, marker: str, smtp: bool, cron_path: Path | None, dns_server: str = "127.0.0.1",
           db_name: str | None = None) -> dict:
    value = {"schema": SAMPLE_SCHEMA, "t": time.time(), "utc": utc(), "boot_id": boot_id(),
             "monotonic": time.monotonic(), "web": probe_http(domain, marker), "dns": probe_dns(domain, server=dns_server),
             "smtp": probe_smtp() if smtp else {"ok": None, "skipped": "no-mail-workload"},
             "cron": probe_cron(cron_path), "panel": probe_panel()}
    if db_name:
        value["db"] = probe_db(db_name, marker)
    return value


def sample_loop(args) -> int:
    root = private_root()
    cron_path = None
    fd = os.open(root / SAMPLES.name, os.O_WRONLY | os.O_CREAT | os.O_APPEND | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    with os.fdopen(fd, "ab") as stream:
        while True:
            started = time.monotonic()
            if cron_path is None or not cron_path.exists():
                cron_path = cron_stamp_path()
            value = sample(args.domain, args.marker, args.smtp, cron_path, args.dns_server, args.db_name)
            stream.write((json.dumps(value, sort_keys=True) + "\n").encode())
            stream.flush()
            os.fsync(stream.fileno())
            time.sleep(max(0.2, args.interval - (time.monotonic() - started)))


def install_sampler(args, identity) -> dict:
    """One enabled lab unit so sampling resumes by itself after a reboot."""
    private_root()
    if SAMPLER_UNIT_PATH.exists() or SAMPLER_UNIT_PATH.is_symlink():
        raise ValueError("sampler unit already exists; never replaced")
    argv = ["/usr/bin/python3", "-I", str(PRIVATE_ROOT / "guest_upd1_workload.py"), "sample-loop",
            "--lab-nonce", identity["nonce"], "--vm-uuid", identity["vm_uuid"], "--cell-id", identity["cell_id"],
            "--node", identity["node"], "--domain", args.domain, "--marker", args.marker,
            "--interval", str(args.interval), "--dns-server", args.dns_server] + (["--smtp"] if args.smtp else []) \
        + (["--db-name", args.db_name] if args.db_name else [])
    unit = ("[Unit]\nDescription=Disposable upd1 workload sampler (lab only; no product authority)\n"
            "After=network-online.target\n\n[Service]\nType=simple\nExecStart=" + shlex.join(argv) +
            "\nRestart=always\nRestartSec=2\nUMask=0077\n\n[Install]\nWantedBy=multi-user.target\n")
    fd = os.open(SAMPLER_UNIT_PATH, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o644)
    with os.fdopen(fd, "w") as stream:
        stream.write(unit)
        stream.flush()
        os.fsync(stream.fileno())
    results = [run(["/usr/bin/systemctl", "daemon-reload"]), run(["/usr/bin/systemctl", "enable", "--now", SAMPLER_UNIT])]
    return {"unit": SAMPLER_UNIT, "unit_sha256": hashlib.sha256(unit.encode()).hexdigest(),
            "results": [{k: r.get(k) for k in ("status", "returncode")} for r in results]}


def origin_unit_text(nonce: str) -> str:
    """L1: the enabled lab unit serving the provisioned fixture origin (survives a restart)."""
    if not re.fullmatch(r"[0-9a-f]{64}", nonce):
        raise ValueError("invalid lab nonce")
    argv = ["/usr/bin/python3", "-I", str(ORIGIN_HELPER), "guest-serve", "--nonce", nonce]
    return ("[Unit]\nDescription=Disposable upd1 celikpanel.net fixture origin (guest loopback only; lab, no product "
            "authority)\nAfter=network.target\n\n[Service]\nType=simple\nExecStart=" + shlex.join(argv) +
            "\nRestart=on-failure\nRestartSec=5\nUMask=0077\n\n[Install]\nWantedBy=multi-user.target\n")


def install_origin(identity) -> dict:
    """Write, enable and start the origin unit once; the origin itself re-checks the sealed intent."""
    private_root()
    if not ORIGIN_PROVISIONED.is_file():
        raise ValueError("the fixture origin is not provisioned; the unit is not installed")
    if ORIGIN_UNIT_PATH.exists() or ORIGIN_UNIT_PATH.is_symlink():
        raise ValueError("origin unit already exists; never replaced")
    unit = origin_unit_text(identity["nonce"])
    fd = os.open(ORIGIN_UNIT_PATH, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o644)
    with os.fdopen(fd, "w") as stream:
        stream.write(unit)
        stream.flush()
        os.fsync(stream.fileno())
    results = [run(["/usr/bin/systemctl", "daemon-reload"]), run(["/usr/bin/systemctl", "enable", "--now", ORIGIN_UNIT])]
    return {"unit": ORIGIN_UNIT, "path": str(ORIGIN_UNIT_PATH), "unit_sha256": hashlib.sha256(unit.encode()).hexdigest(),
            "results": [{k: r.get(k) for k in ("status", "returncode")} for r in results]}


def origin_check() -> dict:
    """Read-only: what celikpanel.net resolves to and whether the fixture answers (after provisioning only)."""
    if not ORIGIN_PROVISIONED.is_file():
        raise ValueError("the fixture origin is not provisioned; celikpanel.net is not looked up")
    getent = run(["getent", "hosts", "celikpanel.net"], timeout=10, limit=4096)
    https = run(["curl", "--silent", "--max-time", "10", "--output", "/dev/null", "--write-out", "%{http_code}",
                 ORIGIN_PROBE_URL], timeout=15, limit=64)
    unit = run(["/usr/bin/systemctl", "show", ORIGIN_UNIT, "-p", "ActiveState", "-p", "UnitFileState", "-p", "NRestarts"])
    return {"boot_id": boot_id(), "getent": dict({k: getent.get(k) for k in ("status", "returncode", "stdout")},
                                                  outcome=getent_outcome(getent)),
            "https": {k: https.get(k) for k in ("status", "returncode", "stdout")},
            "unit": dict(line.split("=", 1) for line in unit.get("stdout", "").splitlines() if "=" in line)}


def cron_availability() -> dict:
    """Read-only: is crontab (the Agent's own gate) present, and which cron daemon unit is loaded."""
    units = {}
    for name in ("cron.service", "cronie.service"):
        shown = run(["/usr/bin/systemctl", "show", name, "-p", "LoadState", "-p", "ActiveState", "-p", "UnitFileState"])
        units[name] = dict(line.split("=", 1) for line in shown.get("stdout", "").splitlines() if "=" in line)
    return {"crontab": shutil.which("crontab", path=ENV["PATH"]), "units": units}


def read_samples(offset: int) -> dict:
    try:
        with open(PRIVATE_ROOT / SAMPLES.name, "rb") as stream:
            stream.seek(offset)
            raw = stream.read(4 * 1024 * 1024)
    except FileNotFoundError:
        raw = b""
    complete = raw[:raw.rfind(b"\n") + 1] if b"\n" in raw else b""
    return {"offset": offset, "next_offset": offset + len(complete),
            "jsonl_base64": base64.b64encode(complete).decode()}


# -- read-only state ---------------------------------------------------------------

def timers() -> dict:
    listed = run(["/usr/bin/systemctl", "list-unit-files", "--type=timer", "--no-legend", "--no-pager"])
    names = sorted({line.split()[0] for line in listed.get("stdout", "").splitlines() if line.strip()})
    result = {}
    for name in names:
        if not any(pattern in name for pattern in TIMER_PATTERNS):
            continue
        shown = run(["/usr/bin/systemctl", "show", name, "-p", "UnitFileState", "-p", "ActiveState",
                     "-p", "SubState", "-p", "LoadState"])
        result[name] = dict(line.split("=", 1) for line in shown.get("stdout", "").splitlines() if "=" in line)
    return result


def services() -> dict:
    result = {}
    for unit in SERVICE_UNITS:
        shown = run(["/usr/bin/systemctl", "show", unit, "-p", "LoadState", "-p", "ActiveState",
                     "-p", "UnitFileState", "-p", "SubState"])
        values = dict(line.split("=", 1) for line in shown.get("stdout", "").splitlines() if "=" in line)
        if values.get("LoadState") not in (None, "not-found"):
            result[unit] = values
    return result


def firewall() -> dict:
    ruleset = run(["/usr/sbin/nft", "-s", "list", "ruleset"], limit=4 * 1024 * 1024)
    if ruleset.get("status") != "ok":
        ruleset = run(["/usr/bin/nft", "-s", "list", "ruleset"], limit=4 * 1024 * 1024)
    text = ruleset.get("stdout", "")
    return {"status": ruleset.get("status"), "returncode": ruleset.get("returncode"),
            "sha256": hashlib.sha256(text.encode()).hexdigest(),
            "tables": sorted(line.strip()[:-1].strip() for line in text.splitlines() if line.startswith("table ") and line.rstrip().endswith("{")),
            "bytes": len(text)}


def build_identity(path: str) -> dict:
    result = run([path, "--inspect-build-identity"], timeout=8, limit=512)
    if result.get("status") != "ok":
        return result
    try:
        digest = hashlib.sha256(Path(path).read_bytes()).hexdigest()
    except OSError:
        digest = None
    return {"returncode": result["returncode"], "identity": result["stdout"], "sha256": digest}


def small_record(path: Path) -> dict:
    try:
        raw = path.read_bytes()[:2048]
    except OSError as exc:
        return {"present": False, "error": type(exc).__name__}
    return {"present": True, "sha256": hashlib.sha256(raw).hexdigest(), "text": raw.decode("ascii", "replace")}


def mailbox(address: str | None) -> dict:
    if not address:
        return {"checked": False}
    result = run(["/usr/bin/doveadm", "user", address], limit=4096)
    return {"checked": True, "status": result.get("status"), "returncode": result.get("returncode"),
            "present": result.get("status") == "ok" and result.get("returncode") == 0}


def budget(snapshot: str | None) -> dict:
    receipts = []
    try:
        directories = sorted(BUDGET_ROOT.iterdir())
    except OSError:
        return {"present": False, "receipts": []}
    for directory in directories[:64]:
        if snapshot and directory.name != snapshot:
            continue
        try:
            entries = sorted(directory.iterdir())
        except OSError:
            continue
        for entry in entries[:16]:
            try:
                info = entry.lstat()
                raw = entry.read_bytes()[:1024] if stat.S_ISREG(info.st_mode) else b""
            except OSError:
                continue
            receipts.append({"snapshot": directory.name, "name": entry.name, "mtime": info.st_mtime,
                             "mtime_utc": utc(info.st_mtime), "sha256": hashlib.sha256(raw).hexdigest(),
                             "text": raw.decode("ascii") if budget_receipt_safe(raw) else None})
    return {"present": True, "receipts": receipts}


def snapshot(args, probe) -> dict:
    cron_path = cron_stamp_path()
    try:
        transaction = probe.observe_transaction()
    except Exception as exc:  # noqa: BLE001 - one unknown observation keeps the others
        transaction = {"status": "unknown", "error": type(exc).__name__}
    return {"schema": SNAPSHOT_SCHEMA, "captured_at": utc(), "boot_id": boot_id(),
            "web": probe_http(args.domain, args.marker), "dns_server": args.dns_server,
            "dns_udp": probe_dns(args.domain, server=args.dns_server),
            "dns_tcp": probe_dns(args.domain, tcp=True, server=args.dns_server),
            "smtp": probe_smtp() if args.smtp else {"ok": None, "skipped": "no-mail-workload"},
            "mailbox": mailbox(args.mailbox), "cron": probe_cron(cron_path), "timers": timers(),
            "services": services(), "firewall": firewall(),
            "build": {name: build_identity("/opt/celikpanel/bin/" + name) for name in ("agent", "panel")},
            "floor": small_record(FLOOR), "foundation": small_record(FOUNDATION),
            "transaction": transaction, "budget": budget(None),
            "db": probe_db(args.db_name, args.marker),
            "sampler": run(["/usr/bin/systemctl", "show", SAMPLER_UNIT, "-p", "ActiveState", "-p", "UnitFileState"])}


def _show(unit: str, *properties: str) -> dict:
    argv = ["/usr/bin/systemctl", "show", unit]
    for name in properties:
        argv += ["-p", name]
    shown = run(argv, timeout=10, limit=8192)
    return dict(line.split("=", 1) for line in (shown.get("stdout") or "").splitlines() if "=" in line)


def _stat(path: str) -> dict:
    try:
        info = os.lstat(path)
    except FileNotFoundError:
        return {"path": path, "present": False}
    except OSError as exc:
        return {"path": path, "present": None, "error": type(exc).__name__}
    return {"path": path, "present": True, "mode": format(stat.S_IMODE(info.st_mode), "04o"), "uid": info.st_uid,
            "gid": info.st_gid, "type": "dir" if stat.S_ISDIR(info.st_mode) else
            "link" if stat.S_ISLNK(info.st_mode) else "file" if stat.S_ISREG(info.st_mode) else "other"}


def inspect(light: bool) -> dict:
    """Read-only side inspection (upd3 observer sidecar v2, folded into the driver).

    ``light``: the hosting-root probe only (the before-first-site series). No
    command's exit status can lose the inspection: each result is recorded as
    data, and ``getent`` exit code 2 is ``not-found``.
    """
    value: dict = {"captured_at": utc(), "boot_id": boot_id(), "light": light}
    value["hosting_parents"] = [_stat(path) for path in HOSTING_PARENTS]
    sites = sorted(Path("/var/www/celikpanel/subscriptions").glob("*/sites/*"))[:8] \
        if Path("/var/www/celikpanel/subscriptions").is_dir() else []
    value["site_paths"] = {str(site): (run(["/usr/bin/namei", "-l", str(site / "public_html")], timeout=5,
                                           limit=4096).get("stdout") or "") for site in sites}
    value["hosting_receipt"] = dict(_stat(str(HOSTING_RECEIPT)), **(small_record(HOSTING_RECEIPT)
                                                                   if HOSTING_RECEIPT.is_file() else {}))
    agent_log = run(["/usr/bin/journalctl", "-u", "celikpanel-agent.service", "--no-pager", "-o", "short-iso-precise",
                     "-n", "2000"], timeout=20, limit=1024 * 1024)
    value["hosting_log_lines"] = [line for line in (agent_log.get("stdout") or "").splitlines()
                                  if re.search(r"hosting root|site refused", line, re.I)][-10:]
    accounts = {}
    for name in WEB_ACCOUNTS:
        result = run(["getent", "passwd", name], timeout=5, limit=1024)
        accounts[name] = {"outcome": getent_outcome(result), "returncode": result.get("returncode"),
                          "entry": (result.get("stdout") or "").strip()}
    value["web_accounts"] = accounts
    if light:
        return value
    release = {}
    try:
        for line in Path("/etc/os-release").read_text().splitlines():
            key, _, raw = line.partition("=")
            if key in ("ID", "VERSION_ID"):
                release[key] = raw.strip('"')
    except OSError:
        pass
    value["os"] = release
    value["cron_units"] = {name: _show(name, "LoadState", "ActiveState", "SubState", "UnitFileState", "NRestarts")
                           for name in ("cron.service", "cronie.service")}
    cron_path = cron_stamp_path()
    value["cron_stamp"] = probe_cron(cron_path)
    value["origin_unit"] = _show(ORIGIN_UNIT, "ActiveState", "SubState", "UnitFileState", "NRestarts")
    try:
        value["hosts_origin_lines"] = [line for line in Path("/etc/hosts").read_text().splitlines() if "celikpanel" in line]
    except OSError:
        value["hosts_origin_lines"] = None
    origin = run(["getent", "hosts", "celikpanel.net"], timeout=10, limit=4096)
    value["origin_lookup"] = {"outcome": getent_outcome(origin), "stdout": (origin.get("stdout") or "")[:512]}
    listeners = run(["/usr/bin/ss", "-ltnpH"], timeout=10, limit=65536)
    wanted = tuple(f":{port}" for port in INSPECT_PORTS)
    value["listeners"] = [" ".join(line.split()[3:6]) for line in (listeners.get("stdout") or "").splitlines()
                          if len(line.split()) > 3 and line.split()[3].endswith(wanted)]
    for unit in ("celikpanel-panel.service", "celikpanel-agent.service", "celikpanel-release-recovery.service",
                 "celikpanel-release-recovery.timer", "celikpanel-firewall-restore.service"):
        value.setdefault("product_units", {})[unit] = _show(
            unit, "LoadState", "ActiveState", "SubState", "UnitFileState", "NRestarts", "Result", "ExecMainStatus",
            "ActiveEnterTimestamp", "InactiveEnterTimestamp")
    value["timers"] = timers()
    records = Path("/var/lib/celikpanel-recovery-observations")
    value["observation_record_names"] = sorted(p.name for p in records.iterdir())[:32] if records.is_dir() else None
    return value


def cli_status(request_id: str) -> dict:
    if not HEX32.fullmatch(request_id):
        raise ValueError("invalid request id")
    out = {"captured_at": utc(), "boot_id": boot_id()}
    for label, extra in (("json", ["--json"]), ("en", ["--lang", "en"]), ("tr", ["--lang", "tr"])):
        result = run([RECOVERY_CLI, "status", "--request-id", request_id, *extra], timeout=10, limit=16384)
        out[label] = {k: result.get(k) for k in ("status", "returncode", "stdout", "stderr")}
    return out


def journal(units: list[str], since: str, lines: int) -> dict:
    argv = ["/usr/bin/journalctl", "--no-pager", "-o", "short-iso-precise", "-n", str(lines), "--since", since]
    for unit in units:
        # A '*' is a journalctl -u glob (e.g. php*-fpm.service); no shell is involved.
        if not re.fullmatch(r"[A-Za-z0-9@_.:*-]{1,120}\.(service|timer)", unit):
            raise ValueError("invalid journal unit")
        argv += ["-u", unit]
    return run(argv, timeout=30, limit=4 * 1024 * 1024)


def tls_leaf() -> dict:
    context = ssl.create_default_context()
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    with socket.create_connection(("127.0.0.1", 2083), timeout=5) as raw:
        with context.wrap_socket(raw, server_hostname="localhost") as connection:
            der = connection.getpeercert(binary_form=True)
    return {"leaf_sha256": hashlib.sha256(der).hexdigest()}


def credentials() -> dict:
    """The baseline owner login generated on the guest; printed to the SSH caller only."""
    path = private_root() / "admin-login.json"
    info = path.lstat()
    if not (stat.S_ISREG(info.st_mode) and info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o600 and info.st_size < 4096):
        raise ValueError("unsafe owner credential file")
    value = json.loads(path.read_text())
    return {"username": value["username"], "password": value["password"]}


def pending_snapshot(probe) -> str | None:
    """Snapshot named by the one present transaction marker; tokens are never read out."""
    found = []
    for name in ("active", "completion.pending", "scheduler-restore.pending"):
        path = Path("/var/lib/celikpanel-release-transaction") / name
        if path.exists() or path.is_symlink():
            try:
                found.append(probe.parse_transaction(probe.bounded_file(path, 8192)).get("snapshot"))
            except (probe.ProbeError, UnicodeError):
                return None
    if len(set(found)) != 1:
        return None
    return found[0]


def owner_retry(request_id: str, snapshot_name: str, execute: bool, probe) -> dict:
    """Run the product's printed one-time retry once, only in the exact paused state."""
    root = private_root()
    attempt = root / ("upd1-owner-retry-" + request_id + ".json")
    if attempt.exists() or attempt.is_symlink():
        raise ValueError("owner retry was already attempted; never repeated")
    status = run([RECOVERY_CLI, "status", "--request-id", request_id, "--json"], timeout=10, limit=8192)
    observed = json.loads(status.get("stdout") or "{}")
    if (observed.get("request_id") != request_id or observed.get("phase") != "recovery_required"
            or observed.get("automatic_recovery") != "paused_retry_limit"):
        raise ValueError("the product does not report the paused retry-limit state for this request")
    if pending_snapshot(probe) != snapshot_name:
        raise ValueError("pending transaction snapshot differs from the expected operation snapshot")
    # Exactly the command the product text tells the owner to read.
    shown = run(["/usr/bin/journalctl", "-u", "celikpanel-release-recovery.service", "--no-pager", "-n", "50"],
                timeout=20, limit=262144)
    command = extract_retry_command(shown.get("stdout", ""), snapshot_name)
    argv = shlex.split(command)
    record = {"schema": RETRY_SCHEMA, "request_id": request_id, "snapshot": snapshot_name, "argv": argv,
              "status_before": observed, "journal_sha256": hashlib.sha256(shown.get("stdout", "").encode()).hexdigest(),
              "journal_text": shown.get("stdout", ""), "attempted_at": utc(), "execute": execute}
    if not execute:
        return dict(record, action="validated-not-executed")
    save_once(attempt, record)
    started = time.time()
    result = run(argv, timeout=3600, limit=262144)
    outcome = dict(record, action="executed-once", started_at=utc(started), finished_at=utc(),
                   result={k: result.get(k) for k in ("status", "returncode", "stdout", "stderr")})
    save_once(root / ("upd1-owner-retry-" + request_id + ".result.json"), outcome)
    return outcome


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("mode", choices=("install-sampler", "sample-loop", "samples", "snapshot", "cli-status",
                                         "budget", "journal", "tls-leaf", "credentials", "owner-retry",
                                         "install-origin", "origin-check", "cron-availability", "inspect",
                                         "db-seed", "db-query"))
    for name in ("lab-nonce", "vm-uuid", "cell-id", "node"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--domain")
    parser.add_argument("--marker")
    parser.add_argument("--mailbox")
    parser.add_argument("--smtp", action="store_true")
    parser.add_argument("--interval", type=float, default=5.0)
    parser.add_argument("--dns-server", default="127.0.0.1")
    parser.add_argument("--offset", type=int, default=0)
    parser.add_argument("--request-id")
    parser.add_argument("--snapshot-name")
    parser.add_argument("--since")
    parser.add_argument("--unit", action="append", default=[])
    parser.add_argument("--lines", type=int, default=2000)
    parser.add_argument("--execute", action="store_true")
    parser.add_argument("--light", action="store_true")
    parser.add_argument("--db-name")
    args = parser.parse_args(argv)
    probe = _load_probe()
    identity = probe.guard_guest(args)
    if args.db_name is not None and not DB_NAME_RE.fullmatch(args.db_name):
        parser.error("--db-name must be a plain database identifier")
    if args.mode in ("db-seed", "db-query") and (not args.db_name or not args.marker
                                                 or not MARKER_RE.fullmatch(args.marker)):
        parser.error("database modes require --db-name and the exact upd1 marker")
    if args.mode in ("install-sampler", "sample-loop", "snapshot"):
        if not args.domain or not DOMAIN_RE.fullmatch(args.domain) or not args.marker or not MARKER_RE.fullmatch(args.marker):
            parser.error("workload modes require an exact domain and upd1 marker")
        if not 1.0 <= args.interval <= 60.0:
            parser.error("interval must be 1..60 seconds")
        try:
            ipaddress.ip_address(args.dns_server)
        except ValueError:
            parser.error("--dns-server must be an IP address")
    if args.mode == "sample-loop":
        return sample_loop(args)
    if args.mode == "install-sampler":
        value = install_sampler(args, identity)
    elif args.mode == "samples":
        value = read_samples(max(0, args.offset))
    elif args.mode == "snapshot":
        value = snapshot(args, probe)
    elif args.mode == "cli-status":
        value = cli_status(args.request_id or "")
    elif args.mode == "budget":
        value = budget(args.snapshot_name)
    elif args.mode == "journal":
        value = journal(args.unit, args.since or "-2h", max(10, min(args.lines, 20000)))
    elif args.mode == "tls-leaf":
        value = tls_leaf()
    elif args.mode == "credentials":
        value = credentials()
    elif args.mode == "install-origin":
        value = install_origin(identity)
    elif args.mode == "origin-check":
        value = origin_check()
    elif args.mode == "cron-availability":
        value = cron_availability()
    elif args.mode == "inspect":
        value = inspect(args.light)
    elif args.mode == "db-seed":
        value = db_seed(args.db_name, args.marker)
    elif args.mode == "db-query":
        value = probe_db(args.db_name, args.marker)
    else:
        if not args.request_id or not HEX32.fullmatch(args.request_id) or not args.snapshot_name:
            parser.error("owner-retry requires the exact request id and pending snapshot")
        value = owner_retry(args.request_id, args.snapshot_name, args.execute, probe)
    print(json.dumps(value, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:  # noqa: BLE001 - every refusal is reported as data, never retried here
        print(json.dumps({"refused": type(exc).__name__, "reason": str(exc)[:300]}), file=sys.stderr)
        raise SystemExit(2)
