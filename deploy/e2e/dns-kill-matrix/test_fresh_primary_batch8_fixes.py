#!/usr/bin/env python3
"""Offline tests: harness corrections after batch 8 (fresh paired PowerDNS primary).

1. The pair check compares both servers' answers with the prepared scenario's
   records, not with --dns-address (the address the queries are sent to);
   built from the real captured answers of batch 8 cells c01-c07.
2. The controller waits (read-only, bounded) for the Agent to report the host
   idle before the measured BeginServiceMutation (batch 8 c08).
3. --zone-lifecycle with --reboot-after-recovery --disable-management-before-
   reboot runs in a defined order (lifecycle before the reboot).
5. Every pair query records its server address and full answer section.

Nothing here starts a guest, runs --execute, talks to an Agent or a peer, or
reboots anything. Nothing here is native evidence.
"""

from __future__ import annotations

import argparse
import io
import json
import os
from pathlib import Path
import struct
import subprocess
import sys
import tempfile
import unittest
from dataclasses import replace
from unittest import mock

import guest_bootstrap as bootstrap
import native_pdns_bind_peer as bind_peer
import test_fresh_primary_v3 as base

run_cell = base.run_cell
HERE = Path(__file__).parent
BATCH8 = HERE / "evidence" / "batch8-pdns-primary-20260930"
FORWARD_CELLS = (
    "c01-pri-intent", "c02-pri-staged", "c03-pri-enable", "c04-pri-started-zl",
    "c05-pri-verified", "c06-pri-committed", "c07-pri-prejournal",
)
TRIGGER_SOURCE = HERE.parents[2] / "cmd" / "dns-kill-matrix-trigger" / "pdns_fresh_primary_v3.go"


def evidence_json(cell: str, *parts: str) -> object:
    return json.loads(BATCH8.joinpath(cell, *parts).read_text(encoding="utf-8"))


def controller_dns_address(cell: str) -> str:
    argv = evidence_json(cell, "raw", "fixture", "controller-argv.json")
    return argv[argv.index("--dns-address") + 1]


def observation(address: str, name: str, kind: str, values: dict) -> dict:
    """An answer section as query_dns_observation records it, from captured values."""

    def section(transport: str) -> dict:
        rdata = [
            (f"ns1.s1-kill.test. hostmaster.s1-kill.test. {value} 10800 3600 604800 3600"
             if kind == "SOA" else value)
            for value in values[transport]
        ]
        return {
            "transport": transport, "aa": True, "rcode": 0, "answer_count": len(rdata),
            "authority_count": 0, "additional_count": 0,
            "answers": [{"name": name + ".", "type": kind, "class": "IN", "ttl": 300,
                         "rdata": item} for item in rdata],
            "values": list(values[transport]),
        }

    return {"server": address, "port": 53, "qname": name, "qtype": kind,
            "udp": section("udp"), "tcp": section("tcp")}


def captured_answers(cell: str, dns_address: str):
    """query_dns_observation replaying the cell's recorded pair answers."""

    pair = evidence_json(cell, "raw", "results", "result.json")["fresh_primary_v3"]["pair"]
    by_server = {dns_address: pair["primary"], run_cell.FRESH_PRIMARY_V3_PEER_IP: pair["secondary"]}
    labels = {(run_cell.PAIRED_SECONDARY_ZONE, "SOA"): "member_soa",
              (run_cell.PAIRED_SECONDARY_QUERY, "A"): "www_a",
              (run_cell.FRESH_PRIMARY_V3_CATALOG, "SOA"): "catalog_soa"}

    def query(address, port, name, kind, timeout):
        if address not in by_server:
            raise OSError(f"no captured answer from {address}")
        return observation(address, name, kind, by_server[address][labels[(name, kind)]])

    return query, pair


