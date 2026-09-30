#!/usr/bin/env python3
"""Offline tests: the two catalog-serial rules of the fresh paired PowerDNS
primary's pair check, and the zero-zone scenario variant (--zero-zones).

1. Serial rules. Before any zone publication in the cell the served catalog
   serial equals the state receipt's (pre-publication). After the zone
   lifecycle it must be >= the receipt, equal on UDP/TCP and on the secondary,
   equal to the producer SOA in the primary's database, and the catalogs must
   list exactly the expected zone set (post-publication). Batch 8r c04 failed
   only because the after-reboot check applied the first rule after the
   lifecycle; its recorded answers are replayed here.
2. Zero zones: a fresh paired primary prepared with "zones": [] (the shape the
   setup wizard installs on a new server) is judged by its catalog alone.

Nothing here starts a guest, runs --execute, talks to an Agent or a peer, or
reboots anything. Nothing here is native evidence.
"""

from __future__ import annotations

import argparse
import io
import json
import os
from pathlib import Path
import sqlite3
import struct
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import guest_bootstrap as bootstrap
import native_pdns_bind_peer as bind_peer
import test_fresh_primary_v3 as base

run_cell = base.run_cell
HERE = Path(__file__).parent
BATCH8R = HERE / "evidence" / "batch8r-pdns-primary-20261001"
SHELL = (HERE / "guest_bootstrap.sh").read_text(encoding="utf-8")
README = (HERE / "README.md").read_text(encoding="utf-8")
TRIGGER_SOURCE = HERE.parents[2] / "cmd" / "dns-kill-matrix-trigger" / "pdns_fresh_primary_v3.go"
CATALOG = run_cell.FRESH_PRIMARY_V3_CATALOG
CHILD = run_cell.FRESH_PRIMARY_V3_CHILD_ZONE
PRE = run_cell.FRESH_PRIMARY_SERIAL_RULE_PRE
POST = run_cell.FRESH_PRIMARY_SERIAL_RULE_POST
RECEIPT = 1790723542


def serial(value: int | list, tcp: int | list | None = None) -> dict:
    udp = value if isinstance(value, list) else [value]
    other = udp if tcp is None else (tcp if isinstance(tcp, list) else [tcp])
    return {"udp": list(udp), "tcp": list(other)}


def judge(rule: str, primary: dict, *, secondary: dict | None = None, database: int | None = None,
          members: list | None = None, expected: list | None = None,
          secondary_members: list | None = None, stable: bool = True) -> dict:
    listed = ["s1-kill.test"] if members is None else members
    served = primary["udp"][0] if primary["udp"] else None
    return run_cell.judge_fresh_primary_catalog(
        rule, receipt_serial=RECEIPT, primary=primary,
        secondary=primary if secondary is None else secondary,
        secondary_address=run_cell.FRESH_PRIMARY_V3_PEER_IP,
        database={"soa_serial": served if database is None else database,
                  "read_at": "t", "stable": stable, "served_after": primary},
        expected_members=["s1-kill.test"] if expected is None else expected,
        members={"primary": listed,
                 "secondary": listed if secondary_members is None else secondary_members,
                 "database": listed},
    )


class SerialRuleSelectionTest(unittest.TestCase):
    def test_rule_is_chosen_by_what_happened_in_the_cell(self) -> None:
        pre = run_cell.fresh_primary_serial_rule(None)
        self.assertEqual((pre["rule"], pre["zone_publication"], pre["catalog_members_added"]),
                         (PRE, False, []))
        self.assertIn("batch8r", pre["reason"])
        post = run_cell.fresh_primary_serial_rule({"outcome": "passed"})
        self.assertEqual((post["rule"], post["catalog_members_added"]), (POST, [CHILD]))
        self.assertIn("floor", post["reason"])
        unsure = run_cell.fresh_primary_serial_rule({"outcome": "not-passed"})
        self.assertEqual((unsure["rule"], unsure["catalog_members_added"]), (POST, None))
        expected = {"catalog_members": ["s1-kill.test"]}
        self.assertEqual(run_cell.fresh_primary_expected_members(expected, pre), ["s1-kill.test"])
        self.assertEqual(run_cell.fresh_primary_expected_members(expected, post),
                         ["s1-kill.test", CHILD])
        self.assertEqual(run_cell.fresh_primary_expected_members({"catalog_members": []}, post),
                         [CHILD])
        self.assertIsNone(run_cell.fresh_primary_expected_members(expected, unsure))

    def test_the_evidence_behind_the_pre_publication_rule_is_retained(self) -> None:
        # c01 kept one catalog serial for minutes across a reboot of both
        # guests: no second daemon re-stamp without a membership change.
        sampler = (BATCH8R / "c01-pri-intent" / "peer-dns-sampler.log").read_text(encoding="utf-8")
        served = {token.split("/")[-1] for token in sampler.split()
                  if token.startswith("10:cat:udp=aa/")}
        self.assertEqual(served, {"1790722508"})
        # c04 and c06: the second re-stamp came about 60 s after the first-start
        # one and only after the lifecycle's add changed the member set.
        for cell, first, second in (("c04-pri-started-zl-rb", "23:12:22", "23:13:22"),
                                    ("c06-pri-committed-zl", "23:25:35", "23:26:36")):
            timeline = (BATCH8R / cell / "catalog-restamp-timeline.txt").read_text(encoding="utf-8")
            restamps = [line for line in timeline.splitlines()
                        if "new CATALOG-HASH" in line and "pdns_server" in line]
            self.assertEqual([line[11:19] for line in restamps], [first, second], cell)

    def test_pair_check_callers_name_their_rule(self) -> None:
        source = (HERE / "run_cell.py").read_text(encoding="utf-8")
        definition = source[source.index("def judge_fresh_primary_pass_definition("):]
        self.assertIn("check_fresh_primary_pair(settings, serial_rule=fresh_primary_serial_rule(None))",
                      definition[:2000])
        verify = source[source.index("def _verify_after_recovery_reboot("):]
        self.assertIn('lifecycle if isinstance(lifecycle, dict) else None', verify[:3000])


