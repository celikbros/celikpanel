#!/usr/bin/env python3
"""Offline tests: guest probe wire parsing/native classification and DNS verdicts."""

from __future__ import annotations

import hashlib
import json
import os
import sqlite3
import struct
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))

import dns_checks as dc  # noqa: E402
import guest_probe as gp  # noqa: E402

ZONE = "pair-accept.example"
P, S = "192.0.2.11", "192.0.2.10"


def rr(owner: bytes, rtype: int, rdata: bytes, ttl: int = 300) -> bytes:
    return owner + struct.pack("!HHIH", rtype, 1, ttl, len(rdata)) + rdata


def response(query_id: int, name: str, qtype: int, answers: list[bytes], *, rcode: int = 0, aa: bool = True) -> bytes:
    flags = 0x8000 | (0x0400 if aa else 0) | rcode
    header = struct.pack("!HHHHHH", query_id, flags, 1, len(answers), 0, 0)
    question = gp.encode_name(name) + struct.pack("!HH", qtype, 1)
    return header + question + b"".join(answers)


class WireTest(unittest.TestCase):
    def test_parse_soa_a_ns_txt_ptr_with_compression(self) -> None:
        pointer = struct.pack("!H", 0xC000 | 12)  # the question name
        soa = gp.encode_name("ns1.ns-accept.example") + gp.encode_name("hostmaster.pair-accept.example") + \
            struct.pack("!IIIII", 2026092901, 3600, 600, 604800, 300)
        message = response(7, ZONE, 6, [
            rr(pointer, 6, soa),
            rr(pointer, 2, gp.encode_name("ns2.ns-accept.example")),
            rr(gp.encode_name("accept." + ZONE), 1, bytes([198, 51, 100, 10])),
            rr(pointer, 16, b"\x05hello\x03 x "),
            rr(pointer, 12, gp.encode_name("member.example")),
        ])
        parsed = gp.parse_response(message, 7)
        self.assertEqual(parsed["rcode"], "NOERROR")
        self.assertTrue(parsed["authoritative"])
        types = [record["type"] for record in parsed["answer"]]
        self.assertEqual(types, ["SOA", "NS", "A", "TXT", "PTR"])
        self.assertEqual(parsed["answer"][0]["data"].split()[2], "2026092901")
        self.assertEqual(parsed["answer"][2]["data"], "198.51.100.10")
        self.assertEqual(parsed["answer"][4]["data"], "member.example.")
        with self.assertRaises(gp.ProbeError):
            gp.parse_response(message, 8)

    def test_refused_and_query_validation(self) -> None:
        parsed = gp.parse_response(response(9, ZONE, 6, [], rcode=5, aa=False), 9)
        self.assertEqual((parsed["rcode"], parsed["authoritative"]), ("REFUSED", False))
        with self.assertRaises(gp.ProbeError):
            gp.dns_command(P, [f"{ZONE}/AXFR"])
        with self.assertRaises(gp.ProbeError):
            gp.dns_command(P, [f"{ZONE}"])
        with self.assertRaises(ValueError):
            gp.dns_command("not-an-ip", [f"{ZONE}/SOA"])
        with mock.patch.object(gp, "query", side_effect=lambda server, name, qtype, tcp: {
                "server": server, "name": name, "qtype": qtype, "transport": "tcp" if tcp else "udp"}):
            out = gp.dns_command(P, [f"{ZONE}/SOA", f"accept.{ZONE}/A"])
        self.assertEqual([(a["qtype"], a["transport"]) for a in out["answers"]],
                         [("SOA", "udp"), ("SOA", "tcp"), ("A", "udp"), ("A", "tcp")])


