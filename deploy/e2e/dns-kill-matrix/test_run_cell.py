#!/usr/bin/env python3
"""Isolated contract tests for the per-cell SIGKILL controller."""

from __future__ import annotations

import importlib.util
import json
import os
import socket
import struct
import sys
import tempfile
import threading
import unittest
from dataclasses import replace
from unittest import mock
from pathlib import Path


MODULE_PATH = Path(__file__).with_name("run_cell.py")
SPEC = importlib.util.spec_from_file_location("dns_kill_run_cell", MODULE_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError(f"cannot import {MODULE_PATH}")
run_cell = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = run_cell
SPEC.loader.exec_module(run_cell)


def cell(
    phase: str = "intent",
    edge: str = "before-write",
    *,
    driver: str = "bind",
    role: str = "standalone",
    peer: str = "reachable",
) -> object:
    point = "pre_intent" if edge == "window" else edge.replace("-", "_")
    return run_cell.CellSpec(
        cell_id=f"{driver}.{phase}.{edge}.{role}.{peer}",
        driver=driver,
        role=role,
        peer_reachability=peer,
        phase=phase,
        edge=edge,
        point=point,
    )


def boundary_identity() -> dict[str, object]:
    return {
        "mode": "switch",
        "mutation_owner_id": "2" * 32,
        "manifest_qualifier": "dns-engine-switch/v1:sha256:" + "3" * 64,
        "source_engine": "pdns",
        "target_engine": "bind",
        "source_epoch": 4,
        "target_epoch": 5,
        "source_revision": 6,
        "topology": "standalone",
        "pair_role": "",
    }


def boundary_identity_for_driver(driver: str) -> dict[str, object]:
    identity = boundary_identity()
    if driver in ("bind", "signed-update-finalize"):
        return identity
    identity["target_engine"] = "pdns"
    identity["source_engine"] = "" if driver == "pdns-adopt" else "bind"
    if driver == "pdns-adopt":
        identity["mode"] = "adopt"
    return identity


def observed_journal_value(
    selected: object,
    phase: str,
    journal_path: str,
    identity: dict[str, object],
    *,
    request_id: str = "1" * 32,
) -> dict[str, object]:
    observed = {
        "path": journal_path,
        "schema": run_cell.JOURNAL_SCHEMA,
        "phase": phase,
        "mutation_request_id": request_id,
        **identity,
    }
    for optional in run_cell.OPTIONAL_BOUNDARY_JOURNAL_IDENTITY_FIELDS:
        if observed[optional] == "":
            del observed[optional]
    return observed


def marker_value(
    selected: object,
    marker_path: str,
    journal_path: str,
    identity: dict[str, object],
    *,
    request_id: str = "1" * 32,
    nonce: str = "a" * 32,
) -> dict[str, object]:
    return {
        "schema": run_cell.MARKER_SCHEMA,
        "cell_id": selected.cell_id,
        "driver": selected.driver,
        "observed_driver": selected.driver,
        "point": selected.point,
        "phase": selected.phase,
        "request_id": request_id,
        "nonce": nonce,
        "marker": marker_path,
        "ready_fd": 9,
        "pid": 123,
        "process_start_ticks": "456",
        "recorded_at": "2026-08-31T00:00:00Z",
        "observed_journal": observed_journal_value(
            selected,
            selected.phase,
            journal_path,
            identity,
            request_id=request_id,
        ),
    }


def add_rollback_precursor(
    marker: dict[str, object],
    selected: object,
    journal_path: str,
    identity: dict[str, object],
    *,
    request_id: str = "1" * 32,
) -> None:
    precursor_phase = run_cell.rollback_precursor_phase(selected)
    if precursor_phase is None:
        raise AssertionError("test cell does not require a rollback precursor")
    marker["rollback_precursor"] = {
        "schema": run_cell.ROLLBACK_PRECURSOR_SCHEMA,
        "driver": selected.driver,
        "observed_driver": selected.driver,
        "point": "after_write",
        "phase": precursor_phase,
        "request_id": request_id,
        "action": run_cell.ROLLBACK_PRECURSOR_ACTION,
        "observed_journal": observed_journal_value(
            selected,
            precursor_phase,
            journal_path,
            identity,
            request_id=request_id,
        ),
    }


def source_preinstall_value(selected: object) -> dict[str, object]:
    return {
        "schema": run_cell.SOURCE_PREINSTALL_SCHEMA,
        "cell_id": selected.cell_id,
        "scope": "managed-pdns-source-preparation-for-bind-only",
        "package_install_origin": "harness-source-preinstall",
        "source_packages": [
            {
                "name": "pdns-backend-sqlite3",
                "status": "install ok installed",
                "version": "4.9.2-1+deb13u1",
            },
            {
                "name": "pdns-server",
                "status": "install ok installed",
                "version": "4.9.2-1+deb13u1",
            },
        ],
        "measured_target_packages": [{"name": "bind9", "status": "absent"}],
        "install_guard": {
            "unit": "pdns.service",
            "persistent_mask_target": "/dev/null",
            "package_hooks_could_not_start": True,
        },
        "mask_removed_before_external_source_start": True,
        "source_unit_before_external_configuration": {
            "name": "pdns.service",
            "load_state": "loaded",
            "active_state": "inactive",
            "unit_file_state": "enabled",
        },
        "dns_state_absent": True,
        "dns_journal_absent": True,
        "dns_ownership_receipts_absent": True,
        "global_udp_tcp_53_bindable": True,
        "production_pdns_adoption_pending": True,
    }


def source_adoption_value(selected: object) -> dict[str, object]:
    return {
        "schema": run_cell.SOURCE_ADOPTION_SCHEMA,
        "cell_id": selected.cell_id,
        "scope": "external-pdns-source-for-production-adoption-before-bind",
        "construction_origin": "harness-external-pdns",
        "production_adoption_driver": "pdns-adopt",
        "source_setup_scenario_sha256": "4" * 64,
        "source_setup_identity_receipt_sha256": "5" * 64,
        "source_packages": [
            {
                "name": "pdns-backend-sqlite3",
                "status": "install ok installed",
                "version": "4.9.2-1+deb13u1",
            },
            {
                "name": "pdns-server",
                "status": "install ok installed",
                "version": "4.9.2-1+deb13u1",
            },
        ],
        "measured_target_packages": [{"name": "bind9", "status": "absent"}],
        "main_config": {
            "path": "/etc/powerdns/pdns.conf",
            "sha256": "6" * 64,
            "owner": "root:pdns",
            "mode": "0640",
        },
        "managed_config": {
            "path": "/etc/powerdns/pdns.d/celikpanel.conf",
            "sha256": "7" * 64,
            "owner": "root:root",
            "mode": "0644",
        },
        "cluster_config": {
            "path": "/etc/powerdns/pdns.d/celikpanel-cluster.conf",
            "status": "absent",
        },
        "database": {
            "path": "/var/lib/powerdns/pdns.sqlite3",
            "sha256": "8" * 64,
            "owner": "pdns:pdns",
            "mode": "0640",
            "schema_path": "/usr/share/pdns-backend-sqlite3/schema/schema.sqlite3.sql",
            "schema_sha256": "9" * 64,
            "quick_check": "ok",
            "sidecars": {
                "rollback_journal": {
                    "path": "/var/lib/powerdns/pdns.sqlite3-journal",
                    "status": "absent",
                },
                "write_ahead_log": {
                    "path": "/var/lib/powerdns/pdns.sqlite3-wal",
                    "file_type": "regular",
                    "owner": "pdns:pdns",
                    "mode": "0640",
                    "link_count": 1,
                    "device": 3,
                    "inode": 8,
                    "size": 0,
                    "content_policy": "empty",
                },
                "shared_memory": {
                    "path": "/var/lib/powerdns/pdns.sqlite3-shm",
                    "file_type": "regular",
                    "owner": "pdns:pdns",
                    "mode": "0640",
                    "link_count": 1,
                    "device": 3,
                    "inode": 9,
                    "size": 32768,
                    "content_policy": "volatile-unhashed",
                },
            },
        },
        "source_unit_after_adoption": {
            "name": "pdns.service",
            "load_state": "loaded",
            "active_state": "active",
            "sub_state": "running",
            "unit_file_state": "enabled",
        },
        "production_receipts": {
            "state_sha256": "a" * 64,
            "active_ownership_sha256": "a" * 64,
            "source_install_ownership_absent": True,
            "measured_target_ownership_absent": True,
            "measured_target_install_ownership_absent": True,
            "switch_journal_absent": True,
        },
        "external_artifacts_unchanged_by_adoption": True,
    }


def source_normalization_zone() -> dict[str, object]:
    return {
        "ordinal": 0,
        "domain": "s1-kill.test",
        "desired_generation": 1,
        "delete": False,
        "zone_type": "NATIVE",
        "records": [
            {
                "name": "s1-kill.test",
                "type": "SOA",
                "content": (
                    "ns1.s1-kill.test hostmaster.s1-kill.test "
                    "2026083101 10800 3600 604800 3600"
                ),
                "ttl": 3600,
                "prio": 0,
                "disabled": False,
            },
            {
                "name": "s1-kill.test",
                "type": "NS",
                "content": "ns1.s1-kill.test",
                "ttl": 3600,
                "prio": 0,
                "disabled": False,
            },
            {
                "name": "ns1.s1-kill.test",
                "type": "A",
                "content": "192.0.2.10",
                "ttl": 300,
                "prio": 0,
                "disabled": False,
            },
            {
                "name": "www.s1-kill.test",
                "type": "A",
                "content": "192.0.2.10",
                "ttl": 300,
                "prio": 0,
                "disabled": False,
            },
        ],
        "zone_qualifier": "",
    }


def external_pdns_scenario() -> dict[str, object]:
    return {
        "schema": "celikpanel-dns-kill-matrix-trigger/v1",
        "driver": "pdns-adopt",
        "source_fixture": "external-pdns-adoption",
        "mode": "adopt",
        "source_engine": "",
        "target_engine": "pdns",
        "source_epoch": 0,
        "target_epoch": 1,
        "source_revision": 0,
        "topology": "standalone",
        "zones": [source_normalization_zone()],
    }


def external_pdns_preinstall_evidence(selected: object) -> dict[str, object]:
    value = source_preinstall_value(selected)
    value["scope"] = (
        "external-pdns-source-preparation-for-measured-adoption-only"
    )
    value["measured_target_packages"] = [
        {
            "name": "pdns-backend-sqlite3",
            "status": "preexisting-required-by-adoption",
        },
        {
            "name": "pdns-server",
            "status": "preexisting-required-by-adoption",
        },
    ]
    value["sha256"] = "d" * 64
    return value


def external_pdns_preimage_value(
    selected: object,
    scenario: dict[str, object],
    preinstall: dict[str, object],
    *,
    state_dir: str = os.path.abspath("external-pdns-state"),
    address: str = "192.0.2.10",
) -> dict[str, object]:
    return {
        "schema": run_cell.EXTERNAL_PDNS_PREIMAGE_SCHEMA,
        "cell_id": selected.cell_id,
        "scope": "external-pdns-measured-adoption-preimage",
        "source_fixture": "external-pdns-adoption",
        "construction_origin": "harness-external-pdns",
        "production_adoption_driver": "pdns-adopt",
        "production_adoption_pending": True,
        "scenario_sha256": "c" * 64,
        "source_preinstall_proof_path": run_cell.SOURCE_PREINSTALL_PROOF_PATH,
        "source_preinstall_proof_sha256": preinstall["sha256"],
        "source_packages": preinstall["source_packages"],
        "main_config": {
            "path": "/etc/powerdns/pdns.conf",
            "sha256": "1" * 64,
            "owner": "root:pdns",
            "mode": "0640",
        },
        "managed_config": {
            "path": "/etc/powerdns/pdns.d/celikpanel.conf",
            "sha256": "2" * 64,
            "owner": "root:root",
            "mode": "0644",
        },
        "cluster_config": {
            "path": "/etc/powerdns/pdns.d/celikpanel-cluster.conf",
            "status": "absent",
        },
        "database": {
            "path": "/var/lib/powerdns/pdns.sqlite3",
            "sha256": "3" * 64,
            "owner": "pdns:pdns",
            "mode": "0640",
            "schema_path": "/usr/share/pdns-backend-sqlite3/schema/schema.sqlite3.sql",
            "schema_sha256": "4" * 64,
            "quick_check": "ok",
            "journal_mode": "wal",
            "zone_snapshot_sha256": run_cell._external_pdns_zone_snapshot_sha256(
                scenario
            ),
            "domain_count": 1,
            "record_count": 4,
            "auxiliary_authority_count": 0,
            "sidecars": {
                "rollback_journal": {
                    "path": "/var/lib/powerdns/pdns.sqlite3-journal",
                    "status": "absent",
                },
                "write_ahead_log": {
                    "path": "/var/lib/powerdns/pdns.sqlite3-wal",
                    "file_type": "regular",
                    "owner": "pdns:pdns",
                    "mode": "0640",
                    "link_count": 1,
                    "device": 7,
                    "inode": 11,
                    "size": 0,
                    "content_policy": "empty",
                },
                "shared_memory": {
                    "path": "/var/lib/powerdns/pdns.sqlite3-shm",
                    "file_type": "regular",
                    "owner": "pdns:pdns",
                    "mode": "0640",
                    "link_count": 1,
                    "device": 7,
                    "inode": 12,
                    "size": 32768,
                    "content_policy": "volatile-unhashed",
                },
            },
        },
        "source_unit_before_tagged_agent": {
            "name": "pdns.service",
            "load_state": "loaded",
            "active_state": "active",
            "sub_state": "running",
            "unit_file_state": "enabled",
        },
        "authoritative_preflight": {
            "claimed": True,
            "address": address,
            "port": 53,
            "name": "www.s1-kill.test",
            "type": "A",
            "udp": True,
            "tcp": True,
        },
        "production_receipts_absent": run_cell._external_pdns_receipt_paths(
            state_dir
        ),
    }


class ControllerProtocolTest(unittest.TestCase):
    def test_tagged_environment_has_exact_eight_selectors(self) -> None:
        selected = cell()
        base = {"PATH": "/usr/bin", "LANG": "C.UTF-8", "KEEP": "no"}
        tagged = run_cell.tagged_agent_environment(
            base,
            selected,
            "1" * 32,
            "a" * 32,
            os.path.abspath("marker.json"),
            9,
            os.path.abspath("state"),
            os.path.abspath("mutation.lock"),
            os.path.abspath("agent.sock"),
            os.path.abspath("agent.token"),
        )
        selectors = {
            name: value
            for name, value in tagged.items()
            if name.startswith(run_cell.SELECTOR_PREFIX)
        }
        self.assertEqual(set(selectors), set(run_cell.SELECTOR_NAMES))
        self.assertEqual(len(selectors), 8)
        self.assertEqual(selectors[run_cell.SELECTOR_NAMES[0]], selected.cell_id)
        self.assertEqual(selectors[run_cell.SELECTOR_NAMES[7]], "9")
        self.assertNotIn("KEEP", tagged)
        self.assertEqual(tagged["PATH"], "/usr/bin")
        self.assertEqual(
            tagged["CELIKPANEL_AGENT_TOKEN_FILE"], os.path.abspath("agent.token")
        )

        ordinary = run_cell.ordinary_environment(
            base,
            os.path.abspath("state"),
            os.path.abspath("mutation.lock"),
            os.path.abspath("agent.sock"),
            os.path.abspath("agent.token"),
            cell=selected,
            request_id="1" * 32,
            nonce="a" * 32,
            proof_path=os.path.abspath("proof.json"),
        )
        self.assertFalse(
            any(name.startswith(run_cell.SELECTOR_PREFIX) for name in ordinary)
        )
        self.assertNotIn(run_cell.EXTERNAL_LOCK_FD_ENV, ordinary)
        self.assertEqual(ordinary["CELIKPANEL_S1_CELL_ID"], selected.cell_id)
        with self.assertRaises(run_cell.ControllerError):
            run_cell.tagged_agent_environment(
                {run_cell.SELECTOR_NAMES[0]: "stale"},
                selected,
                "1" * 32,
                "a" * 32,
                os.path.abspath("marker.json"),
                9,
                os.path.abspath("state"),
                os.path.abspath("mutation.lock"),
                os.path.abspath("agent.sock"),
                os.path.abspath("agent.token"),
            )

    def test_startup_mode_is_narrowly_gated_and_has_no_trigger_command(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            executable = os.path.realpath(sys.executable)
            settings = run_cell.Settings(
                cell=cell(
                    "rolled-back",
                    "before-write",
                    driver="signed-update-finalize",
                ),
                request_id="1" * 32,
                nonce="a" * 32,
                tagged_agent_command=(
                    executable,
                    "--prepare-bind-generation-root-under-external-lock",
                ),
                trigger_mode="startup",
                trigger_command=None,
                recovery_command=(
                    executable,
                    "--prepare-bind-generation-root-under-external-lock",
                ),
                source_proof_path=None,
                agent_restart_command=(executable, "-V"),
                panel_restart_command=(executable, "-V"),
                recovery_probe_command=(executable, "-V"),
                peer_partition_command=None,
                command_cwd=root,
                state_dir=root,
                mutation_lock=os.path.join(root, "mutation.lock"),
                agent_socket=os.path.join(root, "agent.sock"),
                agent_token_file=os.path.join(root, "agent.token"),
                journal_path=os.path.join(root, "journal.json"),
                marker_path=os.path.join(root, "marker.json"),
                proof_path=os.path.join(root, "proof.json"),
                result_path=os.path.join(root, "result.json"),
                transcript_path=os.path.join(root, "transcript.log"),
                dns_address="127.0.0.1",
                dns_port=53,
                dns_name="matrix.test.",
                dns_type="SOA",
                panel_address="127.0.0.1",
                panel_port=8080,
                startup_timeout=1,
                boundary_timeout=1,
                stop_timeout=1,
                kill_timeout=1,
                command_timeout=1,
                recovery_timeout=1,
                endpoint_timeout=1,
                dns_timeout=1,
                stability_seconds=1,
                stability_interval=1,
            )
            evidence = run_cell.validate_settings(settings)
            self.assertNotIn("scenario_trigger", evidence)
            self.assertIn("recovery", evidence)
            with self.assertRaises(run_cell.ControllerError):
                run_cell.validate_settings(
                    replace(settings, recovery_command=(executable, "-V"))
                )
            with self.assertRaises(run_cell.ControllerError):
                run_cell.validate_settings(
                    replace(settings, trigger_mode="socket")
                )
            with self.assertRaises(run_cell.ControllerError):
                run_cell.validate_settings(
                    replace(settings, trigger_command=(executable, "-V"))
                )

    def test_socket_retry_reuses_exact_trigger_identity_contract(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            executable = os.path.realpath(sys.executable)
            scenario = os.path.join(root, "scenario.json")
            receipt = os.path.join(root, "identity.json")
            trigger = (
                executable,
                "rpc-switch",
                "--scenario",
                scenario,
                "--identity-receipt",
                receipt,
                "--timeout",
                "45m",
            )
            retry = (executable, "rpc-retry", *trigger[2:])
            contract = run_cell.socket_trigger_retry_contract(trigger, retry)
            self.assertEqual(contract["scenario_path"], scenario)
            self.assertEqual(contract["identity_receipt_path"], receipt)
            self.assertEqual(contract["operation_timeout"], "45m")
            for changed in (
                (executable, "rpc-switch", *retry[2:]),
                (*retry[:-1], "44m"),
                (*retry[:5], os.path.join(root, "other.json"), *retry[6:]),
            ):
                with self.assertRaises(run_cell.ControllerError):
                    run_cell.socket_trigger_retry_contract(trigger, changed)

    def test_trigger_receipt_binds_exact_deterministic_owner(self) -> None:
        selected = cell()
        request_id = "1" * 32
        owner_id = run_cell.deterministic_trigger_owner(
            selected.cell_id, request_id
        )
        self.assertEqual(owner_id, "06391d7b9a5c99111341e44d0dcaf893")
        receipt = {
            "schema": run_cell.TRIGGER_IDENTITY_RECEIPT_SCHEMA,
            "cell_id": selected.cell_id,
            "driver": selected.driver,
            "source_fixture": "bind-running",
            "request_id": request_id,
            "owner_id": owner_id,
            "manifest_qualifier": "dns-engine-switch/v1:sha256:" + "2" * 64,
        }
        status = mock.Mock(st_dev=10, st_ino=20, st_mode=0o100600)
        with mock.patch.object(
            run_cell, "secure_read_json", return_value=(receipt, status)
        ), mock.patch.object(
            run_cell, "sha256_file", return_value="3" * 64
        ), mock.patch.object(run_cell.os, "geteuid", return_value=0, create=True):
            observed = run_cell.validate_trigger_identity_receipt(
                os.path.abspath("identity.json"), selected, request_id
            )
            self.assertEqual(observed["owner_id"], owner_id)
            poisoned = dict(receipt)
            poisoned["owner_id"] = "4" * 32
            with mock.patch.object(
                run_cell, "secure_read_json", return_value=(poisoned, status)
            ):
                with self.assertRaises(run_cell.ControllerError):
                    run_cell.validate_trigger_identity_receipt(
                        os.path.abspath("identity.json"), selected, request_id
                    )

    def test_recovery_precedes_final_liveness_in_controller_source(self) -> None:
        source = MODULE_PATH.read_text(encoding="utf-8")
        flow = source[source.index("def run_cell(settings:") :]
        self.assertGreaterEqual(flow.count("for ordinal in (1, 2):"), 2)
        self.assertLess(
            flow.index('f"startup-recovery-attempt-{ordinal}"'),
            flow.index("run_recovery_probe(settings, ordinary, transcript, ordinal)"),
        )
        self.assertLess(
            flow.index("run_recovery_probe(settings, ordinary, transcript, ordinal)"),
            flow.index('"external-lock-released-after-recovery"'),
        )
        socket_flow = flow[flow.index('if settings.trigger_mode == "socket":', flow.index('recovery["agent_ready"]')) :]
        self.assertLess(
            flow.index('"agent-ready-for-recovery"'),
            flow.index('recovery["agent_ready"]'),
        )
        self.assertLess(
            socket_flow.index('f"post-restart-rpc-retry-{ordinal}"'),
            socket_flow.index(
                "run_recovery_probe(settings, ordinary, transcript, ordinal)"
            ),
        )
        self.assertLess(
            socket_flow.index(
                "run_recovery_probe(settings, ordinary, transcript, ordinal)"
            ),
            socket_flow.index('attempt["peer_after_probe"]'),
        )
        self.assertLess(
            socket_flow.index('attempt["peer_after_probe"]'),
            socket_flow.index('"panel-restart"'),
        )
        self.assertLess(
            socket_flow.index('"panel-restart"'),
            socket_flow.index('"agent-after-restart"'),
        )
        stability_flow = source[
            source.index("def run_stability_window(") : source.index(
                "def run_cell(settings:"
            )
        ]
        self.assertIn("observe_peer_once(", stability_flow)

        self.assertLess(
            flow.index('"trigger-identity-receipt-proven-before-kill"'),
            flow.index("marker = validate_marker("),
        )
        self.assertLess(
            flow.index("marker = validate_marker("),
            flow.index("kill = kill_exact_stopped_child("),
        )
        self.assertLess(
            flow.index("kill = kill_exact_stopped_child("),
            flow.index("atomic_write_new_json(settings.proof_path, proof)"),
        )

    def test_status_buckets_preserve_safety_and_verification_meaning(self) -> None:
        self.assertEqual(run_cell.classify_cell_status([], []), ("passed", "passed"))
        self.assertEqual(
            run_cell.classify_cell_status([], ["peer dimension drifted"]),
            ("passed", "unverified"),
        )
        self.assertEqual(
            run_cell.classify_cell_status(["DNS not serving"], []),
            ("failed", "failed"),
        )
        self.assertEqual(
            run_cell.classify_cell_status(
                ["DNS not serving"], ["peer dimension drifted"]
            ),
            ("failed", "unverified"),
        )

    def test_expected_journal_state_covers_every_edge_shape(self) -> None:
        self.assertIsNone(
            run_cell.expected_journal_phase(cell("pre-intent", "window"))
        )
        self.assertIsNone(
            run_cell.expected_journal_phase(cell("intent", "before-write"))
        )
        self.assertEqual(
            run_cell.expected_journal_phase(cell("source-stopped", "before-write")),
            "target-staged",
        )
        self.assertEqual(
            run_cell.expected_journal_phase(cell("target-started", "after-write")),
            "target-started",
        )
        self.assertEqual(
            run_cell.expected_journal_phase(
                cell(
                    "target-verified",
                    "before-write",
                    driver="pdns-adopt",
                )
            ),
            "intent",
        )
        self.assertEqual(
            run_cell.expected_journal_phase(cell("rolled-back", "before-write")),
            "rolling-back",
        )
        for driver, predecessor in run_cell.ROLLBACK_PRECURSOR_PHASES.items():
            with self.subTest(driver=driver):
                self.assertEqual(
                    run_cell.expected_journal_phase(
                        cell("rolling-back", "before-write", driver=driver)
                    ),
                    predecessor,
                )
                self.assertEqual(
                    run_cell.expected_journal_phase(
                        cell("rolling-back", "after-write", driver=driver)
                    ),
                    "rolling-back",
                )
                self.assertEqual(
                    run_cell.expected_journal_phase(
                        cell("rolled-back", "before-write", driver=driver)
                    ),
                    "rolling-back",
                )
                self.assertEqual(
                    run_cell.expected_journal_phase(
                        cell("rolled-back", "after-write", driver=driver)
                    ),
                    "rolled-back",
                )
        with self.assertRaises(run_cell.ControllerError):
            run_cell.expected_journal_phase(
                cell(
                    "rolling-back",
                    "before-write",
                    driver="signed-update-finalize",
                )
            )
        self.assertNotIn(
            "--expected-journal-phase",
            run_cell.build_argument_parser()._option_string_actions,
        )

    def test_proc_stat_parser_handles_spaces_and_parentheses(self) -> None:
        fields = ["T"] + ["0"] * 18 + ["424242"] + ["0"] * 4
        parsed = run_cell.parse_proc_stat(
            "321 (agent worker ) name) " + " ".join(fields)
        )
        self.assertEqual(parsed.pid, 321)
        self.assertEqual(parsed.state, "T")
        self.assertEqual(parsed.start_ticks, "424242")
        with self.assertRaises(run_cell.ControllerError):
            run_cell.parse_proc_stat("321 (truncated) S 0")

    def test_exit_137_normalization_is_exact(self) -> None:
        self.assertEqual(run_cell.normalize_wait_exit(-9), 137)
        self.assertEqual(run_cell.normalize_wait_exit(137), 137)
        self.assertNotEqual(run_cell.normalize_wait_exit(-15), 137)

    def test_ready_wait_fails_fast_when_socket_trigger_exits(self) -> None:
        agent = mock.Mock()
        agent.poll.return_value = None
        trigger = mock.Mock()
        trigger.poll.return_value = 23
        with mock.patch.object(
            run_cell.select, "select", return_value=([], [], [])
        ):
            with self.assertRaises(run_cell.TriggerExitedBeforeBoundary) as caught:
                run_cell.read_ready_nonce(
                    7,
                    "a" * 32,
                    30,
                    agent,
                    trigger=trigger,
                )
        self.assertEqual(caught.exception.raw_returncode, 23)
        self.assertEqual(caught.exception.exit_code, 23)

    def test_ready_notification_wins_over_simultaneous_trigger_exit(self) -> None:
        read_fd, write_fd = os.pipe()
        expected = ("a" * 32 + "\n").encode("ascii")
        try:
            os.write(write_fd, expected)
            os.close(write_fd)
            write_fd = -1
            agent = mock.Mock()
            agent.poll.return_value = None
            trigger = mock.Mock()
            trigger.poll.return_value = 23
            with mock.patch.object(
                run_cell.select, "select", return_value=([read_fd], [], [])
            ):
                observed = run_cell.read_ready_nonce(
                    read_fd,
                    "a" * 32,
                    30,
                    agent,
                    trigger=trigger,
                )
            self.assertEqual(observed, expected)
            trigger.poll.assert_not_called()
        finally:
            os.close(read_fd)
            if write_fd >= 0:
                os.close(write_fd)

    def test_early_trigger_exit_is_durable_and_requests_safe_cleanup(self) -> None:
        trigger = mock.Mock()
        trigger.wait.return_value = 17
        trigger.returncode = 17
        tagged = mock.Mock(pid=321)
        transcript = mock.Mock()
        result: dict[str, object] = {}
        error = run_cell.TriggerExitedBeforeBoundary(17)
        with mock.patch.object(run_cell, "cleanup_child") as cleanup:
            run_cell.handle_trigger_exit_before_boundary(
                trigger,
                tagged,
                "424242",
                error,
                result,
                transcript,
            )
        self.assertEqual(result["scenario_trigger_returncode"], 17)
        report = result["trigger_exit_before_boundary"]
        self.assertEqual(report["raw_returncode"], 17)
        self.assertEqual(report["exit_code"], 17)
        self.assertEqual(report["reason"], str(error))
        self.assertTrue(report["tagged_agent_cleanup_requested"])
        cleanup.assert_called_once_with(
            tagged, "424242", transcript, "tagged-agent"
        )
        transcript.event.assert_any_call(
            "command-finish", label="scenario-trigger", returncode=17
        )
        transcript.event.assert_any_call(
            "scenario-trigger-exited-before-boundary", **report
        )

    def test_duplicate_json_keys_fail_closed(self) -> None:
        with self.assertRaises(run_cell.ControllerError):
            run_cell.decode_json(b'{"phase":"intent","phase":"committed"}', "test")

    def test_marker_requires_runtime_observed_driver(self) -> None:
        selected = cell()
        marker_path = os.path.abspath("marker.json")
        journal_path = os.path.abspath("journal.json")
        marker = {
            "schema": run_cell.MARKER_SCHEMA,
            "cell_id": selected.cell_id,
            "driver": selected.driver,
            "point": selected.point,
            "phase": selected.phase,
            "request_id": "1" * 32,
            "nonce": "a" * 32,
            "marker": marker_path,
            "ready_fd": 9,
            "pid": 123,
            "process_start_ticks": "456",
            "recorded_at": "2026-08-31T00:00:00Z",
            "observed_journal": {
                "path": journal_path,
                "schema": run_cell.JOURNAL_SCHEMA,
                "phase": "intent",
                "mode": "switch",
                "mutation_request_id": "1" * 32,
                "mutation_owner_id": "2" * 32,
                "manifest_qualifier": "dns-engine-switch/v1:sha256:" + "3" * 64,
                "source_engine": "pdns",
                "target_engine": "bind",
                "source_epoch": 4,
                "target_epoch": 5,
                "source_revision": 6,
                "topology": "standalone",
            },
        }
        identity = boundary_identity()
        with mock.patch.object(
            run_cell, "secure_read_json", return_value=(marker, None)
        ), mock.patch.object(run_cell.os, "geteuid", return_value=0, create=True):
            with self.assertRaises(run_cell.BoundaryUnverified):
                run_cell.validate_marker(
                    marker_path,
                    selected,
                    "1" * 32,
                    "a" * 32,
                    9,
                    123,
                    "456",
                    journal_path,
                    identity,
                )
            marker["observed_driver"] = selected.driver
            validated = run_cell.validate_marker(
                marker_path,
                selected,
                "1" * 32,
                "a" * 32,
                9,
                123,
                "456",
                journal_path,
                identity,
            )
        self.assertEqual(validated["observed_driver"], selected.driver)

        for field in run_cell.BOUNDARY_JOURNAL_IDENTITY_FIELDS:
            poisoned_marker = json.loads(json.dumps(marker))
            current = poisoned_marker["observed_journal"].get(field, "")
            poisoned_marker["observed_journal"][field] = (
                current + "wrong" if isinstance(current, str) else current + 1
            )
            with mock.patch.object(
                run_cell,
                "secure_read_json",
                return_value=(poisoned_marker, None),
            ), mock.patch.object(
                run_cell.os, "geteuid", return_value=0, create=True
            ):
                with self.assertRaises(run_cell.BoundaryUnverified, msg=field):
                    run_cell.validate_marker(
                        marker_path,
                        selected,
                        "1" * 32,
                        "a" * 32,
                        9,
                        123,
                        "456",
                        journal_path,
                        identity,
                    )

    def test_rollback_marker_requires_one_exact_precursor_for_all_drivers(self) -> None:
        marker_path = os.path.abspath("marker.json")
        journal_path = os.path.abspath("journal.json")
        for driver in run_cell.ROLLBACK_PRECURSOR_PHASES:
            for phase in ("rolling-back", "rolled-back"):
                for edge in ("before-write", "after-write"):
                    with self.subTest(driver=driver, phase=phase, edge=edge):
                        selected = cell(phase, edge, driver=driver)
                        identity = boundary_identity_for_driver(driver)
                        marker = marker_value(
                            selected, marker_path, journal_path, identity
                        )
                        add_rollback_precursor(
                            marker, selected, journal_path, identity
                        )
                        with mock.patch.object(
                            run_cell,
                            "secure_read_json",
                            return_value=(marker, None),
                        ), mock.patch.object(
                            run_cell.os, "geteuid", return_value=0, create=True
                        ):
                            validated = run_cell.validate_marker(
                                marker_path,
                                selected,
                                "1" * 32,
                                "a" * 32,
                                9,
                                123,
                                "456",
                                journal_path,
                                identity,
                            )
                        self.assertEqual(
                            validated["rollback_precursor"]["phase"],
                            run_cell.ROLLBACK_PRECURSOR_PHASES[driver],
                        )

    def test_manifest_has_exactly_64_non_signed_rollback_precursor_cells(self) -> None:
        manifest = json.loads(MODULE_PATH.with_name("manifest.json").read_text())
        rollback_cells = [
            raw
            for raw in manifest["cells"]
            if raw["status"] == "runnable"
            and raw["driver"] != "signed-update-finalize"
            and raw["boundary"]["phase"] in ("rolling-back", "rolled-back")
        ]
        self.assertEqual(len(rollback_cells), 64)
        for raw in rollback_cells:
            selected = run_cell.CellSpec.from_manifest(manifest, raw["id"])
            with self.subTest(cell_id=selected.cell_id):
                self.assertEqual(
                    run_cell.rollback_precursor_phase(selected),
                    run_cell.ROLLBACK_PRECURSOR_PHASES[selected.driver],
                )

    def test_rollback_marker_fails_closed_without_precursor(self) -> None:
        selected = cell("rolling-back", "before-write")
        marker_path = os.path.abspath("marker.json")
        journal_path = os.path.abspath("journal.json")
        identity = boundary_identity_for_driver(selected.driver)
        marker = marker_value(selected, marker_path, journal_path, identity)
        with mock.patch.object(
            run_cell, "secure_read_json", return_value=(marker, None)
        ), mock.patch.object(run_cell.os, "geteuid", return_value=0, create=True):
            with self.assertRaises(run_cell.BoundaryUnverified):
                run_cell.validate_marker(
                    marker_path,
                    selected,
                    "1" * 32,
                    "a" * 32,
                    9,
                    123,
                    "456",
                    journal_path,
                    identity,
                )

    def test_forward_and_signed_markers_forbid_rollback_precursor_field(self) -> None:
        marker_path = os.path.abspath("marker.json")
        journal_path = os.path.abspath("journal.json")
        selected_cells = (
            cell("intent", "after-write", driver="bind"),
            cell(
                "rolled-back",
                "before-write",
                driver="signed-update-finalize",
            ),
        )
        for selected in selected_cells:
            with self.subTest(driver=selected.driver, phase=selected.phase):
                identity = boundary_identity_for_driver(selected.driver)
                marker = marker_value(
                    selected, marker_path, journal_path, identity
                )
                marker["rollback_precursor"] = None
                with mock.patch.object(
                    run_cell,
                    "secure_read_json",
                    return_value=(marker, None),
                ), mock.patch.object(
                    run_cell.os, "geteuid", return_value=0, create=True
                ):
                    with self.assertRaises(run_cell.BoundaryUnverified):
                        run_cell.validate_marker(
                            marker_path,
                            selected,
                            "1" * 32,
                            "a" * 32,
                            9,
                            123,
                            "456",
                            journal_path,
                            identity,
                        )

    def test_rollback_precursor_top_level_fields_are_exact_and_bound(self) -> None:
        selected = cell("rolled-back", "after-write", driver="pdns-adopt")
        marker_path = os.path.abspath("marker.json")
        journal_path = os.path.abspath("journal.json")
        identity = boundary_identity_for_driver(selected.driver)
        base = marker_value(selected, marker_path, journal_path, identity)
        add_rollback_precursor(base, selected, journal_path, identity)
        mutations = {
            "schema": "wrong-schema",
            "driver": "bind",
            "observed_driver": "bind",
            "point": "before_write",
            "phase": "target-staged",
            "request_id": "f" * 32,
            "action": "continued",
        }
        for field, wrong in mutations.items():
            with self.subTest(field=field):
                marker = json.loads(json.dumps(base))
                marker["rollback_precursor"][field] = wrong
                with mock.patch.object(
                    run_cell,
                    "secure_read_json",
                    return_value=(marker, None),
                ), mock.patch.object(
                    run_cell.os, "geteuid", return_value=0, create=True
                ):
                    with self.assertRaises(run_cell.BoundaryUnverified):
                        run_cell.validate_marker(
                            marker_path,
                            selected,
                            "1" * 32,
                            "a" * 32,
                            9,
                            123,
                            "456",
                            journal_path,
                            identity,
                        )

    def test_rollback_precursor_observed_journal_is_exact_and_bound(self) -> None:
        selected = cell("rolling-back", "before-write", driver="bind")
        marker_path = os.path.abspath("marker.json")
        journal_path = os.path.abspath("journal.json")
        identity = boundary_identity_for_driver(selected.driver)
        base = marker_value(selected, marker_path, journal_path, identity)
        add_rollback_precursor(base, selected, journal_path, identity)
        wrong_values = {
            "path": os.path.abspath("other-journal.json"),
            "schema": "wrong-schema",
            "phase": "intent",
            "mutation_request_id": "f" * 32,
        }
        for field in run_cell.BOUNDARY_JOURNAL_IDENTITY_FIELDS:
            current = base["rollback_precursor"]["observed_journal"].get(
                field, ""
            )
            wrong_values[field] = (
                current + "wrong" if isinstance(current, str) else current + 1
            )
        for field, wrong in wrong_values.items():
            with self.subTest(field=field):
                marker = json.loads(json.dumps(base))
                marker["rollback_precursor"]["observed_journal"][field] = wrong
                with mock.patch.object(
                    run_cell,
                    "secure_read_json",
                    return_value=(marker, None),
                ), mock.patch.object(
                    run_cell.os, "geteuid", return_value=0, create=True
                ):
                    with self.assertRaises(run_cell.BoundaryUnverified):
                        run_cell.validate_marker(
                            marker_path,
                            selected,
                            "1" * 32,
                            "a" * 32,
                            9,
                            123,
                            "456",
                            journal_path,
                            identity,
                        )

    def test_rollback_precursor_rejects_impossible_or_nonexact_shapes(self) -> None:
        selected = cell("rolling-back", "after-write")
        marker_path = os.path.abspath("marker.json")
        journal_path = os.path.abspath("journal.json")
        identity = boundary_identity_for_driver(selected.driver)
        base = marker_value(selected, marker_path, journal_path, identity)
        add_rollback_precursor(base, selected, journal_path, identity)
        poisoned_markers: list[dict[str, object]] = []
        duplicate_list = json.loads(json.dumps(base))
        duplicate_list["rollback_precursor"] = [
            base["rollback_precursor"],
            base["rollback_precursor"],
        ]
        poisoned_markers.append(duplicate_list)
        for container, key in (
            ("rollback_precursor", "unexpected"),
            ("observed_journal", "unexpected"),
        ):
            marker = json.loads(json.dumps(base))
            target = marker["rollback_precursor"]
            if container == "observed_journal":
                target = target["observed_journal"]
            target[key] = True
            poisoned_markers.append(marker)
        missing = json.loads(json.dumps(base))
        del missing["rollback_precursor"]["action"]
        poisoned_markers.append(missing)
        for marker in poisoned_markers:
            with mock.patch.object(
                run_cell, "secure_read_json", return_value=(marker, None)
            ), mock.patch.object(
                run_cell.os, "geteuid", return_value=0, create=True
            ):
                with self.assertRaises(run_cell.BoundaryUnverified):
                    run_cell.validate_marker(
                        marker_path,
                        selected,
                        "1" * 32,
                        "a" * 32,
                        9,
                        123,
                        "456",
                        journal_path,
                        identity,
                    )
        with self.assertRaises(run_cell.ControllerError):
            run_cell.decode_json(
                b'{"rollback_precursor":{},"rollback_precursor":{}}',
                "duplicate precursor",
            )

    def test_socket_boundary_identity_requires_exact_receipt_provenance(self) -> None:
        identity_path = os.path.abspath("measured/identity.json")
        source_proof = {
            "source_fixture": "managed-pdns",
            "identity_receipt_path": identity_path,
            "scenario_identity": {
                key: value
                for key, value in boundary_identity().items()
                if key not in {"mutation_owner_id", "manifest_qualifier"}
            },
        }
        receipt = {
            "source_fixture": "managed-pdns",
            "path": identity_path,
            "owner_id": "2" * 32,
            "manifest_qualifier": "dns-engine-switch/v1:sha256:" + "3" * 64,
        }
        self.assertEqual(
            run_cell.validate_socket_boundary_identity(source_proof, receipt),
            boundary_identity(),
        )
        for poisoned in (
            dict(receipt, source_fixture="uninitialized"),
            dict(receipt, path=os.path.abspath("measured/other.json")),
        ):
            with self.assertRaises(run_cell.BoundaryUnverified):
                run_cell.validate_socket_boundary_identity(source_proof, poisoned)

    def test_recovery_probe_must_converge_twice_to_same_fingerprint(self) -> None:
        output = json.dumps(
            {
                "schema": run_cell.RECOVERY_PROBE_SCHEMA,
                "converged": True,
                "recovery_outcome": "target_converged",
                "active_dns_engine": "bind",
                "fingerprint": "1" * 64,
                "detail": "converged",
            }
        ).encode()
        command = run_cell.CommandResult(("probe",), 0, output, False, 0.1)
        first = run_cell.decode_recovery_probe(command, 1)
        second = run_cell.decode_recovery_probe(command, 2)
        self.assertEqual(run_cell.assess_recovery_probes(first, second), [])
        second["fingerprint"] = "2" * 64
        self.assertIn(
            "recovery fingerprint changed on the second probe",
            run_cell.assess_recovery_probes(first, second),
        )
        second["converged"] = False
        self.assertIn(
            "second recovery probe did not converge",
            run_cell.assess_recovery_probes(first, second),
        )

        rolled_first = dict(first)
        rolled_second = dict(first)
        for probe in (rolled_first, rolled_second):
            probe["converged"] = False
            probe["recovery_outcome"] = "rolled_back_source_active"
            probe["active_dns_engine"] = "pdns"
        summary = run_cell.summarize_recovery_outcome(
            rolled_first, rolled_second, True
        )
        self.assertEqual(summary["classification"], "rolled_back_source_serving")
        self.assertTrue(summary["rolled_back_source_serving"])
        self.assertFalse(summary["target_converged"])

    def test_uninitialized_source_proof_binds_absence_and_negative_port53(self) -> None:
        selected = cell()
        selected = replace(
            selected,
            source_fixture_policy="uninitialized-permitted-noncritical",
        )
        scenario_path = os.path.abspath("scenario.json")
        identity_path = os.path.abspath("measured/identity.json")
        state_dir = os.path.abspath("state")
        journal_path = os.path.join(state_dir, "dns-engine-switch-journal.json")
        scenario = {
            "mode": "switch",
            "source_fixture": "uninitialized",
            "source_engine": "",
            "target_engine": "bind",
            "source_epoch": 0,
            "target_epoch": 1,
            "source_revision": 0,
            "topology": "standalone",
        }
        scenario_evidence = {
            "path": scenario_path,
            "sha256": "1" * 64,
            "device": 1,
            "inode": 2,
            "mode": "0600",
        }
        proof = {
            "schema": run_cell.SOURCE_PROOF_SCHEMA,
            "cell_id": selected.cell_id,
            "source_fixture": "uninitialized",
            "scenario_sha256": "1" * 64,
            "identity_receipt_path": identity_path,
            "identity_receipt_preexisting": False,
            "engine": "",
            "engine_epoch": 0,
            "source_revision": 0,
            "serving_before_tagged_agent": False,
            "engine_state_receipt_path": "",
            "engine_state_receipt_sha256": "",
            "engine_state_identity": None,
            "authoritative_preflight": {
                "claimed": False,
                "address": "127.0.0.1",
                "port": 53,
                "name": "matrix.test",
                "type": "A",
                "udp": False,
                "tcp": False,
            },
            "uninitialized_global_port53": {
                "udp_bindable": True,
                "tcp_bindable": True,
                "authoritative_answer_observed": False,
            },
            "receipt_origin": "absent-by-proof",
            "source_setup_scenario_sha256": "absent",
            "source_setup_identity_receipt_sha256": "absent",
            "source_preinstall_proof_path": "absent",
            "source_preinstall_proof_sha256": "absent",
            "source_adoption_proof_path": "absent",
            "source_adoption_proof_sha256": "absent",
            "external_pdns_preimage_path": "absent",
            "external_pdns_preimage_sha256": "absent",
            "source_normalization_identity_receipt_path": "absent",
            "source_normalization_identity_receipt_sha256": "absent",
        }
        raw = (json.dumps(proof, indent=2, sort_keys=True) + "\n").encode()
        status = mock.Mock(st_dev=3, st_ino=4, st_mode=0o100600)
        absent: list[str] = []
        with mock.patch.object(
            run_cell,
            "validate_source_scenario",
            return_value=(scenario, scenario_evidence),
        ), mock.patch.object(
            run_cell,
            "secure_json_with_digest",
            return_value=(proof, raw, "2" * 64, status),
        ), mock.patch.object(
            run_cell,
            "require_absent_path",
            side_effect=lambda path, _label: absent.append(path),
        ):
            observed = run_cell.validate_socket_source_proof(
                os.path.abspath("source-proof.json"),
                selected,
                scenario_path,
                identity_path,
                state_dir,
                journal_path,
                "127.0.0.1",
                53,
                "matrix.test.",
                "A",
            )
        self.assertFalse(observed["serving_before_tagged_agent"])
        self.assertEqual(
            observed["scenario_identity"],
            {
                "mode": "switch",
                "source_engine": "",
                "target_engine": "bind",
                "source_epoch": 0,
                "target_epoch": 1,
                "source_revision": 0,
                "topology": "standalone",
                "pair_role": "",
            },
        )
        self.assertIn(journal_path, absent)
        for engine in ("bind", "pdns"):
            self.assertIn(
                os.path.join(state_dir, f"dns-engine-ownership-{engine}.json"),
                absent,
            )
            self.assertIn(
                os.path.join(
                    state_dir, f"dns-engine-install-ownership-{engine}.json"
                ),
                absent,
            )

    def test_managed_source_state_is_canonical_and_topology_bound(self) -> None:
        scenario = {
            "source_epoch": 3,
            "source_revision": 4,
            "topology": "standalone",
        }
        state = {
            "schema": "celikpanel-dns-engine-state/v1",
            "mode": "switch",
            "engine": "pdns",
            "engine_epoch": 3,
            "source_revision": 4,
            "manifest_qualifier": "dns-engine-switch/v1:sha256:" + "1" * 64,
            "mutation_request_id": "2" * 32,
            "mutation_owner_id": "3" * 32,
        }
        canonical = run_cell.canonical_dns_state_bytes(state)
        self.assertEqual(
            run_cell.validate_managed_source_state(
                state, canonical, scenario, "pdns"
            ),
            state,
        )
        with self.assertRaises(run_cell.ControllerError):
            run_cell.validate_managed_source_state(
                state,
                (json.dumps(state, indent=2) + "\n").encode(),
                scenario,
                "pdns",
            )
        contaminated = dict(state, pair_role="primary")
        with self.assertRaises(run_cell.ControllerError):
            run_cell.validate_managed_source_state(
                contaminated,
                run_cell.canonical_dns_state_bytes(contaminated),
                scenario,
                "pdns",
            )

        paired_scenario = dict(
            scenario,
            topology="paired",
            pair_role="primary",
            local_ip="192.0.2.10",
            peer_ip="192.0.2.11",
        )
        paired = dict(
            state,
            pair_role="primary",
            pair_local_ip="192.0.2.10",
            pair_peer_ip="192.0.2.11",
            primary_catalog_serial=7,
        )
        self.assertEqual(
            run_cell.validate_managed_source_state(
                paired,
                run_cell.canonical_dns_state_bytes(paired),
                paired_scenario,
                "pdns",
            ),
            paired,
        )

        source_provenance = {
            "receipt_origin": "production-pdns-adopt-normalized",
            "source_setup_scenario_sha256": "4" * 64,
            "source_setup_identity_receipt_sha256": "5" * 64,
        }
        self.assertEqual(
            run_cell.validate_source_setup_provenance(
                source_provenance, "managed-pdns"
            ),
            source_provenance,
        )
        with self.assertRaises(run_cell.ControllerError):
            run_cell.validate_source_setup_provenance(
                dict(source_provenance, receipt_origin="claimed-by-test"),
                "managed-pdns",
            )
        with self.assertRaises(run_cell.ControllerError):
            run_cell.validate_source_setup_provenance(
                source_provenance, "managed-bind"
            )

    def test_source_preinstall_document_rejects_every_identity_drift(self) -> None:
        selected = cell("source-stopped")
        value = source_preinstall_value(selected)
        canonical = (json.dumps(value, indent=2, sort_keys=True) + "\n").encode()
        self.assertEqual(
            run_cell.validate_source_preinstall_document(value, canonical, selected),
            value,
        )
        changed = []
        current = json.loads(json.dumps(value))
        current["unexpected"] = True
        changed.append(current)
        changed.append(dict(value, cell_id="wrong-cell"))
        changed.append(dict(value, scope="unbounded"))
        current = json.loads(json.dumps(value))
        current["measured_target_packages"][0]["status"] = "install ok installed"
        changed.append(current)
        current = json.loads(json.dumps(value))
        current["source_unit_before_external_configuration"]["active_state"] = "active"
        changed.append(current)
        current = json.loads(json.dumps(value))
        current["source_packages"][0]["version"] = ""
        changed.append(current)
        for tampered in changed:
            with self.subTest(tampered=tampered), self.assertRaises(
                run_cell.ControllerError
            ):
                run_cell.validate_source_preinstall_document(
                    tampered,
                    (json.dumps(tampered, indent=2, sort_keys=True) + "\n").encode(),
                    selected,
                )
        with self.assertRaises(run_cell.ControllerError):
            run_cell.validate_source_preinstall_document(
                value, json.dumps(value).encode(), selected
            )
        with self.assertRaises(run_cell.ControllerError):
            run_cell.validate_source_preinstall_document(
                value, canonical, cell("committed")
            )

    def test_source_preinstall_path_hash_missing_and_absent_contracts(self) -> None:
        selected = cell("source-stopped")
        value = source_preinstall_value(selected)
        raw = (json.dumps(value, indent=2, sort_keys=True) + "\n").encode()
        digest = "a" * 64
        proof = {
            "source_preinstall_proof_path": run_cell.SOURCE_PREINSTALL_PROOF_PATH,
            "source_preinstall_proof_sha256": digest,
        }
        status = mock.Mock(st_dev=7, st_ino=9, st_mode=0o100600)
        with mock.patch.object(
            run_cell,
            "secure_json_with_digest",
            return_value=(value, raw, digest, status),
        ):
            evidence = run_cell.validate_source_preinstall_provenance(
                proof, "managed-pdns", selected
            )
        self.assertEqual(evidence["sha256"], digest)
        with mock.patch.object(
            run_cell,
            "secure_json_with_digest",
            return_value=(value, raw, "b" * 64, status),
        ), self.assertRaises(run_cell.ControllerError):
            run_cell.validate_source_preinstall_provenance(
                proof, "managed-pdns", selected
            )
        with mock.patch.object(
            run_cell,
            "secure_json_with_digest",
            side_effect=run_cell.ControllerError("preinstall proof missing"),
        ), self.assertRaises(run_cell.ControllerError):
            run_cell.validate_source_preinstall_provenance(
                proof, "managed-pdns", selected
            )

        absent = {
            "source_preinstall_proof_path": "absent",
            "source_preinstall_proof_sha256": "absent",
        }
        with mock.patch.object(run_cell, "require_absent_path") as require_absent:
            evidence = run_cell.validate_source_preinstall_provenance(
                absent, "uninitialized", cell()
            )
        self.assertFalse(evidence["exists"])
        require_absent.assert_called_once_with(
            run_cell.SOURCE_PREINSTALL_PROOF_PATH,
            "uninitialized source preinstall proof",
        )
        with self.assertRaises(run_cell.ControllerError):
            run_cell.validate_source_preinstall_provenance(
                dict(absent, source_preinstall_proof_sha256=digest),
                "uninitialized",
                cell(),
            )

    def test_external_adoption_preinstall_is_explicitly_preexisting(self) -> None:
        selected = cell("intent", "after-write", driver="pdns-adopt")
        evidence = external_pdns_preinstall_evidence(selected)
        value = {key: item for key, item in evidence.items() if key != "sha256"}
        canonical = (json.dumps(value, indent=2, sort_keys=True) + "\n").encode()
        self.assertEqual(
            run_cell.validate_source_preinstall_document(
                value, canonical, selected
            ),
            value,
        )
        for status in ("absent", "install ok installed", ""):
            tampered = json.loads(json.dumps(value))
            tampered["measured_target_packages"][0]["status"] = status
            with self.subTest(status=status), self.assertRaises(
                run_cell.ControllerError
            ):
                run_cell.validate_source_preinstall_document(
                    tampered,
                    (
                        json.dumps(tampered, indent=2, sort_keys=True) + "\n"
                    ).encode(),
                    selected,
                )

    def test_external_pdns_preimage_seals_live_wal_and_unhashed_shm(self) -> None:
        selected = cell("intent", "after-write", driver="pdns-adopt")
        scenario = external_pdns_scenario()
        preinstall = external_pdns_preinstall_evidence(selected)
        state_dir = os.path.abspath("external-pdns-state")
        value = external_pdns_preimage_value(
            selected, scenario, preinstall, state_dir=state_dir
        )
        canonical = (json.dumps(value, indent=2, sort_keys=True) + "\n").encode()
        self.assertEqual(
            run_cell.validate_external_pdns_preimage_document(
                value,
                canonical,
                selected,
                scenario,
                "c" * 64,
                preinstall,
                state_dir,
                "192.0.2.10",
                53,
                "www.s1-kill.test.",
                "A",
            ),
            value,
        )
        sidecars = value["database"]["sidecars"]
        self.assertNotIn("sha256", sidecars["shared_memory"])
        self.assertEqual(
            sidecars["shared_memory"]["content_policy"], "volatile-unhashed"
        )
        changed = []
        current = json.loads(json.dumps(value))
        current["database"]["journal_mode"] = "delete"
        changed.append(current)
        current = json.loads(json.dumps(value))
        current["database"]["sidecars"]["rollback_journal"]["status"] = "present"
        changed.append(current)
        current = json.loads(json.dumps(value))
        current["database"]["sidecars"]["write_ahead_log"]["size"] = 1
        changed.append(current)
        current = json.loads(json.dumps(value))
        current["database"]["sidecars"]["write_ahead_log"]["link_count"] = True
        changed.append(current)
        current = json.loads(json.dumps(value))
        current["database"]["sidecars"]["shared_memory"]["size"] = 0
        changed.append(current)
        current = json.loads(json.dumps(value))
        current["database"]["sidecars"]["shared_memory"]["content_policy"] = (
            "sha256-bound"
        )
        changed.append(current)
        current = json.loads(json.dumps(value))
        current["database"]["sidecars"]["shared_memory"]["sha256"] = "5" * 64
        changed.append(current)
        current = json.loads(json.dumps(value))
        current["database"]["sidecars"]["shared_memory"]["inode"] = current[
            "database"
        ]["sidecars"]["write_ahead_log"]["inode"]
        changed.append(current)
        current = json.loads(json.dumps(value))
        current["production_receipts_absent"]["dns_engine_state"]["path"] += (
            ".wrong"
        )
        changed.append(current)
        for tampered in changed:
            with self.subTest(tampered=tampered), self.assertRaises(
                run_cell.ControllerError
            ):
                run_cell.validate_external_pdns_preimage_document(
                    tampered,
                    (
                        json.dumps(tampered, indent=2, sort_keys=True) + "\n"
                    ).encode(),
                    selected,
                    scenario,
                    "c" * 64,
                    preinstall,
                    state_dir,
                    "192.0.2.10",
                    53,
                    "www.s1-kill.test",
                    "A",
                )
        with self.assertRaises(run_cell.ControllerError):
            run_cell.validate_external_pdns_preimage_document(
                value,
                json.dumps(value).encode(),
                selected,
                scenario,
                "c" * 64,
                preinstall,
                state_dir,
                "192.0.2.10",
                53,
                "www.s1-kill.test",
                "A",
            )

    def test_external_preimage_controller_forbids_immutable_sqlite_uri(
        self,
    ) -> None:
        source = MODULE_PATH.read_text(encoding="utf-8")
        body = source.split(
            "def validate_external_pdns_preimage_provenance(", 1
        )[1].split("\ndef _normalization_request_id(", 1)[0]
        query = body.split("connection = sqlite3.connect(", 1)[1].split(
            "\n    database_after, database_after_status", 1
        )[0]
        self.assertNotIn("immutable=1", query)
        self.assertEqual(query.count("?mode=ro"), 1)
        ordered = [
            '"file:/var/lib/powerdns/pdns.sqlite3?mode=ro"',
            "timeout=5.0",
            'connection.execute("PRAGMA query_only=ON")',
            'query_only = connection.execute("PRAGMA query_only").fetchone()',
            "query_only != (1,)",
            'journal_mode != ("wal",)',
            '"external PowerDNS rollback journal after query"',
        ]
        cursor = -1
        for fragment in ordered:
            with self.subTest(fragment=fragment):
                cursor = query.index(fragment, cursor + 1)
        self.assertEqual(
            body.count('sidecars["rollback_journal"]["path"]'), 2
        )
        self.assertIn(
            '"external PowerDNS write-ahead log after query"', body
        )
        self.assertIn('"external PowerDNS shared memory after query"', body)

    def test_source_adoption_document_rejects_identity_and_target_drift(self) -> None:
        selected = cell("source-stopped")
        value = source_adoption_value(selected)
        canonical = (json.dumps(value, indent=2, sort_keys=True) + "\n").encode()
        self.assertEqual(
            run_cell.validate_source_adoption_document(value, canonical, selected),
            value,
        )
        changed = []
        changed.append(dict(value, production_adoption_driver="pdns-switch"))
        current = json.loads(json.dumps(value))
        current["measured_target_packages"][0]["status"] = "install ok installed"
        changed.append(current)
        current = json.loads(json.dumps(value))
        current["production_receipts"][
            "measured_target_install_ownership_absent"
        ] = False
        changed.append(current)
        current = json.loads(json.dumps(value))
        current["database"]["sidecars"]["write_ahead_log"]["size"] = 1
        changed.append(current)
        current = json.loads(json.dumps(value))
        current["database"]["sidecars"]["write_ahead_log"]["link_count"] = True
        changed.append(current)
        current = json.loads(json.dumps(value))
        current["database"]["sidecars"]["shared_memory"]["size"] = 0
        changed.append(current)
        current = json.loads(json.dumps(value))
        current["database"]["sidecars"]["shared_memory"]["content_policy"] = (
            "sha256-bound"
        )
        changed.append(current)
        current = json.loads(json.dumps(value))
        current["database"]["sidecars"]["shared_memory"]["inode"] = current[
            "database"
        ]["sidecars"]["write_ahead_log"]["inode"]
        changed.append(current)
        current = json.loads(json.dumps(value))
        current["database"]["sidecars"]["rollback_journal"]["status"] = "present"
        changed.append(current)
        current = json.loads(json.dumps(value))
        current["main_config"]["sha256"] = "not-a-hash"
        changed.append(current)
        for tampered in changed:
            with self.subTest(tampered=tampered), self.assertRaises(
                run_cell.ControllerError
            ):
                run_cell.validate_source_adoption_document(
                    tampered,
                    (json.dumps(tampered, indent=2, sort_keys=True) + "\n").encode(),
                    selected,
                )
        with self.assertRaises(run_cell.ControllerError):
            run_cell.validate_source_adoption_document(
                value, json.dumps(value).encode(), selected
            )
        with self.assertRaises(run_cell.ControllerError):
            run_cell.validate_source_adoption_document(
                value, canonical, cell("committed")
            )

    def test_source_adoption_is_a_historical_checkpoint_before_normalization(self) -> None:
        selected = cell("target-started")
        value = source_adoption_value(selected)
        raw = (json.dumps(value, indent=2, sort_keys=True) + "\n").encode()
        proof_digest = "b" * 64
        proof = {
            "source_adoption_proof_path": run_cell.SOURCE_ADOPTION_PROOF_PATH,
            "source_adoption_proof_sha256": proof_digest,
            "source_setup_scenario_sha256": "4" * 64,
            "source_setup_identity_receipt_sha256": "5" * 64,
        }
        proof_status = mock.Mock(st_dev=1, st_ino=2, st_mode=0o100600)
        artifact_bytes = {
            value["main_config"]["path"]: b"main",
            value["managed_config"]["path"]: b"managed",
            value["database"]["path"]: b"database",
            value["database"]["schema_path"]: b"schema",
        }
        artifact_status = {
            value["main_config"]["path"]: mock.Mock(
                st_dev=3, st_ino=4, st_mode=0o100640, st_uid=0, st_gid=108
            ),
            value["managed_config"]["path"]: mock.Mock(
                st_dev=3, st_ino=5, st_mode=0o100644, st_uid=0, st_gid=0
            ),
            value["database"]["path"]: mock.Mock(
                st_dev=3, st_ino=6, st_mode=0o100640, st_uid=107, st_gid=108
            ),
            value["database"]["schema_path"]: mock.Mock(
                st_dev=3, st_ino=7, st_mode=0o100644, st_uid=0, st_gid=0
            ),
        }
        sidecars = value["database"]["sidecars"]
        sidecar_status = {
            sidecars["write_ahead_log"]["path"]: mock.Mock(
                st_dev=3,
                st_ino=8,
                st_mode=0o100640,
                st_uid=107,
                st_gid=108,
                st_nlink=1,
                st_size=0,
            ),
            sidecars["shared_memory"]["path"]: mock.Mock(
                st_dev=3,
                st_ino=9,
                st_mode=0o100640,
                st_uid=107,
                st_gid=108,
                st_nlink=1,
                st_size=32768,
            ),
        }
        value["main_config"]["sha256"] = run_cell.hashlib.sha256(b"main").hexdigest()
        value["managed_config"]["sha256"] = run_cell.hashlib.sha256(b"managed").hexdigest()
        value["database"]["sha256"] = run_cell.hashlib.sha256(b"database").hexdigest()
        value["database"]["schema_sha256"] = run_cell.hashlib.sha256(b"schema").hexdigest()
        raw = (json.dumps(value, indent=2, sort_keys=True) + "\n").encode()

        def secure_read(path: str, _label: str, **_kwargs: object) -> tuple[bytes, object]:
            status = artifact_status[path]
            required_uid = _kwargs.get("required_uid")
            if required_uid is not None and status.st_uid != required_uid:
                raise run_cell.ControllerError("test owner mismatch")
            return artifact_bytes[path], status

        with mock.patch.object(
            run_cell,
            "secure_json_with_digest",
            return_value=(value, raw, proof_digest, proof_status),
        ), mock.patch.object(
            run_cell, "secure_read_bytes", side_effect=secure_read
        ), mock.patch.object(
            run_cell,
            "secure_regular_metadata",
            side_effect=lambda path, _label, **_kwargs: sidecar_status[path],
        ), mock.patch.object(
            run_cell.os, "geteuid", return_value=0, create=True
        ), mock.patch.object(
            run_cell, "resolve_exact_pdns_owner_identity", return_value=(107, 108)
        ), mock.patch.object(run_cell, "require_absent_path") as require_absent:
            evidence = run_cell.validate_source_adoption_provenance(
                proof, "managed-pdns", selected
            )
        self.assertEqual(evidence["sha256"], proof_digest)
        self.assertEqual(evidence["production_adoption_driver"], "pdns-adopt")
        self.assertEqual(
            evidence["artifacts"]["adoption_checkpoint"]["database"]["sha256"],
            value["database"]["sha256"],
        )
        self.assertEqual(require_absent.call_count, 1)
        require_absent.assert_called_once_with(
            value["cluster_config"]["path"], "source adoption cluster config"
        )

        with mock.patch.object(
            run_cell,
            "secure_json_with_digest",
            return_value=(value, raw, "c" * 64, proof_status),
        ), self.assertRaises(run_cell.ControllerError):
            run_cell.validate_source_adoption_provenance(
                proof, "managed-pdns", selected
            )
        absent = {
            "source_adoption_proof_path": "absent",
            "source_adoption_proof_sha256": "absent",
        }
        with mock.patch.object(run_cell, "require_absent_path") as require_absent:
            evidence = run_cell.validate_source_adoption_provenance(
                absent, "uninitialized", cell()
            )
        self.assertFalse(evidence["exists"])
        require_absent.assert_called_once_with(
            run_cell.SOURCE_ADOPTION_PROOF_PATH,
            "uninitialized source adoption proof",
        )

    def test_pdns_v3_source_qualifier_matches_production_commitment(self) -> None:
        zone = source_normalization_zone()
        qualifier = run_cell._canonical_pdns_v3_qualifier(1, zone)
        self.assertEqual(
            qualifier,
            "dns-zone-sync/v3:sha256:"
            "547009d10494c36f4c404ab9d3c64e582950698d8b9567cc469d4ce370776408",
        )
        reordered = json.loads(json.dumps(zone))
        reordered["records"].reverse()
        self.assertEqual(run_cell._canonical_pdns_v3_qualifier(1, reordered), qualifier)
        changed = json.loads(json.dumps(zone))
        changed["records"][-1]["content"] = "192.0.2.11"
        self.assertNotEqual(run_cell._canonical_pdns_v3_qualifier(1, changed), qualifier)

    def test_source_normalization_binds_receipt_ledger_and_private_schema(self) -> None:
        selected = cell("target-started")
        zone = source_normalization_zone()
        scenario = {"source_epoch": 1, "zones": [zone]}
        base_request_id = run_cell.hashlib.sha256(
            (selected.cell_id + "\x00source-pdns-normalize").encode()
        ).digest()[:16].hex()
        configure_request_id = run_cell._normalization_request_id(
            base_request_id, "configure"
        )
        zone_request_id = run_cell._normalization_request_id(
            base_request_id, "zone-sync/0/s1-kill.test"
        )
        qualifier = run_cell._canonical_pdns_v3_qualifier(1, zone)
        configure = {
            "method": "Agent.ConfigurePowerDNSSQLite",
            "request_id": configure_request_id,
            "owner_id": run_cell.deterministic_trigger_owner(
                selected.cell_id, configure_request_id
            ),
            "kind": "pdns_configure",
            "target": "pdns",
            "package_name": "",
            "terminal_phase": "completed",
        }
        zone_sync = {
            "method": "Agent.SyncDNSZoneV3",
            "request_id": zone_request_id,
            "owner_id": run_cell.deterministic_trigger_owner(
                selected.cell_id, zone_request_id
            ),
            "kind": "dns_zone_sync",
            "target": "s1-kill.test",
            "package_name": qualifier,
            "terminal_phase": (
                "commit/dns-zone-sync/v3/published/"
                + zone_request_id
                + "/s1-kill.test/"
                + qualifier
            ),
            "engine": "pdns",
            "engine_epoch": 1,
            "desired_generation": 1,
            "domain": "s1-kill.test",
            "delete": False,
            "zone_type": "NATIVE",
            "qualifier": qualifier,
        }
        receipt = {
            "schema": run_cell.SOURCE_NORMALIZATION_IDENTITY_SCHEMA,
            "cell_id": selected.cell_id,
            "driver": "bind",
            "source_fixture": "managed-pdns",
            "base_request_id": base_request_id,
            "source_engine": "pdns",
            "source_epoch": 1,
            "configure": configure,
            "zone_syncs": [zone_sync],
        }
        raw = (json.dumps(receipt, separators=(",", ":")) + "\n").encode()
        digest = run_cell.hashlib.sha256(raw).hexdigest()
        proof = {
            "source_normalization_identity_receipt_path": (
                run_cell.SOURCE_NORMALIZATION_IDENTITY_PATH
            ),
            "source_normalization_identity_receipt_sha256": digest,
        }
        jobs = {}
        for operation in (configure, zone_sync):
            jobs[operation["request_id"]] = {
                "request_id": operation["request_id"],
                "owner_id": operation["owner_id"],
                "kind": operation["kind"],
                "target": operation["target"],
                "package_name": operation["package_name"],
                "status": "succeeded",
                "phase": operation["terminal_phase"],
                "attempt": 1,
                "finished_at": "2026-08-31T12:00:00Z",
            }
        ledger = {"version": 1, "active_request_id": "", "jobs": jobs}
        receipt_status = mock.Mock(st_dev=1, st_ino=2, st_mode=0o100600)
        ledger_status = mock.Mock(st_dev=1, st_ino=3, st_mode=0o100600)
        main = b"include-dir=/etc/powerdns/pdns.d\n"
        managed = (
            b"# Managed by CelikPanel; do not edit by hand.\n"
            b"launch=gsqlite3\n"
            b"gsqlite3-dnssec=yes\n"
            b"gsqlite3-database=/var/lib/powerdns/pdns.sqlite3\n"
            b"local-address=192.0.2.10\n"
            b"zone-cache-refresh-interval=0\nwebserver=no\napi=no\n"
        )
        config_statuses = {
            "/etc/powerdns/pdns.conf": mock.Mock(
                st_dev=4, st_ino=5, st_mode=0o100640, st_uid=0, st_gid=108
            ),
            "/etc/powerdns/pdns.d/celikpanel.conf": mock.Mock(
                st_dev=4, st_ino=6, st_mode=0o100644, st_uid=0, st_gid=0
            ),
        }
        database_status = mock.Mock(
            st_dev=4,
            st_ino=7,
            st_mode=0o100640,
            st_uid=107,
            st_gid=108,
            st_nlink=1,
            st_size=4096,
        )
        expected_row = (
            "s1-kill.test",
            "pdns",
            1,
            zone_request_id,
            zone_sync["owner_id"],
            qualifier,
            1,
            "sync",
            "NATIVE",
            "dns-zone-sync/v3",
        )
        connection = mock.Mock()

        def execute(statement: str) -> object:
            compact = " ".join(statement.split())
            result = mock.Mock()
            if compact == "PRAGMA quick_check":
                result.fetchall.return_value = [("ok",)]
            elif "FROM celikpanel_dns_zone_sync_v3_receipts" in compact:
                result.fetchall.return_value = [expected_row]
            elif "FROM celikpanel_dns_zone_sync_receipts" in compact:
                result.fetchone.return_value = (0,)
            elif "FROM celikpanel_dns_engine_manifest_receipt" in compact:
                result.fetchone.return_value = (0,)
            elif compact == "SELECT COUNT(*) FROM domains":
                result.fetchone.return_value = (1,)
            elif "SELECT (SELECT COUNT(*) FROM supermasters)" in compact:
                result.fetchone.return_value = (0,)
            else:
                raise AssertionError(f"unexpected SQLite query: {compact}")
            return result

        connection.execute.side_effect = execute

        def secure_config(
            path: str, _label: str, **_kwargs: object
        ) -> tuple[bytes, object]:
            data = main if path.endswith("pdns.conf") else managed
            return data, config_statuses[path]

        with mock.patch.object(
            run_cell,
            "secure_json_with_digest",
            return_value=(receipt, raw, digest, receipt_status),
        ), mock.patch.object(
            run_cell,
            "secure_read_json",
            return_value=(ledger, ledger_status),
        ), mock.patch.object(
            run_cell, "secure_read_bytes", side_effect=secure_config
        ), mock.patch.object(
            run_cell, "resolve_exact_pdns_owner_identity", return_value=(107, 108)
        ), mock.patch.object(
            run_cell.os, "geteuid", return_value=0, create=True
        ), mock.patch.object(
            run_cell.os, "lstat", return_value=database_status
        ), mock.patch.object(
            run_cell.sqlite3, "connect", return_value=connection
        ), mock.patch.object(run_cell, "require_absent_path"):
            evidence = run_cell.validate_source_normalization_provenance(
                proof,
                "managed-pdns",
                selected,
                scenario,
                os.path.abspath("state"),
                "192.0.2.10",
            )
        self.assertEqual(evidence["database"]["v3_receipt_count"], 1)
        self.assertEqual(evidence["configure"], configure)
        self.assertEqual(evidence["zone_syncs"], [zone_sync])

        tampered = json.loads(json.dumps(receipt))
        tampered["zone_syncs"][0]["terminal_phase"] = "completed"
        tampered_raw = (json.dumps(tampered, separators=(",", ":")) + "\n").encode()
        with mock.patch.object(
            run_cell,
            "secure_json_with_digest",
            return_value=(tampered, tampered_raw, digest, receipt_status),
        ), self.assertRaises(run_cell.ControllerError):
            run_cell.validate_source_normalization_provenance(
                proof,
                "managed-pdns",
                selected,
                scenario,
                os.path.abspath("state"),
                "192.0.2.10",
            )

    def test_pdns_owner_identity_requires_exact_stable_getent_records(self) -> None:
        passwd = b"pdns:x:107:108:PowerDNS:/var/spool/powerdns:/usr/sbin/nologin\n"
        group = b"pdns:x:108:\n"
        self.assertEqual(
            run_cell._parse_pdns_getent_records(passwd, group), (107, 108)
        )
        for bad_passwd, bad_group in (
            (passwd.replace(b":107:", b":0107:"), group),
            (passwd, b"pdns:x:109:\n"),
            (passwd, b"pdns:x:108:member\n"),
            (passwd + b"extra\n", group),
        ):
            with self.subTest(
                passwd=bad_passwd, group=bad_group
            ), self.assertRaises(run_cell.ControllerError):
                run_cell._parse_pdns_getent_records(bad_passwd, bad_group)

        executable = mock.Mock(
            st_mode=0o100755,
            st_uid=0,
            st_gid=0,
        )

        def completed(stdout: bytes) -> object:
            return run_cell.subprocess.CompletedProcess(
                args=["getent"], returncode=0, stdout=stdout, stderr=b""
            )

        stable = [completed(passwd), completed(group), completed(passwd), completed(group)]
        with mock.patch.object(
            run_cell.os, "lstat", return_value=executable
        ), mock.patch.object(
            run_cell.subprocess, "run", side_effect=stable
        ) as process:
            self.assertEqual(run_cell.resolve_exact_pdns_owner_identity(), (107, 108))
        self.assertEqual(process.call_count, 4)

        drifted_passwd = passwd.replace(b":107:", b":109:")
        drift = [
            completed(passwd),
            completed(group),
            completed(drifted_passwd),
            completed(group),
        ]
        with mock.patch.object(
            run_cell.os, "lstat", return_value=executable
        ), mock.patch.object(
            run_cell.subprocess, "run", side_effect=drift
        ), self.assertRaises(run_cell.ControllerError):
            run_cell.resolve_exact_pdns_owner_identity()

    @unittest.skipUnless(os.name == "posix", "requires O_NOFOLLOW file semantics")
    def test_secure_sidecar_metadata_rejects_symlink_hardlink_and_nonempty_wal(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            wal = root / "pdns.sqlite3-wal"
            wal.write_bytes(b"")
            wal.chmod(0o640)
            status = run_cell.secure_regular_metadata(
                str(wal),
                "test WAL",
                required_mode=0o640,
                required_uid=os.geteuid(),
                required_gid=os.getegid(),
                required_size=0,
                require_empty=True,
            )
            self.assertEqual(status.st_size, 0)

            wal.write_bytes(b"x")
            with self.assertRaises(run_cell.ControllerError):
                run_cell.secure_regular_metadata(
                    str(wal),
                    "test WAL",
                    required_mode=0o640,
                    required_uid=os.geteuid(),
                    required_gid=os.getegid(),
                    required_size=0,
                    require_empty=True,
                )
            wal.write_bytes(b"")

            alias = root / "wal-hardlink"
            os.link(wal, alias)
            with self.assertRaises(run_cell.ControllerError):
                run_cell.secure_regular_metadata(
                    str(wal),
                    "test WAL",
                    required_mode=0o640,
                    required_uid=os.geteuid(),
                    required_gid=os.getegid(),
                    required_size=0,
                    require_empty=True,
                )
            alias.unlink()

            symlink = root / "wal-symlink"
            symlink.symlink_to(wal)
            with self.assertRaises(run_cell.ControllerError):
                run_cell.secure_regular_metadata(
                    str(symlink),
                    "test WAL symlink",
                    required_mode=0o640,
                    required_uid=os.geteuid(),
                    required_gid=os.getegid(),
                    required_size=0,
                    require_empty=True,
                )

    def test_peer_unreachable_requires_every_ssh_sample_to_fail(self) -> None:
        with mock.patch.object(
            run_cell, "read_ssh_banner", side_effect=OSError("down")
        ):
            report, error = run_cell.observe_peer_after_kill(
                "192.0.2.2", "unreachable", 0.01, 0.001, 0.001
            )
        self.assertIsNone(error)
        self.assertTrue(report["samples"])
        with mock.patch.object(
            run_cell,
            "read_ssh_banner",
            return_value={"address": "192.0.2.2", "port": 22, "banner": "SSH-2.0-test"},
        ):
            _report, error = run_cell.observe_peer_after_kill(
                "192.0.2.2", "unreachable", 0.01, 0.001, 0.001
            )
        self.assertIsNotNone(error)

    @unittest.skipUnless(os.name == "posix", "dual transport bind proof is POSIX-only")
    def test_uninitialized_live_bind_probe_uses_one_udp_tcp_port(self) -> None:
        observed = run_cell.probe_udp_tcp_bindability("127.0.0.1", 0)
        self.assertGreater(observed["port"], 0)
        self.assertTrue(observed["udp_bindable"])
        self.assertTrue(observed["tcp_bindable"])
        self.assertFalse(observed["authoritative_answer_observed"])

    @unittest.skipUnless(os.name == "posix", "directory fsync contract is POSIX-only")
    def test_atomic_json_output_is_create_new_and_mode_0600(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            target = os.path.join(root, "proof.json")
            run_cell.atomic_write_new_json(target, {"kill_proven": True})
            self.assertEqual(Path(target).stat().st_mode & 0o777, 0o600)
            self.assertEqual(
                json.loads(Path(target).read_text(encoding="utf-8")),
                {"kill_proven": True},
            )
            with self.assertRaises(run_cell.ControllerError):
                run_cell.atomic_write_new_json(target, {"kill_proven": False})

    @unittest.skipUnless(
        os.name == "posix" and hasattr(os, "geteuid") and os.geteuid() == 0,
        "signed-update artifact ownership contract requires root on POSIX",
    )
    def test_signed_startup_preconditions_and_external_flock(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            request_id = "1" * 32
            owner_id = "2" * 32
            qualifier = "dns-engine-switch/v1:sha256:" + "3" * 64
            journal_path = os.path.join(root, "dns-engine-switch.json")
            ledger_path = os.path.join(root, "service-mutations.json")
            lock_path = os.path.join(root, "mutation.lock")
            Path(journal_path).write_text(
                json.dumps(
                    {
                        "schema": run_cell.JOURNAL_SCHEMA,
                        "phase": "rolling-back",
                        "mode": "switch",
                        "mutation_request_id": request_id,
                        "mutation_owner_id": owner_id,
                        "manifest_qualifier": qualifier,
                        "source_engine": "pdns",
                        "target_engine": "bind",
                        "source_epoch": 1,
                        "target_epoch": 2,
                        "source_revision": 3,
                        "topology": "standalone",
                    }
                ),
                encoding="utf-8",
            )
            Path(ledger_path).write_text(
                json.dumps(
                    {
                        "version": 1,
                        "jobs": {
                            request_id: {
                                "request_id": request_id,
                                "owner_id": owner_id,
                                "kind": "dns_engine_switch",
                                "target": "bind",
                                "package_name": qualifier,
                                "status": "failed",
                                "phase": "failed",
                                "attempt": 1,
                                "started_at": "2026-08-31T00:00:00Z",
                                "updated_at": "2026-08-31T00:00:02Z",
                                "deadline_at": "2026-08-31T00:10:00Z",
                                "finished_at": "2026-08-31T00:00:02Z",
                                "lease_expires_at": "0001-01-01T00:00:00Z",
                            }
                        },
                    }
                ),
                encoding="utf-8",
            )
            Path(lock_path).write_bytes(b"")
            for path in (journal_path, ledger_path, lock_path):
                os.chmod(path, 0o600)
            evidence = run_cell.validate_signed_startup_preconditions(
                root,
                journal_path,
                request_id,
                cell(
                    "rolled-back",
                    "before-write",
                    driver="signed-update-finalize",
                ),
            )
            self.assertEqual(evidence["journal"]["phase"], "rolling-back")
            self.assertEqual(evidence["ledger"]["job_status"], "failed")
            self.assertEqual(
                evidence["marker_identity"],
                {
                    "mode": "switch",
                    "mutation_owner_id": owner_id,
                    "manifest_qualifier": qualifier,
                    "source_engine": "pdns",
                    "target_engine": "bind",
                    "source_epoch": 1,
                    "target_epoch": 2,
                    "source_revision": 3,
                    "topology": "standalone",
                    "pair_role": "",
                },
            )
            lock_fd, lock_evidence = run_cell.acquire_external_mutation_lock(
                lock_path
            )
            try:
                self.assertTrue(lock_evidence["exclusive_flock"])
                with self.assertRaises(run_cell.ControllerError):
                    run_cell.acquire_external_mutation_lock(lock_path)
            finally:
                os.close(lock_fd)


class DNSProbeTest(unittest.TestCase):
    @staticmethod
    def _response(query: bytes) -> bytes:
        transaction_id = struct.unpack("!H", query[:2])[0]
        header = struct.pack("!HHHHHH", transaction_id, 0x8400, 1, 1, 0, 0)
        answer = (
            b"\xc0\x0c"
            + struct.pack("!HHIH", 1, 1, 60, 4)
            + socket.inet_aton("127.0.0.1")
        )
        return header + query[12:] + answer

    def test_udp_and_tcp_must_both_be_authoritative(self) -> None:
        udp = None
        tcp = None
        port = 0
        for _attempt in range(32):
            candidate_tcp = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            candidate_udp = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
            try:
                candidate_tcp.bind(("127.0.0.1", 0))
                port = candidate_tcp.getsockname()[1]
                candidate_udp.bind(("127.0.0.1", port))
            except OSError:
                candidate_udp.close()
                candidate_tcp.close()
                continue
            tcp = candidate_tcp
            udp = candidate_udp
            break
        if udp is None or tcp is None:
            self.fail("could not reserve one loopback port for both UDP and TCP")
        self.addCleanup(udp.close)
        self.addCleanup(tcp.close)
        tcp.listen(1)
        udp.settimeout(2)
        tcp.settimeout(2)
        errors: list[BaseException] = []

        def serve_udp() -> None:
            try:
                query, address = udp.recvfrom(65535)
                udp.sendto(self._response(query), address)
            except BaseException as exc:  # surfaced in the test thread below
                errors.append(exc)

        def serve_tcp() -> None:
            try:
                connection, _ = tcp.accept()
                with connection:
                    length = struct.unpack(
                        "!H", run_cell._recv_exact(connection, 2)
                    )[0]
                    query = run_cell._recv_exact(connection, length)
                    response = self._response(query)
                    connection.sendall(struct.pack("!H", len(response)) + response)
            except BaseException as exc:  # surfaced in the test thread below
                errors.append(exc)

        threads = [
            threading.Thread(target=serve_udp, daemon=True),
            threading.Thread(target=serve_tcp, daemon=True),
        ]
        for thread in threads:
            thread.start()
        try:
            report = run_cell.query_authoritative_dns(
                "127.0.0.1", port, "matrix.test.", "A", 2
            )
        finally:
            for thread in threads:
                thread.join(3)
            udp.close()
            tcp.close()
        self.assertEqual(errors, [])
        self.assertEqual(report["udp"]["answers"], 1)
        self.assertEqual(report["tcp"]["answers"], 1)

    def test_non_authoritative_response_fails(self) -> None:
        raw = struct.pack("!HHHHHH", 7, 0x8000, 1, 1, 0, 0)
        with self.assertRaises(run_cell.ControllerError):
            run_cell.validate_dns_response(raw, 7, "udp")


if __name__ == "__main__":
    unittest.main()