class SerialRuleJudgementTest(unittest.TestCase):
    def assertPassed(self, verdict: dict) -> None:
        self.assertEqual((verdict["failures"], verdict["unknown"]), ([], []))
        self.assertTrue(all(check["passed"] for check in verdict["checks"].values()),
                        verdict["checks"])

    def test_serial_equal_to_the_receipt_passes_both_rules(self) -> None:
        for rule in (PRE, POST):
            with self.subTest(rule=rule):
                verdict = judge(rule, serial(RECEIPT))
                self.assertPassed(verdict)
                self.assertEqual(verdict["checks"]["a_receipt"]["observed"], serial(RECEIPT))

    def test_higher_serial_after_publication_passes(self) -> None:
        verdict = judge(POST, serial(1790723604),
                        members=["s1-kill.test", CHILD], expected=["s1-kill.test", CHILD])
        self.assertPassed(verdict)
        self.assertEqual(verdict["checks"]["a_receipt"]["expected"], f">= {RECEIPT}")

    def test_higher_serial_before_any_publication_fails(self) -> None:
        verdict = judge(PRE, serial(1790723604))
        self.assertEqual(verdict["failures"], [
            f"the primary's udp catalog serial [1790723604] differs from the state receipt's {RECEIPT}",
            f"the primary's tcp catalog serial [1790723604] differs from the state receipt's {RECEIPT}",
        ])
        self.assertFalse(verdict["checks"]["a_receipt"]["passed"])

    def test_lower_serial_fails_in_both_rules(self) -> None:
        verdict = judge(POST, serial(RECEIPT - 1))
        self.assertTrue(any("is lower than the state receipt's" in item
                            for item in verdict["failures"]), verdict["failures"])
        self.assertTrue(judge(PRE, serial(RECEIPT - 1))["failures"])

    def test_udp_and_tcp_that_differ_fail(self) -> None:
        for rule in (PRE, POST):
            with self.subTest(rule=rule):
                verdict = judge(rule, serial(1790723604, 1790723603))
                self.assertTrue(any("identical on UDP" in item for item in verdict["failures"]))
                self.assertFalse(verdict["checks"]["b_udp_tcp"]["passed"])

    def test_secondary_that_serves_another_serial_fails(self) -> None:
        verdict = judge(POST, serial(1790723604), secondary=serial(1790723603),
                        members=["s1-kill.test", CHILD], expected=["s1-kill.test", CHILD])
        self.assertTrue(any(item.startswith("secondary udp catalog SOA [1790723603]")
                            for item in verdict["failures"]), verdict["failures"])
        self.assertFalse(verdict["checks"]["c_secondary"]["passed"])

    def test_database_serial_must_equal_the_served_one(self) -> None:
        verdict = judge(POST, serial(1790723604), database=1790723603,
                        members=["s1-kill.test", CHILD], expected=["s1-kill.test", CHILD])
        self.assertTrue(any("database's producer SOA serial is 1790723603" in item
                            for item in verdict["failures"]), verdict["failures"])
        moved = judge(POST, serial(1790723604), database=1790723603, stable=False,
                      members=["s1-kill.test", CHILD], expected=["s1-kill.test", CHILD])
        self.assertEqual(moved["failures"], [])
        self.assertTrue(any("moved while the database was read" in item for item in moved["unknown"]))

    def test_member_list_that_differs_from_the_zone_set_fails(self) -> None:
        # After the lifecycle the child must be listed; after a delete it must not.
        verdict = judge(POST, serial(1790723604), members=["s1-kill.test"],
                        expected=["s1-kill.test", CHILD])
        self.assertEqual(len([item for item in verdict["failures"] if "catalog lists" in item]), 3)
        self.assertFalse(verdict["checks"]["e_members"]["passed"])
        verdict = judge(POST, serial(1790723604), members=["s1-kill.test", CHILD],
                        secondary_members=["s1-kill.test"], expected=["s1-kill.test", CHILD])
        self.assertEqual([item for item in verdict["failures"] if "catalog lists" in item],
                         ["the secondary catalog lists ['s1-kill.test']; the expected zone set at "
                          f"this point is ['s1-kill.test', '{CHILD}']"])

    def test_unreadable_sources_are_unknown_not_a_pass(self) -> None:
        verdict = run_cell.judge_fresh_primary_catalog(
            POST, receipt_serial=None, primary=serial(1790723604),
            secondary=serial(1790723604), secondary_address="192.0.2.11",
            database={"error": "sidecar absent"}, expected_members=None,
            members={"primary": "transfer refused", "secondary": [], "database": []})
        self.assertEqual(verdict["failures"], [])
        self.assertEqual(len(verdict["unknown"]), 4)