class NativeTest(unittest.TestCase):
    def test_rndc_exact_absence_only(self) -> None:
        absent = f"rndc: 'zonestatus' failed: not found\nno matching zone '{ZONE}' in any view"
        with mock.patch.object(gp.os.path, "exists", return_value=True), \
                mock.patch.object(gp, "bounded", return_value={"returncode": 1, "output": absent}):
            self.assertEqual(gp.rndc_zone_state(ZONE)["state"], "unloaded")
        with mock.patch.object(gp.os.path, "exists", return_value=True), \
                mock.patch.object(gp, "bounded", return_value={"returncode": 1, "output": "rndc: connection refused"}):
            self.assertEqual(gp.rndc_zone_state(ZONE)["state"], "unknown")
        loaded = "name: pair-accept.example\ntype: secondary\nserial: 2026092903\n"
        with mock.patch.object(gp.os.path, "exists", return_value=True), \
                mock.patch.object(gp, "bounded", return_value={"returncode": 0, "output": loaded}):
            state = gp.rndc_zone_state(ZONE)
        self.assertEqual((state["state"], state["type"], state["serial"]), ("loaded", "secondary", "2026092903"))

    def test_pdns_rows(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            database = os.path.join(directory, "pdns.sqlite3")
            connection = sqlite3.connect(database)
            connection.executescript(
                "CREATE TABLE domains (id INTEGER PRIMARY KEY, name TEXT, master TEXT, type TEXT, catalog TEXT, options TEXT);"
                "CREATE TABLE records (id INTEGER PRIMARY KEY, domain_id INT, name TEXT, type TEXT, content TEXT, ttl INT);"
                f"INSERT INTO domains VALUES (1, '{ZONE}', '192.0.2.11', 'SLAVE', 'catalog-c000020b.celikpanel.invalid', '{{\"consumer\":1}}');"
                f"INSERT INTO records VALUES (1, 1, '{ZONE}', 'SOA', 'ns1. h. 1 2 3 4 5', 300);"
            )
            connection.commit()
            connection.close()
            rows = gp.pdns_rows(ZONE, database)
            self.assertTrue(rows["zone_row_present"])
            self.assertTrue(rows["domains"][0]["options_present"])
            self.assertEqual(len(rows["zone_records"]), 1)
            self.assertFalse(gp.pdns_rows("other.example", database)["zone_row_present"])
            self.assertFalse(gp.pdns_rows(ZONE, os.path.join(directory, "missing"))["read"])
            with mock.patch.object(gp, "pdns_rows", return_value=gp.pdns_rows("other.example", database)), \
                    mock.patch.object(gp, "bounded", return_value={"returncode": 0, "output": "x.example.\n"}):
                self.assertEqual(gp.native_command("pdns", "other.example", None)["native_state"], "absent")
            with mock.patch.object(gp, "pdns_rows", return_value=gp.pdns_rows("other.example", database)), \
                    mock.patch.object(gp, "bounded", return_value={"returncode": 1, "output": "denied"}):
                self.assertEqual(gp.native_command("pdns", "other.example", None)["native_state"], "absent-by-database")

    def test_bind_native_requires_no_zone_files(self) -> None:
        with mock.patch.object(gp, "rndc_zone_state", return_value={"state": "unloaded"}), \
                mock.patch.object(gp, "bind_zone_files", return_value=["/var/cache/bind/celikpanel/x/pair-accept.example.zone"]):
            self.assertEqual(gp.native_command("bind", ZONE, None)["native_state"], "unknown")
        with mock.patch.object(gp, "rndc_zone_state", return_value={"state": "unloaded"}), \
                mock.patch.object(gp, "bind_zone_files", return_value=[]):
            self.assertEqual(gp.native_command("bind", ZONE, None)["native_state"], "absent")

    def test_ledger_digest(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = os.path.join(directory, "ledger.json")
            raw = json.dumps({"jobs": [{"request_id": "r", "state": "succeeded", "payload": "secret-ish"}]}).encode()
            Path(path).write_bytes(raw)
            with mock.patch.object(gp, "LEDGER", path):
                out = gp.ledger_command()
        self.assertEqual(out["sha256"], hashlib.sha256(raw).hexdigest())
        self.assertEqual(out["jobs"], [{"request_id": "r", "state": "succeeded"}])


def answer(server: str, observer: str, name: str, qtype: str, records: list, *, transport: str = "udp",
           rcode: str = "NOERROR", aa: bool = True) -> dict:
    return {"server": server, "observer": observer, "name": name + ".", "qtype": qtype, "transport": transport,
            "rcode": rcode, "authoritative": aa, "truncated": False,
            "answer": [{"name": n + ".", "type": t, "ttl": 300, "data": d} for n, t, d in records]}


def observation(serial_p: int = 5, serial_s: int = 5, a_value: str = "198.51.100.10", *,
                secondary_aa: bool = True, missing_name: bool = False) -> list[dict]:
    out = []
    for observer in ("arch", "debian13"):
        for server, serial in ((P, serial_p), (S, serial_s)):
            answers = []
            for transport in ("udp", "tcp"):
                aa = secondary_aa or server == P
                answers.append(answer(server, observer, ZONE, "SOA", [
                    (ZONE, "SOA", f"ns1.ns-accept.example. h. {serial} 1 2 3 4")], transport=transport, aa=aa))
                answers.append(answer(server, observer, ZONE, "NS", [
                    (ZONE, "NS", "ns1.ns-accept.example."), (ZONE, "NS", "ns2.ns-accept.example.")], transport=transport, aa=aa))
                if missing_name:
                    answers.append(answer(server, observer, "accept." + ZONE, "A", [], transport=transport, rcode="NXDOMAIN", aa=aa))
                else:
                    answers.append(answer(server, observer, "accept." + ZONE, "A", [
                        ("accept." + ZONE, "A", a_value)], transport=transport, aa=aa))
            out.append({"observer": observer, "server": server, "answers": answers})
    return out


class VerdictTest(unittest.TestCase):
    kwargs = {"servers": [P, S], "zone": ZONE, "nameservers": ["ns1.ns-accept.example", "ns2.ns-accept.example"]}

    def test_present_pass_and_serial_advance(self) -> None:
        expected = {("accept." + ZONE, "A"): {"198.51.100.10"}}
        verdict = dc.evaluate_present(observation(), expected=expected, **self.kwargs)
        self.assertTrue(verdict["passed"], verdict["reasons"])
        self.assertEqual(verdict["serial"], 5)
        stale = dc.evaluate_present(observation(), expected=expected, min_serial=5, **self.kwargs)
        self.assertFalse(stale["passed"])

    def test_present_failures(self) -> None:
        expected = {("accept." + ZONE, "A"): {"198.51.100.10"}}
        self.assertFalse(dc.evaluate_present(observation(5, 4), expected=expected, **self.kwargs)["passed"])
        self.assertFalse(dc.evaluate_present(observation(secondary_aa=False), expected=expected, **self.kwargs)["passed"])
        wrong = dc.evaluate_present(observation(a_value="198.51.100.20"), expected=expected, **self.kwargs)
        self.assertFalse(wrong["passed"])
        only_primary = [item for item in observation() if item["server"] == P]
        self.assertFalse(dc.evaluate_present(only_primary, expected=expected, **self.kwargs)["passed"])

    def test_expected_empty_accepts_authoritative_nxdomain(self) -> None:
        expected = {("accept." + ZONE, "A"): set()}
        self.assertTrue(dc.evaluate_present(observation(missing_name=True), expected=expected, **self.kwargs)["passed"])
        self.assertFalse(dc.evaluate_present(observation(), expected=expected, **self.kwargs)["passed"])

    def test_absent_dns_and_native_and_catalog(self) -> None:
        refused = [{"observer": "arch", "server": server, "answers": [
            answer(server, "arch", ZONE, "SOA", [], rcode="REFUSED", aa=False, transport=t) for t in ("udp", "tcp")]}
            for server in (P, S)]
        self.assertTrue(dc.evaluate_absent_dns(refused, servers=[P, S], zone=ZONE)["passed"])
        still = observation()
        self.assertFalse(dc.evaluate_absent_dns(still, servers=[P, S], zone=ZONE)["passed"])
        self.assertTrue(dc.evaluate_native({"native_state": "absent", "engine": "pdns"}, expect_present=False)["passed"])
        self.assertFalse(dc.evaluate_native({"native_state": "absent-by-database"}, expect_present=False)["passed"])
        self.assertFalse(dc.evaluate_native({"native_state": "unknown"}, expect_present=False)["passed"])
        self.assertTrue(dc.evaluate_native({"native_state": "present"}, expect_present=True)["passed"])
        catalog = {"transferred": True, "serial": 9, "members": [ZONE + "."]}
        self.assertTrue(dc.evaluate_catalog(catalog, zone=ZONE, expect_member=True)["passed"])
        self.assertFalse(dc.evaluate_catalog(catalog, zone=ZONE, expect_member=False)["passed"])
        self.assertFalse(dc.evaluate_catalog({"transferred": False, "rcode": "REFUSED"}, zone=ZONE, expect_member=False)["passed"])
        self.assertEqual(dc.catalog_name("192.0.2.10"), "catalog-c000020a.celikpanel.invalid")


if __name__ == "__main__":
    unittest.main()
