#!/usr/bin/env python3
"""Read-only guest-side observations for the DNS pair acceptance driver.

The host streams this file to ``sudo -n /usr/bin/python3 - <command> ...`` on
one of the two disposable kill-matrix guests; nothing is installed and no
file, unit, database or DNS state is changed. Standard library only; it must
run on Debian 13 (Python 3.13) and Arch (Python 3.14).

Commands (each prints exactly one JSON document on stdout):

``dns``         authoritative UDP and TCP answers from one server
``native``      native loaded-zone state: BIND ``rndc zonestatus`` + zone files
                or the PowerDNS SQLite ``domains``/``records`` rows
``catalog``     catalog AXFR membership from a primary
``management``  CelikPanel and DNS unit active/enabled state
``versions``    DNS package and daemon versions, CelikPanel binary digests
``ledger``      digest and bounded job summary of the Agent mutation ledger
``tls``         SHA-256 of the panel's served leaf certificate (for pinning)

A DNS REFUSED or NXDOMAIN answer alone is never reported as absence: the
``native`` command reports the daemon's own loaded-zone view separately.
"""

from __future__ import annotations

import argparse
import hashlib
import ipaddress
import json
import os
import socket
import ssl
import struct
import subprocess
import sys

DNS_TYPES = {"A": 1, "NS": 2, "CNAME": 5, "SOA": 6, "PTR": 12, "MX": 15, "TXT": 16, "AAAA": 28, "AXFR": 252}
DNS_TYPE_NAMES = {value: key for key, value in DNS_TYPES.items()}
FLAG_QR, FLAG_AA, FLAG_TC = 0x8000, 0x0400, 0x0200
RCODES = {0: "NOERROR", 1: "FORMERR", 2: "SERVFAIL", 3: "NXDOMAIN", 4: "NOTIMP", 5: "REFUSED", 9: "NOTAUTH"}
RNDC = ("/usr/sbin/rndc", "/usr/bin/rndc")
PDNS_DATABASE = "/var/lib/powerdns/pdns.sqlite3"
LEDGER = "/var/lib/celikpanel-agent-private/service-mutations.json"
BIND_ZONE_DIRECTORIES = ("/var/cache/bind", "/var/named", "/etc/bind", "/var/lib/bind")
MANAGEMENT_UNITS = ("celikpanel-panel.service", "celikpanel-agent.service")
DNS_UNITS = ("named.service", "bind9.service", "pdns.service")
BINARIES = ("/opt/celikpanel/bin/panel", "/opt/celikpanel/bin/agent")
MAX_OUTPUT = 64 << 10


class ProbeError(RuntimeError):
    pass


# ---------------------------------------------------------------------------
# DNS wire format
# ---------------------------------------------------------------------------

def encode_name(name: str) -> bytes:
    name = name.rstrip(".")
    out = bytearray()
    for label in name.split(".") if name else []:
        raw = label.encode("ascii")
        if not raw or len(raw) > 63:
            raise ProbeError("invalid DNS label")
        out.append(len(raw))
        out += raw
    out.append(0)
    return bytes(out)


def decode_name(message: bytes, offset: int) -> tuple[str, int]:
    labels: list[str] = []
    following = None
    for _ in range(128):
        if offset >= len(message):
            raise ProbeError("DNS name is truncated")
        length = message[offset]
        if length & 0xC0 == 0xC0:
            if offset + 1 >= len(message):
                raise ProbeError("DNS pointer is truncated")
            if following is None:
                following = offset + 2
            offset = ((length & 0x3F) << 8) | message[offset + 1]
            continue
        if length == 0:
            name = ".".join(labels) + "."
            return name.lower(), following if following is not None else offset + 1
        offset += 1
        labels.append(message[offset:offset + length].decode("latin-1"))
        offset += length
    raise ProbeError("DNS name compression loop")


def build_query(name: str, qtype: int, query_id: int) -> bytes:
    # No RD flag: an authoritative server must answer from its own data.
    return struct.pack("!HHHHHH", query_id, 0, 1, 0, 0, 0) + encode_name(name) + struct.pack("!HH", qtype, 1)


