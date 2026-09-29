#!/usr/bin/env python3
"""Read-only observation of a panel-free native PRIMARY DNS peer.

Runs on the disposable peer guest prepared by ``native_primary_peer.py``. It
changes no service, file, database or DNS state. It reports what the native
primary serves (catalog serial, members and catalog producer format, parsed
with the same rules and the same BIND-then-PowerDNS selection as the Agent's
peer catalog reader) and which transfer/NOTIFY lines the native service logged
for the guest under test. It is a fixture observation,
not an Agent receipt, and proves nothing about the secondary by itself.

The module is standard-library only so it can be copied to the peer alone.
"""

from __future__ import annotations

import argparse
import hashlib
import ipaddress
import json
import os
import re
import socket
import struct
import subprocess
import sys
from typing import Iterable


ZONE = "s1-kill.test"
QUERY = "www." + ZONE
PEER_NETWORK = ipaddress.IPv4Network("192.0.2.0/24")
CATALOG_SUFFIX = ".celikpanel.invalid"
CATALOG_SERIAL = 1
FIXTURE_ACCOUNT = "celikpanel-fixture-native-primary"

DNS_TYPE_A = 1
DNS_TYPE_NS = 2
DNS_TYPE_SOA = 6
DNS_TYPE_PTR = 12
DNS_TYPE_TXT = 16
DNS_TYPE_APL = 42
DNS_TYPE_AXFR = 252
DNS_CLASS_IN = 1
FLAG_QR = 0x8000
FLAG_AA = 0x0400
FLAG_TC = 0x0200
RCODE_MASK = 0x000F

# cmd/agent/dns_catalog_axfr.go constants.
CATALOG_TTL = 60
BIND_MEMBER_LABEL_SIZE = 56
PDNS_MEMBER_LABEL_SIZE = 32
AXFR_MAX_BYTES = 16 << 20
AXFR_MAX_MESSAGES = 4096
AXFR_MAX_MEMBERS = 65536

PRODUCER_BIND = "bind"
PRODUCER_POWERDNS = "powerdns"
# dnsCatalogAXFRProducer.String() in cmd/agent/dns_catalog_axfr.go: the name
# the Agent logs once per operation for the accepted peer catalog format.
AGENT_PRODUCER_NAMES = {PRODUCER_BIND: "BIND", PRODUCER_POWERDNS: "PowerDNS"}
# Fixture catalog formats a native primary peer can serve (native_primary_peer.py
# --catalog-format) and the producer the Agent's reader must accept for each.
CATALOG_FORMATS = {"bind": PRODUCER_BIND, "pdns-native": PRODUCER_POWERDNS}

ENGINE_UNITS = {"bind": "named.service", "pdns": "pdns.service"}
FOREIGN_UNITS = {
    "bind": ("pdns.service",),
    "pdns": ("named.service", "bind9.service"),
}
MANAGEMENT_BINARIES = ("/opt/celikpanel/bin/agent", "/opt/celikpanel/bin/panel")
LOG_KEYWORDS = re.compile(r"(axfr|ixfr|transfer of|notif)", re.IGNORECASE)
LOG_LINE_LIMIT = 400


class CatalogAXFRError(ValueError):
    """The catalog transfer differs from what the Agent's reader accepts."""


class CatalogAXFRFormatError(CatalogAXFRError):
    """errDNSCatalogAXFRProducerFormat: a refusal only the producer format explains.

    Raised for the two encodings the BIND and PowerDNS producers write
    differently: the TTL (0 or 60) of the version/member records and a member
    label of exactly the other producer's shape. Every other refusal is common
    to both formats and is never retried in the other format.
    """


# ---------------------------------------------------------------------------
# Names (internal/hostname.CanonicalFQDN and cmd/agent encode/decodeDNSName).
# ---------------------------------------------------------------------------

def canonical_fqdn(value: str) -> bool:
    """servicemutationledger.ServiceMutationCanonicalFQDN, for ASCII input."""

    if not isinstance(value, str) or not value or len(value) > 253:
        return False
    if value != value.strip().lower().rstrip(".") or value.endswith("."):
        return False
    if "." not in value:
        return False
    try:
        ipaddress.ip_address(value)
        return False
    except ValueError:
        pass
    for label in value.split("."):
        if not label or len(label) > 63 or label[0] == "-" or label[-1] == "-":
            return False
        if any(not ("a" <= char <= "z" or "0" <= char <= "9" or char == "-")
               for char in label):
            return False
    return True


