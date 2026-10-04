#!/usr/bin/env python3
"""Read-only native catalog/member observation on a disposable Arch peer."""

from __future__ import annotations

import argparse
import hashlib
import ipaddress
import json
import sqlite3
import subprocess
import sys


def dig(address: str, name: str, kind: str, *, tcp: bool = False) -> str:
    command = ["dig", "+time=2", "+tries=1", "+norecurse", "+noall", "+comments", "+answer"]
    if tcp:
        command.append("+tcp")
    command.extend(["@" + address, name, kind])
    return subprocess.run(command, check=True, capture_output=True, text=True, timeout=8).stdout


def parse_catalog_axfr(answer: str, catalog: str, *, producer: str = "bind") -> tuple[int, set[str]]:
    if producer not in {"bind", "powerdns"}:
        raise ValueError("unsupported catalog AXFR producer")
    if "status: NOERROR" not in answer:
        raise ValueError("primary catalog AXFR has no verified success status")
    rows: list[tuple[str, str, tuple[str, ...]]] = []
    for line in answer.splitlines():
        if not line or line.startswith((";", "#")):
            continue
        fields = line.split()
        if len(fields) < 4 or "IN" not in fields:
            raise ValueError("primary catalog AXFR contains an unparsed record")
        index = fields.index("IN")
        if index < 1 or index + 2 >= len(fields):
            raise ValueError("primary catalog AXFR record shape differs")
        rows.append((fields[0], fields[index + 1], tuple(fields[index + 2:])))
    apex = catalog + "."
    soa = [data for owner, kind, data in rows if owner == apex and kind == "SOA"]
    if len(soa) != 2 or soa[0] != soa[1] or len(soa[0]) != 7:
        raise ValueError("primary catalog AXFR SOA framing differs")
    if soa[0][:2] != ("invalid.", "invalid.") or not soa[0][2].isdigit():
        raise ValueError("primary catalog AXFR SOA identity differs")
    serial = int(soa[0][2])
    if not 0 < serial <= 0xFFFFFFFF:
        raise ValueError("primary catalog AXFR serial is invalid")
    members: set[str] = set()
    counts = {"SOA": 0, "NS": 0, "TXT": 0}
    for owner, kind, data in rows:
        if kind == "SOA" and owner == apex:
            counts["SOA"] += 1
        elif kind == "NS" and owner == apex and data == ("invalid.",):
            counts["NS"] += 1
        elif kind == "TXT" and owner == "version." + apex and data == ('"2"',):
            counts["TXT"] += 1
        elif kind == "PTR":
            if len(data) != 1 or not data[0].endswith("."):
                raise ValueError("primary catalog PTR target differs")
            member = data[0][:-1]
            labels = member.split(".")
            if (not member or member != member.lower() or len(member) > 253
                    or any(not label or len(label) > 63 or label[0] == "-" or label[-1] == "-"
                           or any((char < "a" or char > "z") and (char < "0" or char > "9")
                                  and char != "-" for char in label)
                           for label in labels)):
                raise ValueError("primary catalog PTR member is not canonical")
            if producer == "bind":
                expected_owner = (hashlib.sha224(member.encode("ascii")).hexdigest()
                                  + ".zones." + apex)
                exact_owner = owner == expected_owner
            else:
                suffix = ".zones." + apex
                label = owner[:-len(suffix)] if owner.endswith(suffix) else ""
                exact_owner = (len(label) == 32 and all("0" <= char <= "9" or "a" <= char <= "v"
                                                       for char in label))
            if not exact_owner or member in members:
                raise ValueError("primary catalog PTR owner or member differs")
            members.add(member)
        else:
            raise ValueError("primary catalog AXFR contains an unexpected record")
    if counts != {"SOA": 2, "NS": 1, "TXT": 1}:
        raise ValueError("primary catalog AXFR required records differ")
    return serial, members