class PairExpectationTest(unittest.TestCase):
    def selected(self, root: str, dns_address: str) -> object:
        return base.settings(root, base.v3("committed", "after-write"), dns_address=dns_address)

    def test_expected_records_come_from_the_published_scenario(self) -> None:
        scenario = evidence_json("c01-pri-intent", "raw", "fixture", "scenario.json")
        expected = run_cell.fresh_primary_expected_pair(scenario)
        self.assertEqual(expected["member_soa"], [2026083101])
        self.assertEqual(expected["www_a"], ["192.0.2.10"])
        self.assertEqual(controller_dns_address("c01-pri-intent"), "10.0.2.15")
        self.assertNotIn(controller_dns_address("c01-pri-intent"), expected["www_a"])
        self.assertEqual(expected["zone_set"], run_cell.FRESH_PRIMARY_ZONE_SET_MEMBER)
        self.assertEqual(expected["catalog_members"], ["s1-kill.test"])
        # An explicit empty list is the zero-zone variant (no member to judge),
        # never a scenario whose member went missing.
        empty = run_cell.fresh_primary_expected_pair({**scenario, "zones": []})
        self.assertEqual((empty["zone_set"], empty["catalog_members"], empty["member_soa"]),
                         (run_cell.FRESH_PRIMARY_ZONE_SET_EMPTY, [], None))
        for broken in (
            {**scenario, "zones": [{**scenario["zones"][0], "delete": True}]},
            {**scenario, "zones": [{**scenario["zones"][0], "domain": "other.test"}]},
            {**scenario, "zones": [{**scenario["zones"][0], "records": [
                record for record in scenario["zones"][0]["records"]
                if record["name"] != "www.s1-kill.test"]}]},
        ):
            with self.assertRaises(run_cell.ControllerError):
                run_cell.fresh_primary_expected_pair(broken)

    def test_the_seven_failed_cells_pass_with_their_captured_answers(self) -> None:
        for cell in FORWARD_CELLS:
            with self.subTest(cell=cell), tempfile.TemporaryDirectory() as root:
                recorded = evidence_json(cell, "raw", "results", "result.json")
                # The recorded verdict failed only on the wrong expectation.
                self.assertEqual(recorded["fresh_primary_v3"]["failures"], [
                    "pair: primary udp www A ['192.0.2.10']",
                    "pair: primary tcp www A ['192.0.2.10']",
                ])
                dns_address = controller_dns_address(cell)
                query, pair = captured_answers(cell, dns_address)
                expected = run_cell.fresh_primary_expected_pair(
                    evidence_json(cell, "raw", "fixture", "scenario.json"))
                state = {"exists": True, "semantic": {
                    "primary_catalog_serial": pair["state_catalog_serial"]}}
                with mock.patch.object(run_cell, "query_dns_observation", side_effect=query), \
                        mock.patch.object(run_cell, "read_dns_state_optional", return_value=state), \
                        base.catalog_sources(pair["state_catalog_serial"], ["s1-kill.test"]):
                    report = run_cell.check_fresh_primary_pair(
                        self.selected(root, dns_address), expected)
                self.assertEqual(report["failures"], [])
                self.assertEqual(report["unknown"], [])
                self.assertEqual(report["primary"], pair["primary"])
                self.assertEqual(report["secondary"], pair["secondary"])
                # Item 5: which address each query went to, and the answers.
                self.assertEqual(report["query_targets"]["primary"], "10.0.2.15")
                self.assertEqual(report["query_targets"]["secondary"], "192.0.2.11")
                www = report["observations"]["primary"]["www_a"]
                self.assertEqual(www["server"], "10.0.2.15")
                self.assertEqual(www["udp"]["answers"][0]["rdata"], "192.0.2.10")
                self.assertEqual(report["observations"]["secondary"]["www_a"]["server"],
                                 "192.0.2.11")
                self.assertEqual(report["expected"]["www_a"], ["192.0.2.10"])

    def test_answers_that_do_not_match_the_scenario_fail(self) -> None:
        cell = "c01-pri-intent"
        dns_address = controller_dns_address(cell)
        query, pair = captured_answers(cell, dns_address)
        expected = run_cell.fresh_primary_expected_pair(
            evidence_json(cell, "raw", "fixture", "scenario.json"))
        serial = pair["state_catalog_serial"]

        def altered(role_address: str, label: str, values: dict):
            def replay(address, port, name, kind, timeout):
                item = query(address, port, name, kind, timeout)
                labels = {"SOA": "member_soa" if name == run_cell.PAIRED_SECONDARY_ZONE
                          else "catalog_soa", "A": "www_a"}
                if address == role_address and labels[kind] == label:
                    return observation(address, name, kind, values)
                return item
            return replay

        cases = (
            # The old (wrong) expectation: www A equal to the query address.
            (altered(dns_address, "www_a", {"udp": ["10.0.2.15"], "tcp": ["10.0.2.15"]}),
             serial, "primary udp www_a ['10.0.2.15'] (queried at 10.0.2.15); the scenario "
                     "publishes ['192.0.2.10']"),
            (altered("192.0.2.11", "www_a", {"udp": ["192.0.2.99"], "tcp": ["192.0.2.99"]}),
             serial, "secondary udp www_a ['192.0.2.99'] (queried at 192.0.2.11)"),
            (altered("192.0.2.11", "member_soa", {"udp": [2026083100], "tcp": [2026083100]}),
             serial, "secondary udp member_soa [2026083100]"),
            (altered("192.0.2.11", "catalog_soa", {"udp": [1], "tcp": [1]}),
             serial, "secondary udp catalog SOA [1]"),
            (query, serial + 1, "differs from the state receipt's"),
        )
        for replay, state_serial, text in cases:
            with self.subTest(text=text), tempfile.TemporaryDirectory() as root:
                state = {"exists": True, "semantic": {"primary_catalog_serial": state_serial}}
                with mock.patch.object(run_cell, "query_dns_observation", side_effect=replay), \
                        mock.patch.object(run_cell, "read_dns_state_optional", return_value=state), \
                        base.catalog_sources(serial, ["s1-kill.test"]):
                    report = run_cell.check_fresh_primary_pair(
                        self.selected(root, dns_address), expected)
                self.assertTrue(any(text in item for item in report["failures"]),
                                report["failures"])

    def test_unreadable_scenario_is_unknown_not_a_pass(self) -> None:
        cell = "c01-pri-intent"
        dns_address = controller_dns_address(cell)
        query, pair = captured_answers(cell, dns_address)
        state = {"exists": True, "semantic": {"primary_catalog_serial": pair["state_catalog_serial"]}}
        with tempfile.TemporaryDirectory() as root, \
                mock.patch.object(run_cell, "query_dns_observation", side_effect=query), \
                mock.patch.object(run_cell, "read_dns_state_optional", return_value=state), \
                base.catalog_sources(pair["state_catalog_serial"], ["s1-kill.test"]):
            report = run_cell.check_fresh_primary_pair(self.selected(root, dns_address))
        self.assertEqual(report["failures"], [])
        self.assertTrue(any("expected records could not be read" in item
                            for item in report["unknown"]))

    def test_no_pair_check_uses_the_dns_address_as_expected_rdata(self) -> None:
        source = (HERE / "run_cell.py").read_text(encoding="utf-8")
        for text in ("[local]", "[settings.dns_address]", "== settings.dns_address",
                     "!= settings.dns_address"):
            self.assertNotIn(text, source)