class PairCheckTest(unittest.TestCase):
    """check_fresh_primary_pair with its DNS, state, database and AXFR readers mocked."""

    def run_pair(self, *, primary: dict, secondary: dict, receipt: int, rule: dict,
                 expected: dict, members: list, database: int | None = None,
                 secondary_members: list | None = None, primary_after: dict | None = None,
                 member_queries_refused: bool = False) -> dict:
        calls = {"primary_catalog": 0}

        def query(address, port, name, kind, timeout):
            if member_queries_refused and name != CATALOG:
                raise run_cell.ControllerError("udp DNS response RCODE is 5")
            if name == CATALOG:
                if address == run_cell.FRESH_PRIMARY_V3_PEER_IP:
                    values = secondary
                else:
                    calls["primary_catalog"] += 1
                    values = primary if primary_after is None or calls["primary_catalog"] < 3 \
                        else primary_after
            elif kind == "SOA":
                values = serial(2026083101)
            else:
                values = {"udp": ["192.0.2.10"], "tcp": ["192.0.2.10"]}
            return {"server": address, "port": 53, "qname": name, "qtype": kind,
                    **{transport: {"values": values[transport], "answers": []}
                       for transport in ("udp", "tcp")}}

        state = {"exists": True, "semantic": {"primary_catalog_serial": receipt}}
        with tempfile.TemporaryDirectory() as root, \
                mock.patch.object(run_cell, "query_dns_observation", side_effect=query), \
                mock.patch.object(run_cell, "read_dns_state_optional", return_value=state), \
                mock.patch.object(run_cell.time, "sleep"), \
                base.catalog_sources(primary["udp"][0], members,
                                     secondary_members=secondary_members,
                                     database_serial=database):
            selected = base.settings(root, base.v3("committed", "after-write"),
                                     dns_address="10.0.2.15")
            return run_cell.check_fresh_primary_pair(selected, expected, serial_rule=rule)

    MEMBER = {"zone_set": "scenario-member", "member_soa": [2026083101],
              "www_a": ["192.0.2.10"], "catalog_members": ["s1-kill.test"]}

    def test_batch8r_c04_after_reboot_passes_under_the_post_publication_rule(self) -> None:
        result = json.loads((BATCH8R / "c04-pri-started-zl-rb" / "raw" / "results" / "result.json")
                            .read_text(encoding="utf-8"))
        recorded = result["reboot_after_recovery"]["fresh_primary_pair"]
        self.assertEqual(recorded["failures"], [
            "the primary's udp catalog serial [1790723604] differs from the state receipt's 1790723542",
            "the primary's tcp catalog serial [1790723604] differs from the state receipt's 1790723542",
        ])
        primary = recorded["primary"]["catalog_soa"]
        secondary = recorded["secondary"]["catalog_soa"]
        members = ["s1-kill.test", CHILD]  # both catalogs after the re-add (peer verdict)
        passed = self.run_pair(primary=primary, secondary=secondary,
                               receipt=recorded["state_catalog_serial"],
                               rule=run_cell.fresh_primary_serial_rule({"outcome": "passed"}),
                               expected=self.MEMBER, members=members)
        self.assertEqual((passed["failures"], passed["unknown"]), ([], []))
        self.assertEqual(passed["serial_rule"]["rule"], POST)
        checks = passed["catalog"]["checks"]
        self.assertEqual(checks["a_receipt"]["expected"], ">= 1790723542")
        self.assertEqual(checks["d_database"]["observed"], 1790723604)
        self.assertEqual(checks["e_members"]["expected"], members)
        # The same answers under the pre-publication rule: exactly the recorded failure.
        failed = self.run_pair(primary=primary, secondary=secondary,
                               receipt=recorded["state_catalog_serial"],
                               rule=run_cell.fresh_primary_serial_rule(None),
                               expected=self.MEMBER, members=["s1-kill.test"])
        self.assertEqual(failed["failures"], recorded["failures"])

    def test_secondary_behind_after_the_wait_bound_fails(self) -> None:
        report = self.run_pair(primary=serial(1790723604), secondary=serial(1790723603),
                               receipt=RECEIPT, expected=self.MEMBER,
                               rule=run_cell.fresh_primary_serial_rule({"outcome": "passed"}),
                               members=["s1-kill.test", CHILD])
        self.assertIn("the secondary's member or catalog answers differ from the primary's",
                      report["failures"])
        self.assertTrue(any(item.startswith("secondary udp catalog SOA [1790723603]")
                            for item in report["failures"]))
        self.assertGreaterEqual(report["secondary_attempts"], 1)

    def test_a_primary_that_moves_during_the_wait_is_unknown_not_a_failure(self) -> None:
        report = self.run_pair(primary=serial(1790723604), secondary=serial(1790723664),
                               primary_after=serial(1790723664), receipt=RECEIPT,
                               expected=self.MEMBER,
                               rule=run_cell.fresh_primary_serial_rule({"outcome": "passed"}),
                               members=["s1-kill.test", CHILD])
        self.assertFalse(any("secondary" in item for item in report["failures"]), report["failures"])
        self.assertTrue(any("changed while the secondary was polled" in item
                            for item in report["unknown"]))

    def test_zero_zone_pair_judges_the_catalog_only(self) -> None:
        empty = run_cell.fresh_primary_expected_pair({"zones": []})
        report = self.run_pair(primary=serial(1790800000), secondary=serial(1790800000),
                               receipt=1790800000, expected=empty, members=[],
                               rule=run_cell.fresh_primary_serial_rule(None),
                               member_queries_refused=True)
        self.assertEqual((report["failures"], report["unknown"]), ([], []))
        self.assertEqual(report["zone_set"], "empty")
        self.assertEqual(set(report["primary"]), {"catalog_soa"})
        self.assertEqual(report["catalog"]["checks"]["e_members"]["expected"], [])
        # A zero-zone catalog that lists a member fails.
        listed = self.run_pair(primary=serial(1790800000), secondary=serial(1790800000),
                               receipt=1790800000, expected=empty, members=["s1-kill.test"],
                               rule=run_cell.fresh_primary_serial_rule(None),
                               member_queries_refused=True)
        self.assertTrue(any("catalog lists ['s1-kill.test']" in item for item in listed["failures"]))
        # After the lifecycle the child is the only member (the server's first zone).
        after = self.run_pair(primary=serial(1790800125), secondary=serial(1790800125),
                              receipt=1790800000, expected=empty, members=[CHILD],
                              rule=run_cell.fresh_primary_serial_rule({"outcome": "passed"}),
                              member_queries_refused=True)
        self.assertEqual((after["failures"], after["unknown"]), ([], []))