def _rdata(message: bytes, rtype: int, start: int, end: int) -> str:
    if rtype == 1 and end - start == 4:
        return str(ipaddress.IPv4Address(message[start:end]))
    if rtype == 28 and end - start == 16:
        return str(ipaddress.IPv6Address(message[start:end]))
    if rtype in (2, 5, 12):
        return decode_name(message, start)[0]
    if rtype == 15:
        preference = struct.unpack("!H", message[start:start + 2])[0]
        return f"{preference} {decode_name(message, start + 2)[0]}"
    if rtype == 6:
        mname, offset = decode_name(message, start)
        rname, offset = decode_name(message, offset)
        numbers = struct.unpack("!IIIII", message[offset:offset + 20])
        return " ".join([mname, rname, *[str(value) for value in numbers]])
    if rtype == 16:
        parts, offset = [], start
        while offset < end:
            length = message[offset]
            parts.append(message[offset + 1:offset + 1 + length].decode("utf-8", "replace"))
            offset += 1 + length
        return " ".join(json.dumps(part) for part in parts)
    return message[start:end].hex()


def parse_response(message: bytes, query_id: int) -> dict:
    if len(message) < 12 or struct.unpack("!H", message[:2])[0] != query_id:
        raise ProbeError("DNS response identity mismatch")
    flags, qd, an, ns, ar = struct.unpack("!HHHHH", message[2:12])
    if not flags & FLAG_QR:
        raise ProbeError("DNS message is not a response")
    offset = 12
    for _ in range(qd):
        _name, offset = decode_name(message, offset)
        offset += 4
    sections: dict[str, list] = {"answer": [], "authority": [], "additional": []}
    for section, count in (("answer", an), ("authority", ns), ("additional", ar)):
        for _ in range(count):
            owner, offset = decode_name(message, offset)
            rtype, _rclass, ttl, length = struct.unpack("!HHIH", message[offset:offset + 10])
            start = offset + 10
            end = start + length
            if end > len(message):
                raise ProbeError("DNS record exceeds its message")
            if rtype != 41:
                sections[section].append({
                    "name": owner,
                    "type": DNS_TYPE_NAMES.get(rtype, str(rtype)),
                    "ttl": ttl,
                    "data": _rdata(message, rtype, start, end),
                })
            offset = end
    return {
        "rcode": RCODES.get(flags & 0x000F, str(flags & 0x000F)),
        "authoritative": bool(flags & FLAG_AA),
        "truncated": bool(flags & FLAG_TC),
        **sections,
    }


def _read_exact(connection: socket.socket, size: int) -> bytes:
    data = bytearray()
    while len(data) < size:
        chunk = connection.recv(size - len(data))
        if not chunk:
            raise ProbeError("DNS TCP stream closed early")
        data += chunk
    return bytes(data)


def query(server: str, name: str, qtype: str, *, tcp: bool, timeout: float = 4.0, port: int = 53) -> dict:
    query_id = struct.unpack("!H", os.urandom(2))[0]
    packet = build_query(name, DNS_TYPES[qtype], query_id)
    try:
        if tcp:
            with socket.create_connection((server, port), timeout=timeout) as connection:
                connection.sendall(struct.pack("!H", len(packet)) + packet)
                size = struct.unpack("!H", _read_exact(connection, 2))[0]
                response = _read_exact(connection, size)
        else:
            with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as connection:
                connection.settimeout(timeout)
                connection.sendto(packet, (server, port))
                response, _peer = connection.recvfrom(65535)
        result = parse_response(response, query_id)
    except (OSError, ProbeError, struct.error) as exc:
        result = {"error": type(exc).__name__ + ": " + str(exc)[:200]}
    result.update({"server": server, "name": name.rstrip(".").lower() + ".", "qtype": qtype,
                   "transport": "tcp" if tcp else "udp"})
    return result