def encode_name(domain: str) -> bytes:
    if domain == "" or domain.endswith("."):
        raise CatalogAXFRError("DNS query name must be canonical")
    encoded = bytearray()
    for label in domain.split("."):
        raw = label.encode("ascii")
        if not raw or len(raw) > 63:
            raise CatalogAXFRError("DNS query name has an invalid label")
        encoded.append(len(raw))
        encoded += raw
    encoded.append(0)
    if len(encoded) > 255:
        raise CatalogAXFRError("DNS query name is too long")
    return bytes(encoded)


def decode_name(message: bytes, offset: int) -> tuple[str, int]:
    if offset < 0 or offset >= len(message):
        raise CatalogAXFRError("DNS name offset is outside the packet")
    labels: list[str] = []
    following = -1
    visited: set[int] = set()
    for _ in range(128):
        if offset >= len(message) or offset in visited:
            raise CatalogAXFRError("DNS name compression is cyclic or truncated")
        visited.add(offset)
        length = message[offset]
        kind = length & 0xC0
        if kind == 0xC0:
            if offset + 1 >= len(message):
                raise CatalogAXFRError("DNS name pointer is truncated")
            if following < 0:
                following = offset + 2
            offset = ((length & 0x3F) << 8) | message[offset + 1]
            continue
        if kind != 0:
            raise CatalogAXFRError("DNS name uses an unsupported label encoding")
        offset += 1
        if length == 0:
            if following < 0:
                following = offset
            return ".".join(labels) + ".", following
        if length > 63 or offset + length > len(message):
            raise CatalogAXFRError("DNS name label is invalid")
        labels.append(message[offset:offset + length].decode("latin-1"))
        offset += length
    raise CatalogAXFRError("DNS name exceeds the compression step limit")


# ---------------------------------------------------------------------------
# Catalog identity (internal/binddns/pairing.go).
# ---------------------------------------------------------------------------

def catalog_name(primary_ip: str) -> str:
    """binddns.CatalogDomain: catalog-<hex(IPv4)>.celikpanel.invalid."""

    address = ipaddress.IPv4Address(primary_ip)
    if str(address) != primary_ip:
        raise ValueError("catalog primary IPv4 address must be canonical")
    return "catalog-" + address.packed.hex() + CATALOG_SUFFIX


def catalog_member_label(member: str) -> str:
    """binddns.CatalogMemberLabel: lowercase SHA-224 hex of the member."""

    if not canonical_fqdn(member):
        raise ValueError("catalog member is not canonical")
    return hashlib.sha224(member.encode("ascii")).hexdigest()


def exact_bind_member_owner(owner: str, catalog: str, member: str) -> bool:
    """exactDNSCatalogMemberOwner: <sha224hex(member)>.zones.<catalog>."""

    suffix = ".zones." + catalog
    if not owner.endswith(suffix):
        return False
    label = owner[: -len(suffix)]
    if len(label) != BIND_MEMBER_LABEL_SIZE or "." in label:
        return False
    if any(not ("0" <= char <= "9" or "a" <= char <= "f") for char in label):
        return False
    try:
        return label == catalog_member_label(member)
    except ValueError:
        return False


def exact_pdns_member_owner(owner: str, catalog: str) -> bool:
    """exactPDNSCatalogMemberOwner: a 32-character base32hex label under .zones."""

    suffix = ".zones." + catalog
    if not owner.endswith(suffix):
        return False
    label = owner[: -len(suffix)]
    if len(label) != PDNS_MEMBER_LABEL_SIZE or "." in label:
        return False
    return all("0" <= char <= "9" or "a" <= char <= "v" for char in label)


# ---------------------------------------------------------------------------
# Port of cmd/agent/dns_catalog_axfr.go dnsCatalogAXFRState.
# ---------------------------------------------------------------------------