def dns_name(name: str) -> bytes:
    return b"".join(bytes([len(label)]) + label.encode() for label in name.split(".")) + b"\0"


def wire_reply(transaction_id: int, name: str, kind: str, records: list[tuple[int, bytes]],
               *, flags: int = 0x8400) -> bytes:
    """Header, one question, answers whose owner points at the question name."""

    header = struct.pack("!HHHHHH", transaction_id, flags, 1, len(records), 0, 0)
    question = dns_name(name) + struct.pack("!HH", run_cell.DNS_TYPES[kind], 1)
    answers = b"".join(
        b"\xc0\x0c" + struct.pack("!HHIH", rtype, 1, 300, len(rdata)) + rdata
        for rtype, rdata in records)
    return header + question + answers


class AnswerSectionTest(unittest.TestCase):
    def test_full_answer_section_of_the_captured_answers(self) -> None:
        a_reply = wire_reply(7, "www.s1-kill.test", "A", [(1, bytes([192, 0, 2, 10]))])
        section = run_cell.parse_dns_answer_section(a_reply, 7, "A", "udp")
        self.assertEqual(section["answers"], [{
            "name": "www.s1-kill.test.", "type": "A", "class": "IN", "ttl": 300,
            "rdata": "192.0.2.10"}])
        self.assertEqual(section["values"], run_cell.parse_dns_rrset(a_reply, 7, "A", "udp"))
        self.assertTrue(section["aa"])
        soa_rdata = (dns_name("ns1.s1-kill.test") + dns_name("hostmaster.s1-kill.test")
                     + struct.pack("!IIIII", 2026083101, 10800, 3600, 604800, 3600))
        soa_reply = wire_reply(9, "s1-kill.test", "SOA", [(6, soa_rdata)])
        section = run_cell.parse_dns_answer_section(soa_reply, 9, "SOA", "tcp")
        self.assertEqual(section["answers"][0]["rdata"],
                         "ns1.s1-kill.test. hostmaster.s1-kill.test. 2026083101 10800 3600 "
                         "604800 3600")
        self.assertEqual(section["values"], [2026083101])
        with self.assertRaisesRegex(run_cell.ControllerError, "not authoritative"):
            run_cell.parse_dns_answer_section(
                wire_reply(7, "www.s1-kill.test", "A", [(1, b"\xc0\0\2\x0a")], flags=0x8000),
                7, "A", "udp")

    def test_observation_records_where_each_query_went(self) -> None:
        def exchange(address, port, name, kind, timeout, transport):
            return wire_reply(5, name, kind, [(1, bytes([192, 0, 2, 10]))]), 5

        with mock.patch.object(run_cell, "_dns_exchange", side_effect=exchange) as sent:
            item = run_cell.query_dns_observation("10.0.2.15", 53, "www.s1-kill.test", "A", 1)
        self.assertEqual((item["server"], item["port"], item["qname"]),
                         ("10.0.2.15", 53, "www.s1-kill.test"))
        self.assertEqual(run_cell.observation_values(item),
                         {"udp": ["192.0.2.10"], "tcp": ["192.0.2.10"]})
        self.assertEqual([call.args[5] for call in sent.call_args_list], ["udp", "tcp"])


