#!/usr/bin/env python3
"""Offline tests: fresh paired PowerDNS PRIMARY (register row 6, journal V3)
through the public Agent RPC, the stopped-BIND takeover owner directives and
the reinstall statoverride capture.

Nothing here starts a guest, runs --execute or talks to a peer. The Agent,
systemd, sockets, SSH and the native peer are mocked; SQLite and files live in
temporary directories. Nothing here is native evidence.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from dataclasses import replace
from unittest import mock

import guest_bootstrap as bootstrap
import native_pdns_bind_peer as bind_peer

MODULE_PATH = Path(__file__).with_name("run_cell.py")
SPEC = importlib.util.spec_from_file_location("dns_kill_run_cell_fresh_primary", MODULE_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError(f"cannot import {MODULE_PATH}")
run_cell = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = run_cell
SPEC.loader.exec_module(run_cell)

MANIFEST = json.loads(Path(__file__).with_name("manifest.json").read_text(encoding="utf-8"))
SHELL = Path(__file__).with_name("guest_bootstrap.sh").read_text(encoding="utf-8")
REQUEST = "1" * 32
OWNER = "2" * 32
QUALIFIER = "dns-engine-switch/v1:sha256:" + "3" * 64
TAKEOVER_CELL = "bind__target-staged__after-write__standalone__peer-reachable"


def raw_cell(cell_id: str) -> dict:
    return next(item for item in MANIFEST["cells"] if item["id"] == cell_id)


def spec(cell_id: str) -> object:
    return run_cell.CellSpec.from_manifest(MANIFEST, cell_id)


def v3(phase: str, edge: str) -> object:
    return spec(f"pdns-switch__{phase}__{edge}__paired-primary__peer-reachable")


def settings(root: str, selected: object, **changes: object) -> object:
    trigger = ("/opt/t", "rpc-switch", "--scenario", root + "/scenario.json",
               "--identity-receipt", root + "/identity.json", "--timeout", "45m")
    value = run_cell.Settings(
        cell=selected, request_id=REQUEST, nonce="a" * 32,
        tagged_agent_command=("/opt/agent.kill",), trigger_mode="socket",
        trigger_command=trigger, recovery_command=(trigger[0], "rpc-retry", *trigger[2:]),
        source_proof_path=root + "/source-proof.json",
        agent_restart_command=("/bin/true",), panel_restart_command=("/bin/true",),
        recovery_probe_command=("/bin/true",), peer_partition_command=None,
        command_cwd=root, state_dir=root, mutation_lock=root + "/lock",
        agent_socket=root + "/agent.sock", agent_token_file=root + "/token",
        journal_path=root + "/journal.json", marker_path=root + "/marker.json",
        proof_path=root + "/proof.json", result_path=root + "/result.json",
        transcript_path=root + "/transcript.jsonl", dns_address="192.0.2.10",
        dns_port=53, dns_name="www.s1-kill.test", dns_type="A",
        panel_address="127.0.0.1", panel_port=2083, startup_timeout=1,
        boundary_timeout=1, stop_timeout=1, kill_timeout=1, command_timeout=1,
        recovery_timeout=1, endpoint_timeout=0, dns_timeout=1,
        stability_seconds=2, stability_interval=1,
    )
    return replace(value, **changes) if changes else value


class Transcript:
    def __init__(self) -> None:
        self.events: list[tuple[str, dict]] = []

    def event(self, name: str, **fields: object) -> None:
        self.events.append((name, fields))


def released_job(code: str, status: str = "failed", phase: str = "interrupted") -> dict:
    return {
        "request_id": REQUEST, "owner_id": OWNER, "kind": "dns_engine_switch",
        "target": "pdns", "package_name": QUALIFIER, "status": status, "phase": phase,
        "attempt": 1, "worker_pid": 0, "worker_started": "", "worker_command": "",
        "lease_expires_at": run_cell.ZERO_LEDGER_TIME,
        "finished_at": "2026-09-30T00:00:01Z", "updated_at": "2026-09-30T00:00:01Z",
        "error_code": code, "error_message": "The operation was handled.",
    }


IDENTITY = {"owner_id": OWNER, "manifest_qualifier": QUALIFIER}


class AdmissionTest(unittest.TestCase):
    def test_exactly_twelve_peer_reachable_v3_cells_are_admitted(self) -> None:
        admitted = []
        for raw in MANIFEST["cells"]:
            if raw.get("status") != "runnable":
                continue
            selected = run_cell.CellSpec.from_manifest(MANIFEST, raw["id"])
            if run_cell.is_fresh_primary_v3_cell(selected):
                admitted.append(raw["id"])
                self.assertTrue(bootstrap.fresh_pdns_primary_cell(raw), raw["id"])
            else:
                self.assertFalse(bootstrap.fresh_pdns_primary_cell(raw), raw["id"])
        self.assertEqual(len(admitted), 12)
        self.assertEqual(
            {cell_id.split("__")[1] for cell_id in admitted},
            set(run_cell.FRESH_PRIMARY_V3_BOUNDARIES),
        )
        self.assertEqual(set(run_cell.FRESH_PRIMARY_V3_BOUNDARIES), bootstrap.FRESH_PDNS_PRIMARY_PHASES)
        self.assertTrue(all(cell_id.endswith("__peer-reachable") for cell_id in admitted))

    def test_v3_selector_disk_phase_and_schema_per_boundary(self) -> None:
        expected = {
            ("intent", "before-write"): ("intent", None, "pre-journal"),
            ("intent", "after-write"): ("intent", "intent", "pre-start"),
            ("target-staged", "before-write"): ("target-staged", "intent", "pre-start"),
            ("target-staged", "after-write"): ("target-staged", "target-staged", "pre-start"),
            ("source-stopped", "before-write"): ("target-enable-intent", "target-staged", "pre-start"),
            ("source-stopped", "after-write"): (
                "target-enable-intent", "target-enable-intent", "pre-start"),
            ("target-started", "before-write"): (
                "target-started", "target-enable-intent", "post-start"),
            ("target-started", "after-write"): ("target-started", "target-started", "post-start"),
            ("target-verified", "before-write"): (
                "target-verified", "target-started", "post-start"),
            ("target-verified", "after-write"): (
                "target-verified", "target-verified", "post-start"),
            ("committed", "before-write"): ("committed", "target-verified", "post-start"),
            ("committed", "after-write"): ("committed", "committed", "post-start"),
        }
        for (phase, edge), (selector, disk, variant) in expected.items():
            selected = v3(phase, edge)
            with self.subTest(phase=phase, edge=edge):
                self.assertEqual(run_cell.selector_phase(selected), selector)
                self.assertEqual(run_cell.expected_journal_phase(selected), disk)
                self.assertEqual(run_cell.fresh_primary_v3_variant(selected), variant)
                self.assertEqual(
                    bootstrap.fresh_pdns_primary_variant(raw_cell(selected.cell_id)), variant)
                self.assertEqual(
                    run_cell.expected_journal_schema(selected),
                    "celikpanel-dns-engine-switch-journal/v3",
                )
        # Other cells keep their manifest phase and V1 journal.
        standalone = spec("pdns-switch__source-stopped__after-write__standalone__peer-reachable")
        self.assertEqual(run_cell.selector_phase(standalone), "source-stopped")
        self.assertEqual(run_cell.expected_journal_schema(standalone), run_cell.JOURNAL_SCHEMA)

    def test_tagged_agent_selects_the_v3_journal_phase(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            environment = run_cell.tagged_agent_environment(
                {"PATH": "/usr/bin"}, v3("source-stopped", "after-write"), REQUEST, "a" * 32,
                root + "/marker.json", 5, root, root + "/lock", root + "/agent.sock",
                root + "/token",
            )
        self.assertEqual(environment["CELIKPANEL_DNS_KILL_MATRIX_PHASE"], "target-enable-intent")
        self.assertEqual(environment["CELIKPANEL_DNS_KILL_MATRIX_POINT"], "after_write")

    def test_marker_observation_accepts_the_v3_journal_at_its_selected_phase(self) -> None:
        identity = {
            "mode": "switch", "mutation_owner_id": OWNER, "manifest_qualifier": QUALIFIER,
            "source_engine": "", "target_engine": "pdns", "source_epoch": 0,
            "target_epoch": 1, "source_revision": 0, "topology": "paired",
            "pair_role": "primary",
        }
        observed = {"schema": "celikpanel-dns-engine-switch-journal/v3",
                    "phase": "target-enable-intent", "mutation_request_id": REQUEST,
                    **identity}
        selected = v3("source-stopped", "before-write")
        run_cell.validate_observed_journal(selected, observed, REQUEST, identity)
        with self.assertRaises(run_cell.BoundaryUnverified):
            run_cell.validate_observed_journal(
                selected, dict(observed, schema=run_cell.JOURNAL_SCHEMA), REQUEST, identity)
        with self.assertRaises(run_cell.BoundaryUnverified):
            run_cell.validate_observed_journal(
                selected, dict(observed, phase="source-stopped"), REQUEST, identity)


class RefusalTest(unittest.TestCase):
    def refuse(self, root: str, selected: object, text: str, **changes: object) -> None:
        with self.assertRaisesRegex(run_cell.ControllerError, text):
            run_cell.refuse_unadmitted_fresh_primary(settings(root, selected, **changes))

    def test_cells_without_a_pass_definition_are_refused_before_mutation(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            self.refuse(root, spec(
                "pdns-switch__target-started__after-write__paired-primary__peer-unreachable"),
                "peer-unreachable")
            self.refuse(root, spec(
                "pdns-switch__rolling-back__after-write__paired-primary__peer-reachable"),
                "no hooked rolling-back")
            self.refuse(root, spec("pdns-switch__pre-intent__paired-primary__peer-reachable"),
                        "Nothing was started")

    def test_flag_combinations(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            post, pre = v3("target-started", "after-write"), v3("target-staged", "after-write")
            for changes in ({"owner_inverse_after_restart": True},
                            {"expect_agent_startup_rollback": True},
                            {"peer_catalog_format": "bind"},
                            {"retry_switch_after_rollback": True}):
                self.refuse(root, post, "its own socket flow", **changes)
            self.refuse(root, pre, "live PowerDNS database", owner_edit="sql")
            self.refuse(root, v3("intent", "before-write"), "no journal", owner_edit="config")
            self.refuse(root, post, "no reboot step", owner_edit="config",
                        reboot_after_recovery=True)
            self.refuse(root, pre, "needs --owner-edit-config", owner_release_recovery=True)
            self.refuse(root, post, "pre-start cell", owner_edit="config",
                        owner_release_recovery=True)
            self.refuse(root, spec("pdns-switch__intent__after-write__standalone__peer-reachable"),
                        "apply only to the fresh paired", owner_edit="config")
            for selected, changes in (
                (post, {}), (post, {"owner_edit": "sql"}), (post, {"owner_edit": "config"}),
                (pre, {"owner_edit": "config", "owner_release_recovery": True}),
                (v3("source-stopped", "before-write"), {"owner_edit": "config"}),
                (v3("intent", "before-write"), {"reboot_after_recovery": True}),
            ):
                with self.subTest(cell=selected.cell_id, changes=changes):
                    run_cell.refuse_unadmitted_fresh_primary(settings(root, selected, **changes))

    def test_reboot_is_admitted_for_the_forward_flows(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            run_cell.validate_reboot_settings(settings(
                root, v3("committed", "after-write"), reboot_after_recovery=True,
                disable_management_before_reboot=True, reboot_dir=root))

    def test_command_line_maps_the_owner_flags(self) -> None:
        parser = run_cell.build_argument_parser()
        base = ["--manifest", "/m", "--cell-id", "c", "--request-id", REQUEST, "--nonce", "a",
                "--tagged-agent-command", "[]", "--trigger-mode", "socket",
                "--recovery-command", "[]", "--agent-restart-command", "[]",
                "--panel-restart-command", "[]", "--recovery-probe-command", "[]",
                "--command-cwd", "/", "--state-dir", "/", "--mutation-lock", "/l",
                "--agent-socket", "/s", "--agent-token-file", "/t", "--journal", "/j",
                "--marker", "/m", "--proof", "/p", "--result", "/r", "--transcript", "/x",
                "--dns-address", "192.0.2.10", "--dns-port", "53", "--dns-name", "n",
                "--panel-address", "127.0.0.1", "--panel-port", "1",
                "--startup-timeout", "1", "--boundary-timeout", "1", "--stop-timeout", "1",
                "--kill-timeout", "1", "--command-timeout", "1", "--recovery-timeout", "1",
                "--endpoint-timeout", "1", "--dns-timeout", "1", "--stability-seconds", "1",
                "--stability-interval", "1"]
        parsed = parser.parse_args(base + ["--owner-edit-sql", "--expect-owner-directives"])
        self.assertEqual((parsed.owner_edit, parsed.expect_owner_directives), ("sql", True))
        self.assertIsNone(parser.parse_args(base).owner_edit)
        with self.assertRaises(SystemExit), mock.patch("sys.stderr", io.StringIO()):
            parser.parse_args(base + ["--owner-edit-sql", "--owner-edit-config"])


class GateProbeTest(unittest.TestCase):
    def command(self, returncode: int, value: object) -> object:
        output = (json.dumps(value) + "\n").encode() if value is not None else b""
        return run_cell.CommandResult(("/opt/t", "rpc-gate-probe"), returncode, output, False, 0.1)

    def test_decode_distinguishes_closed_open_and_unknown(self) -> None:
        schema = "celikpanel/dns-kill-matrix-gate-probe/v1"
        cases = (
            (0, {"schema": schema, "gate": "closed", "agent_error": "paused"}, "closed"),
            (0, {"schema": schema, "gate": "open"}, "open"),
            (75, {"schema": schema, "gate": "unknown"}, "unknown"),
            (75, {"schema": schema, "gate": "closed"}, "unknown"),
            (0, {"schema": "other", "gate": "closed"}, "unknown"),
            (0, None, "unknown"),
        )
        for returncode, value, gate in cases:
            with self.subTest(returncode=returncode, value=value):
                self.assertEqual(run_cell.decode_gate_probe(self.command(returncode, value))["gate"], gate)

    def test_probe_runs_before_the_trigger_and_closed_exits_four(self) -> None:
        source = MODULE_PATH.read_text(encoding="utf-8")
        flow = source[source.index("def run_cell(settings:"):]
        self.assertLess(flow.index("run_fresh_primary_gate_probe("),
                        flow.index("trigger = start_async_command("))
        self.assertIn("except FreshPrimaryGateClosed as closed:", flow)
        self.assertIn("return GATE_CLOSED_EXIT", flow)
        self.assertEqual(run_cell.GATE_CLOSED_EXIT, bootstrap.GATE_CLOSED_EXIT)
        with tempfile.TemporaryDirectory() as root:
            argv = run_cell.fresh_primary_gate_probe_argv(
                settings(root, v3("intent", "after-write")))
        self.assertEqual(argv[:4], ("/opt/t", "rpc-gate-probe", "--scenario", root + "/scenario.json"))

    def test_run_probe_records_an_unrun_probe_as_unknown(self) -> None:
        with tempfile.TemporaryDirectory() as root, mock.patch.object(
            run_cell, "run_bounded_command", side_effect=run_cell.ControllerError("gone")
        ):
            report = run_cell.run_fresh_primary_gate_probe(
                settings(root, v3("intent", "after-write")), {}, Transcript())
        self.assertEqual(report["gate"], "unknown")


def v3_journal(root: str, *, candidate: bool = True, native: bool = False) -> dict:
    conf = b"launch=gsqlite3\n"
    plan: dict = {"kind": "pdns-fresh-primary/v3", "config_after": []}
    if candidate:
        plan["candidate"] = {"path": root + "/candidate.sqlite3"}
    if native:
        plan["native"] = {"observed": {"SourceSerial": 1, "NativeSerial": 1790588837,
                                       "CatalogHash": "x"}}
    return {
        "schema": "celikpanel-dns-engine-switch-journal/v3", "phase": "target-staged",
        "pdns_candidate_path": root + "/candidate.sqlite3",
        "primary_catalog_serial": 1,
        "config_before": [
            {"path": root + "/pdns.conf", "exists": True, "mode": 0o640,
             "data": base64.b64encode(conf).decode()},
            {"path": root + "/celikpanel.conf", "exists": False, "mode": 0},
        ],
        "target_units_before": [{"name": "pdns.service", "load_state": "masked",
                                 "active_state": "inactive", "unit_file_state": "masked"}],
        "pdns_fresh_plan": plan,
    }


class JudgementTest(unittest.TestCase):
    def test_journal_facts_read_configs_candidate_units_and_native_serial(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            facts = run_cell.fresh_primary_journal_facts(v3_journal(root, native=True))
        self.assertEqual(facts["config_before"][0]["sha256"],
                         hashlib.sha256(b"launch=gsqlite3\n").hexdigest())
        self.assertEqual(facts["config_before"][1], {"path": facts["config_before"][1]["path"],
                                                     "exists": False, "sha256": None})
        self.assertEqual(facts["native_serial"], 1790588837)
        self.assertEqual(facts["target_unit_before"]["load_state"], "masked")
        self.assertTrue(facts["candidate_sealed"])

    def pre_install(self, root: str, unit: dict, *, listeners=None, units=None) -> dict:
        cut = {"journal": run_cell.fresh_primary_journal_facts(v3_journal(root))}
        with mock.patch.object(run_cell, "read_unit_properties", return_value=unit), \
                mock.patch.object(run_cell, "inspect_dns_unit_states", return_value=units or {
                    "pdns.service": "inactive", "named.service": "inactive",
                    "bind9.service": "inactive"}), \
                mock.patch.object(run_cell, "observe_port53_listeners",
                                  return_value=listeners or {"tcp": [], "udp": []}), \
                mock.patch.object(run_cell, "PDNS_DATABASE_PATH", root + "/pdns.sqlite3"):
            return run_cell.judge_fresh_primary_pre_install(
                settings(root, v3("target-staged", "after-write")), {}, cut)

    def test_pre_install_state_after_the_agent_rollback(self) -> None:
        standby = {"LoadState": "masked", "ActiveState": "inactive", "SubState": "dead",
                   "UnitFileState": "masked", "MainPID": "0"}
        with tempfile.TemporaryDirectory() as root:
            Path(root, "pdns.conf").write_bytes(b"launch=gsqlite3\n")
            self.assertEqual(self.pre_install(root, standby)["failures"], [])
            self.assertTrue(any("frozen standby" in item for item in self.pre_install(
                root, dict(standby, LoadState="loaded", UnitFileState="enabled"))["failures"]))
            self.assertTrue(any("not stopped" in item for item in self.pre_install(
                root, dict(standby, ActiveState="active", MainPID="77"))["failures"]))
            self.assertTrue(any("port-53" in item for item in self.pre_install(
                root, standby, listeners={"tcp": [{"pids": [5]}], "udp": []})["failures"]))
            Path(root, "candidate.sqlite3").write_bytes(b"x")
            self.assertTrue(any("remain" in item for item in self.pre_install(root, standby)["failures"]))
            os.unlink(Path(root, "candidate.sqlite3"))
            Path(root, "pdns.conf").write_bytes(b"launch=gsqlite3\nowner=1\n")
            self.assertTrue(any("not restored" in item for item in self.pre_install(root, standby)["failures"]))
            Path(root, "pdns.conf").write_bytes(b"launch=gsqlite3\n")
            Path(root, "dns-engine-state.json").write_text("{}", encoding="utf-8")
            self.assertTrue(any("state receipt" in item for item in self.pre_install(root, standby)["failures"]))

    def restart(self, root: str, selected: object, job: dict, *, journal: bool = False,
                owner_named: bool = False, forward=None, pre=None) -> dict:
        if journal:
            Path(root, "journal.json").write_text("{}", encoding="utf-8")
        release = {"released": True, "reads": 1, "ledger": {"active_request_id": ""}, "job": job}
        with mock.patch.object(run_cell, "wait_for_agent_release", return_value=release), \
                mock.patch.object(run_cell, "judge_fresh_primary_forward",
                                  return_value=forward or {"failures": [], "unknown": []}), \
                mock.patch.object(run_cell, "judge_fresh_primary_pre_install",
                                  return_value=pre or {"failures": [], "unknown": []}), \
                mock.patch.object(run_cell, "find_agent_owner_refusal",
                                  return_value={"observed": owner_named, "attempts": 1}):
            return run_cell.observe_fresh_primary_restart(
                settings(root, selected), {}, Transcript(),
                {"owner_id": OWNER, "manifest_qualifier": QUALIFIER}, {}, 0)

    def test_restart_verdicts_by_variant(self) -> None:
        finalized = ("commit/dns-engine-switch/v2/finalized/" + REQUEST + "/" + QUALIFIER)
        succeeded = dict(released_job(""), status="succeeded", phase=finalized)
        with tempfile.TemporaryDirectory() as root:
            pre = v3("target-staged", "after-write")
            post = v3("target-verified", "before-write")
            rolled = released_job(run_cell.AGENT_ROLLED_BACK_AFTER_RESTART)
            self.assertTrue(self.restart(root, pre, rolled)["retry_allowed"])
            self.assertFalse(self.restart(root, pre, rolled, owner_named=True)["retry_allowed"])
            self.assertFalse(self.restart(
                root, pre, released_job(run_cell.RESTART_BEFORE_SWITCH_COMMIT))["retry_allowed"])
            self.assertTrue(self.restart(
                root, v3("intent", "before-write"),
                released_job(run_cell.RESTART_BEFORE_SWITCH_COMMIT))["retry_allowed"])
            self.assertTrue(self.restart(root, post, succeeded)["retry_allowed"])
            report = self.restart(root, post, rolled)
            self.assertTrue(any("forward only" in item for item in report["failures"]))
            report = self.restart(root, post, released_job(run_cell.AGENT_RELEASED_NATIVE_UNKNOWN))
            self.assertTrue(any("did not complete" in item for item in report["failures"]))
            report = self.restart(root, post, succeeded, journal=True)
            self.assertTrue(any("kept the V3 journal" in item for item in report["failures"]))

    def test_forward_records_pid_history_and_the_daemon_restamp(self) -> None:
        state = {"sha256": "x", "schema": run_cell.FRESH_PRIMARY_V3_STATE_SCHEMA,
                 "native_catalog": run_cell.FRESH_PRIMARY_V3_NATIVE_CATALOG,
                 "semantic": {"engine": "pdns", "pair_role": "primary",
                              "mutation_request_id": REQUEST,
                              "primary_catalog_serial": 1790588837}}
        cut = {"pdns_main_pid": 700, "journal": {"primary_catalog_serial": 1,
                                                 "native_serial": 1790588837}}

        def judge(pid: int, served: list) -> dict:
            with tempfile.TemporaryDirectory() as root, \
                    mock.patch.object(run_cell, "read_fresh_primary_state", return_value=state), \
                    mock.patch.object(run_cell, "read_unit_main_pid", return_value=pid), \
                    mock.patch.object(run_cell, "capture_pdns_journal", return_value={}), \
                    mock.patch.object(run_cell, "fresh_primary_catalog_serials",
                                      return_value={"primary": {"udp": served, "tcp": served}}):
                return run_cell.judge_fresh_primary_forward(
                    settings(root, v3("committed", "after-write")), {}, Transcript(), cut, 0)

        report = judge(700, [1790588837])
        self.assertEqual((report["failures"], report["unknown"]), ([], []))
        self.assertTrue(report["catalog_restamp"]["restamped"])
        self.assertEqual(report["pdns_pid_history"], {"at_cut": 700, "after_recovery": 700})
        self.assertTrue(any("MainPID changed" in item for item in judge(701, [1790588837])["unknown"]))
        self.assertTrue(judge(700, [1])["failures"])

    def test_pair_check_requires_the_secondary_to_mirror_the_primary(self) -> None:
        # The primary is queried at the QEMU management address; the expected
        # www A is the scenario's record (batch 8 failed on this distinction).
        member = {"udp": [2026083101], "tcp": [2026083101]}
        www = {"udp": ["192.0.2.10"], "tcp": ["192.0.2.10"]}
        catalog = {"udp": [1790588837], "tcp": [1790588837]}
        expected = {"member_soa": [2026083101], "www_a": ["192.0.2.10"]}

        def observed(address, name, kind, values):
            return {"server": address, "port": 53, "qname": name, "qtype": kind,
                    **{transport: {"values": values[transport], "answers": []}
                       for transport in ("udp", "tcp")}}

        def answers(secondary_www):
            def query(address, port, name, kind, timeout):
                if name == run_cell.FRESH_PRIMARY_V3_CATALOG:
                    return observed(address, name, kind, catalog)
                if kind == "SOA":
                    return observed(address, name, kind, member)
                return observed(address, name, kind,
                                www if address == "10.0.2.15" else secondary_www)
            return query

        state = {"exists": True, "semantic": {"primary_catalog_serial": 1790588837}}
        with tempfile.TemporaryDirectory() as root:
            selected = settings(root, v3("committed", "after-write"), dns_address="10.0.2.15")
            with mock.patch.object(run_cell, "query_dns_observation", side_effect=answers(www)), \
                    mock.patch.object(run_cell, "read_dns_state_optional", return_value=state):
                self.assertEqual(
                    run_cell.check_fresh_primary_pair(selected, expected)["failures"], [])
            other = {"udp": ["192.0.2.99"], "tcp": ["192.0.2.99"]}
            with mock.patch.object(run_cell, "query_dns_observation", side_effect=answers(other)), \
                    mock.patch.object(run_cell, "read_dns_state_optional", return_value=state):
                self.assertTrue(run_cell.check_fresh_primary_pair(selected, expected)["failures"])

    def test_release_message_must_be_typed_and_name_the_next_step(self) -> None:
        typed = ("The first install of PowerDNS as the paired primary found that the PowerDNS "
                 "configuration is not as this install wrote it. Next step: the server "
                 "administrator ... --request-id " + REQUEST + ".")
        self.assertEqual(run_cell.judge_release_message(typed, REQUEST), [])
        generic = run_cell.GENERIC_RELEASE_PREFIX + " Next step " + REQUEST
        self.assertTrue(run_cell.judge_release_message(generic, REQUEST))
        self.assertTrue(run_cell.judge_release_message("Changed. " + REQUEST, REQUEST))

    def test_stability_window_can_record_dns_without_judging_it(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            selected = settings(root, v3("target-staged", "after-write"),
                                stability_seconds=1, stability_interval=1)
            with mock.patch.object(run_cell, "_management_stability_sample", return_value={}), \
                    mock.patch.object(run_cell, "query_authoritative_dns",
                                      side_effect=run_cell.ControllerError("no engine")), \
                    mock.patch.object(run_cell, "observe_peer_once", return_value=({}, None)), \
                    mock.patch.object(run_cell.time, "sleep"):
                report, failures, _ = run_cell.run_stability_window(
                    selected, (1, 2), Transcript(), "192.0.2.11", judge_dns=False)
                self.assertEqual(failures, [])
                self.assertFalse(report["dns_judged"])
                _, judged, _ = run_cell.run_stability_window(
                    selected, (1, 2), Transcript(), "192.0.2.11")
                self.assertEqual(len(judged), 2)


class OwnerEditTest(unittest.TestCase):
    def test_config_edit_appends_one_line_and_the_owner_reverts_it(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            path = Path(root, "pdns.conf")
            path.write_bytes(b"launch=gsqlite3\n")
            os.chmod(path, 0o640)
            selected = settings(root, v3("target-started", "after-write"))
            cut = {"journal": {"config_before": [{"path": str(path)}]}}
            with mock.patch.object(run_cell, "FRESH_PRIMARY_V3_CONFIG_EDIT_PATH", str(path)):
                edit = run_cell.apply_owner_config_edit(selected, cut)
                self.assertEqual(run_cell.judge_owner_edit_preserved(selected, edit), [])
                self.assertTrue(path.read_bytes().endswith(
                    ("(dns-kill-matrix " + REQUEST + ")\n").encode()))
                self.assertEqual(edit["mode"], "0640")
                reverted = run_cell.revert_owner_config_edit(edit)
                self.assertEqual(reverted["sha256"], hashlib.sha256(b"launch=gsqlite3\n").hexdigest())
                self.assertTrue(run_cell.judge_owner_edit_preserved(selected, edit))
                with self.assertRaisesRegex(run_cell.ControllerError, "configuration preimage"):
                    run_cell.apply_owner_config_edit(selected, {"journal": {"config_before": []}})

    def test_sql_edit_inserts_one_member_record_and_reads_it_back(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            database = Path(root, "pdns.sqlite3")
            connection = sqlite3.connect(database)
            connection.executescript(
                "CREATE TABLE domains (id INTEGER PRIMARY KEY, name TEXT);"
                "CREATE TABLE records (id INTEGER PRIMARY KEY, domain_id INTEGER, name TEXT,"
                " type TEXT, content TEXT, ttl INTEGER, prio INTEGER, disabled BOOL,"
                " ordername TEXT, auth BOOL);"
                "INSERT INTO domains (name) VALUES ('s1-kill.test');")
            connection.commit()
            connection.close()
            selected = settings(root, v3("committed", "after-write"))
            with mock.patch.object(run_cell, "PDNS_DATABASE_PATH", str(database)):
                edit = run_cell.apply_owner_sql_edit(selected)
                self.assertEqual(edit["row"]["name"], "owner-note.s1-kill.test")
                self.assertEqual(run_cell.judge_owner_edit_preserved(selected, edit), [])
                connection = sqlite3.connect(database)
                connection.execute("UPDATE records SET content = 'x'")
                connection.commit()
                connection.close()
                self.assertTrue(run_cell.judge_owner_edit_preserved(selected, edit))

    def test_unrelated_begin_verdicts(self) -> None:
        def command(returncode: int, value: dict) -> object:
            return run_cell.CommandResult(("t",), returncode, json.dumps(value).encode(), False, 0)

        with tempfile.TemporaryDirectory() as root:
            selected = settings(root, v3("committed", "after-write"))
            for returncode, value, verdict in (
                (0, {"begun": True, "finished": True}, "dns-only"),
                (1, {"begun": False, "begin_error": "busy"}, "blocked"),
                (75, {"begun": True, "finished": False}, "unknown"),
            ):
                with mock.patch.object(run_cell, "run_bounded_command",
                                       return_value=command(returncode, value)):
                    self.assertEqual(run_cell.run_unrelated_begin_probe(
                        selected, {}, Transcript())["verdict"], verdict)

    def test_hold_flow_dispatch_follows_the_proven_kill(self) -> None:
        source = MODULE_PATH.read_text(encoding="utf-8")
        flow = source[source.index("def run_cell(settings:"):]
        dispatch = flow.index("run_fresh_primary_hold_flow(")
        self.assertLess(flow.index("atomic_write_new_json(settings.proof_path, proof)"), dispatch)
        self.assertLess(dispatch, flow.index("recovery_attempts: list"))
        self.assertLess(dispatch, flow.index("raise FreshPrimaryHoldFinished()"))
        self.assertLess(flow.index("observe_fresh_primary_restart("),
                        flow.index('f"post-restart-rpc-retry-{ordinal}"'))


class HoldFlowTest(unittest.TestCase):
    """The owner-edit hold flow and the Agent-released owner recovery, mocked."""

    TYPED = ("The first install of PowerDNS as the paired primary found that the PowerDNS "
             "configuration is not as this install wrote it. Next step: the server "
             "administrator runs ... --request-id " + REQUEST + ".")

    def run_hold(self, root: str, selected: object, *, unrelated: str = "dns-only",
                 journal_changes: bool = False, initial: dict | None = None,
                 **changes: object) -> tuple[dict, list, list]:
        config = Path(root, "pdns.conf")
        config.write_bytes(b"launch=gsqlite3\n")
        journal = Path(root, "journal.json")
        journal.write_text('{"phase":"target-staged"}', encoding="utf-8")
        cut = {
            "journal_at_boundary": {"exists": True, "sha256": run_cell.sha256_file(str(journal))},
            "journal": {"config_before": [{"path": str(config), "exists": True,
                                           "sha256": hashlib.sha256(b"launch=gsqlite3\n").hexdigest()}]},
            "files": {"database": {"path": root + "/pdns.sqlite3"}},
        }
        job = dict(released_job(run_cell.AGENT_RELEASED_NATIVE_UNKNOWN), error_message=self.TYPED)

        def release(*_args, **_kwargs):
            if journal_changes:
                journal.write_text('{"phase":"rolling-back"}', encoding="utf-8")
            return {"released": True, "reads": 1, "ledger": {"active_request_id": ""}, "job": job}

        result: dict = json.loads(json.dumps(initial or {}))
        safety: list[str] = []
        verification: list[str] = []
        with mock.patch.object(run_cell, "FRESH_PRIMARY_V3_CONFIG_EDIT_PATH", str(config)), \
                mock.patch.object(run_cell, "PDNS_DATABASE_PATH", root + "/pdns.sqlite3"), \
                mock.patch.object(run_cell, "checked_command", return_value=({}, None)), \
                mock.patch.object(run_cell, "wait_for_unix_socket", return_value=(1, 2)), \
                mock.patch.object(run_cell, "wait_for_tcp", return_value=None), \
                mock.patch.object(run_cell, "wait_for_agent_release", side_effect=release), \
                mock.patch.object(run_cell, "run_unrelated_begin_probe",
                                  return_value={"verdict": unrelated}), \
                mock.patch.object(run_cell, "run_stability_window",
                                  return_value=({"samples": []}, [], [])) as window, \
                mock.patch.object(run_cell, "record_native_versions", return_value={}), \
                mock.patch.object(run_cell, "run_fresh_primary_owner_release") as owner:
            Path(root, "pdns.sqlite3").write_bytes(b"db")
            run_cell.run_fresh_primary_hold_flow(
                settings(root, selected, owner_edit="config", **changes),
                result=result, transcript=Transcript(), ordinary={}, owner_environment={},
                controller_identity={}, identity_receipt=IDENTITY, cut=cut,
                old_socket_identity=None, safety_failures=safety,
                verification_failures=verification, diagnostic_failures=[],
            )
        result["_judge_dns"] = window.call_args.kwargs["judge_dns"]
        result["_owner_called"] = owner.called
        return result, safety, verification

    def test_config_edit_on_a_post_start_cut_passes_when_the_agent_holds_only_dns(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            result, _, _ = self.run_hold(root, v3("target-started", "after-write"))
        flow = result["fresh_primary_v3_hold"]
        self.assertEqual(flow["failures"], [], flow)
        self.assertEqual(result["status"], "passed")
        self.assertTrue(result["_judge_dns"])
        self.assertFalse(result["_owner_called"])

    def test_hold_flow_records_native_versions_after_beside_before(self) -> None:
        # Batch 7 c05: every flow-finishing path records both; run_cell wrote
        # ``before`` before the tagged Agent, the hold flow adds ``after``.
        before = {"at": "before", "packages": {}, "daemons": {}}
        for owner_release in (False, True):
            with self.subTest(owner_release_recovery=owner_release), \
                    tempfile.TemporaryDirectory() as root:
                result, _, _ = self.run_hold(
                    root, v3("target-staged", "after-write"),
                    initial={"native_versions": {"before": before}},
                    owner_release_recovery=owner_release)
                self.assertEqual(result["native_versions"]["before"], before)
                self.assertIn("after", result["native_versions"])

    def test_verified_deviations_fail_the_hold(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            result, _, _ = self.run_hold(root, v3("target-started", "after-write"), unrelated="blocked")
            self.assertEqual(result["status"], "failed")
        with tempfile.TemporaryDirectory() as root:
            result, _, _ = self.run_hold(root, v3("committed", "before-write"), journal_changes=True)
            self.assertTrue(any("journal changed" in item
                                for item in result["fresh_primary_v3_hold"]["failures"]))
        with tempfile.TemporaryDirectory() as root:
            result, _, _ = self.run_hold(root, v3("target-staged", "after-write"),
                                         owner_release_recovery=True)
            self.assertFalse(result["_judge_dns"])
            self.assertTrue(result["_owner_called"])

    def test_owner_release_recovery_judges_status_command_ledger_and_rerun(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            ledger = Path(root, "service-mutations.json")
            ledger.write_text('{"version":1}', encoding="utf-8")
            config = Path(root, "pdns.conf")
            config.write_bytes(b"launch=gsqlite3\nowner\n")
            edit = {"path": str(config), "after_sha256": run_cell.sha256_file(str(config)),
                    "before_size": len(b"launch=gsqlite3\n"),
                    "before_sha256": hashlib.sha256(b"launch=gsqlite3\n").hexdigest()}
            job = released_job(run_cell.AGENT_RELEASED_NATIVE_UNKNOWN)
            needle = f"recover-dns-pdns-fresh-prestart --request-id {REQUEST}"

            def run(output: str, rerun_code: int = 0) -> dict:
                flow: dict = {"failures": [], "unknown": []}
                config.write_bytes(b"launch=gsqlite3\nowner\n")
                commands = iter([{"ran": True, "returncode": 0},
                                 {"ran": True, "returncode": rerun_code}])
                with mock.patch.object(run_cell, "read_request_ledger",
                                       return_value=({"active_request_id": ""}, job)), \
                        mock.patch.object(run_cell, "record_recovery_status", return_value={
                            "available": True, "mutated": False, "changed_evidence": [],
                            "command": {"ran": True, "output": output}}), \
                        mock.patch.object(run_cell, "run_owner_command",
                                          side_effect=lambda *a, **k: next(commands)), \
                        mock.patch.object(run_cell, "judge_fresh_primary_pre_install",
                                          return_value={"failures": [], "unknown": []}), \
                        mock.patch.object(run_cell, "snapshot_private_evidence", return_value={}):
                    run_cell.run_fresh_primary_owner_release(
                        settings(root, v3("target-staged", "after-write")), flow, edit=edit,
                        cut={}, transcript=Transcript(), owner_environment={})
                return flow

            passed = run("... " + needle + " ...")
            self.assertEqual((passed["failures"], passed["unknown"]), ([], []), passed)
            self.assertEqual(config.read_bytes(), b"launch=gsqlite3\n")
            self.assertTrue(any("does not name" in item for item in run("nothing")["failures"]))
            self.assertTrue(any("re-run exited" in item
                                for item in run(needle, rerun_code=1)["failures"]))


class TakeoverAndReinstallTest(unittest.TestCase):
    def test_owner_directives_are_proved_against_the_sealed_preimage(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            options = Path(root, "named.conf.options")
            text = "options {\n" + "".join(run_cell.OWNER_BIND_DIRECTIVES) + "\tdirectory \"/var/cache/bind\";\n};\n"
            options.write_text(text, encoding="utf-8")
            digest = hashlib.sha256(text.encode()).hexdigest()
            proof = {"source_preinstall_proof": {"owner_files": {str(options): {"sha256": digest}}}}
            with mock.patch.object(run_cell, "OWNER_BIND_OPTIONS_PATH", str(options)):
                self.assertEqual(run_cell.prove_owner_bind_directives(proof)["sha256"], digest)
                self.assertEqual(run_cell.judge_takeover_rollback_owner_files(proof)["failures"], [])
                options.write_text("options {\n\tdirectory \"/var/cache/bind\";\n};\n", encoding="utf-8")
                with self.assertRaisesRegex(run_cell.ControllerError, "prepared directives"):
                    run_cell.prove_owner_bind_directives(proof)
                self.assertTrue(run_cell.judge_takeover_rollback_owner_files(proof)["failures"])

    def test_takeover_judgement_needs_the_restored_files_and_convergence(self) -> None:
        passed = {"status": "passed", "recovery": {"owner_files_before_retry": {
            "failures": [], "unknown": []}}, "recovery_outcome": {"classification": "target_converged"}}
        with mock.patch.object(run_cell, "_file_presence", return_value={}):
            result = json.loads(json.dumps(passed))
            run_cell.judge_owner_directives_takeover(result, [])
            self.assertEqual(result["status"], "passed")
            result = json.loads(json.dumps(passed))
            result["recovery"]["owner_files_before_retry"]["failures"] = ["changed"]
            run_cell.judge_owner_directives_takeover(result, [])
            self.assertEqual(result["status"], "failed")
            result = json.loads(json.dumps(passed))
            del result["recovery"]["owner_files_before_retry"]
            unknown: list[str] = []
            run_cell.judge_owner_directives_takeover(result, unknown)
            self.assertEqual(result["status"], "unverified")

    def test_guest_directive_writer_matches_the_controller_block(self) -> None:
        program = SHELL.split("<<'PYOWNERDIRECTIVES'\n", 1)[1].split("\nPYOWNERDIRECTIVES\n", 1)[0]
        with tempfile.TemporaryDirectory() as root:
            options = Path(root, "named.conf.options")
            options.write_text("options {\n\tdirectory \"/var/cache/bind\";\n};\n", encoding="utf-8")
            completed = subprocess.run([sys.executable, "-c", program, str(options)],
                                       check=False, capture_output=True, text=True)
            self.assertEqual(completed.returncode, 0, completed.stderr)
            self.assertIn("options {\n" + "".join(run_cell.OWNER_BIND_DIRECTIVES),
                          options.read_text(encoding="utf-8"))
            again = subprocess.run([sys.executable, "-c", program, str(options)],
                                   check=False, capture_output=True, text=True)
            self.assertNotEqual(again.returncode, 0)

    def test_statoverride_capture_is_recorded_unjudged(self) -> None:
        completed = subprocess.CompletedProcess(
            ["dpkg-statoverride"], 0, stdout=b"root bind 0755 /run/named\n", stderr=b"")
        with mock.patch.object(run_cell.subprocess, "run", return_value=completed):
            report = run_cell.record_dpkg_statoverrides({}, 1)
        self.assertEqual((report["judged"], report["returncode"]), (False, 0))
        self.assertIn("/run/named", report["stdout"])
        source = MODULE_PATH.read_text(encoding="utf-8")
        flow = source[source.index("def run_cell(settings:"):]
        self.assertLess(flow.index('"before": record_dpkg_statoverrides('),
                        flow.index("tagged = start_tagged_agent("))
        self.assertIn('result["dpkg_statoverride"]["after"] = record_dpkg_statoverrides(', flow)


class HostTest(unittest.TestCase):
    def args(self, raw: dict, **changes: object) -> argparse.Namespace:
        values = dict(
            work_root=Path("/w"), cell_id=raw["id"], manifest=Path("/m"), node="debian13",
            identity_file=Path("/k"), source_fixture="uninitialized", execute=False,
            owner_edit=None, owner_release_recovery=False, zone_lifecycle=False,
            owner_directives=False, disable_management_before_reboot=False,
        )
        values.update(changes)
        return argparse.Namespace(**values)

    def test_run_validation(self) -> None:
        pre = raw_cell("pdns-switch__source-stopped__after-write__paired-primary__peer-reachable")
        post = raw_cell("pdns-switch__committed__before-write__paired-primary__peer-reachable")
        journal_free = raw_cell("pdns-switch__intent__before-write__paired-primary__peer-reachable")
        ok = (
            (pre, {"owner_edit": "config", "owner_release_recovery": True}, False),
            (post, {"owner_edit": "sql"}, False),
            (post, {"zone_lifecycle": True}, True),
            # Defined order since batch 8: the lifecycle runs before the reboot.
            (post, {"zone_lifecycle": True, "disable_management_before_reboot": True}, True),
        )
        for raw, changes, reboot in ok:
            bootstrap.validate_fresh_primary_run(self.args(raw, **changes), raw, reboot)
        refused = (
            (pre, {"owner_edit": "sql"}, False, "live database"),
            (journal_free, {"owner_edit": "config"}, False, "no journal"),
            (post, {"owner_edit": "config"}, True, "no reboot"),
            (post, {"owner_edit": "config", "owner_release_recovery": True}, False, "pre-start"),
            (post, {"source_fixture": "managed-bind"}, False, "uninitialized"),
            (raw_cell(TAKEOVER_CELL), {"owner_edit": "config"}, False, "apply only"),
            (post, {"owner_directives": True}, False, "takeover cell"),
        )
        for raw, changes, reboot, text in refused:
            with self.subTest(changes=changes), self.assertRaisesRegex(bootstrap.BootstrapError, text):
                bootstrap.validate_fresh_primary_run(self.args(raw, **changes), raw, reboot)
        takeover = raw_cell(TAKEOVER_CELL)
        bootstrap.validate_fresh_primary_run(
            self.args(takeover, source_fixture=bootstrap.UNMANAGED_BIND_STOPPED,
                      owner_directives=True), takeover, False)

    def test_prepared_flags_keep_the_canonical_order(self) -> None:
        args = self.args(raw_cell("pdns-switch__target-staged__after-write__paired-primary__peer-reachable"),
                         owner_edit="config", owner_release_recovery=True,
                         stop_after_kill_for_independent_recovery=False)
        self.assertEqual(bootstrap.prepared_flags(args),
                         ["--owner-edit-config", "--owner-release-recovery"])
        order = bootstrap.RUN_PREPARED_CODE.split("order = [", 1)[1].split("]", 1)[0]
        names = [name.strip() for name in order.replace("\n", " ").split(",") if name.strip()]
        self.assertEqual(len(names), len(bootstrap.PREPARED_FLAG_ORDER))

    @unittest.skipUnless(
        sys.platform == "linux" and hasattr(os, "geteuid") and os.geteuid() == 0,
        "prepared argv owner proof requires a root Linux fixture",
    )
    def test_guest_program_admits_the_new_flags_only_where_they_apply(self) -> None:
        primary = "pdns-switch__target-started__after-write__paired-primary__peer-reachable"
        cases = (
            (primary, ["--owner-edit-config"], True),
            (primary, ["--owner-edit-sql"], True),
            (primary, ["--owner-edit-config", "--owner-release-recovery"], True),
            (primary, ["--reboot-after-recovery", "--disable-management-before-reboot"], True),
            (primary, ["--owner-edit-config", "--owner-edit-sql"], False),
            (primary, ["--owner-release-recovery"], False),
            (primary, ["--owner-edit-config", "--reboot-after-recovery"], False),
            (primary, ["--expect-owner-directives"], False),
            (TAKEOVER_CELL, ["--expect-owner-directives"], True),
            (TAKEOVER_CELL, ["--owner-edit-config"], False),
            ("pdns-switch__target-started__after-write__paired-primary__peer-unreachable",
             ["--owner-edit-config"], False),
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
                base = [str(executable), "--cell-id", cell_id, "--trigger-mode", "socket",
                        "--result", str(root / "result.json")]
                prepared.write_text(json.dumps(base), encoding="utf-8")
                prepared.chmod(0o600)
                captured.unlink(missing_ok=True)
                with self.subTest(cell_id=cell_id, flags=flags):
                    returncode = subprocess.run(
                        [sys.executable, "-c", code, cell_id, *flags],
                        check=False, capture_output=True).returncode
                    self.assertEqual(returncode == 0, accepted)
                    self.assertEqual(captured.exists(), accepted)

    def test_zone_step_verdicts_and_remote_command(self) -> None:
        published = {"outcome": "verified_published"}
        self.assertEqual(bootstrap.judge_zone_step("add", published, None), "passed")
        self.assertEqual(bootstrap.judge_zone_step("add", published, "mismatch: x"), "failed")
        self.assertEqual(bootstrap.judge_zone_step("add", published, "OSError: x"), "unverified")
        self.assertEqual(bootstrap.judge_zone_step(
            "delete", {"outcome": "pending_exact_operation"}, None), "pending")
        self.assertEqual(bootstrap.judge_zone_step(
            "edit", {"outcome": "pending_exact_operation"}, None), "unverified")
        self.assertEqual(bootstrap.judge_zone_step("edit", {"outcome": "refused"}, None), "failed")
        remote = bootstrap.zone_lifecycle_remote("re-add")
        self.assertIn("rpc-pdns-primary-zone-v3 ", remote)
        self.assertIn("runuser -u root -g celikpanel", remote)
        self.assertIn("rpc-pdns-primary-zone-v3-recover",
                      bootstrap.zone_lifecycle_remote("delete", recover=True))
        with self.assertRaises(bootstrap.BootstrapError):
            bootstrap.zone_lifecycle_remote("add", recover=True)
        self.assertEqual(bootstrap.decode_zone_step("")["outcome"], "unknown")

    def test_observation_retries_until_the_secondary_converges(self) -> None:
        calls = []

        def flaky():
            calls.append(1)
            if len(calls) < 3:
                raise ValueError("catalog lags")
            return {"ok": True}

        self.assertEqual(bootstrap.observe_until_converged(flaky, sleep=lambda _: None),
                         ({"ok": True}, None))
        value, error = bootstrap.observe_until_converged(
            lambda: (_ for _ in ()).throw(ValueError("differs")), attempts=2, sleep=lambda _: None)
        self.assertIsNone(value)
        self.assertTrue(error.startswith("mismatch:"))

    def test_gate_closed_skips_the_peer_and_passes_exit_four_through(self) -> None:
        raw = raw_cell("pdns-switch__intent__after-write__paired-primary__peer-reachable")
        with mock.patch.object(bootstrap, "finish_fresh_primary_peer_verdict") as verdict, \
                mock.patch("sys.stdout", new_callable=io.StringIO) as output:
            self.assertEqual(bootstrap.finish_fresh_primary_run(self.args(raw), {}, 4), 4)
        verdict.assert_not_called()
        self.assertEqual(json.loads(output.getvalue())["status"], "gate-closed")
        with mock.patch.object(bootstrap, "finish_fresh_primary_peer_verdict", return_value=0), \
                mock.patch.object(bootstrap, "run_zone_lifecycle", return_value=0) as lifecycle:
            self.assertEqual(bootstrap.finish_fresh_primary_run(
                self.args(raw, zone_lifecycle=True), {}, 0), 0)
        lifecycle.assert_called_once()
        with mock.patch.object(bootstrap, "finish_fresh_primary_peer_verdict") as verdict:
            self.assertEqual(bootstrap.finish_fresh_primary_run(
                self.args(raw, owner_edit="config"), {}, 1), 1)
        verdict.assert_not_called()


class PeerObservationTest(unittest.TestCase):
    PRIMARY, SECONDARY = "192.0.2.10", "192.0.2.11"

    @staticmethod
    def answer(name: str, kind: str, value: str) -> str:
        return (f";; ->>HEADER<<- opcode: QUERY, status: NOERROR\n;; flags: qr aa;\n"
                f"{name}. 60 IN {kind} {value}\n")

    @staticmethod
    def nxdomain() -> str:
        return ";; ->>HEADER<<- opcode: QUERY, status: NXDOMAIN\n;; flags: qr aa;\n"

    def catalog(self, members: list[str]) -> str:
        catalog = bind_peer.catalog_name(self.PRIMARY) + "."
        soa = f"{catalog} 60 IN SOA invalid. invalid. 7 60 30 3600 30\n"
        rows = "".join(
            f"{label}.zones.{catalog} 60 IN PTR {member}.\n"
            for label, member in zip(("tlhnkturltuku63bhersj40lbi0vkdvt", "0" * 32), members))
        return (";; ->>HEADER<<- opcode: QUERY, status: NOERROR\n" + soa
                + f"{catalog} 60 IN NS invalid.\n" + f'version.{catalog} 60 IN TXT "2"\n'
                + rows + soa)

    def observe(self, step: str, *, members: list[str], changed: bool, serial: str,
                present: bool = True) -> dict:
        def read(ssh, command, execute):
            if "AXFR" in command:
                return self.catalog(members)
            if "SOA" in command and bind_peer.CHILD in command:
                if not present:
                    return self.nxdomain()
                return self.answer(bind_peer.CHILD, "SOA",
                                   f"ns1.s1-kill.test. hostmaster.s1-kill.test. {serial} 10800 3600 604800 3600")
            if bind_peer.CHILD_CHANGED in command:
                return (self.answer(bind_peer.CHILD_CHANGED, "A", "192.0.2.10")
                        if changed else self.nxdomain())
            if bind_peer.CHILD_QUERY in command:
                return self.answer(bind_peer.CHILD_QUERY, "A", "192.0.2.10")
            return ""

        args = argparse.Namespace(step=step, cell_id="c", execute=True)
        with mock.patch.object(bind_peer, "selected", return_value=(
                self.PRIMARY, self.SECONDARY, {}, Path("/k"))), \
                mock.patch.object(bind_peer, "verify_guest"), \
                mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]), \
                mock.patch.object(bind_peer, "remote_read", side_effect=read):
            return bind_peer.observe_child(args)

    def test_child_lifecycle_observation(self) -> None:
        both = [bind_peer.ZONE, bind_peer.CHILD]
        report = self.observe("add", members=both, changed=False, serial="2026092801")
        self.assertEqual(len(report["answers"]), 4)
        self.observe("edit", members=both, changed=True, serial="2026092802")
        self.observe("delete", members=[bind_peer.ZONE], changed=False, serial="", present=False)
        self.observe("re-add", members=both, changed=False, serial="2026092803")
        with self.assertRaises(ValueError):
            self.observe("re-add", members=both, changed=True, serial="2026092803")
        with self.assertRaises(ValueError):
            self.observe("add", members=both, changed=False, serial="2026092899")
        with self.assertRaises(ValueError):
            self.observe("delete", members=both, changed=False, serial="", present=False)

    def test_admitted_v3_cells_select_the_bind_peer(self) -> None:
        for phase in sorted(bootstrap.FRESH_PDNS_PRIMARY_PHASES):
            raw = raw_cell(f"pdns-switch__{phase}__before-write__paired-primary__peer-reachable")
            self.assertTrue(bind_peer.accepted_cell(raw, "uninitialized"))
            self.assertFalse(bind_peer.accepted_cell(raw, "managed-bind"))
        self.assertFalse(bind_peer.accepted_cell(raw_cell(
            "pdns-switch__target-started__after-write__paired-primary__peer-unreachable"),
            "uninitialized"))


if __name__ == "__main__":
    unittest.main()