class CatalogAXFRState:
    """Incremental catalog AXFR reader with the Agent's exact rules.

    One instance reads one transfer in one producer format: BIND (every record
    TTL 60, SHA-224 member labels) or PowerDNS (TTL 0 on the version and member
    records, 32-character base32hex member labels). Which format a PEER catalog
    is read in is decided by select_peer_catalog, the port of
    cmd/agent/dns_peer_catalog.go selectDNSPeerCatalogAXFR.
    """

    def __init__(self, query_id: int, catalog: str, producer: str = PRODUCER_BIND):
        if producer not in (PRODUCER_BIND, PRODUCER_POWERDNS):
            raise CatalogAXFRError("catalog AXFR producer is invalid")
        if not canonical_fqdn(catalog):
            raise CatalogAXFRError("BIND catalog AXFR identity is invalid")
        self.query_id = query_id
        self.catalog = catalog
        self.producer = producer
        self.question_seen = False
        self.opened = False
        self.closed = False
        self.soa_count = 0
        self.serial = 0
        self.ns_seen = False
        self.version_seen = False
        self.record_owners: set[str] = set()
        self.members: set[str] = set()

    def _claim(self, owner: str) -> None:
        if owner in self.record_owners:
            raise CatalogAXFRError("BIND catalog AXFR contains a duplicate record owner")
        self.record_owners.add(owner)

    def _exact_member_owner(self, owner: str, member: str) -> bool:
        if self.producer == PRODUCER_BIND:
            return exact_bind_member_owner(owner, self.catalog, member)
        return exact_pdns_member_owner(owner, self.catalog)

    def _other_producer_member_owner(self, owner: str, member: str) -> bool:
        """otherProducerMemberOwner: the refused owner is the other producer's shape."""

        if self.producer == PRODUCER_BIND:
            return exact_pdns_member_owner(owner, self.catalog)
        return exact_bind_member_owner(owner, self.catalog, member)

    def _parse_soa(self, message: bytes, owner: str, rdata: int, end: int) -> int:
        if owner != self.catalog:
            raise CatalogAXFRError("BIND catalog AXFR contains a foreign SOA")
        mname, numbers = decode_name(message, rdata)
        if mname != "invalid.":
            raise CatalogAXFRError("BIND catalog AXFR SOA primary is not exact")
        rname, numbers = decode_name(message, numbers)
        if rname != "invalid." or numbers + 20 != end:
            raise CatalogAXFRError("BIND catalog AXFR SOA payload is invalid")
        serial, refresh, retry, expire, minimum = struct.unpack(
            "!IIIII", message[numbers:numbers + 20]
        )
        if serial == 0 or (refresh, retry, expire, minimum) != (60, 30, 3600, 30):
            raise CatalogAXFRError("BIND catalog AXFR SOA timers are not exact")
        return serial

    def parse_message(self, message: bytes) -> None:
        if self.closed:
            raise CatalogAXFRError("BIND catalog AXFR contains data after its closing SOA")
        if len(message) < 12 or struct.unpack("!H", message[:2])[0] != self.query_id:
            raise CatalogAXFRError("BIND catalog AXFR response identity mismatch")
        flags, questions, answers, authorities, additionals = struct.unpack(
            "!HHHHH", message[2:12]
        )
        if flags != FLAG_QR | FLAG_AA:
            raise CatalogAXFRError("BIND catalog AXFR response flags are not exact")
        if ((not self.question_seen and questions != 1)
                or (self.question_seen and questions > 1)
                or answers == 0 or authorities != 0 or additionals != 0):
            raise CatalogAXFRError("BIND catalog AXFR section counts are not exact")
        offset = 12
        if questions == 1:
            name, following = decode_name(message, offset)
            if (name != self.catalog + "." or following + 4 > len(message)
                    or struct.unpack("!HH", message[following:following + 4])
                    != (DNS_TYPE_AXFR, DNS_CLASS_IN)):
                raise CatalogAXFRError("BIND catalog AXFR question is invalid")
            offset = following + 4
            self.question_seen = True
        for index in range(answers):
            owner_raw, following = decode_name(message, offset)
            if following + 10 > len(message):
                raise CatalogAXFRError("BIND catalog AXFR record is invalid")
            owner = owner_raw[:-1] if owner_raw.endswith(".") else owner_raw
            if owner_raw != owner + "." or not canonical_fqdn(owner):
                raise CatalogAXFRError("BIND catalog AXFR owner is not canonical")
            record_type, record_class, ttl, length = struct.unpack(
                "!HHIH", message[following:following + 10]
            )
            rdata = following + 10
            end = rdata + length
            if end > len(message):
                raise CatalogAXFRError("BIND catalog AXFR record exceeds its message")
            expected_ttl = CATALOG_TTL
            producer_ttl = record_type in (DNS_TYPE_TXT, DNS_TYPE_PTR)
            if self.producer == PRODUCER_POWERDNS and producer_ttl:
                expected_ttl = 0
            if record_class != DNS_CLASS_IN:
                raise CatalogAXFRError("BIND catalog AXFR record class or TTL is not exact")
            if ttl != expected_ttl:
                if producer_ttl and ttl in (0, CATALOG_TTL):
                    raise CatalogAXFRFormatError(
                        "BIND catalog AXFR record class or TTL is not exact"
                    )
                raise CatalogAXFRError("BIND catalog AXFR record class or TTL is not exact")
            if not self.opened and record_type != DNS_TYPE_SOA:
                raise CatalogAXFRError("BIND catalog AXFR does not start with its SOA")
            if record_type == DNS_TYPE_SOA:
                serial = self._parse_soa(message, owner, rdata, end)
                if self.soa_count == 0:
                    if index != 0:
                        raise CatalogAXFRError("BIND catalog AXFR opening SOA is misplaced")
                    self.opened = True
                    self.serial = serial
                elif self.soa_count == 1:
                    if serial != self.serial or index != answers - 1:
                        raise CatalogAXFRError("BIND catalog AXFR closing SOA is not exact")
                    self.closed = True
                else:
                    raise CatalogAXFRError("BIND catalog AXFR has more than two SOAs")
                self.soa_count += 1
            elif record_type == DNS_TYPE_NS:
                self._claim(owner)
                if owner != self.catalog or self.ns_seen:
                    raise CatalogAXFRError("BIND catalog AXFR NS is not unique at the apex")
                target, target_end = decode_name(message, rdata)
                if target_end != end or target != "invalid.":
                    raise CatalogAXFRError("BIND catalog AXFR NS target is not exact")
                self.ns_seen = True
            elif record_type == DNS_TYPE_TXT:
                self._claim(owner)
                if (owner != "version." + self.catalog or self.version_seen
                        or length != 2 or message[rdata:rdata + 2] != b"\x012"):
                    raise CatalogAXFRError("BIND catalog AXFR version property is not exact")
                self.version_seen = True
            elif record_type == DNS_TYPE_PTR:
                self._claim(owner)
                member_raw, member_end = decode_name(message, rdata)
                member = member_raw[:-1] if member_raw.endswith(".") else member_raw
                if (member_end != end or member_raw != member + "."
                        or not canonical_fqdn(member) or member == self.catalog):
                    raise CatalogAXFRError("BIND catalog AXFR member PTR is not exact")
                if not self._exact_member_owner(owner, member):
                    if self._other_producer_member_owner(owner, member):
                        raise CatalogAXFRFormatError(
                            "BIND catalog AXFR member PTR is not exact"
                        )
                    raise CatalogAXFRError("BIND catalog AXFR member PTR is not exact")
                if member in self.members:
                    raise CatalogAXFRError("BIND catalog AXFR contains a duplicate member")
                self.members.add(member)
                if len(self.members) > AXFR_MAX_MEMBERS:
                    raise CatalogAXFRError("BIND catalog AXFR exceeds the member limit")
            elif record_type == DNS_TYPE_APL:
                raise CatalogAXFRError(
                    "BIND catalog AXFR contains an unsupported transfer ACL property"
                )
            else:
                raise CatalogAXFRError("BIND catalog AXFR contains an unsupported record type")
            offset = end
        if offset != len(message):
            raise CatalogAXFRError("BIND catalog AXFR response contains trailing bytes")

    def result(self) -> tuple[int, list[str]]:
        if (not self.question_seen or not self.opened or not self.closed
                or self.soa_count != 2 or self.serial == 0):
            raise CatalogAXFRError("BIND catalog AXFR has an invalid SOA envelope")
        if not self.ns_seen or not self.version_seen:
            raise CatalogAXFRError("BIND catalog AXFR base records are incomplete")
        return self.serial, sorted(self.members)