def dns_name(name: str) -> bytes:
    return b"".join(bytes([len(label)]) + label.encode() for label in name.split(".")) + b"\0"


def rr(owner: str, rtype: int, rdata: bytes) -> bytes:
    return dns_name(owner) + struct.pack("!HHIH", rtype, 1, 3600, len(rdata)) + rdata


def transfer_message(tid: int, records: list[bytes], *, rcode: int = 0) -> bytes:
    header = struct.pack("!HHHHHH", tid, 0x8400 | rcode, 1, len(records), 0, 0)
    return header + dns_name(CATALOG) + struct.pack("!HH", 252, 1) + b"".join(records)


def catalog_records(serial_value: int, members: list[str]) -> list[bytes]:
    soa = rr(CATALOG, 6, dns_name("invalid") + dns_name("invalid")
             + struct.pack("!IIIII", serial_value, 3600, 600, 604800, 3600))
    body = [rr(CATALOG, 2, dns_name("invalid")),
            rr("version." + CATALOG, 16, b"\x012")]
    for index, member in enumerate(members):
        body.append(rr(f"{index:032d}.zones.{CATALOG}", 12, dns_name(member)))
    return [soa, *body, soa]


class CatalogTransferTest(unittest.TestCase):
    def parse(self, records: list[bytes], **kwargs: object) -> dict:
        records_parsed = run_cell.parse_transfer_message(transfer_message(9, records, **kwargs), 9)
        return run_cell.judge_catalog_transfer(records_parsed, CATALOG)

    def test_an_empty_catalog_is_soa_ns_version_and_no_member(self) -> None:
        empty = self.parse(catalog_records(1790800000, []))
        self.assertEqual((empty["serial"], empty["members"], empty["record_count"]),
                         (1790800000, [], 4))
        listed = self.parse(catalog_records(1790800060, ["s2.s1-kill.test", "s1-kill.test"]))
        self.assertEqual(listed["members"], ["s1-kill.test", "s2.s1-kill.test"])

    def test_a_malformed_or_refused_transfer_is_an_error(self) -> None:
        records = catalog_records(1, [])
        for broken in (records[:1] + records[2:], records[:-1] + records[:1][:0] + [records[1]],
                       records[:2] + [rr("x." + CATALOG, 16, b"\x01x")] + records[2:]):
            with self.subTest(), self.assertRaises(run_cell.ControllerError):
                self.parse(broken)
        with self.assertRaisesRegex(run_cell.ControllerError, "RCODE 5"):
            self.parse(records, rcode=5)


