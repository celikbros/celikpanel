#!/usr/bin/env python3
"""Strict direct-authority witness for the isolated BIND/PowerDNS pair."""

from __future__ import annotations

import argparse
import ipaddress
import json
import sqlite3
import subprocess
import sys

ZONE = "celikhost.com"
CATALOG = "catalog-c000020a.celikpanel.invalid"
PRIMARY = "192.0.2.10"
SECONDARY = "192.0.2.11"
EXPECTED = {
    ("celikhost.com.", "SOA"): {(
        "ns1.celikhost.com.", "hostmaster.celikhost.com.", "2026092701",
        "10800", "3600", "604800", "3600",
    )},
    ("celikhost.com.", "NS"): {("ns1.celikhost.com.",), ("ns2.celikhost.com.",)},
    ("frankfurt.celikhost.com.", "A"): {(PRIMARY,)},
    ("boston.celikhost.com.", "A"): {(SECONDARY,)},
}


def answer(address: str, owner: str, kind: str, tcp: bool) -> set[tuple[str, ...]]:
    command = ["dig", "+time=2", "+tries=1", "+norecurse",
               "+noall", "+comments", "+answer"]
    if tcp:
        command.append("+tcp")
    command.extend(["@" + address, owner, kind])
    reply = subprocess.run(command, check=True, capture_output=True, text=True,
                           timeout=8).stdout
    header = next((line for line in reply.splitlines() if "status:" in line), "")
    flags = next((line for line in reply.splitlines() if line.startswith(";; flags:")), "")
    if "status: NOERROR" not in header or "flags: qr aa" not in flags or " tc" in flags:
        raise ValueError(f"{address} {owner} {kind} lacks an authoritative complete answer")
    records: set[tuple[str, ...]] = set()
    for line in reply.splitlines():
        if not line or line.startswith(";"):
            continue
        fields = line.split()
        if len(fields) < 5 or fields[0].lower() != owner.lower() or fields[2] != "IN" or fields[3] != kind:
            raise ValueError(f"{address} {owner} {kind} returned an unexpected record")
        records.add(tuple(fields[4:]))
    return records


def observe() -> dict:
    for unit in ("pdns.service",):
        subprocess.run(["systemctl", "is-active", "--quiet", unit], check=True, timeout=8)
    with sqlite3.connect("file:/var/lib/powerdns/pdns.sqlite3?mode=ro", uri=True) as db:
        catalog = db.execute(
            "SELECT type,master FROM domains WHERE name=? COLLATE NOCASE", (CATALOG,)
        ).fetchall()
        member = db.execute(
            "SELECT type,master,catalog FROM domains WHERE name=? COLLATE NOCASE", (ZONE,)
        ).fetchall()
        if catalog != [("CONSUMER", PRIMARY)] or member != [("SLAVE", PRIMARY, CATALOG)]:
            raise ValueError("native PowerDNS catalog or loaded SLAVE differs")
        records = db.execute(
            "SELECT name,type,content FROM records WHERE domain_id="
            "(SELECT id FROM domains WHERE name=? COLLATE NOCASE)", (ZONE,)
        ).fetchall()
        if ("frankfurt.celikhost.com", "A", PRIMARY) not in records or (
            "boston.celikhost.com", "A", SECONDARY
        ) not in records:
            raise ValueError("native PowerDNS loaded A records differ")
    observations = {}
    for address in (PRIMARY, SECONDARY):
        endpoint = {}
        for (owner, kind), expected in EXPECTED.items():
            for tcp in (False, True):
                actual = answer(address, owner, kind, tcp)
                if actual != expected:
                    raise ValueError(f"{address} {owner} {kind} answer differs")
                endpoint[f"{owner}/{kind}/{'tcp' if tcp else 'udp'}"] = sorted(actual)
        observations[address] = endpoint
    return {"schema": "celikpanel/native-bind-pdns-authority-observation/v1",
            "zone": ZONE, "catalog": CATALOG, "primary": PRIMARY, "secondary": SECONDARY,
            "native_secondary_loaded": True, "answers": observations}


def main() -> int:
    try:
        print(json.dumps(observe(), sort_keys=True))
        return 0
    except (ValueError, OSError, sqlite3.Error, subprocess.SubprocessError) as exc:
        print(f"native authority observation unavailable: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