def read_catalog_axfr_stream(
    stream: bytes, query_id: int, catalog: str, producer: str = PRODUCER_BIND
) -> tuple[int, list[str]]:
    """readDNSCatalogAXFRWithProducer over an in-memory TCP byte stream."""

    state = CatalogAXFRState(query_id, catalog, producer)
    offset = 0
    total = 0
    for _ in range(AXFR_MAX_MESSAGES):
        if offset + 2 > len(stream):
            raise CatalogAXFRError("BIND catalog AXFR stream ended early")
        size = struct.unpack("!H", stream[offset:offset + 2])[0]
        offset += 2
        if size < 12 or total + size > AXFR_MAX_BYTES:
            raise CatalogAXFRError("BIND catalog AXFR exceeded its safe response bound")
        if offset + size > len(stream):
            raise CatalogAXFRError("BIND catalog AXFR stream ended early")
        state.parse_message(stream[offset:offset + size])
        offset += size
        total += size
        if state.closed:
            # Like the Agent, stop at the closing SOA; later bytes are unread.
            return state.result()
    raise CatalogAXFRError("BIND catalog AXFR did not terminate")


def select_peer_catalog(read, catalog: str) -> tuple[int, list[str], str]:
    """Port of selectDNSPeerCatalogAXFR (cmd/agent/dns_peer_catalog.go).

    ``read(producer)`` performs one FRESH transfer read in that producer
    format. The BIND format is tried first; only a CatalogAXFRFormatError
    (a refusal the two formats encode differently) is retried in the PowerDNS
    format. Transport errors and refusals common to both formats propagate
    unchanged. If both formats refuse, the error names both reasons, as the
    Agent's dnsPeerCatalogFormatError does. Returns (serial, members, producer).
    """

    try:
        serial, members = read(PRODUCER_BIND)
        return serial, members, PRODUCER_BIND
    except CatalogAXFRFormatError as bind_error:
        try:
            serial, members = read(PRODUCER_POWERDNS)
        except (CatalogAXFRError, OSError) as pdns_error:
            def reason(error: Exception) -> str:
                return str(error).removeprefix("BIND catalog AXFR ")

            raise CatalogAXFRError(
                f"the paired primary's catalog {catalog} matches neither supported "
                f"catalog format (BIND format: {reason(bind_error)}; "
                f"PowerDNS format: {reason(pdns_error)})"
            ) from pdns_error
        return serial, members, PRODUCER_POWERDNS