def observe(primary: str, secondary: str, catalog: str, zone: str,
            expected: str, address: str | None) -> dict:
    for value in (primary, secondary):
        ipaddress.IPv4Address(value)
    if catalog != "catalog-" + ipaddress.IPv4Address(primary).packed.hex() + ".celikpanel.invalid":
        raise ValueError("catalog identity differs from primary address")
    if zone != "s1-kill.test" or expected not in {"present", "absent"}:
        raise ValueError("unsupported fixture zone or expectation")
    if expected == "present" and address is None:
        raise ValueError("present member requires an expected A address")
    if address is not None:
        ipaddress.IPv4Address(address)
    if subprocess.run(["systemctl", "is-active", "--quiet", "pdns.service"], check=False).returncode:
        raise ValueError("native pdns.service is inactive")
    db = sqlite3.connect("file:/var/lib/powerdns/pdns.sqlite3?mode=ro", uri=True)
    try:
        catalog_rows = db.execute(
            "SELECT name,type,master,account FROM domains WHERE name=? COLLATE NOCASE", (catalog,)
        ).fetchall()
        member_rows = db.execute(
            "SELECT name,type,master,catalog FROM domains WHERE name=? COLLATE NOCASE", (zone,)
        ).fetchall()
        records = db.execute(
            "SELECT name,type,content FROM records WHERE domain_id IN "
            "(SELECT id FROM domains WHERE name=? COLLATE NOCASE) AND name=? COLLATE NOCASE AND type='A'",
            (zone, "www." + zone),
        ).fetchall()
    finally:
        db.close()
    if catalog_rows != [(catalog, "CONSUMER", primary, "fixture-pdns-peer")]:
        raise ValueError("native CONSUMER catalog row differs")
    axfr = dig(primary, catalog, "AXFR", tcp=True)
    catalog_serial, catalog_members = parse_catalog_axfr(axfr, catalog, producer="powerdns")
    catalog_member = zone in catalog_members
    if catalog_members != ({zone} if expected == "present" else set()):
        raise ValueError("primary catalog member set differs from fixture expectation")
    if expected == "present":
        if not catalog_member or len(member_rows) != 1:
            raise ValueError("catalog member is not loaded")
        if member_rows[0] != (zone, "SLAVE", primary, catalog):
            raise ValueError("loaded member row differs from catalog and primary")
        if ("www." + zone, "A", address) not in records:
            raise ValueError("loaded member A record differs")
        replies = [dig(secondary, "www." + zone, "A"), dig(secondary, "www." + zone, "A", tcp=True)]
        if any("status: NOERROR" not in reply or "flags: qr aa" not in reply or address not in reply for reply in replies):
            raise ValueError("native secondary UDP/TCP answers are not authoritative")
    else:
        if catalog_member or member_rows or records:
            raise ValueError("catalog or loaded member remains after deletion")
        replies = [dig(secondary, "www." + zone, "A"), dig(secondary, "www." + zone, "A", tcp=True)]
        if any("www." + zone + "." in reply and "\tA\t" in reply for reply in replies):
            raise ValueError("deleted member still answers on native secondary")
    return {"schema": "celikpanel/native-pdns-peer-observation/v1", "primary_ip": primary,
            "secondary_ip": secondary, "catalog": catalog, "catalog_row": catalog_rows[0],
            "catalog_member": catalog_member, "catalog_serial": catalog_serial,
            "catalog_members": sorted(catalog_members), "member_row": member_rows[0] if member_rows else None,
            "loaded_a_records": records, "expect": expected,
            "authoritative_udp_tcp": expected == "present", "native_service_active": True}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--primary", required=True)
    parser.add_argument("--secondary", required=True)
    parser.add_argument("--catalog", required=True)
    parser.add_argument("--zone", default="s1-kill.test")
    parser.add_argument("--expect", choices=("present", "absent"), required=True)
    parser.add_argument("--address")
    args = parser.parse_args()
    try:
        print(json.dumps(observe(args.primary, args.secondary, args.catalog,
                                 args.zone, args.expect, args.address), sort_keys=True))
        return 0
    except (ValueError, sqlite3.Error, OSError, subprocess.SubprocessError) as exc:
        print(f"native PowerDNS peer observation unavailable: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
