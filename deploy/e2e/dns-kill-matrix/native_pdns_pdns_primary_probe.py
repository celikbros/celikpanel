#!/usr/bin/env python3
"""Read-only native PowerDNS primary witness for a disposable DNS pair.

This is a fixture observation, not an Agent receipt or a mutation verdict.
It runs on the Debian primary after a separately reviewed production switch.
"""

from __future__ import annotations

import argparse
import ipaddress
import json
from pathlib import Path
import re
import sqlite3
import subprocess
import sys

from native_pdns_peer_probe import dig, parse_catalog_axfr


ZONE = "s1-kill.test"
ACCOUNT = "celikpanel-bind-catalog-v1"


def authoritative_a(reply: str, address: str) -> bool:
    if "status: NOERROR" not in reply or re.search(r"flags: qr aa\b", reply) is None:
        return False
    rows = [line.split() for line in reply.splitlines() if line and not line.startswith(";")]
    if len(rows) != 1 or rows[0][0] != "www." + ZONE + "." or "IN" not in rows[0]:
        return False
    index = rows[0].index("IN")
    return rows[0][index + 1:] == ["A", address]


def observe(primary: str, secondary: str, address: str) -> dict:
    if (primary, secondary) != ("192.0.2.10", "192.0.2.11"):
        raise ValueError("disposable pair identity differs")
    ipaddress.IPv4Address(address)
    if subprocess.run(["systemctl", "is-active", "--quiet", "pdns.service"],
                      check=False).returncode:
        raise ValueError("native primary pdns.service is inactive")
    for unit in ("bind9.service", "named.service"):
        if subprocess.run(["systemctl", "is-active", "--quiet", unit],
                          check=False).returncode == 0:
            raise ValueError("BIND still serves on the purported PowerDNS primary")
    state = json.loads(Path("/var/lib/celikpanel-agent-private/dns-engine-state.json").read_text(encoding="utf-8"))
    if state.get("schema") == "celikpanel-dns-engine-state/v2":
        identity = state.get("acquisition", {})
        publication = state.get("publication", {})
    elif state.get("schema") == "celikpanel-dns-engine-state/v1":
        identity = publication = state
    else:
        raise ValueError("managed DNS state receipt schema differs")
    if (identity.get("engine"), identity.get("pair_role"),
            identity.get("pair_local_ip"), identity.get("pair_peer_ip")) != (
            "pdns", "primary", primary, secondary):
        raise ValueError("managed DNS state is not this PowerDNS primary")
    catalog = "catalog-" + ipaddress.IPv4Address(primary).packed.hex() + ".celikpanel.invalid"
    db = sqlite3.connect("file:/var/lib/powerdns/pdns.sqlite3?mode=ro", uri=True)
    try:
        producer = db.execute(
            "SELECT name,type,COALESCE(master,''),account FROM domains "
            "WHERE name=? COLLATE NOCASE", (catalog,)
        ).fetchall()
        member = db.execute(
            "SELECT name,type,COALESCE(master,''),catalog FROM domains "
            "WHERE name=? COLLATE NOCASE", (ZONE,)
        ).fetchall()
        records = db.execute(
            "SELECT name,type,content FROM records WHERE domain_id IN "
            "(SELECT id FROM domains WHERE name=? COLLATE NOCASE) "
            "AND name=? COLLATE NOCASE AND type='A'",
            (ZONE, "www." + ZONE),
        ).fetchall()
    finally:
        db.close()
    if producer != [(catalog, "PRODUCER", "", ACCOUNT)]:
        raise ValueError("native PowerDNS producer row differs")
    if member != [(ZONE, "NATIVE", "", catalog)]:
        raise ValueError("native PowerDNS member row differs")
    if records != [("www." + ZONE, "A", address)]:
        raise ValueError("native PowerDNS member A row differs")
    axfr = dig("127.0.0.1", catalog, "AXFR", tcp=True)
    serial, members = parse_catalog_axfr(axfr, catalog)
    if publication.get("primary_catalog_serial") != serial:
        raise ValueError("managed publication serial differs from native catalog")
    if members != {ZONE}:
        raise ValueError("PowerDNS producer catalog membership differs")
    for transport in (False, True):
        reply = dig(primary, "www." + ZONE, "A", tcp=transport)
        if not authoritative_a(reply, address):
            raise ValueError("native primary UDP/TCP answer is not authoritative")
    # AXFR is allowed from the enrolled peer and loopback, not arbitrary hosts.
    # The paired secondary fixture separately checks loaded SQLite state.
    return {
        "schema": "celikpanel/native-pdns-primary-observation/v1",
        "primary_ip": primary, "secondary_ip": secondary, "catalog": catalog,
        "catalog_serial": serial, "catalog_members": sorted(members),
        "producer_row": producer[0], "member_row": member[0],
        "member_a_records": records, "authoritative_udp_tcp": True,
        "native_service_active": True,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--primary", required=True)
    parser.add_argument("--secondary", required=True)
    parser.add_argument("--address", required=True)
    args = parser.parse_args()
    try:
        print(json.dumps(observe(args.primary, args.secondary, args.address), sort_keys=True))
        return 0
    except (ValueError, sqlite3.Error, OSError, subprocess.SubprocessError) as exc:
        print(f"native PowerDNS primary observation unavailable: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