# ---------------------------------------------------------------------------
# Live, read-only DNS queries (no dig dependency).
# ---------------------------------------------------------------------------

def build_query(name: str, qtype: int, query_id: int) -> bytes:
    # Flags 0: no RD, exactly like the Agent's buildDNSCatalogAXFRQuery.
    return struct.pack("!HHHHHH", query_id, 0, 1, 0, 0, 0) + encode_name(name) + struct.pack(
        "!HH", qtype, DNS_CLASS_IN
    )


def _random_id() -> int:
    return struct.unpack("!H", os.urandom(2))[0]


def _read_exact(connection: socket.socket, size: int) -> bytes:
    data = bytearray()
    while len(data) < size:
        chunk = connection.recv(size - len(data))
        if not chunk:
            raise OSError("DNS TCP stream closed early")
        data += chunk
    return bytes(data)


def query_catalog_axfr(address: str, catalog: str, timeout: float = 8.0,
                       producer: str = PRODUCER_BIND) -> tuple[int, list[str]]:
    query_id = _random_id()
    query = build_query(catalog, DNS_TYPE_AXFR, query_id)
    state = CatalogAXFRState(query_id, catalog, producer)
    with socket.create_connection((address, 53), timeout=timeout) as connection:
        connection.sendall(struct.pack("!H", len(query)) + query)
        total = 0
        for _ in range(AXFR_MAX_MESSAGES):
            size = struct.unpack("!H", _read_exact(connection, 2))[0]
            if size < 12 or total + size > AXFR_MAX_BYTES:
                raise CatalogAXFRError("BIND catalog AXFR exceeded its safe response bound")
            state.parse_message(_read_exact(connection, size))
            total += size
            if state.closed:
                return state.result()
    raise CatalogAXFRError("BIND catalog AXFR did not terminate")