def dns_command(server: str, queries: list[str]) -> dict:
    """``queries`` are ``NAME/TYPE`` pairs; each is asked over UDP and TCP."""

    ipaddress.IPv4Address(server)
    answers = []
    for item in queries:
        name, separator, qtype = item.rpartition("/")
        if not separator or not name or qtype not in DNS_TYPES or qtype == "AXFR":
            raise ProbeError(f"unsupported query {item!r}")
        encode_name(name)
        for tcp in (False, True):
            answers.append(query(server, name, qtype, tcp=tcp))
    return {"command": "dns", "server": server, "answers": answers}


def catalog_command(server: str, catalog: str, timeout: float = 8.0, port: int = 53) -> dict:
    """Catalog AXFR membership (PTR rdata under ``zones.<catalog>``)."""

    query_id = struct.unpack("!H", os.urandom(2))[0]
    packet = build_query(catalog, DNS_TYPES["AXFR"], query_id)
    members: set[str] = set()
    serials: list[int] = []
    try:
        with socket.create_connection((server, port), timeout=timeout) as connection:
            connection.sendall(struct.pack("!H", len(packet)) + packet)
            soa_seen = 0
            for _ in range(4096):
                size = struct.unpack("!H", _read_exact(connection, 2))[0]
                message = parse_response(_read_exact(connection, size), query_id)
                if message["rcode"] != "NOERROR":
                    return {"command": "catalog", "server": server, "catalog": catalog,
                            "transferred": False, "rcode": message["rcode"]}
                suffix = ".zones." + catalog.rstrip(".").lower() + "."
                for record in message["answer"]:
                    if record["type"] == "SOA":
                        soa_seen += 1
                        serials.append(int(record["data"].split()[2]))
                    elif record["type"] == "PTR" and record["name"].endswith(suffix):
                        members.add(record["data"])
                if soa_seen >= 2:
                    break
    except (OSError, ProbeError, struct.error, ValueError) as exc:
        return {"command": "catalog", "server": server, "catalog": catalog,
                "transferred": False, "error": type(exc).__name__ + ": " + str(exc)[:200]}
    return {"command": "catalog", "server": server, "catalog": catalog, "transferred": True,
            "serial": serials[0] if serials else None, "members": sorted(members)}


# ---------------------------------------------------------------------------
# Native state
# ---------------------------------------------------------------------------

def bounded(argv: list[str], timeout: float = 15.0) -> dict:
    try:
        completed = subprocess.run(
            argv, stdin=subprocess.DEVNULL, capture_output=True, timeout=timeout, check=False,
            env={"LC_ALL": "C", "LANG": "C", "PATH": "/usr/sbin:/usr/bin:/sbin:/bin"},
        )
    except (OSError, subprocess.SubprocessError) as exc:
        return {"argv": argv, "error": type(exc).__name__ + ": " + str(exc)[:200]}
    output = (completed.stdout + completed.stderr)[:MAX_OUTPUT].decode("utf-8", "replace")
    return {"argv": argv, "returncode": completed.returncode, "output": output.strip()}


def rndc_zone_state(zone: str) -> dict:
    control = next((path for path in RNDC if os.path.exists(path)), None)
    if control is None:
        return {"state": "unknown", "reason": "rndc is not installed"}
    result = bounded([control, "zonestatus", zone])
    output = result.get("output", "")
    exact_absent = f"rndc: 'zonestatus' failed: not found\nno matching zone '{zone}' in any view"
    if result.get("returncode") not in (None, 0) and output == exact_absent:
        state = "unloaded"
    elif result.get("returncode") == 0 and "type:" in output:
        state = "loaded"
    else:
        state = "unknown"
    zone_type = None
    serial = None
    for line in output.splitlines():
        if line.startswith("type:"):
            zone_type = line.split(":", 1)[1].strip()
        if line.startswith("serial:"):
            serial = line.split(":", 1)[1].strip()
    return {"state": state, "type": zone_type, "serial": serial, "rndc": result}


def bind_zone_files(zone: str) -> list[str]:
    """Bounded search for files whose name mentions the zone (BIND file names
    embed it; catalog members configured ``in-memory`` must have none)."""

    needle = zone.rstrip(".").lower()
    found: list[str] = []
    for root in BIND_ZONE_DIRECTORIES:
        if not os.path.isdir(root):
            continue
        for directory, subdirectories, files in os.walk(root, followlinks=False):
            if directory.count(os.sep) - root.count(os.sep) > 6:
                subdirectories[:] = []
                continue
            for name in files:
                if needle in name.lower():
                    found.append(os.path.join(directory, name))
                    if len(found) >= 200:
                        return sorted(found)
    return sorted(found)


