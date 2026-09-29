#!/usr/bin/env python3
"""Offline tests: fresh paired SECONDARY cells (rows 3/5) and the BIND
takeover/reinstall fixtures (rows 12/14).

Nothing here starts a guest, runs --execute or talks to a peer. SSH, SCP,
the native primary peer and every native inspection are mocked. Nothing here
is native evidence.
"""

from __future__ import annotations

import importlib.util
import io
import ipaddress
import json
import os
from pathlib import Path
import sqlite3
import struct
import subprocess
import sys
import tempfile
import unittest
from dataclasses import replace
from unittest import mock

import guest_bootstrap as bootstrap
import native_primary_peer

MODULE_PATH = Path(__file__).with_name("run_cell.py")
SPEC = importlib.util.spec_from_file_location("dns_kill_run_cell_secondary", MODULE_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError(f"cannot import {MODULE_PATH}")
run_cell = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = run_cell
SPEC.loader.exec_module(run_cell)

PROBE_PATH = Path(__file__).with_name("guest_recovery_probe.py")
PROBE_SPEC = importlib.util.spec_from_file_location("dns_kill_probe_secondary", PROBE_PATH)
probe = importlib.util.module_from_spec(PROBE_SPEC)
sys.modules[PROBE_SPEC.name] = probe
PROBE_SPEC.loader.exec_module(probe)

MANIFEST_PATH = Path(__file__).with_name("manifest.json")
MANIFEST = json.loads(MANIFEST_PATH.read_text(encoding="utf-8"))
SHELL = Path(__file__).with_name("guest_bootstrap.sh").read_text(encoding="utf-8")
TAKEOVER_CELL = "bind__target-staged__after-write__standalone__peer-reachable"


def raw_cell(cell_id: str) -> dict:
    return next(item for item in MANIFEST["cells"] if item["id"] == cell_id)


def spec(cell_id: str) -> object:
    return run_cell.CellSpec.from_manifest(MANIFEST, cell_id)


def node_of(raw: dict) -> str:
    return bootstrap.NODE_FOR_PLACEMENT[raw["placement"]["kill_host"]]


def heredoc(marker: str) -> str:
    return SHELL.split(f"<<'{marker}'\n", 1)[1].split(f"\n{marker}\n", 1)[0]


def write_private_json(path: Path, value: object) -> None:
    path.write_bytes(bootstrap.json_bytes(value))
    os.chmod(path, 0o600)


def settings_for(root: str, selected: object, **changes: object) -> object:
    trigger = ("/opt/t", "rpc-switch", "--scenario", root + "/scenario.json",
               "--identity-receipt", root + "/identity.json", "--timeout", "45m")
    settings = run_cell.Settings(
        cell=selected, request_id="1" * 32, nonce="a" * 32,
        tagged_agent_command=("/opt/agent.kill",), trigger_mode="socket",
        trigger_command=trigger, recovery_command=(trigger[0], "rpc-retry", *trigger[2:]),
        source_proof_path=root + "/source-proof.json",
        agent_restart_command=("/bin/true",), panel_restart_command=("/bin/true",),
        recovery_probe_command=("/bin/true",), peer_partition_command=None,
        command_cwd=root, state_dir=root, mutation_lock=root + "/lock",
        agent_socket=root + "/agent.sock", agent_token_file=root + "/token",
        journal_path=root + "/journal.json", marker_path=root + "/marker.json",
        proof_path=root + "/proof.json", result_path=root + "/result.json",
        transcript_path=root + "/transcript.jsonl", dns_address="10.0.2.15",
        dns_port=53, dns_name="www.s1-kill.test", dns_type="A",
        panel_address="127.0.0.1", panel_port=2083, startup_timeout=1,
        boundary_timeout=1, stop_timeout=1, kill_timeout=1, command_timeout=1,
        recovery_timeout=1, endpoint_timeout=1, dns_timeout=1,
        stability_seconds=30, stability_interval=1,
    )
    return replace(settings, **changes) if changes else settings


def dns_name(name: str) -> bytes:
    return b"".join(bytes([len(label)]) + label.encode() for label in name.split(".")) + b"\0"


def dns_reply(transaction_id: int, name: str, qtype: int, answers: list[bytes],
              *, flags: int = 0x8400) -> bytes:
    header = struct.pack("!HHHHHH", transaction_id, flags, 1, len(answers), 0, 0)
    question = dns_name(name) + struct.pack("!HH", qtype, 1)
    body = b""
    for rdata in answers:
        body += b"\xc0\x0c" + struct.pack("!HHIH", qtype, 1, 300, len(rdata)) + rdata
    return header + question + body


def soa_rdata(serial: int) -> bytes:
    return dns_name("ns1.s1-kill.test") + dns_name("hostmaster.s1-kill.test") + struct.pack(
        "!IIIII", serial, 10800, 3600, 604800, 3600
    )


class AdmissionTest(unittest.TestCase):
    def secondary_cells(self) -> list[dict]:
        return [
            raw for raw in MANIFEST["cells"]
            if raw["status"] == "runnable" and raw["role"] == "paired-secondary"
            and raw["driver"] in {"bind", "pdns-switch"}
        ]

    def admitted(self, raw: dict) -> bool:
        try:
            bootstrap.validate_supported_cell(raw, node_of(raw), "uninitialized")
        except bootstrap.BootstrapError:
            return False
        return True

    def test_exact_fresh_secondary_cells_are_admitted(self) -> None:
        admitted = [raw for raw in self.secondary_cells() if self.admitted(raw)]
        self.assertEqual(sum(raw["driver"] == "bind" for raw in admitted), 7)
        self.assertEqual(sum(raw["driver"] == "pdns-switch" for raw in admitted), 17)
        for raw in admitted:
            with self.subTest(cell_id=raw["id"]):
                self.assertEqual(raw["peer_reachability"], "reachable")
                self.assertTrue(run_cell.is_admitted_paired_secondary(spec(raw["id"])))
                self.assertNotEqual(raw["placement"]["kill_host"], raw["placement"]["dns_peer_host"])
                # The peer script accepts every admitted cell.
                native_primary_peer.pair_nodes(
                    {"nodes": {
                        "debian13": {"peer": {"address": "192.0.2.10/24"}},
                        "arch": {"peer": {"address": "192.0.2.11/24"}},
                    }}, raw,
                )
        refused = [raw for raw in self.secondary_cells() if not self.admitted(raw)]
        for raw in refused:
            with self.subTest(refused=raw["id"]):
                self.assertFalse(run_cell.is_admitted_paired_secondary(spec(raw["id"])))
                self.assertTrue(
                    raw["peer_reachability"] == "unreachable"
                    or raw["placement"]["source_fixture_policy"] == "managed-pdns-required"
                    or raw["boundary"]["phase"] in {"committed", "rolling-back"}
                )

    def test_other_sources_nodes_and_unreachable_twins_are_refused(self) -> None:
        reachable = raw_cell("bind__intent__after-write__paired-secondary__peer-reachable")
        for fixture_name in ("managed-pdns", "owner-bind", "managed-bind",
                             "unmanaged-bind-stopped", "managed-bind-absent"):
            with self.subTest(fixture=fixture_name), self.assertRaises(bootstrap.BootstrapError):
                bootstrap.validate_bind_cell(reachable, node_of(reachable), fixture_name)
        with self.assertRaisesRegex(bootstrap.BootstrapError, "placement requires node"):
            bootstrap.validate_bind_cell(reachable, "arch", "uninitialized")
        unreachable = raw_cell("bind__intent__after-write__paired-secondary__peer-unreachable")
        with self.assertRaisesRegex(bootstrap.BootstrapError, "own pass definition"):
            bootstrap.validate_bind_cell(unreachable, node_of(unreachable), "uninitialized")
        critical = raw_cell("bind__target-started__after-write__paired-secondary__peer-reachable")
        with self.assertRaisesRegex(bootstrap.BootstrapError, "row 8"):
            bootstrap.validate_bind_cell(critical, node_of(critical), "uninitialized")
        pdns = raw_cell("pdns-switch__committed__after-write__paired-secondary__peer-reachable")
        bootstrap.validate_pdns_switch_cell(pdns, "debian13", "uninitialized")
        with self.assertRaises(bootstrap.BootstrapError):
            bootstrap.validate_pdns_switch_cell(pdns, "debian13", "managed-bind")
        with self.assertRaises(bootstrap.BootstrapError):
            bootstrap.validate_pdns_switch_cell(pdns, "arch", "uninitialized")

    def test_controller_refuses_unadmitted_secondaries_before_any_artifact(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            for cell_id, text in (
                ("bind__intent__after-write__paired-secondary__peer-unreachable", "peer-unreachable"),
                ("bind__committed__after-write__paired-secondary__peer-reachable", "no pass definition"),
            ):
                with self.subTest(cell_id=cell_id), self.assertRaisesRegex(
                    run_cell.ControllerError, text
                ):
                    run_cell.refuse_unadmitted_paired_secondary(settings_for(root, spec(cell_id)))
            admitted = spec("pdns-switch__rolled-back__before-write__paired-secondary__peer-reachable")
            run_cell.refuse_unadmitted_paired_secondary(settings_for(root, admitted))
            with self.assertRaisesRegex(run_cell.ControllerError, "rpc-retry flow"):
                run_cell.refuse_unadmitted_paired_secondary(
                    settings_for(root, admitted, expect_agent_startup_rollback=True)
                )
            # The reconfiguration driver is untouched by this admission.
            run_cell.refuse_unadmitted_paired_secondary(settings_for(
                root, spec("pdns-secondary-reconfigure__intent__after-write__paired-secondary__peer-reachable")
            ))


class ScenarioTest(unittest.TestCase):
    def test_secondary_scenarios_carry_the_panel_mapping_and_no_zones(self) -> None:
        for driver, node, local, peer in (
            ("bind", "arch", "192.0.2.11", "192.0.2.10"),
            ("bind", "debian13", "192.0.2.10", "192.0.2.11"),
            ("pdns-switch", "debian13", "192.0.2.10", "192.0.2.11"),
        ):
            with self.subTest(driver=driver, node=node):
                value = (
                    bootstrap.bind_scenario("uninitialized", role="paired-secondary", node=node)
                    if driver == "bind"
                    else bootstrap.pdns_switch_scenario(
                        role="paired-secondary", source_fixture="uninitialized"
                    )
                )
                self.assertEqual(
                    {key: value[key] for key in (
                        "pair_role", "local_ip", "local_ns", "peer_ip", "peer_ns", "zones",
                        "source_epoch", "target_epoch", "source_revision", "mode",
                    )},
                    {"pair_role": "secondary", "local_ip": local, "local_ns": "ns2.s1-kill.test",
                     "peer_ip": peer, "peer_ns": "ns1.s1-kill.test", "zones": [],
                     "source_epoch": 0, "target_epoch": 1, "source_revision": 0, "mode": "switch"},
                )
        with self.assertRaises(bootstrap.BootstrapError):
            bootstrap.paired_secondary_scenario("pdns-switch", "arch")

    def test_guest_secondary_check_matches_the_python_renderer(self) -> None:
        program = heredoc("PYSECONDARY")
        with tempfile.TemporaryDirectory() as temporary:
            for driver, node, scenario_node, expected in (
                ("bind", "arch", "arch", 0),
                ("bind", "debian13", "debian13", 0),
                ("pdns-switch", "debian13", "debian13", 0),
                ("bind", "debian13", "arch", 1),
                ("pdns-switch", "arch", "debian13", 1),
            ):
                with self.subTest(driver=driver, node=node, scenario_node=scenario_node):
                    value = bootstrap.paired_secondary_scenario(
                        driver, scenario_node if driver == "bind" else "debian13"
                    )
                    path = Path(temporary) / f"{driver}-{node}-{scenario_node}.json"
                    path.write_bytes(bootstrap.json_bytes(value))
                    done = subprocess.run(
                        [sys.executable, "-c", program, str(path)], check=False,
                        capture_output=True, text=True,
                        env={**os.environ, "SECONDARY_DRIVER": driver, "SECONDARY_NODE": node},
                    )
                    self.assertEqual(done.returncode, expected, done.stderr)
            zoned = bootstrap.paired_secondary_scenario("bind", "debian13")
            zoned["zones"] = [bootstrap.zone_snapshot()]
            path = Path(temporary) / "zoned.json"
            path.write_bytes(bootstrap.json_bytes(zoned))
            done = subprocess.run(
                [sys.executable, "-c", program, str(path)], check=False, capture_output=True,
                env={**os.environ, "SECONDARY_DRIVER": "bind", "SECONDARY_NODE": "debian13"},
            )
            self.assertEqual(done.returncode, 1)

    def test_guest_fresh_pdns_check_accepts_the_secondary_role(self) -> None:
        program = heredoc("PYFRESHPDNS")
        with tempfile.TemporaryDirectory() as temporary:
            secondary = Path(temporary) / "secondary.json"
            secondary.write_bytes(bootstrap.json_bytes(bootstrap.pdns_switch_scenario(
                role="paired-secondary", source_fixture="uninitialized")))
            standalone = Path(temporary) / "standalone.json"
            standalone.write_bytes(bootstrap.json_bytes(bootstrap.pdns_switch_scenario(
                source_fixture="uninitialized")))
            for role, path, expected in (
                ("secondary", secondary, 0), ("secondary", standalone, 1),
                ("standalone", secondary, 1), ("primary", secondary, 1),
            ):
                with self.subTest(role=role, path=path.name):
                    done = subprocess.run(
                        [sys.executable, "-c", program, str(path)], check=False,
                        capture_output=True, env={**os.environ, "FRESH_PDNS_ROLE": role},
                    )
                    self.assertEqual(done.returncode, expected, done.stderr)

    def test_controller_accepts_only_the_exact_fresh_secondary_scenario(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            for cell_id, value in (
                ("bind__target-verified__before-write__paired-secondary__peer-reachable",
                 bootstrap.bind_scenario("uninitialized", role="paired-secondary", node="arch")),
                ("pdns-switch__source-stopped__after-write__paired-secondary__peer-reachable",
                 bootstrap.pdns_switch_scenario(role="paired-secondary",
                                                source_fixture="uninitialized")),
            ):
                selected = spec(cell_id)
                path = Path(temporary) / "scenario.json"
                write_private_json(path, value)
                accepted, _ = run_cell.validate_source_scenario(str(path), selected)
                self.assertEqual(accepted["zones"], [])
                run_cell.expected_journal_phase(selected)
                for key, changed in (("zones", [bootstrap.zone_snapshot()]),
                                     ("local_ns", "ns1.s1-kill.test")):
                    with self.subTest(cell_id=cell_id, key=key):
                        write_private_json(path, {**value, key: changed})
                        with self.assertRaises(run_cell.ControllerError):
                            run_cell.validate_source_scenario(str(path), selected)
                path.unlink()


class DNSContentTest(unittest.TestCase):
    def test_rrset_parser_reads_soa_serials_and_addresses(self) -> None:
        soa = dns_reply(7, "s1-kill.test", 6, [soa_rdata(2026083101)])
        self.assertEqual(run_cell.parse_dns_rrset(soa, 7, "SOA", "udp"), [2026083101])
        a = dns_reply(9, "www.s1-kill.test", 1, [ipaddress.IPv4Address("192.0.2.11").packed])
        self.assertEqual(run_cell.parse_dns_rrset(a, 9, "A", "tcp"), ["192.0.2.11"])
        for raw, text in (
            (dns_reply(7, "s1-kill.test", 6, [soa_rdata(1)], flags=0x8000), "not authoritative"),
            (dns_reply(8, "s1-kill.test", 6, [soa_rdata(1)]), "another transaction"),
            (dns_reply(7, "s1-kill.test", 6, [soa_rdata(1)])[:-6], "truncated"),
        ):
            with self.subTest(text=text), self.assertRaisesRegex(run_cell.ControllerError, text):
                run_cell.parse_dns_rrset(raw, 7, "SOA", "udp")

    def serving(self, secondary: dict | Exception, primary: dict | Exception) -> dict:
        def query(address, _port, name, qtype, _timeout):
            source = primary if address == "192.0.2.11" else secondary
            if isinstance(source, Exception):
                raise source
            return source[(name, qtype)]

        settings = mock.Mock(dns_address="10.0.2.15", dns_port=53, dns_timeout=1)
        with mock.patch.object(run_cell, "query_dns_rrset", side_effect=query):
            return run_cell.check_secondary_serving(settings, "192.0.2.11")

    @staticmethod
    def answers(serial: int = 2026083101, address: str = "192.0.2.11") -> dict:
        return {
            ("s1-kill.test", "SOA"): {"udp": [serial], "tcp": [serial]},
            ("www.s1-kill.test", "A"): {"udp": [address], "tcp": [address]},
        }

    def test_secondary_must_answer_with_the_primarys_serial_and_address(self) -> None:
        report = self.serving(self.answers(), self.answers())
        self.assertEqual((report["failures"], report["unknown"]), ([], []))
        report = self.serving(self.answers(serial=1), self.answers())
        self.assertTrue(any("not the primary's" in item for item in report["failures"]))
        report = self.serving(self.answers(address="192.0.2.10"), self.answers())
        self.assertTrue(any("not the primary's address" in item for item in report["failures"]))
        report = self.serving(self.answers(), OSError("peer down"))
        self.assertEqual(report["failures"], [])
        self.assertTrue(report["unknown"])
        report = self.serving(run_cell.ControllerError("not authoritative"), self.answers())
        self.assertIn("does not answer", report["failures"][0])

    def pdns_database(self, root: str, *, account: str = "celikpanel-peer-catalog-v1",
                      member_type: str = "SLAVE", soa: bool = True) -> str:
        path = os.path.join(root, "pdns.sqlite3")
        connection = sqlite3.connect(path)
        connection.executescript(
            "CREATE TABLE domains(id INTEGER PRIMARY KEY, name TEXT, master TEXT, "
            "last_check INTEGER, type TEXT, notified_serial INTEGER, account TEXT, "
            "options TEXT, catalog TEXT);"
            "CREATE TABLE records(id INTEGER PRIMARY KEY, domain_id INTEGER, name TEXT, "
            "type TEXT, content TEXT, ttl INTEGER, prio INTEGER, disabled BOOLEAN, "
            "ordername TEXT, auth BOOLEAN);"
        )
        catalog = run_cell.peer_catalog_name("192.0.2.11")
        connection.execute(
            "INSERT INTO domains(name,type,master,account) VALUES(?, 'CONSUMER', ?, ?)",
            (catalog, "192.0.2.11", account),
        )
        connection.execute(
            "INSERT INTO domains(name,type,master,catalog) VALUES('s1-kill.test', ?, ?, ?)",
            (member_type, "192.0.2.11", catalog),
        )
        if soa:
            connection.execute(
                "INSERT INTO records(domain_id,name,type,content) SELECT id, name, 'SOA', 'x' "
                "FROM domains WHERE name='s1-kill.test'"
            )
        connection.commit()
        connection.close()
        return path

    def test_pdns_secondary_needs_the_consumer_row_and_a_loaded_member(self) -> None:
        for kwargs, expected in (
            ({}, []),
            ({"member_type": "SECONDARY"}, []),
            ({"account": "someone-else"}, ["CONSUMER"]),
            ({"member_type": "MASTER"}, ["exactly one secondary zone"]),
            ({"soa": False}, ["not loaded"]),
        ):
            with self.subTest(kwargs=kwargs), tempfile.TemporaryDirectory() as root:
                path = self.pdns_database(root, **kwargs)
                with mock.patch.object(run_cell, "PDNS_DATABASE_PATH", path):
                    report = run_cell.check_pdns_secondary_rows("192.0.2.11")
                self.assertEqual(report["unknown"], [])
                self.assertEqual(len(report["failures"]), len(expected))
                for text, failure in zip(expected, report["failures"]):
                    self.assertIn(text, failure)
        with mock.patch.object(run_cell, "PDNS_DATABASE_PATH", "/nonexistent/pdns.sqlite3"):
            self.assertTrue(run_cell.check_pdns_secondary_rows("192.0.2.11")["unknown"])

    def test_preflight_requires_the_peer_catalog_and_no_pdns_database(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            settings = settings_for(
                root, spec("pdns-switch__intent__after-write__paired-secondary__peer-reachable")
            )
            catalog = run_cell.peer_catalog_name("192.0.2.11")
            self.assertEqual(catalog, "catalog-c000020b.celikpanel.invalid")
            with mock.patch.object(run_cell, "PDNS_DATABASE_PATH", root + "/absent.sqlite3"), \
                    mock.patch.object(run_cell, "query_dns_rrset",
                                      return_value={"udp": [1], "tcp": [1]}) as query:
                report = run_cell.prove_paired_secondary_preflight(settings, "192.0.2.11")
            query.assert_called_once_with("192.0.2.11", 53, catalog, "SOA", 1)
            self.assertTrue(report["pdns_database_absent"])
            Path(root, "present.sqlite3").write_text("x")
            with mock.patch.object(run_cell, "PDNS_DATABASE_PATH", root + "/present.sqlite3"), \
                    mock.patch.object(run_cell, "query_dns_rrset",
                                      return_value={"udp": [1], "tcp": [1]}), \
                    self.assertRaisesRegex(run_cell.ControllerError, "must be absent"):
                run_cell.prove_paired_secondary_preflight(settings, "192.0.2.11")
            with mock.patch.object(run_cell, "query_dns_rrset", side_effect=OSError("refused")), \
                    self.assertRaisesRegex(run_cell.ControllerError, "native_primary_peer.py"):
                run_cell.prove_paired_secondary_preflight(settings, "192.0.2.11")


class PassDefinitionTest(unittest.TestCase):
    def judge(self, selected: object, result: dict, **patches: object) -> dict:
        verification: list[str] = []
        patches = patches or {"PDNS_DATABASE_PATH": run_cell.PDNS_DATABASE_PATH}
        with tempfile.TemporaryDirectory() as root, mock.patch.multiple(run_cell, **patches):
            run_cell.judge_fixture_pass_definition(
                settings_for(root, selected), result, {}, "192.0.2.11", verification
            )
        result["_verification"] = verification
        return result

    @staticmethod
    def passing(**extra: object) -> dict:
        return {"status": "passed", "recovery_outcome": {"classification": "target_converged"},
                **extra}

    def test_paired_secondary_pass_needs_convergence_content_and_rows(self) -> None:
        clean = {"failures": [], "unknown": []}
        selected = spec("pdns-switch__target-started__after-write__paired-secondary__peer-reachable")
        result = self.judge(selected, self.passing(),
                            check_secondary_serving=mock.Mock(return_value=clean),
                            check_pdns_secondary_rows=mock.Mock(return_value=clean))
        self.assertEqual(result["status"], "passed")
        self.assertEqual(result["fixture_pass_definition"]["definition"], "paired-secondary")
        result = self.judge(selected, self.passing(
            recovery_outcome={"classification": "rolled_back_source_serving"}),
            check_secondary_serving=mock.Mock(return_value=clean),
            check_pdns_secondary_rows=mock.Mock(return_value=clean))
        self.assertEqual(result["status"], "failed")
        result = self.judge(selected, self.passing(),
                            check_secondary_serving=mock.Mock(return_value=clean),
                            check_pdns_secondary_rows=mock.Mock(
                                return_value={"failures": [], "unknown": ["db locked"]}))
        self.assertEqual(result["status"], "unverified")
        bind = spec("bind__intent__after-write__paired-secondary__peer-reachable")
        rows = mock.Mock()
        result = self.judge(bind, self.passing(),
                            check_secondary_serving=mock.Mock(
                                return_value={"failures": ["wrong serial"], "unknown": []}),
                            check_pdns_secondary_rows=rows)
        self.assertEqual(result["status"], "failed")
        rows.assert_not_called()

    def test_other_cells_are_untouched(self) -> None:
        result = self.judge(spec(TAKEOVER_CELL), self.passing(
            source_proof={"source_fixture": "uninitialized"}))
        self.assertNotIn("fixture_pass_definition", result)

    def test_takeover_and_reinstall_pass_definitions(self) -> None:
        clean = {"failures": [], "unknown": []}
        for fixture_name in ("unmanaged-bind-stopped", "managed-bind-absent"):
            with self.subTest(fixture=fixture_name):
                owner_files = mock.Mock(return_value=clean)
                result = self.judge(spec(TAKEOVER_CELL), self.passing(
                    source_proof={"source_fixture": fixture_name},
                    provenance_boundary=clean),
                    check_bind_authority=mock.Mock(return_value=clean),
                    check_takeover_owner_files=owner_files)
                self.assertEqual(result["status"], "passed")
                self.assertEqual(owner_files.called, fixture_name == "unmanaged-bind-stopped")
                result = self.judge(spec(TAKEOVER_CELL), self.passing(
                    source_proof={"source_fixture": fixture_name},
                    provenance_boundary={"failures": ["not adopted"], "unknown": []}),
                    check_bind_authority=mock.Mock(return_value=clean),
                    check_takeover_owner_files=mock.Mock(return_value=clean))
                self.assertEqual(result["status"], "failed")
                result = self.judge(spec(TAKEOVER_CELL), self.passing(
                    source_proof={"source_fixture": fixture_name}),
                    check_bind_authority=mock.Mock(return_value=clean),
                    check_takeover_owner_files=mock.Mock(return_value=clean))
                self.assertEqual(result["status"], "unverified")

    def receipt(self, root: str, value: dict) -> None:
        path = Path(root, "dns-engine-install-ownership-bind.json")
        path.write_text(json.dumps(value, separators=(",", ":")) + "\n", encoding="utf-8")
        os.chmod(path, 0o600)

    def test_boundary_receipt_tells_takeover_and_reinstall_apart(self) -> None:
        base = {"schema": "celikpanel-dns-engine-install-ownership/v1", "engine": "bind",
                "package_manager": "apt", "packages": ["bind9"],
                "manifest_qualifier": "q", "mutation_request_id": "1" * 32,
                "mutation_owner_id": "2" * 32}
        cases = (
            ("unmanaged-bind-stopped", {"missing_before": [], "adopted_present": True}, 0),
            ("unmanaged-bind-stopped", {"missing_before": ["bind9"]}, 1),
            ("managed-bind-absent", {"missing_before": ["bind9"]}, 0),
            ("managed-bind-absent", {"missing_before": [], "adopted_present": True}, 1),
        )
        for fixture_name, fields, failures in cases:
            with self.subTest(fixture=fixture_name, fields=fields), \
                    tempfile.TemporaryDirectory() as root:
                self.receipt(root, {**base, **fields})
                report = run_cell.observe_provenance_boundary(
                    settings_for(root, spec(TAKEOVER_CELL)), fixture_name
                )
                self.assertEqual(len(report["failures"]), failures, report["failures"])
        with tempfile.TemporaryDirectory() as root:
            report = run_cell.observe_provenance_boundary(
                settings_for(root, spec(TAKEOVER_CELL)), "unmanaged-bind-stopped"
            )
            self.assertIn("no BIND install-ownership receipt", report["failures"][0])
            self.receipt(root, {**base, "missing_before": [], "adopted_present": True,
                                "mutation_request_id": "3" * 32})
            report = run_cell.observe_provenance_boundary(
                settings_for(root, spec(TAKEOVER_CELL)), "unmanaged-bind-stopped"
            )
            self.assertIn("another request", report["failures"][0])

    def test_takeover_owner_files_judge_only_what_the_takeover_does_not_rewrite(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            main = Path(root, "named.conf")
            main.write_text("main\n")
            evidence = run_cell.hash_owner_file(str(main))
            files = {str(main): evidence}
            for path in run_cell.TAKEOVER_REWRITTEN_FILES:
                files[path] = dict(evidence, sha256="0" * 64)
            proof = {"source_preinstall_proof": {"owner_files": files}}
            original = run_cell.hash_owner_file

            def hashed(path: str) -> dict:
                if path in run_cell.TAKEOVER_REWRITTEN_FILES:
                    return dict(evidence, sha256="f" * 64)
                return original(path)

            with mock.patch.object(run_cell, "hash_owner_file", side_effect=hashed):
                report = run_cell.check_takeover_owner_files(proof)
                self.assertEqual(report["failures"], [])
                self.assertEqual(sorted(report["rewritten_by_documented_takeover"]),
                                 sorted(run_cell.TAKEOVER_REWRITTEN_FILES))
                main.write_text("owner edit\n")
                report = run_cell.check_takeover_owner_files(proof)
                self.assertIn("changed", report["failures"][0])

    def test_bind_authority_must_be_bind_alone(self) -> None:
        for engines, failures, unknown in (
            ({"tcp": ["bind"], "udp": ["bind"]}, 0, 0),
            ({"tcp": ["bind"], "udp": ["bind", "other"]}, 1, 0),
            (None, 0, 1),
        ):
            with self.subTest(engines=engines), mock.patch.object(
                run_cell, "observe_serving_authority",
                return_value={"authority": {"engines": engines}, "unknown": []},
            ):
                report = run_cell.check_bind_authority(mock.Mock(), {})
                self.assertEqual((len(report["failures"]), len(report["unknown"])),
                                 (failures, unknown))

    def test_run_cell_wires_the_new_judgements_in_order(self) -> None:
        source = MODULE_PATH.read_text(encoding="utf-8")
        flow = source.split("def run_cell(settings: Settings) -> int:", 1)[1]
        self.assertLess(flow.index("refuse_unadmitted_paired_secondary(settings)"),
                        flow.index("transcript = Transcript("))
        self.assertLess(flow.index("prove_paired_secondary_preflight("),
                        flow.index("tagged = start_tagged_agent("))
        self.assertLess(flow.index("atomic_write_new_json(settings.proof_path, proof)"),
                        flow.index('result["provenance_boundary"] = observe_provenance_boundary('))
        self.assertLess(flow.index('result["provenance_boundary"] = observe_provenance_boundary('),
                        flow.index('"agent-restart",'))
        self.assertLess(flow.index("judge_fixture_pass_definition("),
                        flow.index("maybe_request_reboot_after_recovery("))


class RebootWithManagementDisabledTest(unittest.TestCase):
    def test_settings_gate_the_disable_flag_and_admit_secondary_reboots(self) -> None:
        secondary = spec("bind__target-verified__after-write__paired-secondary__peer-reachable")
        with tempfile.TemporaryDirectory() as root:
            base = settings_for(root, secondary, reboot_after_recovery=True, reboot_dir=root,
                                disable_management_before_reboot=True)
            run_cell.validate_reboot_settings(base)
            run_cell.validate_reboot_settings(replace(base, cell=spec(TAKEOVER_CELL)))
            for changed, text in (
                ({"reboot_after_recovery": False, "reboot_dir": None}, "requires --reboot-after-recovery"),
                ({"owner_inverse_after_restart": True}, "not the owner-inverse flow"),
                ({"cell": spec("bind__intent__after-write__paired-primary__peer-reachable")},
                 "standalone cells and the admitted"),
                ({"cell": spec("bind__intent__after-write__paired-secondary__peer-unreachable")},
                 "standalone cells and the admitted"),
            ):
                with self.subTest(changed=changed), self.assertRaisesRegex(
                    run_cell.ControllerError, text
                ):
                    run_cell.validate_reboot_settings(replace(base, **changed))
            parser = run_cell.build_argument_parser()
            self.assertIn("--disable-management-before-reboot", parser._option_string_actions)

    def test_resume_tolerates_the_missing_runtime_directory_only_when_disabled(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            selected = spec("bind__target-verified__after-write__paired-secondary__peer-reachable")
            gone = root + "/run-celikpanel"
            settings = settings_for(
                root, selected, reboot_after_recovery=True, reboot_dir=root,
                resume_after_reboot=True, disable_management_before_reboot=True,
                mutation_lock=gone + "/service-mutation.lock", agent_socket=gone + "/agent.sock",
            )
            source = MODULE_PATH.read_text(encoding="utf-8")
            body = source.split("def validate_settings(settings: Settings)", 1)[1][:1600]
            self.assertIn(
                "if not (settings.resume_after_reboot and settings.disable_management_before_reboot):",
                body,
            )
            self.assertTrue(settings.disable_management_before_reboot)

    def test_disable_is_proven_before_the_reboot_request(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            settings = settings_for(root, spec(TAKEOVER_CELL), reboot_after_recovery=True,
                                    reboot_dir=root, disable_management_before_reboot=True)
            common = dict(
                observe_serving_authority=mock.Mock(return_value={"authority": {}}),
                observe_management_units=mock.Mock(return_value={"units": {}, "unknown": []}),
                read_guest_boot_identity=mock.Mock(return_value={"boot_id": "b"}),
            )
            ok = {"failures": [], "unknown": []}
            with mock.patch.multiple(run_cell, disable_management_units=mock.Mock(return_value=ok),
                                     **common):
                with self.assertRaises(run_cell.RebootRequested) as requested:
                    run_cell.maybe_request_reboot_after_recovery(
                        settings, {"status": "passed"}, {}, {"flow": "rpc-retry", "peer_ip": ""}
                    )
            self.assertTrue(requested.exception.state["management_disabled"])
            result = {"status": "passed"}
            with mock.patch.multiple(
                run_cell, disable_management_units=mock.Mock(
                    return_value={"failures": ["celikpanel-agent.service is not stopped"],
                                  "unknown": []}), **common):
                run_cell.maybe_request_reboot_after_recovery(
                    settings, result, {}, {"flow": "rpc-retry", "peer_ip": ""}
                )
            self.assertEqual(result["status"], "unverified")
            self.assertFalse(result["reboot_after_recovery"]["run"])

    def test_disable_management_units_requires_stopped_and_disabled(self) -> None:
        settings = mock.Mock(command_timeout=5, endpoint_timeout=5)
        stopped = {"unknown": [], "units": {
            unit: {"ActiveState": "inactive", "UnitFileState": "disabled"}
            for unit in run_cell.MANAGEMENT_UNITS}}
        with mock.patch.object(run_cell.subprocess, "run",
                               return_value=subprocess.CompletedProcess([], 0, b"", b"")) as run, \
                mock.patch.object(run_cell, "observe_management_units", return_value=stopped):
            report = run_cell.disable_management_units(settings, {})
        self.assertEqual(report["failures"], [])
        self.assertEqual(run.call_args.args[0][:3], ["/usr/bin/systemctl", "disable", "--now"])
        still = {"unknown": [], "units": {unit: {"ActiveState": "active", "UnitFileState": "enabled"}
                                          for unit in run_cell.MANAGEMENT_UNITS}}
        with mock.patch.object(run_cell.subprocess, "run",
                               return_value=subprocess.CompletedProcess([], 0, b"", b"")), \
                mock.patch.object(run_cell, "observe_management_units", return_value=still):
            self.assertEqual(len(run_cell.disable_management_units(settings, {})["failures"]), 2)

    @staticmethod
    def authority(engines: dict, *, answers: int = 1) -> dict:
        return {"unknown": [], "authority": {"engines": engines, "answered": True,
                                             "answer_counts": {"udp": answers, "tcp": answers}},
                "state": {"semantic": {"engine": "bind"}},
                "evidence": {"journal": {"exists": False}}}

    def verify(self, selected: object, after: dict, *, units: tuple[str, str] = ("inactive", "disabled"),
               serving: dict | None = None) -> dict:
        before = self.authority({"tcp": ["bind"], "udp": ["bind"]})
        state = {"flow": "rpc-retry", "authority_before": before, "management_disabled": True,
                 "peer_ip": "192.0.2.11" if selected.role != "standalone" else ""}
        settings = mock.Mock(cell=selected)
        result: dict = {"status": "passed"}
        ok = {"ok": True}
        window = mock.Mock(return_value=({"samples": [{"dns": ok}] * 31, "dns_only": True}, [], []))
        with mock.patch.multiple(
            run_cell,
            observe_management_units=mock.Mock(return_value={"unknown": [], "units": {
                unit: {"ActiveState": units[0], "UnitFileState": units[1]}
                for unit in run_cell.MANAGEMENT_UNITS}}),
            observe_serving_authority=mock.Mock(return_value=after),
            run_stability_window=window,
            check_secondary_serving=mock.Mock(return_value=serving or {"failures": [], "unknown": []}),
            check_pdns_secondary_rows=mock.Mock(return_value={"failures": [], "unknown": []}),
            wait_for_unix_socket=mock.Mock(side_effect=AssertionError("no Agent expected")),
        ):
            run_cell.verify_after_recovery_reboot(
                settings, state, result=result, transcript=mock.Mock(), ordinary={},
                owner_environment={}, controller_identity={"effective_gid": 1},
                boot={"boot_id_after": "b"}, safety_failures=[], verification_failures=[],
            )
        self.assertEqual(window.call_args.kwargs, {"dns_only": True})
        return result

    def test_after_reboot_the_dns_daemon_must_serve_alone(self) -> None:
        secondary = spec("bind__target-verified__after-write__paired-secondary__peer-reachable")
        same = self.authority({"tcp": ["bind"], "udp": ["bind"]})
        result = self.verify(secondary, same)
        self.assertEqual(result["status"], "passed")
        self.assertEqual(result["reboot_after_recovery"]["safety_assertions"]["panel_started"],
                         "not-applicable: management disabled")
        for kwargs, after, text in (
            ({"units": ("active", "enabled")}, same, "came back after the reboot"),
            ({}, self.authority({"tcp": ["bind", "other"], "udp": ["bind"]}), "authority changed"),
            ({"serving": {"failures": ["wrong serial"], "unknown": []}}, same, "wrong serial"),
        ):
            with self.subTest(text=text):
                result = self.verify(secondary, after, **kwargs)
                self.assertEqual(result["status"], "failed")
                self.assertIn(text, " ".join(result["reboot_after_recovery"]["failures"]))

    def test_stability_window_can_judge_dns_only(self) -> None:
        settings = mock.Mock(
            cell=spec(TAKEOVER_CELL), stability_seconds=0.001, stability_interval=0.001,
            dns_address="10.0.2.15", dns_port=53, dns_name="www.s1-kill.test",
            dns_type="A", dns_timeout=1,
        )
        with mock.patch.object(run_cell, "query_authoritative_dns", return_value={}), \
                mock.patch.object(run_cell, "assert_unix_socket_stable",
                                  side_effect=AssertionError("Agent must not be sampled")):
            report, failures, _ = run_cell.run_stability_window(
                settings, None, mock.Mock(), "", dns_only=True
            )
        self.assertEqual(failures, [])
        self.assertTrue(report["dns_only"])
        self.assertEqual(report["samples"][0]["agent"]["judged"], False)
        with self.assertRaises(run_cell.ControllerError):
            run_cell.run_stability_window(settings, None, mock.Mock(), "")


class ProvenanceFixtureTest(unittest.TestCase):
    def test_fixtures_are_admitted_only_on_their_debian_cell(self) -> None:
        exact = raw_cell(TAKEOVER_CELL)
        for fixture_name in ("unmanaged-bind-stopped", "managed-bind-absent"):
            bootstrap.validate_bind_cell(exact, "debian13", fixture_name)
            for other in ("bind__target-started__after-write__standalone__peer-reachable",
                          "bind__intent__after-write__standalone__peer-reachable",
                          "bind__target-staged__before-write__standalone__peer-unreachable"):
                with self.subTest(fixture=fixture_name, other=other), \
                        self.assertRaises(bootstrap.BootstrapError):
                    bootstrap.validate_bind_cell(raw_cell(other), node_of(raw_cell(other)),
                                                 fixture_name)

    def test_scenarios_and_controller_contract(self) -> None:
        takeover = bootstrap.bind_scenario("unmanaged-bind-stopped")
        fresh = bootstrap.bind_scenario("uninitialized")
        self.assertEqual({k: v for k, v in takeover.items() if k != "source_fixture"},
                         {k: v for k, v in fresh.items() if k != "source_fixture"})
        reinstall = bootstrap.bind_scenario("managed-bind-absent")
        self.assertEqual((reinstall["mode"], reinstall["source_engine"], reinstall["source_epoch"],
                          reinstall["target_epoch"]), ("reinstall", "bind", 1, 1))
        self.assertEqual(reinstall["zones"], fresh["zones"])
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "scenario.json"
            for value in (takeover, reinstall):
                write_private_json(path, value)
                accepted, _ = run_cell.validate_source_scenario(str(path), spec(TAKEOVER_CELL))
                self.assertEqual(accepted["source_fixture"], value["source_fixture"])
            for value, text in (
                ({**reinstall, "target_epoch": 2}, "equal epochs"),
                ({**reinstall, "source_fixture": "managed-bind"}, "fixture is invalid"),
                ({**takeover, "mode": "reinstall"}, "reinstall belongs"),
            ):
                with self.subTest(text=text):
                    write_private_json(path, value)
                    with self.assertRaisesRegex(run_cell.ControllerError, text):
                        run_cell.validate_source_scenario(str(path), spec(TAKEOVER_CELL))
            write_private_json(path, takeover)
            with self.assertRaisesRegex(run_cell.ControllerError, "admitted only on"):
                run_cell.validate_source_scenario(
                    str(path), spec("bind__intent__after-write__standalone__peer-reachable")
                )

    def test_guest_scenario_checks_match_the_python_renderer(self) -> None:
        program = heredoc("PYPROVENANCE")
        setup = heredoc("PYREINSTALLSETUP")
        with tempfile.TemporaryDirectory() as temporary:
            files = {}
            for name in ("unmanaged-bind-stopped", "managed-bind-absent", "uninitialized"):
                files[name] = Path(temporary) / f"{name}.json"
                files[name].write_bytes(bootstrap.json_bytes(bootstrap.bind_scenario(name)))
            for fixture_name, path_name, expected in (
                ("unmanaged-bind-stopped", "unmanaged-bind-stopped", 0),
                ("managed-bind-absent", "managed-bind-absent", 0),
                ("managed-bind-absent", "unmanaged-bind-stopped", 1),
                ("unmanaged-bind-stopped", "uninitialized", 1),
            ):
                with self.subTest(fixture=fixture_name, scenario=path_name):
                    done = subprocess.run(
                        [sys.executable, "-c", program, str(files[path_name])], check=False,
                        capture_output=True, env={**os.environ, "PROVENANCE_FIXTURE": fixture_name},
                    )
                    self.assertEqual(done.returncode, expected, done.stderr)
            done = subprocess.run(
                [sys.executable, "-c", setup, str(files["managed-bind-absent"]),
                 str(files["uninitialized"])], check=False, capture_output=True,
            )
            self.assertEqual(done.returncode, 0, done.stderr)
            done = subprocess.run(
                [sys.executable, "-c", setup, str(files["managed-bind-absent"]),
                 str(files["managed-bind-absent"])], check=False, capture_output=True,
            )
            self.assertEqual(done.returncode, 1)

    def run_source_proof(self, fixture_name: str, serving: str, engine: str, epoch: str,
                         origin_hashes: tuple[str, str]) -> dict:
        body = SHELL.split("\nwrite_source_proof() {\n", 1)[1]
        program = body.split("<<'PY'\n", 1)[1].split("\nPY\n", 1)[0]
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "proof.json"
            environment = {
                **os.environ, "SOURCE_FIXTURE": fixture_name, "CELL_ID": TAKEOVER_CELL,
                "ADDRESS": "10.0.2.15", "AUTH_NAME": "www.s1-kill.test", "SOURCE_REVISION": "0",
                "STATE_SHA": "", "STATE_JSON": "null", "STATE_PATH": "", "SERVING": serving,
                "ENGINE": engine, "EPOCH": epoch, "MEASURED_SCENARIO_SHA": "a" * 64,
                "SETUP_SCENARIO_SHA": origin_hashes[0], "SETUP_IDENTITY_SHA": origin_hashes[1],
                "SOURCE_PREINSTALL_PATH": run_cell.SOURCE_PREINSTALL_BIND_PROOF_PATH,
                "SOURCE_PREINSTALL_SHA": "b" * 64, "SOURCE_ADOPTION_PATH": "absent",
                "SOURCE_ADOPTION_SHA": "absent", "EXTERNAL_PDNS_PREIMAGE_PATH": "absent",
                "EXTERNAL_PDNS_PREIMAGE_SHA": "absent", "SOURCE_NORMALIZATION_PATH": "absent",
                "SOURCE_NORMALIZATION_SHA": "absent",
            }
            subprocess.run([sys.executable, "-c", program, str(output)], check=True,
                           env=environment)
            return json.loads(output.read_text(encoding="utf-8"))

    def test_guest_source_proof_matches_the_controller_contract(self) -> None:
        for fixture_name, engine, epoch, hashes in (
            ("unmanaged-bind-stopped", "", "0", ("absent", "absent")),
            ("managed-bind-absent", "bind", "1", ("c" * 64, "d" * 64)),
        ):
            with self.subTest(fixture=fixture_name):
                proof = self.run_source_proof(fixture_name, "false", engine, epoch, hashes)
                self.assertEqual(set(proof), run_cell.SOURCE_PROOF_KEYS)
                self.assertFalse(proof["serving_before_tagged_agent"])
                self.assertEqual(proof["uninitialized_global_port53"], {
                    "udp_bindable": True, "tcp_bindable": True,
                    "authoritative_answer_observed": False})
                self.assertEqual(proof["engine"], run_cell.SOURCE_FIXTURE_ENGINES[fixture_name])
                run_cell.validate_source_setup_provenance(proof, fixture_name)
                with self.assertRaises(run_cell.ControllerError):
                    run_cell.validate_source_setup_provenance(proof, "uninitialized")

    @staticmethod
    def preinstall(fixture_name: str) -> dict:
        takeover = fixture_name == "unmanaged-bind-stopped"
        item = {"sha256": "e" * 64, "size": 1, "mode": "0644", "uid": 0, "gid": 0}
        return {
            "schema": run_cell.SOURCE_PREINSTALL_BIND_SCHEMA,
            "cell_id": TAKEOVER_CELL,
            "source_fixture": fixture_name,
            "scope": ("owner-installed-stopped-bind-for-takeover" if takeover
                      else "managed-bind-engine-removed-for-reinstall"),
            "package": ({"name": "bind9", "status": "install ok installed", "version": "1:9.20"}
                        if takeover else {"name": "bind9", "status": "absent", "version": ""}),
            "units": {
                "named.service": ({"load_state": "loaded", "active_state": "inactive",
                                   "unit_file_state": "disabled"} if takeover else
                                  {"load_state": "not-found", "active_state": "inactive",
                                   "unit_file_state": ""}),
                "bind9.service": {"load_state": "not-found", "active_state": "inactive",
                                  "unit_file_state": ""},
            },
            "dns_state": {"exists": False} if takeover else {"exists": True, "sha256": "1" * 64},
            "dns_ownership_bind": ({"exists": False} if takeover
                                   else {"exists": True, "sha256": "2" * 64}),
            "dns_install_ownership_bind": {"exists": False},
            "dns_journal_absent": True,
            "pdns_receipts_absent": True,
            "global_udp_tcp_53_bindable": True,
            "owner_files": ({path: item for path in (
                "/etc/bind/named.conf", "/etc/bind/named.conf.local",
                "/etc/bind/named.conf.options", "/etc/bind/rndc.key", "/etc/default/named",
            )} if takeover else {}),
            "product_rewrites_on_takeover": (list(run_cell.TAKEOVER_REWRITTEN_FILES)
                                             if takeover else []),
            "preparation": "fixture",
        }

    def test_bind_preinstall_document_contract(self) -> None:
        selected = spec(TAKEOVER_CELL)
        for fixture_name in ("unmanaged-bind-stopped", "managed-bind-absent"):
            value = self.preinstall(fixture_name)
            raw = bootstrap.json_bytes(value)
            run_cell.validate_source_bind_preinstall_document(value, raw, selected, fixture_name)
            other = ("managed-bind-absent" if fixture_name == "unmanaged-bind-stopped"
                     else "unmanaged-bind-stopped")
            for label, changed, fixture_used in (
                ("other fixture", value, other),
                ("enabled owner unit", {**value, "units": {**value["units"], "named.service": {
                    "load_state": "loaded", "active_state": "inactive",
                    "unit_file_state": "enabled"}}}, fixture_name),
                ("install receipt", {**value, "dns_install_ownership_bind": {"exists": True}},
                 fixture_name),
                ("package", {**value, "package": {"name": "bind9", "status":
                                                  "deinstall ok config-files", "version": ""}},
                 fixture_name),
            ):
                with self.subTest(fixture=fixture_name, label=label), \
                        self.assertRaises(run_cell.ControllerError):
                    run_cell.validate_source_bind_preinstall_document(
                        changed, bootstrap.json_bytes(changed), selected, fixture_used
                    )
            with self.assertRaises(run_cell.ControllerError):
                run_cell.validate_source_bind_preinstall_document(
                    value, raw, spec("bind__intent__after-write__standalone__peer-reachable"),
                    fixture_name,
                )

    def test_non_serving_sources_forbid_any_active_dns_unit(self) -> None:
        for fixture_name in ("unmanaged-bind-stopped", "managed-bind-absent"):
            run_cell.validate_source_unit_states(fixture_name, {
                "bind9.service": "inactive", "named.service": "inactive",
                "pdns.service": "inactive"})
            with self.assertRaises(run_cell.ControllerError):
                run_cell.validate_source_unit_states(fixture_name, {
                    "bind9.service": "inactive", "named.service": "active",
                    "pdns.service": "inactive"})

    def test_dry_run_preparation_uploads_the_setup_only_for_the_reinstall(self) -> None:
        raw = raw_cell(TAKEOVER_CELL)
        for fixture_name, uploaded in (
            ("unmanaged-bind-stopped", "scenario.json"),
            ("managed-bind-absent", "scenario.json source-setup-bind.json"),
        ):
            args = mock.Mock(action="prepare-bind", cell_id=raw["id"], node="debian13",
                             source_fixture=fixture_name, identity_file=Path("/tmp/k"),
                             execute=False, peer_engine=None)
            with mock.patch.object(bootstrap, "load_plan", return_value=({}, raw, {})), \
                    mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/k")), \
                    mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]), \
                    mock.patch.object(bootstrap, "scp_base", return_value=["scp"]), \
                    mock.patch.object(bootstrap, "remote_destination",
                                      side_effect=lambda _n, p: "guest:" + p), \
                    mock.patch.object(bootstrap.subprocess, "run") as run, \
                    mock.patch("sys.stdout", new_callable=io.StringIO) as output:
                bootstrap.prepare(args)
                run.assert_not_called()
            summary = json.loads(output.getvalue().splitlines()[-1])
            self.assertEqual(summary["uploaded"], uploaded)
            self.assertEqual(summary["source_preinstall_proof"],
                             run_cell.SOURCE_PREINSTALL_BIND_PROOF_PATH)
            self.assertIn(f"prepare-bind {raw['id']} debian13 target-staged {fixture_name} ",
                          json.loads(output.getvalue().splitlines()[-2])[-1])

    def test_recovery_probe_maps_the_reinstall_tenure(self) -> None:
        reinstall = bootstrap.bind_scenario("managed-bind-absent")
        probe.validate_scenario(reinstall)
        for changed in ({**reinstall, "source_fixture": "managed-bind"},
                        {**reinstall, "target_epoch": 2},
                        {**bootstrap.bind_scenario("unmanaged-bind-stopped"), "mode": "reinstall"}):
            with self.assertRaises(probe.ProbeObservationError):
                probe.validate_scenario(changed)
        probe.validate_scenario(bootstrap.bind_scenario("unmanaged-bind-stopped"))
        receipt = {"manifest_qualifier": "dns-engine-switch/v1:sha256:" + "a" * 64,
                   "request_id": "1" * 32, "owner_id": "2" * 32}
        state = {"schema": probe.STATE_SCHEMA, "mode": "switch", "engine": "bind",
                 "engine_epoch": 1, "generation": "f" * 64, "source_revision": 0,
                 "manifest_qualifier": receipt["manifest_qualifier"],
                 "mutation_request_id": receipt["request_id"],
                 "mutation_owner_id": receipt["owner_id"]}
        probe.validate_state(state, reinstall, receipt)
        with self.assertRaises(probe.ProbeObservationError):
            probe.validate_state({**state, "mode": "reinstall"}, reinstall, receipt)


class HostOrchestrationTest(unittest.TestCase):
    SECONDARY = "pdns-switch__target-started__after-write__paired-secondary__peer-reachable"

    def args(self, raw: dict, **overrides: object) -> mock.Mock:
        values = dict(
            action="prepare-pdns-switch", cell_id=raw["id"], node=node_of(raw),
            source_fixture="uninitialized", identity_file=Path("/tmp/k"), execute=False,
            authority_acceptance=False, peer_engine="pdns", work_root=Path("/tmp/root"),
            manifest=MANIFEST_PATH, stop_after_kill_for_independent_recovery=False,
            bind_rollback_after_target_started=False, owner_inverse_after_restart=False,
            expect_agent_startup_rollback=False, reboot_before_owner_command=False,
            reboot_after_recovery=False, disable_management_before_reboot=False,
            reboot_timeout=600,
        )
        values.update(overrides)
        return mock.Mock(**values)

    def test_prepare_runs_the_native_primary_before_the_guest(self) -> None:
        raw = raw_cell(self.SECONDARY)
        calls: list[str] = []
        for engine in ("bind", "pdns"):
            calls.clear()
            args = self.args(raw, peer_engine=engine)
            with mock.patch.object(bootstrap, "load_plan", return_value=({}, raw, {})), \
                    mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/k")), \
                    mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]), \
                    mock.patch.object(bootstrap, "scp_base", return_value=["scp"]), \
                    mock.patch.object(bootstrap, "remote_destination",
                                      side_effect=lambda _n, p: "guest:" + p), \
                    mock.patch.object(native_primary_peer, "prepare",
                                      side_effect=lambda a: calls.append("peer-prepare:" + a.engine)
                                      or {"action": "prepare"}), \
                    mock.patch.object(native_primary_peer, "observe",
                                      side_effect=lambda a: calls.append(
                                          f"peer-observe:{a.require_secondary_transfer}")
                                      or {"observation": None}), \
                    mock.patch.object(bootstrap, "run",
                                      side_effect=lambda c, execute: calls.append("guest")), \
                    mock.patch("sys.stdout", new_callable=io.StringIO) as output:
                bootstrap.prepare(args)
            self.assertEqual(calls, [f"peer-prepare:{engine}", "peer-observe:False",
                                     "guest", "guest", "guest"])
            summary = json.loads(output.getvalue().splitlines()[-1])
            self.assertEqual(summary["native_primary_peer_engine"], engine)
        for peer_engine, text in ((None, "needs --peer-engine"),):
            with self.subTest(peer_engine=peer_engine), \
                    mock.patch.object(bootstrap, "load_plan", return_value=({}, raw, {})), \
                    self.assertRaisesRegex(bootstrap.BootstrapError, text):
                bootstrap.prepare(self.args(raw, peer_engine=peer_engine))
        standalone = raw_cell(TAKEOVER_CELL)
        with mock.patch.object(bootstrap, "load_plan", return_value=({}, standalone, {})), \
                self.assertRaisesRegex(bootstrap.BootstrapError, "only to paired-secondary"):
            bootstrap.prepare(self.args(standalone, action="prepare-bind", peer_engine="bind",
                                        source_fixture="uninitialized", node="debian13"))

    def test_peer_verdict_is_pure_and_combines_with_the_guest_exit(self) -> None:
        observation = {"config_sha256": {"/etc/bind/named.conf": "1" * 64}, "catalog_serial": 1,
                       "catalog_members": ["s1-kill.test"], "member_soa": {},
                       "www_a": ["192.0.2.11"],
                       "transfers_to_secondary": {"catalog": True, "s1-kill.test": True}}
        before = dict(observation, transfers_to_secondary={"catalog": False, "s1-kill.test": False})
        self.assertEqual(bootstrap.judge_peer_observations(before, observation, True)["status"],
                         "passed")
        missing = dict(observation, transfers_to_secondary={"catalog": True, "s1-kill.test": False})
        verdict = bootstrap.judge_peer_observations(before, missing, False)
        self.assertEqual(verdict["status"], "failed")
        self.assertIn("s1-kill.test", verdict["failures"][0])
        changed = dict(observation, config_sha256={"/etc/bind/named.conf": "2" * 64})
        self.assertEqual(bootstrap.judge_peer_observations(before, changed, True)["status"],
                         "failed")
        self.assertEqual(bootstrap.judge_peer_observations(before, None, False)["status"],
                         "unverified")
        for guest, peer, combined in ((0, "passed", 0), (0, "failed", 1), (0, "unverified", 2),
                                      (2, "failed", 1), (1, "passed", 1), (64, "passed", 64),
                                      (2, "passed", 2)):
            self.assertEqual(bootstrap.combine_paired_secondary_exit(guest, peer), combined)

    def test_run_prepared_observes_the_peer_around_the_controller(self) -> None:
        raw = raw_cell(self.SECONDARY)
        observation = {"config_sha256": {"/etc/powerdns/pdns.conf": "1" * 64},
                       "catalog_serial": 1, "catalog_members": ["s1-kill.test"],
                       "member_soa": {}, "www_a": ["192.0.2.11"],
                       "transfers_to_secondary": {"catalog": True, "s1-kill.test": True}}
        written: dict[str, object] = {}
        plan = {"cell_directory": "/tmp/cell"}
        for guest_exit, expected in ((0, 0), (2, 2)):
            written.clear()
            args = self.args(raw, action="run-prepared", execute=True, reboot_after_recovery=True,
                             disable_management_before_reboot=True)
            with mock.patch.object(bootstrap, "load_plan", return_value=(plan, raw, {})), \
                    mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/k")), \
                    mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]), \
                    mock.patch.object(bootstrap, "read_peer_evidence",
                                      return_value={"engine": "pdns"}), \
                    mock.patch.object(bootstrap, "write_peer_evidence",
                                      side_effect=lambda _p, name, value, execute:
                                      written.__setitem__(name, value)), \
                    mock.patch.object(native_primary_peer, "observe",
                                      return_value={"observation": observation}), \
                    mock.patch.object(bootstrap.subprocess, "run",
                                      side_effect=[subprocess.CompletedProcess([], 3),
                                                   subprocess.CompletedProcess([], guest_exit)]) as run, \
                    mock.patch.object(bootstrap.fixture, "reboot_guest", return_value={}), \
                    mock.patch("sys.stdout", new_callable=io.StringIO):
                self.assertEqual(bootstrap.run_prepared(args), expected)
            self.assertEqual(sorted(written), ["peer-before-kill.json", "peer-verdict.json"])
            self.assertTrue(run.call_args_list[0].args[0][-1].endswith(
                "--reboot-after-recovery --disable-management-before-reboot"))
            self.assertEqual(written["peer-verdict.json"]["guest_controller_exit"], guest_exit)
        args = self.args(raw, action="run-prepared", execute=True, peer_engine="bind")
        with mock.patch.object(bootstrap, "load_plan", return_value=(plan, raw, {})), \
                mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/k")), \
                mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]), \
                mock.patch.object(bootstrap, "read_peer_evidence", return_value={"engine": "pdns"}), \
                mock.patch.object(bootstrap.subprocess, "run") as run, \
                self.assertRaisesRegex(bootstrap.BootstrapError, "prepared --peer-engine"):
            bootstrap.run_prepared(args)
        run.assert_not_called()

    def test_disable_flag_is_gated_on_the_host_and_in_the_guest_program(self) -> None:
        raw = raw_cell(self.SECONDARY)
        for overrides, text in (
            ({"disable_management_before_reboot": True}, "requires --reboot-after-recovery"),
            ({"reboot_before_owner_command": True}, "requires --owner-inverse"),
        ):
            with self.subTest(overrides=overrides), \
                    mock.patch.object(bootstrap, "load_plan", return_value=({}, raw, {})), \
                    self.assertRaisesRegex(bootstrap.BootstrapError, text):
                bootstrap.run_prepared(self.args(raw, action="run-prepared", **overrides))
        primary = raw_cell("bind__intent__after-write__paired-primary__peer-reachable")
        with mock.patch.object(bootstrap, "load_plan", return_value=({}, primary, {})), \
                self.assertRaisesRegex(bootstrap.BootstrapError, "admitted paired-secondary"):
            bootstrap.run_prepared(self.args(primary, action="run-prepared", node="arch",
                                             peer_engine=None, reboot_after_recovery=True))
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            prepared, captured = root / "controller-argv.json", root / "captured.json"
            executable = root / "run-cell.py"
            executable.write_text(
                "#!/usr/bin/env python3\nimport json, sys\n"
                f"open({str(captured)!r}, 'w').write(json.dumps(sys.argv))\n", encoding="utf-8")
            executable.chmod(0o700)
            code = bootstrap.RUN_PREPARED_CODE.replace(
                "/var/lib/celikpanel-dns-kill-matrix/controller-argv.json", str(prepared)
            ).replace("/opt/celikpanel/libexec/dns-kill-run-cell.py", str(executable))
            base = [str(executable), "--cell-id", self.SECONDARY, "--trigger-mode", "socket",
                    "--result", "/var/lib/x/results/c/result.json"]
            prepared.write_text(json.dumps(base), encoding="utf-8")
            prepared.chmod(0o600)
            after, disable = bootstrap.REBOOT_AFTER_RECOVERY_FLAG, bootstrap.DISABLE_MANAGEMENT_FLAG
            done = subprocess.run([sys.executable, "-c", code, self.SECONDARY, after, disable],
                                  check=False, capture_output=True)
            self.assertEqual(done.returncode, 0, done.stderr)
            self.assertEqual(json.loads(captured.read_text())[len(base):],
                             [after, disable, "--reboot-dir", "/var/lib/x/results/c"])
            captured.unlink()
            for flags in ((disable,), (disable, after)):
                done = subprocess.run([sys.executable, "-c", code, self.SECONDARY, *flags],
                                      check=False, capture_output=True)
                self.assertNotEqual(done.returncode, 0)
                self.assertFalse(captured.exists())


if __name__ == "__main__":
    unittest.main()