def parse_rrset_response(message: bytes, query_id: int, name: str, qtype: int) -> dict:
    if len(message) < 12 or struct.unpack("!H", message[:2])[0] != query_id:
        raise ValueError("DNS response identity mismatch")
    flags, questions, answers, authorities, additionals = struct.unpack("!HHHHH", message[2:12])
    if flags & FLAG_QR == 0 or flags & FLAG_TC or questions != 1:
        raise ValueError("DNS response is not a complete answer")
    question, offset = decode_name(message, 12)
    if (question.lower() != name + "." or offset + 4 > len(message)
            or struct.unpack("!HH", message[offset:offset + 4]) != (qtype, DNS_CLASS_IN)):
        raise ValueError("DNS response question differs")
    offset += 4
    values: list = []
    for index in range(answers + authorities + additionals):
        owner, following = decode_name(message, offset)
        if following + 10 > len(message):
            raise ValueError("DNS response record is invalid")
        record_type, record_class, _ttl, length = struct.unpack(
            "!HHIH", message[following:following + 10]
        )
        rdata = following + 10
        end = rdata + length
        if end > len(message):
            raise ValueError("DNS response record exceeds its message")
        if index < answers and owner.lower() == name + "." and record_class == DNS_CLASS_IN:
            if record_type == DNS_TYPE_SOA == qtype:
                _mname, numbers = decode_name(message, rdata)
                _rname, numbers = decode_name(message, numbers)
                if numbers + 20 != end:
                    raise ValueError("DNS SOA payload is invalid")
                values.append(struct.unpack("!I", message[numbers:numbers + 4])[0])
            elif record_type == DNS_TYPE_A == qtype:
                if length != 4:
                    raise ValueError("DNS A payload is invalid")
                values.append(str(ipaddress.IPv4Address(message[rdata:end])))
        offset = end
    if offset != len(message):
        raise ValueError("DNS response contains trailing bytes")
    return {
        "authoritative": bool(flags & FLAG_AA),
        "rcode": flags & RCODE_MASK,
        "answers": answers,
        "values": sorted(values, key=str),
    }


def query_rrset(address: str, name: str, qtype: int, *, tcp: bool, timeout: float = 4.0) -> dict:
    query_id = _random_id()
    query = build_query(name, qtype, query_id)
    if tcp:
        with socket.create_connection((address, 53), timeout=timeout) as connection:
            connection.sendall(struct.pack("!H", len(query)) + query)
            size = struct.unpack("!H", _read_exact(connection, 2))[0]
            response = _read_exact(connection, size)
    else:
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as connection:
            connection.settimeout(timeout)
            connection.sendto(query, (address, 53))
            response, _peer = connection.recvfrom(65535)
    return parse_rrset_response(response, query_id, name, qtype)


# ---------------------------------------------------------------------------
# Native service log evidence.
# ---------------------------------------------------------------------------

def transfer_log_lines(lines: Iterable[str], names: Iterable[str]) -> list[str]:
    """Bounded, keyword-filtered lines that mention one of the fixture zones."""

    wanted = tuple(names)
    selected = [
        line.rstrip("\n") for line in lines
        if LOG_KEYWORDS.search(line) and any(name in line for name in wanted)
    ]
    return selected[-LOG_LINE_LIMIT:]


def transfers_to(lines: Iterable[str], zone: str, secondary: str) -> bool:
    """True when a logged outgoing transfer of ``zone`` names ``secondary``.

    BIND: "client @0x.. 192.0.2.10#53001 (zone): transfer of 'zone/IN': AXFR started".
    PowerDNS: "AXFR-out zone 'zone', client '192.0.2.10:53001', transfer initiated".
    """

    address = re.escape(secondary)
    pattern = re.compile(
        rf"(\b{address}#\d+ \({re.escape(zone)}\): transfer of '{re.escape(zone)}/IN'"
        rf"|[AI]XFR-out zone '{re.escape(zone)}\.?', client '{address}:\d+')"
    )
    return any(pattern.search(line) for line in lines)