def pdns_rows(zone: str, database: str = PDNS_DATABASE) -> dict:
    import sqlite3  # noqa: PLC0415 - only the PowerDNS view needs it

    if not os.path.exists(database):
        return {"read": False, "reason": "database absent", "database": database}
    try:
        connection = sqlite3.connect(f"file:{database}?mode=ro", uri=True, timeout=5)
        try:
            domains = connection.execute(
                "SELECT id, name, UPPER(type), COALESCE(master,''), COALESCE(catalog,''), "
                "COALESCE(options,'') FROM domains ORDER BY name"
            ).fetchall()
            target = [row for row in domains if row[1].rstrip(".").lower() == zone.rstrip(".").lower()]
            records: list = []
            if target:
                records = connection.execute(
                    "SELECT name, type, content, ttl FROM records WHERE domain_id = ? "
                    "AND type IS NOT NULL ORDER BY name, type, content LIMIT 500",
                    (target[0][0],),
                ).fetchall()
        finally:
            connection.close()
    except sqlite3.Error as exc:
        return {"read": False, "reason": "sqlite: " + str(exc)[:200], "database": database}
    return {
        "read": True,
        "database": database,
        "domains": [
            {"name": row[1], "type": row[2], "master": row[3], "catalog": row[4],
             "options_present": bool(row[5])}
            for row in domains
        ],
        "zone_row_present": bool(target),
        "zone_records": [list(row) for row in records],
    }


def native_command(engine: str, zone: str, catalog: str | None) -> dict:
    if engine == "bind":
        report = {
            "engine": "bind",
            "zone": zone,
            "zone_status": rndc_zone_state(zone),
            "zone_files": bind_zone_files(zone),
        }
        if catalog:
            report["catalog_status"] = rndc_zone_state(catalog)
        state = report["zone_status"]["state"]
        report["native_state"] = (
            "absent" if state == "unloaded" and not report["zone_files"]
            else "present" if state == "loaded" else "unknown"
        )
        return {"command": "native", **report}
    if engine == "pdns":
        rows = pdns_rows(zone)
        control = bounded(["/usr/bin/pdns_control", "list-zones"])
        loaded = None
        if control.get("returncode") == 0:
            listed = {line.strip().rstrip(".").lower() for line in control["output"].splitlines()}
            loaded = zone.rstrip(".").lower() in listed
        if not rows.get("read"):
            native_state = "unknown"
        elif rows["zone_row_present"]:
            native_state = "present"
        elif loaded is False or loaded is None:
            native_state = "absent" if loaded is False else "absent-by-database"
        else:
            native_state = "unknown"
        return {"command": "native", "engine": "pdns", "zone": zone, "database": rows,
                "pdns_control_list_zones": control, "daemon_lists_zone": loaded,
                "native_state": native_state}
    raise ProbeError(f"unsupported engine {engine}")


def unit_state(unit: str) -> dict:
    active = bounded(["systemctl", "is-active", unit])
    enabled = bounded(["systemctl", "is-enabled", unit])
    return {"active": active.get("output", ""), "enabled": enabled.get("output", "")}


def management_command() -> dict:
    listing = bounded(["systemctl", "list-units", "--all", "--no-legend", "--plain", "celikpanel*"])
    return {
        "command": "management",
        "units": {unit: unit_state(unit) for unit in MANAGEMENT_UNITS + DNS_UNITS},
        "celikpanel_units": listing.get("output", ""),
        "boot_id": open("/proc/sys/kernel/random/boot_id", encoding="ascii").read().strip(),
    }


def file_digest(path: str) -> str | None:
    try:
        with open(path, "rb") as handle:
            return hashlib.sha256(handle.read()).hexdigest()
    except OSError:
        return None