class DatabaseReaderTest(unittest.TestCase):
    def database(self, root: str, *, members: tuple[str, ...] = ()) -> str:
        path = os.path.join(root, "pdns.sqlite3")
        connection = sqlite3.connect(path)
        connection.executescript(
            "PRAGMA journal_mode=WAL;"
            "CREATE TABLE domains (id INTEGER PRIMARY KEY, name TEXT, type TEXT, catalog TEXT,"
            " notified_serial INTEGER);"
            "CREATE TABLE records (id INTEGER PRIMARY KEY, domain_id INTEGER, name TEXT, type TEXT,"
            " content TEXT);"
            "CREATE TABLE domainmetadata (id INTEGER PRIMARY KEY, domain_id INTEGER, kind TEXT,"
            " content TEXT);")
        connection.execute("INSERT INTO domains (id, name, type, notified_serial) VALUES (2, ?, "
                           "'PRODUCER', 1)", (CATALOG,))
        connection.execute("INSERT INTO records (domain_id, name, type, content) VALUES (2, ?, 'SOA',"
                           " 'invalid invalid 1790800000 3600 600 604800 3600')", (CATALOG,))
        for member in members:
            connection.execute("INSERT INTO domains (name, type, catalog) VALUES (?, 'MASTER', ?)",
                               (member, CATALOG.upper()))
        connection.commit()
        return path, connection

    def test_reads_serial_members_and_metadata_while_the_sidecars_exist(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            path, holder = self.database(root, members=(CHILD,))
            try:
                reading = run_cell.read_fresh_primary_catalog_database(path)
            finally:
                holder.close()
        self.assertEqual((reading["soa_serial"], reading["members"], reading["notified_serial"]),
                         (1790800000, [CHILD], 1))
        self.assertEqual(reading["metadata"], [])

    def test_never_opens_a_database_without_its_sidecars(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            path, holder = self.database(root)
            holder.close()  # the last connection removes -wal and -shm
            before = sorted(os.listdir(root))
            with self.assertRaisesRegex(run_cell.ControllerError, "is absent"):
                run_cell.read_fresh_primary_catalog_database(path)
            self.assertEqual(sorted(os.listdir(root)), before)


class ZeroZoneControllerTest(unittest.TestCase):
    def scenario(self, root: str, zones: list) -> None:
        value = bootstrap.pdns_switch_scenario(role="paired-primary", source_fixture="uninitialized")
        value["zones"] = zones
        Path(root, "scenario.json").write_bytes(bootstrap.json_bytes(value))
        os.chmod(Path(root, "scenario.json"), 0o600)

    def test_measured_name_and_type_must_fit_the_zone_set(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            self.scenario(root, [])
            cell = base.v3("target-started", "after-write")
            with self.assertRaisesRegex(run_cell.ControllerError, "serves only its catalog"):
                run_cell.validate_fresh_primary_zone_set(base.settings(root, cell))
            self.assertEqual(run_cell.validate_fresh_primary_zone_set(
                base.settings(root, cell, dns_name=CATALOG, dns_type="SOA")), "empty")
        with tempfile.TemporaryDirectory() as root:
            self.scenario(root, bootstrap.pdns_switch_scenario(
                role="paired-primary", source_fixture="uninitialized")["zones"])
            cell = base.v3("target-started", "after-write")
            self.assertEqual(run_cell.validate_fresh_primary_zone_set(base.settings(root, cell)),
                             "scenario-member")
            with self.assertRaisesRegex(run_cell.ControllerError, "samples the catalog"):
                run_cell.validate_fresh_primary_zone_set(
                    base.settings(root, cell, dns_name=CATALOG, dns_type="SOA"))
        self.assertIsNone(run_cell.validate_fresh_primary_zone_set(base.settings(
            "/nonexistent", base.spec("bind__target-staged__after-write__standalone__peer-reachable"))))

    def test_run_cell_validates_the_zone_set_before_any_mutation(self) -> None:
        source = (HERE / "run_cell.py").read_text(encoding="utf-8")
        flow = source[source.index("def run_cell(settings:"):]
        self.assertLess(flow.index("validate_fresh_primary_zone_set(settings)"),
                        flow.index("transcript = Transcript(settings.transcript_path)"))

    def test_forward_records_but_does_not_judge_a_zero_zone_serial_of_one(self) -> None:
        state = {"sha256": "x", "schema": run_cell.FRESH_PRIMARY_V3_STATE_SCHEMA,
                 "native_catalog": run_cell.FRESH_PRIMARY_V3_NATIVE_CATALOG,
                 "semantic": {"engine": "pdns", "pair_role": "primary",
                              "mutation_request_id": base.REQUEST, "primary_catalog_serial": 1}}
        cut = {"pdns_main_pid": 700, "journal": {"primary_catalog_serial": 1, "native_serial": 1}}

        def forward(zone_set: str, served: list) -> dict:
            with tempfile.TemporaryDirectory() as root, \
                    mock.patch.object(run_cell, "read_fresh_primary_state", return_value=state), \
                    mock.patch.object(run_cell, "read_unit_main_pid", return_value=700), \
                    mock.patch.object(run_cell, "fresh_primary_zone_set_or_member",
                                      return_value=zone_set), \
                    mock.patch.object(run_cell, "fresh_primary_catalog_serials",
                                      return_value={"primary": {"udp": served, "tcp": served}}):
                return run_cell.judge_fresh_primary_forward(
                    base.settings(root, base.v3("committed", "after-write")), {},
                    base.Transcript(), cut, 0)

        zero = forward("empty", [1])
        self.assertEqual(zero["failures"], [])
        self.assertEqual(zero["catalog_restamp"]["zero_zone_restamp"]["restamped"], False)
        self.assertEqual(forward("scenario-member", [1])["failures"],
                         ["the state records the staged catalog serial, not the daemon's"])
        self.assertTrue(forward("empty", [2])["failures"])  # served must equal the state

    def test_zero_zone_sql_edit_goes_into_the_catalog_producer_zone(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            database = Path(root, "pdns.sqlite3")
            connection = sqlite3.connect(database)
            connection.executescript(
                "CREATE TABLE domains (id INTEGER PRIMARY KEY, name TEXT);"
                "CREATE TABLE records (id INTEGER PRIMARY KEY, domain_id INTEGER, name TEXT,"
                " type TEXT, content TEXT, ttl INTEGER, prio INTEGER, disabled BOOL,"
                " ordername TEXT, auth BOOL);")
            connection.execute("INSERT INTO domains (name) VALUES (?)", (CATALOG,))
            connection.commit()
            connection.close()
            selected = base.settings(root, base.v3("target-started", "after-write"))
            with mock.patch.object(run_cell, "PDNS_DATABASE_PATH", str(database)):
                with self.assertRaisesRegex(run_cell.ControllerError, "s1-kill.test has 0"):
                    run_cell.apply_owner_sql_edit(selected)
                edit = run_cell.apply_owner_sql_edit(selected, "empty")
                self.assertEqual((edit["domain"], edit["row"]["name"], edit["zone_set"]),
                                 (CATALOG, "owner-note." + CATALOG, "empty"))
                self.assertIn(base.REQUEST, edit["row"]["content"])
                self.assertEqual(run_cell.judge_owner_edit_preserved(selected, edit), [])

    def test_hold_flow_passes_the_zone_set_to_the_sql_edit(self) -> None:
        source = (HERE / "run_cell.py").read_text(encoding="utf-8")
        hold = source[source.index("def run_fresh_primary_hold_flow("):]
        self.assertIn("edit = apply_owner_sql_edit(settings, zone_set)", hold[:3000])


class ZeroZoneHostTest(unittest.TestCase):
    RAW = base.raw_cell("pdns-switch__committed__after-write__paired-primary__peer-reachable")

    def test_scenario_has_an_explicit_empty_zone_list_only_for_the_fresh_primary(self) -> None:
        zero = bootstrap.pdns_switch_scenario(
            role="paired-primary", source_fixture="uninitialized", zero_zones=True)
        member = bootstrap.pdns_switch_scenario(role="paired-primary", source_fixture="uninitialized")
        self.assertEqual(zero["zones"], [])
        self.assertEqual({key: value for key, value in zero.items() if key != "zones"},
                         {key: value for key, value in member.items() if key != "zones"})
        for kwargs in ({"role": "standalone", "source_fixture": "uninitialized"},
                       {"role": "paired-primary", "source_fixture": "managed-bind"},
                       {"role": "paired-secondary", "source_fixture": "uninitialized"},
                       {"role": "paired-primary", "source_fixture": "uninitialized",
                        "authority_acceptance": True}):
            with self.subTest(kwargs=kwargs), self.assertRaises(bootstrap.BootstrapError):
                bootstrap.pdns_switch_scenario(zero_zones=True, **kwargs)
        self.assertEqual(run_cell.fresh_primary_zone_set_of_scenario(zero), "empty")
        self.assertEqual(bootstrap.guest_zone_set_of(zero), "empty")
        self.assertEqual(bootstrap.guest_zone_set_of(member), "scenario")

    def test_guest_scenario_check_accepts_the_empty_primary_only(self) -> None:
        program = SHELL.split("<<'PYFRESHPDNS'\n", 1)[1].split("\nPYFRESHPDNS\n", 1)[0]
        zero = bootstrap.pdns_switch_scenario(
            role="paired-primary", source_fixture="uninitialized", zero_zones=True)
        standalone = bootstrap.pdns_switch_scenario(source_fixture="uninitialized")
        with tempfile.TemporaryDirectory() as root:
            for role, value, expected in (("primary", zero, 0),
                                          ("standalone", {**standalone, "zones": []}, 1)):
                path = Path(root, f"{role}.json")
                path.write_bytes(bootstrap.json_bytes(value))
                done = subprocess.run([sys.executable, "-c", program, str(path)], check=False,
                                      capture_output=True, text=True,
                                      env={**os.environ, "FRESH_PDNS_ROLE": role})
                self.assertEqual(done.returncode, expected, done.stderr)

    def test_guest_writes_the_catalog_soa_as_the_measured_name_and_type(self) -> None:
        body = SHELL.split("\nprepare_fresh_pdns_source() {\n", 1)[1].split(
            "\nprepare_pdns_switch() {\n", 1)[0]
        self.assertIn("MEASURED_DNS_NAME=$ZERO_ZONE_PRIMARY_CATALOG", body)
        self.assertIn("MEASURED_DNS_TYPE=SOA", body)
        self.assertIn(f"ZERO_ZONE_PRIMARY_CATALOG={CATALOG}\n", SHELL)
        argv_body = SHELL.split("\nwrite_controller_argv() {\n", 1)[1]
        program = argv_body.split("<<'PY'\n", 1)[1].split("\nPY\n", 1)[0]
        with tempfile.TemporaryDirectory() as root:
            output = Path(root, "argv.json")
            subprocess.run([sys.executable, "-c", program, str(output)], check=True, env={
                **os.environ, "CELL_ID": self.RAW["id"], "DNS_ADDRESS": "10.0.2.15",
                "DNS_NAME": CATALOG, "DNS_TYPE": "SOA", "REQUEST_ID": "1" * 32,
                "NONCE": "a" * 64, "RESULT_DIR": root})
            argv = json.loads(output.read_text(encoding="utf-8"))
        self.assertEqual(argv[argv.index("--dns-name") + 1], CATALOG)
        self.assertEqual(argv[argv.index("--dns-type") + 1], "SOA")
        proof = SHELL.split("\nwrite_source_proof() {\n", 1)[1]
        self.assertIn('"type": os.environ.get("AUTH_TYPE", "A"),', proof)
        self.assertIn("local authoritative_type=${MEASURED_DNS_TYPE:-A}", proof)

    def args(self, **changes: object) -> argparse.Namespace:
        values = dict(
            work_root=Path("/w"), cell_id=self.RAW["id"], manifest=Path("/m"), node="debian13",
            identity_file=Path("/k"), source_fixture="uninitialized", execute=False,
            owner_edit=None, owner_release_recovery=False, zone_lifecycle=True,
            owner_directives=False, reboot_after_recovery=False,
            disable_management_before_reboot=False, stop_after_kill_for_independent_recovery=False,
            zero_zones=True,
        )
        values.update(changes)
        return argparse.Namespace(**values)

    def test_zero_zones_is_refused_outside_the_fresh_primary(self) -> None:
        bootstrap.validate_fresh_primary_run(self.args(), self.RAW, False)
        standalone = base.raw_cell("pdns-switch__committed__after-write__standalone__peer-reachable")
        with self.assertRaisesRegex(bootstrap.BootstrapError, "--zero-zones apply only"):
            bootstrap.validate_fresh_primary_run(
                self.args(zone_lifecycle=False, cell_id=standalone["id"]), standalone, False)

    def test_host_verifies_the_guest_scenario_before_anything_runs(self) -> None:
        empty = json.dumps({"zones": []})
        member = json.dumps({"zones": [{"domain": "s1-kill.test"}]})
        for stdout, zero, refused in ((empty, True, False), (member, False, False),
                                      (empty, False, True), (member, True, True)):
            with self.subTest(zero=zero, stdout=stdout), \
                    mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]), \
                    mock.patch.object(bootstrap.subprocess, "run", return_value=subprocess.CompletedProcess(
                        ["ssh"], 0, stdout=stdout, stderr="")) as run:
                if refused:
                    with self.assertRaisesRegex(bootstrap.BootstrapError, "exactly when"):
                        bootstrap.verify_guest_zone_set({}, Path("/k"), zero)
                else:
                    bootstrap.verify_guest_zone_set({}, Path("/k"), zero)
                self.assertIn("cat " + bootstrap.GUEST_SCENARIO_PATH, run.call_args.args[0][-1])

    def test_dry_run_names_the_zero_zone_peer_observation(self) -> None:
        with mock.patch.object(bootstrap, "load_plan", return_value=({}, self.RAW, {"n": 1})), \
                mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/k")), \
                mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]), \
                mock.patch("sys.stdout", new_callable=io.StringIO) as output:
            self.assertEqual(bootstrap.run_prepared(self.args()), 0)
        text = output.getvalue()
        self.assertIn("native_pdns_bind_peer.py observe --zero-zones", text)
        self.assertIn("observe-child --step delete --zero-zones", text)
        self.assertEqual(bootstrap.peer_observe_selector(self.args(zero_zones=False)),
                         "--address 192.0.2.10")

    def test_cli_accepts_the_flag_where_documented(self) -> None:
        common = ["--work-root", "/w", "--cell-id", self.RAW["id"], "--node", "debian13",
                  "--identity-file", "/k", "--source-fixture", "uninitialized"]
        for action in ("prepare-pdns-switch", "run-prepared", "zone-lifecycle"):
            self.assertTrue(bootstrap.parse_args([action, *common, "--zero-zones"]).zero_zones)
        with self.assertRaises(SystemExit), mock.patch("sys.stderr", new_callable=io.StringIO):
            bootstrap.parse_args(["install", *common, "--zero-zones", "--agent", "a",
                                  "--tagged-agent", "b", "--panel", "c", "--trigger", "d",
                                  "--web-dir", "e"])


def empty_catalog_axfr(serial_value: int, members: tuple[str, ...] = ()) -> str:
    catalog = bind_peer.catalog_name("192.0.2.10") + "."
    soa = f"{catalog} 60 IN SOA invalid. invalid. {serial_value} 60 30 3600 30\n"
    ptrs = "".join(f"{index:032d}.zones.{catalog} 60 IN PTR {member}.\n"
                   for index, member in enumerate(members))
    return (";; ->>HEADER<<- opcode: QUERY, status: NOERROR\n" + soa
            + f"{catalog} 60 IN NS invalid.\n" + f'version.{catalog} 60 IN TXT "2"\n' + ptrs + soa)


class ZeroZonePeerTest(unittest.TestCase):
    def namespace(self, **extra: object) -> argparse.Namespace:
        values: dict = dict(
            work_root=Path("/w"), cell_id="c", source_fixture="uninitialized",
            identity_file=Path("/k"), manifest=Path("/m"), execute=True, zero_zones=True)
        values.update(extra)
        return argparse.Namespace(**values)

    def replay(self, *, members: tuple[str, ...] = (), loaded: tuple[str, ...] | None = None,
               catalog_serial: int = 1790800000, child: str = "REFUSED"):
        catalog = bind_peer.catalog_name("192.0.2.10")

        def read(ssh, command, execute):
            if command.startswith("systemctl"):
                return ""
            if f" {catalog} AXFR" in command:
                listed = members if "@192.0.2.10 " in command or loaded is None else loaded
                return empty_catalog_axfr(catalog_serial, listed)
            if f" {catalog} SOA" in command:
                return (";; ->>HEADER<<- opcode: QUERY, status: NOERROR\n;; flags: qr aa;\n"
                        f"{catalog}. 60 IN SOA invalid. invalid. {catalog_serial} 60 30 3600 30\n")
            if f" {bind_peer.CHILD} SOA" in command and child == "REFUSED":
                return ";; ->>HEADER<<- opcode: QUERY, status: REFUSED\n;; flags: qr;\n"
            return ";; ->>HEADER<<- opcode: QUERY, status: NXDOMAIN\n;; flags: qr aa;\n"
        return read

    def run_peer(self, action, args, read):
        with mock.patch.object(bind_peer, "selected",
                               return_value=("192.0.2.10", "192.0.2.11", {}, Path("/k"))), \
                mock.patch.object(bind_peer.bootstrap, "ssh_base", return_value=["ssh"]), \
                mock.patch.object(bind_peer, "verify_guest"), \
                mock.patch.object(bind_peer, "remote_read", side_effect=read):
            return action(args)

    def test_observe_judges_an_empty_catalog_on_both_servers(self) -> None:
        report = self.run_peer(bind_peer.observe, self.namespace(address=None), self.replay())
        self.assertEqual((report["catalog_members"], report["catalog_members_expected"]), ([], []))
        self.assertIsNone(report["member"])
        self.assertEqual(set(report["answers"].values()), {"1790800000"})
        self.assertEqual(len(report["answers"]), 4)
        with self.assertRaisesRegex(ValueError, "expected members"):
            self.run_peer(bind_peer.observe, self.namespace(address=None),
                          self.replay(members=("s1-kill.test",)))
        with self.assertRaisesRegex(ValueError, "differs"):
            self.run_peer(bind_peer.observe, self.namespace(address=None),
                          self.replay(loaded=("stale.test",)))
        child = self.run_peer(bind_peer.observe, self.namespace(address=None, with_child=True),
                              self.replay(members=(bind_peer.CHILD,)))
        self.assertEqual(child["catalog_members_expected"], [bind_peer.CHILD])

    def test_deleted_first_zone_is_refused_by_both_servers(self) -> None:
        report = self.run_peer(bind_peer.observe_child, self.namespace(step="delete"), self.replay())
        self.assertEqual(report["catalog_members_expected"], [])
        self.assertEqual(set(value["soa"] for value in report["answers"].values()), {"REFUSED"})
        with self.assertRaisesRegex(ValueError, "does not refuse"):
            self.run_peer(bind_peer.observe_child, self.namespace(step="delete"),
                          self.replay(child="NXDOMAIN"))
        # Without --zero-zones the parent is served and denies the child (NXDOMAIN aa).
        with self.assertRaisesRegex(ValueError, "differs from the PowerDNS primary or the step"):
            self.run_peer(bind_peer.observe_child, self.namespace(step="delete", zero_zones=False),
                          self.replay())

    def test_cli_requires_the_address_only_without_zero_zones(self) -> None:
        for argv, ok in ((["observe", "--zero-zones"], True), (["observe"], False),
                         (["observe", "--zero-zones", "--address", "192.0.2.10"], False)):
            with self.subTest(argv=argv), \
                    mock.patch.object(sys, "argv", ["peer", *argv, "--work-root", "/w", "--cell-id",
                                                    "c", "--identity-file", "/k"]), \
                    mock.patch.object(bind_peer, "observe", return_value={"ok": True}), \
                    mock.patch("sys.stdout", new_callable=io.StringIO), \
                    mock.patch("sys.stderr", new_callable=io.StringIO):
                if ok:
                    bind_peer.main()
                else:
                    with self.assertRaises(SystemExit):
                        bind_peer.main()


class ZeroZoneTriggerAndReadmeTest(unittest.TestCase):
    def test_trigger_accepts_the_explicit_empty_zone_list(self) -> None:
        source = TRIGGER_SOURCE.read_text(encoding="utf-8")
        self.assertIn("func freshPairedPrimaryZeroZoneScenario(value scenario) bool", source)
        self.assertIn("return value.Zones != nil && len(value.Zones) == 0", source)

    def test_readme_documents_the_six_zero_zone_cells_and_both_rules(self) -> None:
        section = README[README.index("#### Zero-zone fresh paired PowerDNS primary"):]
        for cell, flags in (
            ("z01", ("target-staged__after-write",)),
            ("z02", ("target-started__after-write",)),
            ("z03", ("committed__after-write",)),
            ("z04", ("committed__after-write", "--zone-lifecycle")),
            ("z05", ("target-started__after-write", "--zone-lifecycle", "--reboot-after-recovery",
                     "--disable-management-before-reboot")),
            ("z06", ("target-started__after-write", "--owner-edit sql")),
        ):
            line = next(line for line in section.splitlines() if line.startswith(f"# {cell}"))
            block = section[section.index(line):].split("\n# z", 1)[0]
            for flag in flags:
                self.assertIn(flag, block, cell)
            self.assertIn("--zero-zones", block, cell)
        for text in ("pre-publication", "post-publication", "what it does not prove"):
            self.assertIn(text, section)


if __name__ == "__main__":
    unittest.main()