def read_service_journal(unit: str) -> list[str]:
    output = subprocess.run(
        ["journalctl", "--no-pager", "-o", "cat", "-b", "-u", unit],
        check=True, capture_output=True, text=True, timeout=20,
    ).stdout
    return output.splitlines()


def file_digest(path: str) -> str | None:
    try:
        with open(path, "rb") as handle:
            return hashlib.sha256(handle.read()).hexdigest()
    except FileNotFoundError:
        return None


def unit_active(unit: str) -> bool:
    return subprocess.run(
        ["systemctl", "is-active", "--quiet", unit], check=False
    ).returncode == 0


# ---------------------------------------------------------------------------
# Observation.
# ---------------------------------------------------------------------------

def read_pdns_catalog_rows(database: str, catalog: str) -> dict:
    """Read-only SQL view of the PowerDNS catalog rows (recorded, not judged).

    The domains rows and the catalog's domainmetadata kinds show whether the
    peer publishes a PRODUCER catalog with assigned members (pdns-native) or an
    ordinary MASTER catalog zone (bind format). PowerDNS may add its own
    metadata while it serves; that is recorded as observed.
    """

    import sqlite3  # noqa: PLC0415 - only the PowerDNS flavour needs it

    try:
        connection = sqlite3.connect(f"file:{database}?mode=ro", uri=True, timeout=5)
        try:
            domains = connection.execute(
                "SELECT name, UPPER(type), COALESCE(catalog,''), COALESCE(account,'') "
                "FROM domains ORDER BY name COLLATE BINARY"
            ).fetchall()
            metadata = connection.execute(
                "SELECT m.kind FROM domainmetadata m JOIN domains d ON m.domain_id = d.id "
                "WHERE d.name = ? ORDER BY m.kind COLLATE BINARY", (catalog,)
            ).fetchall()
        finally:
            connection.close()
    except sqlite3.Error as exc:
        return {"read": False, "error": str(exc)}
    return {
        "read": True,
        "domains": [list(row) for row in domains],
        "catalog_metadata_kinds": [row[0] for row in metadata],
    }


def validate_identity(engine: str, primary: str, secondary: str, catalog: str) -> None:
    if engine not in ENGINE_UNITS:
        raise ValueError("unsupported native primary engine")
    first = ipaddress.IPv4Address(primary)
    second = ipaddress.IPv4Address(secondary)
    if str(first) != primary or str(second) != secondary:
        raise ValueError("peer addresses must be canonical IPv4")
    if first == second or first not in PEER_NETWORK or second not in PEER_NETWORK:
        raise ValueError("peer addresses must be distinct on the isolated 192.0.2.0/24 link")
    if catalog != catalog_name(primary):
        raise ValueError("catalog identity differs from the primary address")


def validate_catalog_format(engine: str, catalog_format: str) -> str:
    """The producer the fixture format must be served as; BIND serves only bind."""

    if catalog_format not in CATALOG_FORMATS:
        raise ValueError(f"unsupported catalog format {catalog_format!r}")
    if engine == "bind" and catalog_format != "bind":
        raise ValueError("a native BIND primary serves the catalog in the bind format only")
    return CATALOG_FORMATS[catalog_format]