def versions_command() -> dict:
    report: dict = {"command": "versions", "packages": {}, "daemons": {}, "binaries": {}}
    if os.path.exists("/usr/bin/pacman"):
        for name in ("bind", "powerdns"):
            result = bounded(["/usr/bin/pacman", "-Q", name])
            report["packages"][name] = result.get("output") if result.get("returncode") == 0 else None
    elif os.path.exists("/usr/bin/dpkg-query"):
        for name in ("bind9", "bind9-libs", "pdns-server", "pdns-backend-sqlite3"):
            result = bounded(["/usr/bin/dpkg-query", "-W", "-f=${db:Status-Abbrev} ${Version}", name])
            report["packages"][name] = result.get("output") if result.get("returncode") == 0 else None
    for label, argv in (("named", ["/usr/sbin/named", "-v"]), ("pdns_server", ["/usr/sbin/pdns_server", "--version"])):
        if os.path.exists(argv[0]):
            report["daemons"][label] = bounded(argv).get("output", "")[:300]
    for path in BINARIES:
        report["binaries"][path] = file_digest(path)
    with open("/etc/os-release", encoding="utf-8") as handle:
        report["os_release"] = {
            key: value.strip('"') for key, _, value in
            (line.strip().partition("=") for line in handle if "=" in line)
            if key in {"ID", "VERSION_ID", "PRETTY_NAME"}
        }
    report["kernel"] = os.uname().release
    return report


def ledger_command() -> dict:
    """Digest of the Agent's mutation ledger plus a bounded job summary.

    Two equal digests around a read-only Panel phase prove no mutation was
    admitted in between. Job payloads are not copied.
    """

    try:
        with open(LEDGER, "rb") as handle:
            raw = handle.read(8 << 20)
    except FileNotFoundError:
        return {"command": "ledger", "present": False}
    summary = []
    try:
        document = json.loads(raw)
        jobs = document.get("jobs") if isinstance(document, dict) else None
        if isinstance(jobs, dict):
            jobs = list(jobs.values())
        for job in (jobs or [])[-200:]:
            if isinstance(job, dict):
                summary.append({key: job.get(key) for key in (
                    "request_id", "kind", "operation", "state", "status", "phase",
                    "error_code", "attempt", "updated_at") if key in job})
    except ValueError:
        summary = [{"parse": "failed"}]
    return {"command": "ledger", "present": True, "sha256": hashlib.sha256(raw).hexdigest(),
            "bytes": len(raw), "jobs": summary}


def tls_command(port: int) -> dict:
    context = ssl.create_default_context()
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    with socket.create_connection(("127.0.0.1", port), timeout=5) as raw:
        with context.wrap_socket(raw, server_hostname="localhost") as connection:
            der = connection.getpeercert(binary_form=True)
    return {"command": "tls", "port": port, "leaf_sha256": hashlib.sha256(der).hexdigest()}


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="command", required=True)
    current = sub.add_parser("dns")
    current.add_argument("--server", required=True)
    current.add_argument("--query", action="append", required=True, help="NAME/TYPE, repeatable")
    current = sub.add_parser("catalog")
    current.add_argument("--server", required=True)
    current.add_argument("--catalog", required=True)
    current = sub.add_parser("native")
    current.add_argument("--engine", required=True, choices=("bind", "pdns"))
    current.add_argument("--zone", required=True)
    current.add_argument("--catalog")
    sub.add_parser("management")
    sub.add_parser("versions")
    sub.add_parser("ledger")
    current = sub.add_parser("tls")
    current.add_argument("--port", type=int, default=2083)
    args = parser.parse_args(argv)
    try:
        if args.command == "dns":
            result = dns_command(args.server, args.query)
        elif args.command == "catalog":
            result = catalog_command(args.server, args.catalog)
        elif args.command == "native":
            result = native_command(args.engine, args.zone, args.catalog)
        elif args.command == "management":
            result = management_command()
        elif args.command == "versions":
            result = versions_command()
        elif args.command == "ledger":
            result = ledger_command()
        else:
            result = tls_command(args.port)
    except (ProbeError, OSError, ValueError) as exc:
        print(json.dumps({"command": args.command, "error": type(exc).__name__ + ": " + str(exc)[:300]}))
        return 2
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