class HostIdleWaitTest(unittest.TestCase):
    def clock(self):
        now = [0.0]

        def clock() -> float:
            return now[0]

        def sleep(seconds: float) -> None:
            now[0] += seconds

        return clock, sleep

    def test_waits_for_settled_idle_and_records_what_was_running(self) -> None:
        answers = iter([
            {"ready": False, "code": "HOST_MUTATION_BUSY", "reason": "agent_mutation_active"},
            {"ready": False, "code": "HOST_MUTATION_BUSY", "reason": "agent_mutation_active"},
            {"ready": False, "code": "HOST_MUTATION_BUSY", "reason": "package_manager_active"},
            {"ready": True}, {"ready": True},
        ])
        activity = mock.Mock(return_value={"ledger_active_request_id": "a" * 32})
        clock, sleep = self.clock()
        transcript = base.Transcript()
        with tempfile.TemporaryDirectory() as root:
            report = run_cell.wait_for_host_idle(
                base.settings(root, base.v3("intent", "after-write")), {}, transcript,
                probe=lambda: next(answers), activity=activity, clock=clock, sleep=sleep)
        self.assertTrue(report["idle"])
        self.assertEqual(report["polls"], 5)
        self.assertEqual([item["reason"] for item in report["busy"]],
                         ["agent_mutation_active", "package_manager_active"])
        self.assertEqual(report["busy"][0]["seconds"], 1.0)
        self.assertEqual(activity.call_count, 2)
        self.assertEqual(transcript.events[-1][0], "host-idle-wait")

    def test_never_idle_is_refused_before_any_mutation(self) -> None:
        clock, sleep = self.clock()
        probe = mock.Mock(return_value={"ready": False, "code": "HOST_MUTATION_BUSY",
                                        "reason": "host_lock_busy"})
        with tempfile.TemporaryDirectory() as root:
            selected = replace(base.settings(root, base.v3("intent", "after-write")),
                               host_idle_timeout=5.0)
            report = run_cell.wait_for_host_idle(
                selected, {}, base.Transcript(), probe=probe,
                activity=lambda: {"package_manager_processes": []}, clock=clock, sleep=sleep)
        self.assertFalse(report["idle"])
        self.assertEqual(report["busy"][0]["seconds"], 5.0)
        self.assertIn("last_activity", report)
        refusal = run_cell.host_idle_refusal(report)
        self.assertIn("host_lock_busy for 5.0 s", refusal)
        self.assertIn("nothing was started or cancelled", refusal)
        # An idle answer interrupted by a busy one does not count as settled.
        answers = iter([{"ready": True}, {"ready": None, "error": "exit 75"}, {"ready": True},
                        {"ready": True}])
        clock, sleep = self.clock()
        with tempfile.TemporaryDirectory() as root:
            report = run_cell.wait_for_host_idle(
                base.settings(root, base.v3("intent", "after-write")), {}, base.Transcript(),
                probe=lambda: next(answers), activity=dict, clock=clock, sleep=sleep)
        self.assertTrue(report["idle"])
        self.assertEqual(report["busy"][0]["error"], "exit 75")

    def test_decode_and_the_wait_runs_between_the_gate_and_the_trigger(self) -> None:
        schema = run_cell.HOST_READINESS_SCHEMA

        def command(returncode: int, value: object) -> object:
            output = (json.dumps(value) + "\n").encode() if value is not None else b""
            return run_cell.CommandResult(("/opt/t", "rpc-host-readiness"), returncode, output,
                                          False, 0.1)

        self.assertTrue(run_cell.decode_host_readiness(command(0, {"schema": schema, "ready": True}))["ready"])
        busy = run_cell.decode_host_readiness(command(0, {
            "schema": schema, "ready": False, "code": "HOST_MUTATION_BUSY",
            "reason": "agent_mutation_active"}))
        self.assertEqual((busy["ready"], busy["reason"]), (False, "agent_mutation_active"))
        for returncode, value in ((75, {"schema": schema, "ready": True}),
                                  (0, {"schema": "other", "ready": True}), (0, None),
                                  (69, {"schema": schema, "ready": False, "error": "absent"})):
            self.assertIsNone(run_cell.decode_host_readiness(command(returncode, value))["ready"])
        source = (HERE / "run_cell.py").read_text(encoding="utf-8")
        flow = source[source.index("def run_cell(settings:"):]
        self.assertLess(flow.index("run_fresh_primary_gate_probe("),
                        flow.index("wait_for_host_idle(settings, ordinary, transcript)"))
        self.assertLess(flow.index("wait_for_host_idle(settings, ordinary, transcript)"),
                        flow.index("trigger = start_async_command("))
        with tempfile.TemporaryDirectory() as root:
            argv = run_cell.host_readiness_argv(base.settings(root, base.v3("intent", "after-write")))
        self.assertEqual(argv[:2], ("/opt/t", "rpc-host-readiness"))

    def test_activity_is_read_only_and_names_lock_holders(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            proc = Path(root) / "proc"
            for pid, comm in (("41", "apt-get"), ("42", "bash"), ("43", "unattended-upgr")):
                (proc / pid).mkdir(parents=True)
                (proc / pid / "comm").write_text(comm + "\n", encoding="utf-8")
            lock = Path(root) / "lock-frontend"
            lock.write_text("", encoding="utf-8")
            status = os.stat(lock)
            device = f"{os.major(status.st_dev):02x}:{os.minor(status.st_dev):02x}:{status.st_ino}"
            (proc / "locks").write_text(
                f"1: POSIX  ADVISORY  WRITE 43 {device} 0 EOF\n"
                "2: FLOCK  ADVISORY  WRITE 42 00:1a:99 0 EOF\n", encoding="ascii")
            before = sorted(str(path) for path in Path(root).rglob("*"))
            report = run_cell.observe_host_activity(
                str(Path(root) / "state"), proc_root=str(proc), lock_paths=(str(lock),),
                pacman_lock=str(Path(root) / "db.lck"))
            self.assertEqual(sorted(str(path) for path in Path(root).rglob("*")), before)
        self.assertEqual(report["package_manager_processes"], [{"pid": 41, "comm": "apt-get"}])
        self.assertEqual(report["package_manager_lock_holders"][0]["pid"], 43)
        self.assertEqual(report["package_manager_lock_holders"][0]["comm"], "unattended-upgr")
        self.assertFalse(report["pacman_lock_present"])
        self.assertTrue(any("ledger" in item for item in report["unknown"]))


class ZoneLifecycleBeforeRebootTest(unittest.TestCase):
    def settings(self, root: str, **changes: object) -> object:
        values: dict = {"reboot_after_recovery": True, "disable_management_before_reboot": True,
                        "zone_lifecycle_before_reboot": True, "reboot_dir": root}
        values.update(changes)
        return base.settings(root, base.v3("target-started", "after-write"), **values)

    def test_child_constants_mirror_the_trigger(self) -> None:
        source = TRIGGER_SOURCE.read_text(encoding="utf-8")
        self.assertIn('freshPrimaryZoneDomain   = "' + run_cell.FRESH_PRIMARY_V3_CHILD_ZONE + '"',
                      source)
        serial = run_cell.FRESH_PRIMARY_V3_CHILD_AFTER_LIFECYCLE["soa_serial"][0]
        self.assertIn(f'{{name: "re-add", generation: 4, serial: "{serial}", predecessor: "delete"}}',
                      source)
        self.assertIn('{Name: "www." + domain, Type: "A", Content: "'
                      + run_cell.FRESH_PRIMARY_V3_CHILD_AFTER_LIFECYCLE["www_a"][0] + '", TTL: 300}',
                      source)
        self.assertEqual(bind_peer.CHILD, run_cell.FRESH_PRIMARY_V3_CHILD_ZONE)
        self.assertEqual(int(bind_peer.CHILD_STEPS["re-add"]["serial"]), serial)

    def test_settings_admit_the_full_combination_only_where_it_applies(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            run_cell.validate_reboot_settings(self.settings(root))
            run_cell.validate_reboot_settings(self.settings(root, zone_lifecycle_outcome="passed"))
            for changes, text in (
                ({"reboot_after_recovery": False, "disable_management_before_reboot": False},
                 "--zone-lifecycle-before-reboot applies only"),
                ({"owner_edit": "config"}, "--zone-lifecycle-before-reboot applies only"),
                ({"zone_lifecycle_before_reboot": False, "zone_lifecycle_outcome": "passed"},
                 "--zone-lifecycle-outcome continues"),
                ({"zone_lifecycle_outcome": "passed", "resume_after_reboot": True},
                 "--zone-lifecycle-outcome continues"),
            ):
                with self.subTest(changes=changes), self.assertRaisesRegex(
                        run_cell.ControllerError, text):
                    run_cell.validate_reboot_settings(self.settings(root, **changes))
            with self.assertRaisesRegex(run_cell.ControllerError, "applies only"):
                run_cell.validate_reboot_settings(base.settings(
                    root, base.spec("bind__target-staged__after-write__standalone__peer-reachable"),
                    reboot_after_recovery=True, zone_lifecycle_before_reboot=True, reboot_dir=root))
            first = self.settings(root)
            self.assertEqual(
                run_cell.settings_fingerprint(first),
                run_cell.settings_fingerprint(replace(first, zone_lifecycle_outcome="passed")))
        actions = run_cell.build_argument_parser()._option_string_actions
        self.assertEqual(tuple(actions["--zone-lifecycle-outcome"].choices), ("passed", "not-passed"))
        self.assertIsNone(actions["--zone-lifecycle-outcome"].default)
        self.assertEqual(actions["--host-idle-timeout"].default, 120.0)
        self.assertIn("--zone-lifecycle-before-reboot", actions)

    def test_a_complete_pass_suspends_for_the_lifecycle_before_anything_else(self) -> None:
        boot = {"boot_id": "b1", "product_uuid": "u"}
        with tempfile.TemporaryDirectory() as root, \
                mock.patch.object(run_cell, "pre_reboot_verdict", return_value={"passed": True}), \
                mock.patch.object(run_cell, "read_guest_boot_identity", return_value=boot), \
                mock.patch.object(run_cell, "observe_serving_authority") as authority, \
                mock.patch.object(run_cell, "disable_management_units") as disable:
            result: dict = {}
            with self.assertRaises(run_cell.RebootRequested) as raised:
                run_cell.maybe_request_reboot_after_recovery(
                    self.settings(root), result, {}, {"flow": "rpc-retry"})
        self.assertEqual(raised.exception.stage, run_cell.ZONE_LIFECYCLE_BEFORE_REBOOT)
        self.assertEqual(run_cell.suspended_exit(raised.exception),
                         run_cell.ZONE_LIFECYCLE_REQUESTED_EXIT)
        self.assertEqual(run_cell.ZONE_LIFECYCLE_REQUESTED_EXIT,
                         bootstrap.ZONE_LIFECYCLE_REQUESTED_EXIT)
        authority.assert_not_called()
        disable.assert_not_called()
        self.assertEqual(result["zone_lifecycle_before_reboot"]["order"][1],
                         "zone lifecycle (host, Agent running)")
        # A failed pre-reboot verdict never suspends for the lifecycle.
        with tempfile.TemporaryDirectory() as root, mock.patch.object(
                run_cell, "pre_reboot_verdict", return_value={"passed": False, "reasons": ["x"]}):
            result = {}
            run_cell.maybe_request_reboot_after_recovery(
                self.settings(root), result, {}, {"flow": "rpc-retry"})
        self.assertFalse(result["reboot_after_recovery"]["run"])
        self.assertNotIn("zone_lifecycle_before_reboot", result)

    def checkpoint(self, root: str, stage: str = run_cell.ZONE_LIFECYCLE_BEFORE_REBOOT) -> dict:
        path = Path(root) / "reboot-checkpoint-1.json"
        path.write_text("{}", encoding="utf-8")
        return {
            "stage": stage, "boot": {"boot_id": "b1", "product_uuid": "u"},
            "state": {"flow": "rpc-retry", "peer_ip": "", "boot": {"boot_id": "b1"}},
            "result": {"status": "passed", "safety_status": "passed"},
            "failures": {"safety": [], "verification": [], "diagnostic": []},
            "kill_proven": True, "transcripts": [],
        }

    def continue_run(self, root: str, outcome: str, *, boot_id: str = "b1",
                     child: dict | None = None, stage: str = run_cell.ZONE_LIFECYCLE_BEFORE_REBOOT):
        selected = self.settings(root, zone_lifecycle_outcome=outcome,
                                 result_path=root + "/result.json")
        reboot = mock.Mock(side_effect=run_cell.RebootRequested(
            run_cell.REBOOT_AFTER_RECOVERY, {"boot": {"boot_id": "b1"}}))
        with mock.patch.object(run_cell, "validate_controller_identity"), \
                mock.patch.object(run_cell, "validate_settings", return_value={}), \
                mock.patch.object(run_cell, "load_pending_reboot_checkpoint",
                                  return_value=(1, self.checkpoint(root, stage))), \
                mock.patch.object(run_cell, "read_guest_boot_identity",
                                  return_value={"boot_id": boot_id, "product_uuid": "u"}), \
                mock.patch.object(run_cell, "check_fresh_primary_child",
                                  return_value=child or {"failures": [], "unknown": [],
                                                         "primary": {"p": 1}, "secondary": {"p": 1}}), \
                mock.patch.object(run_cell, "request_after_recovery_reboot", reboot), \
                mock.patch.object(run_cell, "write_reboot_checkpoint") as write:
            code = run_cell.continue_after_zone_lifecycle(selected)
        return code, reboot, write

    def test_continuation_disables_and_reboots_only_after_a_passed_lifecycle(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            code, reboot, write = self.continue_run(root, "passed")
            self.assertEqual(code, run_cell.REBOOT_REQUESTED_EXIT)
            state = reboot.call_args.args[3]
            self.assertEqual(state["zone_lifecycle"]["outcome"], "passed")
            self.assertEqual(state["zone_lifecycle"]["child_before_reboot"],
                             {"primary": {"p": 1}, "secondary": {"p": 1}})
            self.assertNotIn("boot", state)
            self.assertEqual(write.call_args.args[1], 2)
            self.assertTrue((Path(root) / "reboot-resumed-1.json").exists())
        with tempfile.TemporaryDirectory() as root:
            code, reboot, write = self.continue_run(root, "not-passed")
            self.assertEqual(code, 0)
            reboot.assert_not_called()
            write.assert_not_called()
            result = json.loads((Path(root) / "result.json").read_text(encoding="utf-8"))
            self.assertFalse(result["reboot_after_recovery"]["run"])
            self.assertEqual(result["zone_lifecycle_before_reboot"]["outcome"], "not-passed")
        with tempfile.TemporaryDirectory() as root:
            code, reboot, _ = self.continue_run(root, "passed", child={
                "failures": ["secondary udp s2 soa_serial [2026092802]"], "unknown": []})
            self.assertEqual(code, 1)
            reboot.assert_not_called()
        with tempfile.TemporaryDirectory() as root, self.assertRaisesRegex(
                run_cell.ControllerError, "rebooted or changed"):
            self.continue_run(root, "passed", boot_id="b2")
        with tempfile.TemporaryDirectory() as root, self.assertRaisesRegex(
                run_cell.ControllerError, "not the zone lifecycle"):
            self.continue_run(root, "passed", stage=run_cell.REBOOT_AFTER_RECOVERY)

    def test_resume_refuses_a_checkpoint_that_waits_for_the_lifecycle(self) -> None:
        with tempfile.TemporaryDirectory() as root, \
                mock.patch.object(run_cell, "validate_controller_identity"), \
                mock.patch.object(run_cell, "validate_settings", return_value={}), \
                mock.patch.object(run_cell, "load_pending_reboot_checkpoint",
                                  return_value=(1, self.checkpoint(root))), \
                self.assertRaisesRegex(run_cell.ControllerError, "waits for the host's zone"):
            run_cell.resume_cell(self.settings(root, resume_after_reboot=True))

    def test_after_reboot_checks_include_the_zone_set_after_the_lifecycle(self) -> None:
        source = (HERE / "run_cell.py").read_text(encoding="utf-8")
        verify = source[source.index("def _verify_after_recovery_reboot("):]
        self.assertLess(
            verify.index('check_fresh_primary_child(settings, lifecycle.get("child_before_reboot"))'),
            verify.index('if state.get("management_disabled") is True:'))

    def test_child_check_expects_the_re_add_on_both_servers(self) -> None:
        good = {"soa_serial": {"udp": [2026092803], "tcp": [2026092803]},
                "www_a": {"udp": ["192.0.2.10"], "tcp": ["192.0.2.10"]}}

        def replay(secondary_serial: int):
            def query(address, port, name, kind, timeout):
                label = "soa_serial" if kind == "SOA" else "www_a"
                values = dict(good[label])
                if address == run_cell.FRESH_PRIMARY_V3_PEER_IP and kind == "SOA":
                    values = {"udp": [secondary_serial], "tcp": [secondary_serial]}
                return observation(address, name, kind, values)
            return query

        with tempfile.TemporaryDirectory() as root:
            selected = base.settings(root, base.v3("committed", "after-write"),
                                     dns_address="10.0.2.15")
            with mock.patch.object(run_cell, "query_dns_observation", side_effect=replay(2026092803)):
                report = run_cell.check_fresh_primary_child(selected)
                self.assertEqual(report["failures"], [])
                self.assertEqual(report["observations"]["10.0.2.15"]["www_a"]["server"], "10.0.2.15")
                baseline = {"primary": report["primary"], "secondary": report["secondary"]}
                self.assertEqual(run_cell.check_fresh_primary_child(selected, baseline)["failures"], [])
            with mock.patch.object(run_cell, "query_dns_observation", side_effect=replay(2026092802)):
                report = run_cell.check_fresh_primary_child(selected, baseline)
            self.assertTrue(any("the lifecycle's last step publishes [2026092803]" in item
                                for item in report["failures"]))
            self.assertTrue(any("changed across the reboot" in item for item in report["failures"]))


class HostOrderTest(unittest.TestCase):
    RAW = base.raw_cell("pdns-switch__target-started__after-write__paired-primary__peer-reachable")

    def args(self, **changes: object) -> argparse.Namespace:
        values = dict(
            work_root=Path("/w"), cell_id=self.RAW["id"], manifest=Path("/m"), node="debian13",
            identity_file=Path("/k"), source_fixture="uninitialized", execute=True,
            owner_edit=None, owner_release_recovery=False, zone_lifecycle=True,
            owner_directives=False, reboot_after_recovery=True,
            disable_management_before_reboot=True, stop_after_kill_for_independent_recovery=False,
        )
        values.update(changes)
        return argparse.Namespace(**values)

    def test_flags_admit_the_full_combination_in_canonical_order(self) -> None:
        args = self.args()
        bootstrap.validate_fresh_primary_run(args, self.RAW, True)
        self.assertEqual(bootstrap.prepared_flags(args), [
            bootstrap.REBOOT_AFTER_RECOVERY_FLAG, bootstrap.DISABLE_MANAGEMENT_FLAG,
            bootstrap.ZONE_LIFECYCLE_BEFORE_REBOOT_FLAG,
        ])
        self.assertEqual(bootstrap.prepared_flags(self.args(reboot_after_recovery=False,
                                                            disable_management_before_reboot=False)), [])
        order = bootstrap.RUN_PREPARED_CODE.split("order = [", 1)[1].split("]", 1)[0]
        names = [name.strip() for name in order.replace("\n", " ").split(",") if name.strip()]
        self.assertEqual(len(names), len(bootstrap.PREPARED_FLAG_ORDER))
        self.assertEqual(bootstrap.fresh_primary_expected_www(), "192.0.2.10")

    def run_host(self, codes: list[int], *, peer: int = 0, lifecycle: int = 0,
                 after_reboot: int = 0, **changes: object):
        events: list[str] = []
        runs: list[str] = []

        def run(command, check=False):
            runs.append(command[-1])
            events.append("guest")
            return subprocess.CompletedProcess(command, codes[len(runs) - 1])

        def peer_verdict(args, plan, guest_returncode, **kwargs):
            events.append("peer-after-reboot" if kwargs.get("child_step") else "peer")
            return bootstrap.worst_exit(
                guest_returncode, after_reboot if kwargs.get("child_step") else peer)

        def reboot(plan, node, identity, timeout):
            events.append("reboot " + node)
            return {"node": node}

        def zone_lifecycle(args, plan, **kwargs):
            events.append("lifecycle")
            return lifecycle, {0: "passed", 1: "failed"}.get(lifecycle, "unverified")

        with mock.patch.object(bootstrap, "load_plan", return_value=({}, self.RAW, {"n": 1})), \
                mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/k")), \
                mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]), \
                mock.patch.object(bootstrap.subprocess, "run", side_effect=run), \
                mock.patch.object(bootstrap, "finish_fresh_primary_peer_verdict",
                                  side_effect=peer_verdict), \
                mock.patch.object(bootstrap, "run_zone_lifecycle_status",
                                  side_effect=zone_lifecycle), \
                mock.patch.object(bootstrap, "verify_guest_zone_set"), \
                mock.patch.object(bootstrap.fixture, "reboot_guest", side_effect=reboot), \
                mock.patch("sys.stdout", new_callable=io.StringIO):
            code = bootstrap.run_prepared(self.args(**changes))
        return code, events, runs

    def test_execute_runs_lifecycle_then_disable_then_reboot_then_checks(self) -> None:
        code, events, runs = self.run_host([5, 3, 0])
        self.assertEqual(code, 0)
        self.assertEqual(events, ["guest", "peer", "lifecycle", "guest", "reboot arch",
                                  "reboot debian13", "guest", "peer-after-reboot"])
        self.assertTrue(runs[0].endswith(bootstrap.ZONE_LIFECYCLE_BEFORE_REBOOT_FLAG))
        self.assertTrue(runs[1].endswith(bootstrap.ZONE_LIFECYCLE_OUTCOME_PREFIX + "passed"))
        self.assertIn(bootstrap.DISABLE_MANAGEMENT_FLAG, runs[1])
        self.assertTrue(runs[2].endswith(bootstrap.RESUME_FLAG))

    def test_a_lifecycle_that_did_not_pass_is_continued_without_a_reboot(self) -> None:
        code, events, runs = self.run_host([5, 0], lifecycle=1)
        self.assertEqual(code, 1)
        self.assertEqual(events, ["guest", "peer", "lifecycle", "guest"])
        self.assertTrue(runs[1].endswith(bootstrap.ZONE_LIFECYCLE_OUTCOME_PREFIX + "not-passed"))
        code, events, _ = self.run_host([5, 0], peer=2)
        self.assertEqual((code, events), (2, ["guest", "peer", "guest"]))
        # A failed pre-reboot verdict: no suspension, no lifecycle, no reboot.
        code, events, _ = self.run_host([1], peer=0)
        self.assertEqual((code, events), (1, ["guest", "peer"]))

    def test_dry_run_prints_the_defined_order(self) -> None:
        with mock.patch.object(bootstrap, "load_plan", return_value=({}, self.RAW, {"n": 1})), \
                mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/k")), \
                mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]), \
                mock.patch("sys.stdout", new_callable=io.StringIO) as output:
            self.assertEqual(bootstrap.run_prepared(self.args(execute=False)), 0)
        lines = [json.loads(line) for line in output.getvalue().splitlines()]
        plan = next(line for line in lines
                    if isinstance(line, dict) and line.get("on_exit") == 5)
        order = plan["order"]
        self.assertLess(order.index("zone lifecycle step re-add (Agent running) + observe-child "
                                    "--step re-add"),
                        order.index(bootstrap.DISABLE_MANAGEMENT_FLAG
                                    + ": stop and disable Panel and Agent"))
        self.assertLess(order.index(bootstrap.DISABLE_MANAGEMENT_FLAG
                                    + ": stop and disable Panel and Agent"),
                        order.index("reboot both guests (native BIND secondary first)"))
        self.assertIn("--zone-lifecycle-outcome=<passed|not-passed>", plan["continue"][-1])
        reboot = next(line for line in lines
                      if isinstance(line, dict) and line.get("on_exit") == 3)
        self.assertTrue(reboot["then"][-1].endswith(bootstrap.RESUME_FLAG))

    @unittest.skipUnless(
        sys.platform == "linux" and hasattr(os, "geteuid") and os.geteuid() == 0,
        "prepared argv owner proof requires a root Linux fixture",
    )
    def test_guest_program_admits_the_lifecycle_flags_only_where_they_apply(self) -> None:
        primary = self.RAW["id"]
        lifecycle = [bootstrap.REBOOT_AFTER_RECOVERY_FLAG, bootstrap.DISABLE_MANAGEMENT_FLAG,
                     bootstrap.ZONE_LIFECYCLE_BEFORE_REBOOT_FLAG]
        cases = (
            (primary, lifecycle, True),
            (primary, lifecycle + ["--zone-lifecycle-outcome=passed"], True),
            (primary, lifecycle + ["--zone-lifecycle-outcome=not-passed"], True),
            (primary, lifecycle + [bootstrap.RESUME_FLAG], True),
            (primary, lifecycle + ["--zone-lifecycle-outcome=maybe"], False),
            (primary, lifecycle + [bootstrap.RESUME_FLAG, "--zone-lifecycle-outcome=passed"], False),
            (primary, [bootstrap.ZONE_LIFECYCLE_BEFORE_REBOOT_FLAG], False),
            (primary, [bootstrap.REBOOT_AFTER_RECOVERY_FLAG, "--zone-lifecycle-outcome=passed"], False),
            ("bind__target-staged__after-write__standalone__peer-reachable",
             [bootstrap.REBOOT_AFTER_RECOVERY_FLAG, bootstrap.ZONE_LIFECYCLE_BEFORE_REBOOT_FLAG],
             False),
        )
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            prepared = root / "controller-argv.json"
            captured = root / "captured.json"
            executable = root / "run-cell.py"
            executable.write_text(
                "#!/usr/bin/env python3\nimport json, sys\n"
                f"open({str(captured)!r}, 'w').write(json.dumps(sys.argv))\n", encoding="utf-8")
            executable.chmod(0o700)
            code = bootstrap.RUN_PREPARED_CODE.replace(
                "/var/lib/celikpanel-dns-kill-matrix/controller-argv.json", str(prepared)
            ).replace("/opt/celikpanel/libexec/dns-kill-run-cell.py", str(executable))
            for cell_id, flags, accepted in cases:
                base_argv = [str(executable), "--cell-id", cell_id, "--trigger-mode", "socket",
                             "--result", str(root / "result.json")]
                prepared.write_text(json.dumps(base_argv), encoding="utf-8")
                prepared.chmod(0o600)
                captured.unlink(missing_ok=True)
                with self.subTest(cell_id=cell_id, flags=flags):
                    returncode = subprocess.run(
                        [sys.executable, "-c", code, cell_id, *flags],
                        check=False, capture_output=True).returncode
                    self.assertEqual(returncode == 0, accepted)


class PeerWithChildTest(unittest.TestCase):
    def test_observe_with_child_expects_both_catalog_members(self) -> None:
        import test_native_pdns_bind_peer as peer_tests

        def catalog(members: list[str]) -> str:
            text = peer_tests.pdns_catalog_axfr()
            if bind_peer.CHILD in members:
                name = bind_peer.catalog_name("192.0.2.10") + "."
                soa_line = text.splitlines()[1] + "\n"
                text = text[:-len(soa_line)] + (
                    f"0000000000000000000000000000000a.zones.{name} 60 IN PTR {bind_peer.CHILD}.\n"
                    + soa_line)
            return text

        soa = peer_tests.member_answer(bind_peer.ZONE, "SOA",
                                       "ns.test. hostmaster.test. 1 60 30 3600 60")
        address = peer_tests.member_answer(bind_peer.QUERY, "A", "192.0.2.10")

        def run(members: list[str], with_child: bool) -> dict:
            args = argparse.Namespace(cell_id=bind_peer.CELL, execute=True, address="192.0.2.10",
                                      with_child=with_child, identity_file=Path("/k"))
            replies = ["", catalog(members), catalog(members)] + [soa] * 4 + [address] * 4
            with mock.patch.object(bind_peer, "selected", return_value=(
                    "192.0.2.10", "192.0.2.11", {}, Path("/k"))), \
                    mock.patch.object(bind_peer, "verify_guest"), \
                    mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]), \
                    mock.patch.object(bind_peer, "remote_read", side_effect=replies):
                return bind_peer.observe(args)

        both = [bind_peer.ZONE, bind_peer.CHILD]
        report = run(both, True)
        self.assertEqual(report["catalog_members"], sorted(both))
        self.assertEqual(report["answers"]["192.0.2.11/tcp/www.s1-kill.test/A"], ["192.0.2.10"])
        with self.assertRaisesRegex(ValueError, "expected members"):
            run([bind_peer.ZONE], True)
        with self.assertRaisesRegex(ValueError, "expected members"):
            run(both, False)


if __name__ == "__main__":
    unittest.main()