def observe(engine: str, primary: str, secondary: str, catalog: str,
            expected_members: list[str], config_paths: list[str],
            require_secondary_transfer: bool, catalog_format: str = "bind",
            pdns_database: str | None = None) -> dict:
    validate_identity(engine, primary, secondary, catalog)
    expected_producer = validate_catalog_format(engine, catalog_format)
    if any(os.path.lexists(path) for path in MANAGEMENT_BINARIES):
        raise ValueError("management software is present on the native primary peer")
    unit = ENGINE_UNITS[engine]
    if not unit_active(unit):
        raise ValueError(f"native {unit} is inactive")
    if any(unit_active(other) for other in FOREIGN_UNITS[engine]):
        raise ValueError("another DNS engine is active on the native primary peer")
    serial, members, producer = select_peer_catalog(
        lambda selected: query_catalog_axfr("127.0.0.1", catalog, producer=selected),
        catalog,
    )
    if producer != expected_producer:
        raise ValueError(
            f"native primary serves its catalog in the {AGENT_PRODUCER_NAMES[producer]} "
            f"format, but the fixture was prepared with --catalog-format {catalog_format}"
        )
    if members != sorted(expected_members):
        raise ValueError("native primary catalog members differ from the fixture expectation")
    member_soa: dict[str, dict] = {}
    for member in members:
        replies = {
            "udp": query_rrset(primary, member, DNS_TYPE_SOA, tcp=False),
            "tcp": query_rrset(primary, member, DNS_TYPE_SOA, tcp=True),
        }
        for reply in replies.values():
            if not reply["authoritative"] or reply["rcode"] != 0 or len(reply["values"]) != 1:
                raise ValueError(f"native primary is not authoritative for {member}")
        if replies["udp"]["values"] != replies["tcp"]["values"]:
            raise ValueError(f"native primary UDP/TCP SOA serials differ for {member}")
        member_soa[member] = {"serial": replies["udp"]["values"][0], "authoritative_udp_tcp": True}
    catalog_soa = query_rrset(primary, catalog, DNS_TYPE_SOA, tcp=False)
    if (not catalog_soa["authoritative"] or catalog_soa["rcode"] != 0
            or catalog_soa["values"] != [serial]):
        raise ValueError("native primary catalog SOA differs from its AXFR")
    www = None
    if ZONE in members:
        www = query_rrset(primary, QUERY, DNS_TYPE_A, tcp=False)
    journal = read_service_journal(unit)
    names = [catalog] + members
    lines = transfer_log_lines(journal, names)
    transferred = {name: transfers_to(lines, name, secondary) for name in names}
    if require_secondary_transfer and not all(transferred.values()):
        missing = sorted(name for name, seen in transferred.items() if not seen)
        raise ValueError("no logged transfer to the secondary for: " + ", ".join(missing))
    return {
        "schema": "celikpanel/native-primary-peer-observation/v1",
        "engine": engine,
        "unit": unit,
        "primary_ip": primary,
        "secondary_ip": secondary,
        "catalog": catalog,
        "catalog_serial": serial,
        "catalog_members": members,
        "catalog_parser": "agent-peer-catalog-selection",
        "catalog_format": catalog_format,
        "catalog_producer": producer,
        "agent_catalog_format_name": AGENT_PRODUCER_NAMES[producer],
        "pdns_catalog_rows": (
            read_pdns_catalog_rows(pdns_database, catalog)
            if engine == "pdns" and pdns_database else None
        ),
        "member_soa": member_soa,
        "www_a": www["values"] if www else None,
        "transfers_to_secondary": transferred,
        "transfer_notify_log": lines,
        "config_sha256": {path: file_digest(path) for path in config_paths},
        "management_installed_on_primary": False,
        "native_service_active": True,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--engine", choices=sorted(ENGINE_UNITS), required=True)
    parser.add_argument("--primary", required=True)
    parser.add_argument("--secondary", required=True)
    parser.add_argument("--catalog", required=True)
    parser.add_argument("--member", action="append", default=None,
                        help="expected catalog member; repeat; default s1-kill.test")
    parser.add_argument("--config", action="append", default=[],
                        help="native config or zone file to digest (read-only)")
    parser.add_argument("--require-secondary-transfer", action="store_true")
    parser.add_argument("--catalog-format", choices=sorted(CATALOG_FORMATS), default="bind",
                        help="catalog producer format the fixture peer was prepared with")
    parser.add_argument("--pdns-database", default=None,
                        help="PowerDNS SQLite database to read catalog rows from (read-only)")
    args = parser.parse_args()
    members = args.member if args.member is not None else [ZONE]
    try:
        print(json.dumps(observe(
            args.engine, args.primary, args.secondary, args.catalog, members,
            args.config, args.require_secondary_transfer,
            catalog_format=args.catalog_format, pdns_database=args.pdns_database,
        ), sort_keys=True))
        return 0
    except (ValueError, OSError, subprocess.SubprocessError) as exc:
        print(f"native primary peer observation unavailable: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
