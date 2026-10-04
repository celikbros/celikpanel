#!/usr/bin/env python3
"""Isolated contract tests for the per-cell SIGKILL controller."""

from __future__ import annotations

import importlib.util
import hashlib
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
import guest_bootstrap


MODULE_PATH = Path(__file__).with_name("run_cell.py")
SPEC = importlib.util.spec_from_file_location("dns_kill_run_cell", MODULE_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError(f"cannot import {MODULE_PATH}")
run_cell = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = run_cell
SPEC.loader.exec_module(run_cell)


def v2_state_document(
    *, engine: str, engine_epoch: int, qualifier: str, request_id: str, owner_id: str,
    mode: str = "switch", source_revision: int = 0, generation: str = "c" * 64,
) -> tuple[dict, bytes]:
    """The canonical v2 DNS state receipt the product writes (acquisition/publication)."""

    def compact(value: object) -> bytes:
        return (json.dumps(value, separators=(",", ":")) + "\n").encode()

    acquisition = {
        "schema": "celikpanel-dns-engine-acquisition/v1", "mode": mode, "engine": engine,
        "engine_epoch": engine_epoch, "source_revision": source_revision,
        "manifest_qualifier": qualifier, "mutation_request_id": request_id,
        "mutation_owner_id": owner_id,
    }
    publication = {
        "schema": "celikpanel-dns-engine-publication/v1",
        "acquisition_sha256": hashlib.sha256(compact(acquisition)).hexdigest(),
        "generation": generation,
    }
    value = {"schema": "celikpanel-dns-engine-state/v2", "acquisition": acquisition,
             "publication": publication}
    return value, compact(value)


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

    def test_later_bind_rollback_selector_precursor_and_target_proof(self) -> None:
        selected = replace(
            cell("rolling-back", "after-write"),
            cell_id=run_cell.BIND_HANDOFF_CELL,
        )
        self.assertEqual(run_cell.rollback_precursor_phase(selected), "target-staged")
        self.assertEqual(
            run_cell.rollback_precursor_phase(
                selected, bind_rollback_after_target_started=True
            ),
            "target-started",
        )
        args = (
            {"PATH": "/usr/bin"}, selected, "1" * 32, "a" * 32,
            os.path.abspath("marker.json"), 9, os.path.abspath("state"),
            os.path.abspath("mutation.lock"), os.path.abspath("agent.sock"),
            os.path.abspath("agent.token"),
        )
        tagged = run_cell.tagged_agent_environment(
            *args, bind_rollback_after_target_started=True
        )
        self.assertEqual(
            tagged[run_cell.LATER_BIND_ROLLBACK_SELECTOR], "target-started"
        )
        with self.assertRaises(run_cell.ControllerError):
            run_cell.tagged_agent_environment(
                args[0], cell("rolling-back", "after-write"), *args[2:],
                bind_rollback_after_target_started=True,
            )
        identity = boundary_identity()
        journal_path = os.path.abspath("journal.json")
        marker = marker_value(
            selected, os.path.abspath("marker.json"), journal_path, identity
        )
        marker["observed_journal"]["schema"] = run_cell.BIND_HANDOFF_JOURNAL_SCHEMA
        add_rollback_precursor(marker, selected, journal_path, identity)
        precursor = marker["rollback_precursor"]
        precursor["phase"] = "target-started"
        precursor["observed_journal"]["phase"] = "target-started"
        precursor["observed_journal"]["schema"] = run_cell.BIND_HANDOFF_JOURNAL_SCHEMA
        run_cell.validate_rollback_precursor(
            marker, selected, "1" * 32, journal_path, identity,
            bind_rollback_after_target_started=True,
        )
        with self.assertRaises(run_cell.BoundaryUnverified):
            run_cell.validate_rollback_precursor(
                marker, selected, "1" * 32, journal_path, identity
            )
        settings = mock.Mock(
            cell=selected, bind_rollback_after_target_started=True,
            command_timeout=1.0, dns_address="192.0.2.10", dns_port=53,
            dns_name="www.s1-kill.test", dns_type="A", dns_timeout=1.0,
        )
        units = {
            "bind9.service": "active", "named.service": "active",
            "pdns.service": "inactive",
        }
        dns = {"udp": {"answers": 1}, "tcp": {"answers": 1}}
        with mock.patch.object(
            run_cell, "inspect_dns_unit_states", return_value=units
        ), mock.patch.object(
            run_cell, "query_authoritative_dns", return_value=dns
        ) as query:
            proof = run_cell.prove_later_bind_target_before_kill(
                settings, {"PATH": "/usr/bin"}
            )
        self.assertEqual(proof, {"units": units, "authoritative": dns})
        query.assert_called_once()
        with mock.patch.object(
            run_cell, "inspect_dns_unit_states",
            return_value=dict(units, **{"pdns.service": "active"}),
        ), mock.patch.object(
            run_cell, "query_authoritative_dns"
        ) as query, self.assertRaises(run_cell.BoundaryUnverified):
            run_cell.prove_later_bind_target_before_kill(
                settings, {"PATH": "/usr/bin"}
            )
        query.assert_not_called()
        with mock.patch.object(
            run_cell, "inspect_dns_unit_states", return_value=units
        ), mock.patch.object(
            run_cell, "query_authoritative_dns",
            side_effect=run_cell.ControllerError("not authoritative"),
        ), self.assertRaises(run_cell.BoundaryUnverified):
            run_cell.prove_later_bind_target_before_kill(
                settings, {"PATH": "/usr/bin"}
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

    def test_native_dns_observer_uses_exact_request_without_mutation(self) -> None:
        request_id = "a" * 32
        base = ("/opt/celikpanel/bin/recovery", "dns-switch-status", "--quiesced")
        expected = (*base, "--request-id", request_id)
        settings = mock.Mock(
            native_dns_status_command=base,
            request_id=request_id,
            recovery_timeout=5.0,
            command_cwd="/",
        )
        completed = run_cell.CommandResult(expected, 0, b"failed\n", False, 0.1)
        with mock.patch.object(
            run_cell, "run_bounded_command", return_value=completed
        ) as runner:
            result = run_cell.run_native_dns_status(
                settings, {}, mock.Mock(), "before-retry"
            )
        self.assertEqual(runner.call_args.args[0], expected)
        self.assertEqual(runner.call_args.args[1], "native-dns-status-before-retry")
        self.assertTrue(result["observed"])
        self.assertEqual(result["command"]["argv"], list(expected))

    def test_recovery_precedes_final_liveness_in_controller_source(self) -> None:
        source = MODULE_PATH.read_text(encoding="utf-8")
        flow = source[source.index("def run_cell(settings:") :]
        self.assertGreaterEqual(flow.count("for ordinal in (1, 2):"), 2)
        self.assertLess(
            flow.index('result["native_post_kill_status"]'),
            flow.index('"agent-restart"'),
        )
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
            socket_flow.index('"dns-before-retry"'),
            socket_flow.index('f"post-restart-rpc-retry-{ordinal}"'),
        )
        self.assertLess(
            socket_flow.index("pre_retry_probe = run_recovery_probe("),
            socket_flow.index('f"post-restart-rpc-retry-{ordinal}"'),
        )
        self.assertLess(
            socket_flow.index("run_native_dns_status("),
            socket_flow.index('f"post-restart-rpc-retry-{ordinal}"'),
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

    def test_recovery_probe_requires_stable_terminal_outcome(self) -> None:
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
        second["fingerprint"] = first["fingerprint"]
        second["converged"] = False
        second["recovery_outcome"] = "indeterminate"
        self.assertIn(
            "second recovery probe is indeterminate",
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
        self.assertEqual(run_cell.assess_recovery_probes(rolled_first, rolled_second), [])
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

    def test_bind_handoff_full_managed_source_preflight(self) -> None:
        selected = replace(
            cell("rolling-back", "after-write"),
            cell_id=run_cell.BIND_HANDOFF_CELL,
        )
        scenario_path = os.path.abspath("scenario.json")
        proof_path = os.path.abspath("source-proof.json")
        identity_path = os.path.abspath("measured/identity.json")
        state_dir = os.path.abspath("state")
        journal_path = os.path.join(state_dir, "dns-engine-switch-journal.json")
        state_path = os.path.join(state_dir, "dns-engine-state.json")
        ownership_path = os.path.join(state_dir, "dns-engine-ownership-pdns.json")
        scenario = guest_bootstrap.bind_scenario("managed-pdns")
        scenario_evidence = {"sha256": "1" * 64}
        state = {
            "schema": "celikpanel-dns-engine-state/v1",
            "mode": "adopt", "engine": "pdns", "engine_epoch": 1,
            "source_revision": 0,
            "manifest_qualifier": "dns-engine-switch/v1:sha256:" + "2" * 64,
            "mutation_request_id": "3" * 32,
            "mutation_owner_id": "4" * 32,
        }
        state_raw = run_cell.canonical_dns_state_bytes(state)
        state_digest = hashlib.sha256(state_raw).hexdigest()
        preinstall = source_preinstall_value(selected)
        adoption = source_adoption_value(selected)
        adoption["production_receipts"]["state_sha256"] = state_digest
        adoption["production_receipts"]["active_ownership_sha256"] = state_digest
        schema_raw = b"package schema"
        adoption["database"]["schema_sha256"] = hashlib.sha256(schema_raw).hexdigest()
        preinstall_raw = (json.dumps(preinstall, indent=2, sort_keys=True) + "\n").encode()
        adoption_raw = (json.dumps(adoption, indent=2, sort_keys=True) + "\n").encode()
        preinstall_digest = hashlib.sha256(preinstall_raw).hexdigest()
        adoption_digest = hashlib.sha256(adoption_raw).hexdigest()
        proof = {
            "schema": run_cell.SOURCE_PROOF_SCHEMA,
            "cell_id": selected.cell_id,
            "source_fixture": "managed-pdns",
            "scenario_sha256": scenario_evidence["sha256"],
            "identity_receipt_path": identity_path,
            "identity_receipt_preexisting": False,
            "engine": "pdns", "engine_epoch": 1, "source_revision": 0,
            "serving_before_tagged_agent": True,
            "engine_state_receipt_path": state_path,
            "engine_state_receipt_sha256": state_digest,
            "engine_state_identity": state,
            "authoritative_preflight": {
                "claimed": True, "address": "192.0.2.10", "port": 53,
                "name": "www.s1-kill.test", "type": "A",
                "udp": True, "tcp": True,
            },
            "uninitialized_global_port53": {
                "udp_bindable": False, "tcp_bindable": False,
                "authoritative_answer_observed": False,
            },
            "receipt_origin": "production-pdns-adopt-normalized",
            "source_setup_scenario_sha256": adoption["source_setup_scenario_sha256"],
            "source_setup_identity_receipt_sha256": adoption["source_setup_identity_receipt_sha256"],
            "source_preinstall_proof_path": run_cell.SOURCE_PREINSTALL_PROOF_PATH,
            "source_preinstall_proof_sha256": preinstall_digest,
            "source_adoption_proof_path": run_cell.SOURCE_ADOPTION_PROOF_PATH,
            "source_adoption_proof_sha256": adoption_digest,
            "external_pdns_preimage_path": "absent",
            "external_pdns_preimage_sha256": "absent",
            "source_normalization_identity_receipt_path": run_cell.SOURCE_NORMALIZATION_IDENTITY_PATH,
            "source_normalization_identity_receipt_sha256": "5" * 64,
        }
        proof_raw = (json.dumps(proof, indent=2, sort_keys=True) + "\n").encode()
        status = mock.Mock(st_dev=1, st_ino=2, st_mode=0o100600, st_gid=0)
        documents = {
            proof_path: (proof, proof_raw),
            run_cell.SOURCE_PREINSTALL_PROOF_PATH: (preinstall, preinstall_raw),
            run_cell.SOURCE_ADOPTION_PROOF_PATH: (adoption, adoption_raw),
            state_path: (state, state_raw),
            ownership_path: (state, state_raw),
        }
        visited = []
        def read_document(path: str, _label: str, **_kwargs: object):
            visited.append(path)
            value, raw = documents[path]
            return value, raw, hashlib.sha256(raw).hexdigest(), status
        with mock.patch.object(
            run_cell, "validate_source_scenario", return_value=(scenario, scenario_evidence)
        ), mock.patch.object(
            run_cell, "secure_json_with_digest", side_effect=read_document
        ), mock.patch.object(
            run_cell, "secure_read_bytes", return_value=(schema_raw, status)
        ), mock.patch.object(
            run_cell, "validate_source_normalization_provenance",
            return_value={"path": run_cell.SOURCE_NORMALIZATION_IDENTITY_PATH},
        ), mock.patch.object(
            run_cell, "require_absent_path"
        ), mock.patch.object(
            run_cell.os, "geteuid", return_value=0, create=True
        ):
            observed = run_cell.validate_socket_source_proof(
                proof_path, selected, scenario_path, identity_path, state_dir,
                journal_path, "192.0.2.10", 53, "www.s1-kill.test", "A",
            )
        self.assertIn(run_cell.SOURCE_PREINSTALL_PROOF_PATH, visited)
        self.assertIn(run_cell.SOURCE_ADOPTION_PROOF_PATH, visited)
        self.assertEqual(observed["source_preinstall_proof"]["sha256"], preinstall_digest)
        self.assertEqual(observed["source_adoption_proof"]["sha256"], adoption_digest)
        self.assertEqual(observed["source_fixture"], "managed-pdns")
        for wrong in (
            replace(selected, cell_id="bind__rolling-back__before-write__standalone__peer-reachable"),
            replace(selected, source_fixture_policy="managed-pdns-required"),
            replace(selected, role="paired-primary"),
        ):
            with self.subTest(wrong=wrong):
                bad_preinstall = source_preinstall_value(wrong)
                bad_raw = (json.dumps(bad_preinstall, indent=2, sort_keys=True) + "\n").encode()
                with self.assertRaises(run_cell.ControllerError):
                    run_cell.validate_source_preinstall_document(bad_preinstall, bad_raw, wrong)
                bad_adoption = source_adoption_value(wrong)
                bad_adoption_raw = (
                    json.dumps(bad_adoption, indent=2, sort_keys=True) + "\n"
                ).encode()
                with self.assertRaises(run_cell.ControllerError):
                    run_cell.validate_source_adoption_document(
                        bad_adoption, bad_adoption_raw, wrong
                    )

    def test_rolled_back_bind_accepts_exact_managed_source_preinstall(self) -> None:
        selected = cell("rolled-back", "after-write")
        value = source_preinstall_value(selected)
        canonical = (json.dumps(value, indent=2, sort_keys=True) + "\n").encode()
        self.assertEqual(
            run_cell.validate_source_preinstall_document(value, canonical, selected),
            value,
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

    def test_adoption_checkpoint_keeps_state_hash_across_ownership_normalization(self) -> None:
        checkpoint = {
            "state_sha256": "a" * 64,
            "active_ownership_sha256": "a" * 64,
        }
        run_cell.validate_adopted_source_state_checkpoint(checkpoint, "a" * 64)
        with self.assertRaises(run_cell.ControllerError):
            run_cell.validate_adopted_source_state_checkpoint(checkpoint, "b" * 64)

    def test_rolled_back_bind_accepts_exact_source_adoption_proof(self) -> None:
        selected = cell("rolled-back", "after-write")
        value = source_adoption_value(selected)
        canonical = (json.dumps(value, indent=2, sort_keys=True) + "\n").encode()
        self.assertEqual(
            run_cell.validate_source_adoption_document(value, canonical, selected),
            value,
        )

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

    def test_owner_bind_normalization_requires_exact_absence(self) -> None:
        proof = {
            "source_normalization_identity_receipt_path": "absent",
            "source_normalization_identity_receipt_sha256": "absent",
        }
        with mock.patch.object(run_cell, "require_absent_path") as require_absent:
            evidence = run_cell.validate_source_normalization_provenance(
                proof, "owner-bind", cell("rolling-back"), {}, "state", "127.0.0.1"
            )
        self.assertEqual(evidence, {"path": "absent", "sha256": "absent", "exists": False})
        require_absent.assert_called_once_with(
            run_cell.SOURCE_NORMALIZATION_IDENTITY_PATH,
            "owner-bind source normalization identity receipt",
        )
        for field, invalid in (
            ("source_normalization_identity_receipt_path", run_cell.SOURCE_NORMALIZATION_IDENTITY_PATH),
            ("source_normalization_identity_receipt_sha256", "a" * 64),
        ):
            unsafe = dict(proof, **{field: invalid})
            with self.subTest(field=field), self.assertRaises(run_cell.ControllerError):
                run_cell.validate_source_normalization_provenance(
                    unsafe, "owner-bind", cell("rolling-back"), {}, "state", "127.0.0.1"
                )

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


    def test_owner_bind_native_preflight_rejects_extra_inventory(self) -> None:
        data = {
            "/etc/bind/named.conf":
                b'include "/etc/bind/named.conf.options";\ninclude "/etc/bind/named.conf.local";\ninclude "/etc/bind/named.conf.root-hints";\n',
            "/etc/bind/named.conf.options":
                b'options { directory "/var/cache/bind"; recursion no; listen-on { any; }; listen-on-v6 { none; }; };\n',
            "/etc/bind/named.conf.local":
                b'zone "owner.test" IN { type master; file "/etc/bind/db.owner.test"; allow-update { none; }; };\n',
            "/etc/bind/named.conf.root-hints":
                b'zone "." { type hint; file "/usr/share/dns/root.hints"; };\n',
            "/etc/bind/db.owner.test": b'www IN A 192.0.2.10\n',
        }
        def read(path: str, *_args: object, **_kwargs: object) -> tuple[bytes, object]:
            return data[path], mock.Mock(st_dev=1, st_ino=2)
        native = b"owner.test IN _default master\n. IN _default hint\n"
        def command(argv: list[str], **_kwargs: object) -> object:
            return mock.Mock(returncode=0, stderr=b"",
                             stdout=b"install ok installed" if argv[0] == "/usr/bin/dpkg-query" else native)
        with mock.patch.object(run_cell.os, "geteuid", return_value=0, create=True), mock.patch.object(
            run_cell, "secure_read_bytes", side_effect=read
        ), mock.patch.object(run_cell, "require_absent_path"), mock.patch.object(
            run_cell.subprocess, "run", side_effect=command
        ):
            result = run_cell.validate_owner_bind_native_source()
            self.assertEqual(result["inventory"], native.decode().splitlines())
            native = native + b"foreign.test IN _default master\n"
            with self.assertRaises(run_cell.ControllerError):
                run_cell.validate_owner_bind_native_source()

    def test_owner_bind_handoff_requires_exact_source_bind_envelope(self) -> None:
        selected = replace(cell("rolling-back", "after-write"),
                           cell_id=run_cell.BIND_HANDOFF_CELL)
        identity = boundary_identity()
        identity.update(source_engine="", source_epoch=0, target_epoch=1,
                        source_revision=0)
        observed = observed_journal_value(
            selected, "rolling-back", "/tmp/journal.json", identity
        )
        observed["schema"] = run_cell.BIND_HANDOFF_JOURNAL_SCHEMA
        source = {
            "kind": "bind-adoption-source/v1",
            "files": [
                {"path": "/etc/bind/db.owner.test", "sha256": "a" * 64,
                 "uid": 0},
                {"path": "/usr/share/dns/root.hints", "sha256": "b" * 64,
                 "uid": 0},
            ],
            "zones": [
                {"name": "owner.test", "class": "IN", "type": "master",
                 "file": "/etc/bind/db.owner.test", "soa_serial": 7},
                {"name": ".", "class": "IN", "type": "hint",
                 "file": "/usr/share/dns/root.hints", "soa_serial": 0},
            ],
        }
        plan = {
            "kind": "bind-switch-config/v1", "host_layout": "apt",
            "config_after": [{}, {}],
            "bind_unchanged_config": [
                {"path": "/etc/bind/named.conf"},
                {"path": "/etc/bind/named.conf.root-hints"},
            ],
            "source_bind": source, "digest": "c" * 64,
        }
        journal = dict(observed, inverse_plan=plan,
                       state_before={"exists": False},
                       target_units_before=[
                           {"name": "bind9.service", "active_state": "active"},
                           {"name": "named.service", "active_state": "active"},
                       ])
        status = mock.Mock(st_dev=1, st_ino=2, st_mode=0o100600)
        with mock.patch.object(run_cell, "secure_read_json",
                               return_value=(journal, status)), mock.patch.object(
                               run_cell, "sha256_file", return_value="d" * 64):
            run_cell.validate_journal_disk_state(
                "/tmp/journal.json", "rolling-back", "1" * 32,
                identity, cell=selected, bind_rollback_after_target_started=True,
            )
        for bad in (
            dict(journal, inverse_plan={**plan, "source_bind": None}),
            dict(journal, inverse_plan={**plan, "source_pdns": {}}),
            dict(journal, state_before={"exists": True}),
        ):
            with mock.patch.object(run_cell, "secure_read_json",
                                   return_value=(bad, status)), self.assertRaises(
                                   run_cell.BoundaryUnverified):
                run_cell.validate_journal_disk_state(
                    "/tmp/journal.json", "rolling-back", "1" * 32,
                    identity, cell=selected, bind_rollback_after_target_started=True,
                )

    def test_bind_handoff_v2_marker_and_source_proof_are_exactly_scoped(self) -> None:
        selected = replace(
            cell("rolling-back", "after-write"),
            cell_id=run_cell.BIND_HANDOFF_CELL,
        )
        identity = boundary_identity()
        observed = observed_journal_value(
            selected, "rolling-back", "/tmp/journal.json", identity
        )
        observed["schema"] = run_cell.BIND_HANDOFF_JOURNAL_SCHEMA
        run_cell.validate_observed_journal(
            selected, observed, "1" * 32, identity
        )
        with self.assertRaises(run_cell.BoundaryUnverified):
            run_cell.validate_observed_journal(
                cell("rolling-back", "after-write"),
                observed, "1" * 32, identity
            )
        old = dict(observed, schema=run_cell.JOURNAL_SCHEMA)
        with self.assertRaises(run_cell.BoundaryUnverified):
            run_cell.validate_observed_journal(
                selected, old, "1" * 32, identity
            )
        plan = {
            "kind": "bind-switch-config/v1", "host_layout": "apt",
            "config_after": [{}, {}], "digest": "a" * 64,
            "source_pdns": {
                "kind": "pdns-source/v1", "config_before": [{}],
                "database": {"logical_sha256": "b" * 64},
            },
        }
        journal = dict(observed, inverse_plan=plan)
        status = mock.Mock(st_dev=1, st_ino=2, st_mode=0o100600)
        with mock.patch.object(
            run_cell, "secure_read_json", return_value=(journal, status)
        ), mock.patch.object(run_cell, "sha256_file", return_value="c" * 64):
            proof = run_cell.validate_journal_disk_state(
                "/tmp/journal.json", "rolling-back", "1" * 32,
                identity, cell=selected,
            )
            self.assertEqual(proof["observed_phase"], "rolling-back")
            for bad in (
                dict(journal, inverse_plan=None),
                dict(journal, inverse_plan={**plan, "source_pdns": None}),
                dict(journal, schema=run_cell.JOURNAL_SCHEMA),
            ):
                with mock.patch.object(
                    run_cell, "secure_read_json", return_value=(bad, status)
                ), self.assertRaises(run_cell.BoundaryUnverified):
                    run_cell.validate_journal_disk_state(
                        "/tmp/journal.json", "rolling-back", "1" * 32,
                        identity, cell=selected,
                    )
            # Shape captured from the actual Debian V2 producer at the late
            # target-started rollback boundary, before the disposable VM reset.
            producer = json.loads(
                (Path(__file__).with_name("testdata")
                 / "late-bind-v2-producer-shape.json").read_text(encoding="utf-8")
            )
            self.assertEqual(producer["phase"], "rolling-back")
            self.assertEqual(producer["schema"], run_cell.BIND_HANDOFF_JOURNAL_SCHEMA)
            full_plan = {
                **plan,
                "config_after": [{"path": path} for path in producer["config_after_paths"]],
                "bind_unchanged_config": [
                    {"path": path} for path in producer["unchanged_paths"]
                ],
                "source_pdns": {
                    **plan["source_pdns"],
                    "config_before": [
                        {"path": path} for path in producer["source_paths"]
                    ],
                    "database": {
                        "path": producer["database_path"],
                        "logical_sha256": "b" * 64, "mode": 416,
                        "uid": 100, "gid": 100, "device": 1, "inode": 2,
                    },
                },
            }
            self.assertEqual(set(full_plan), set(producer["plan_keys"]))
            self.assertEqual(set(full_plan["source_pdns"]), set(producer["source_keys"]))
            self.assertEqual(set(full_plan["source_pdns"]["database"]),
                             set(producer["database_keys"]))
            full_journal = dict(journal, inverse_plan=full_plan)
            with mock.patch.object(
                run_cell, "secure_read_json", return_value=(full_journal, status)
            ):
                run_cell.validate_journal_disk_state(
                    "/tmp/journal.json", "rolling-back", "1" * 32,
                    identity, cell=selected,
                    bind_rollback_after_target_started=True,
                )
            for bad_plan in (
                plan,
                {**full_plan, "bind_unchanged_config": []},
                {**full_plan, "source_pdns": {
                    **full_plan["source_pdns"], "config_before": [],
                }},
                {**full_plan, "source_pdns": {
                    **full_plan["source_pdns"],
                    "config_before": list(reversed(
                        full_plan["source_pdns"]["config_before"]
                    )),
                }},
            ):
                with mock.patch.object(
                    run_cell, "secure_read_json",
                    return_value=(dict(journal, inverse_plan=bad_plan), status),
                ), self.assertRaises(run_cell.BoundaryUnverified):
                    run_cell.validate_journal_disk_state(
                        "/tmp/journal.json", "rolling-back", "1" * 32,
                        identity, cell=selected,
                        bind_rollback_after_target_started=True,
                    )

def owner_cell(phase: str = "target-staged", **changes: object) -> object:
    selected = run_cell.CellSpec(
        cell_id=f"bind__{phase}__after-write__standalone__peer-reachable",
        driver="bind",
        role="standalone",
        peer_reachability="reachable",
        phase=phase,
        edge="after-write",
        point="after_write",
        source_fixture_policy=(
            "managed-pdns-required"
            if phase in run_cell.OWNER_INVERSE_CRITICAL_PHASES
            else "driver-specific"
        ),
    )
    return replace(selected, **changes) if changes else selected


def owner_v2_plan() -> dict[str, object]:
    producer = json.loads(
        (Path(__file__).with_name("testdata") / "late-bind-v2-producer-shape.json")
        .read_text(encoding="utf-8")
    )
    return {
        "kind": "bind-switch-config/v1",
        "host_layout": "apt",
        "config_after": [{"path": path} for path in producer["config_after_paths"]],
        "digest": "a" * 64,
        "source_pdns": {
            "kind": "pdns-source/v1",
            "config_before": [{"path": path} for path in producer["source_paths"]],
            "database": {"path": producer["database_path"], "logical_sha256": "b" * 64},
        },
    }


class FakeOwnerGuest:
    """Scripted guest observations for the owner-inverse flow (no real host)."""

    REQUEST = "1" * 32
    OWNER = "2" * 32
    QUALIFIER = "dns-engine-switch/v1:sha256:" + "3" * 64

    def __init__(
        self,
        *,
        command: str = "recover-dns-bind-switch",
        target: str = "bind",
        pid_key: str = "pdns_main_pid",
        **deviations: object,
    ) -> None:
        self.command = command
        self.target = target
        self.pid_key = pid_key
        self.d = {
            "release_code": run_cell.AGENT_RELEASED_NATIVE_UNKNOWN,
            "released": True,
            "journal_phase": "rolling-back",
            "refusal_logged": True,
            "status_names": True,
            "status_mutates": False,
            "owner_retires": True,
            "owner_changes_ledger": None,
            "rerun_mutates": False,
            "dns_after_owner_ok": True,
            "pid_after_owner": 600,
            "owner_files_changed": False,
            # Critical variant (source stopped before the cut).
            "pdns_step2_state": "inactive",
            "bind_step2_active": False,
            "pdns_after_owner_active": True,
            "bind_after_owner_active": False,
            "named_after_owner": [],
            "journal_readable": True,
            # Completed re-runs exit 0 since f7a844f7.
            "rerun_exit": 0,
            # V2 rollback standby end state (f7a844f7).
            "bind_sealed": True,
            "tree_after": False,
            "tree_at_step2": True,
            "summary": None,
            **deviations,
        }
        self.journal_exists = True
        self.after_owner = False
        self.ledger_sha = "4" * 64
        self.job = self.job_for(self.d["release_code"])
        self.status_calls = 0
        self.told_calls = 0
        self.owner_calls: list[str] = []

    def job_for(self, code: object) -> dict[str, object]:
        return {
            "request_id": self.REQUEST, "owner_id": self.OWNER,
            "kind": "dns_engine_switch", "target": self.target,
            "package_name": self.QUALIFIER, "status": "failed",
            "phase": "interrupted", "attempt": 1,
            "started_at": "2026-09-29T10:00:00Z",
            "updated_at": "2026-09-29T10:01:00Z",
            "finished_at": "2026-09-29T10:01:00Z",
            "deadline_at": "2026-09-29T10:45:00Z",
            "lease_expires_at": run_cell.ZERO_LEDGER_TIME,
            "error_code": code, "error_message": "recorded by the product",
        }

    def snapshot(self, state_dir: str, journal_path: str) -> dict[str, object]:
        value = {
            label: {"path": label, "exists": True, "sha256": "5" * 64}
            for label, _ in run_cell.PRIVATE_EVIDENCE_FILES
        }
        value["journal"] = {"path": journal_path, "exists": self.journal_exists}
        value["ledger"] = {"path": "ledger", "exists": True, "sha256": self.ledger_sha,
                           "size": 900}
        return value

    def read_ledger(self, state_dir: str, request_id: str):
        return {"active_request_id": "", "sha256": self.ledger_sha}, dict(self.job)

    def wait_release(self, settings: object, transcript: object) -> dict[str, object]:
        if not self.d["released"]:
            return {"released": False, "reads": 3, "last_error": None}
        ledger, job = self.read_ledger("", self.REQUEST)
        return {"released": True, "reads": 1, "ledger": ledger, "job": job}

    def journal_state(self, *args: object, **kwargs: object) -> dict[str, object]:
        expected = args[1]
        if not self.journal_exists or self.d["journal_phase"] != expected:
            raise run_cell.BoundaryUnverified(
                f"DNS switch journal phase {self.d['journal_phase']!r}, want {expected!r}"
            )
        return {"exists": True, "observed_phase": expected}

    def run_owner(self, settings, argv, label, environment, transcript):
        if label == "owner-dns-switch-status-after-owner-command":
            self.told_calls += 1
            text = "No DNS switch journal was observed for request " + self.REQUEST + ".\n"
            return {"ran": True, "argv": list(argv), "returncode": 0,
                    "output": text, "_raw_output": text.encode()}
        if label == "owner-dns-switch-status":
            self.status_calls += 1
            if self.d["status_mutates"]:
                self.ledger_sha = "9" * 64
            text = (
                "The server owner can continue that exact inverse with: "
                f"/usr/libexec/celikpanel/recovery {self.command} "
                f"--request-id {self.REQUEST}.\n"
                if self.d["status_names"]
                else "No owner recovery command applies to this journal.\n"
            )
            return {"ran": True, "argv": list(argv), "returncode": 0,
                    "output": text, "_raw_output": text.encode()}
        self.owner_calls.append(label)
        output = "evidence only\n"
        if label == f"owner-{self.command}":
            self.after_owner = True
            if self.d["owner_retires"]:
                # The admitted inverse retires the journal and leaves the
                # Agent-written released ledger untouched.
                self.journal_exists = False
                returncode = 0
            else:
                returncode = 1
            if self.d["owner_changes_ledger"] is not None:
                self.job = self.job_for(self.d["owner_changes_ledger"])
                self.ledger_sha = "6" * 64
            output = self.summary_text()
        else:
            returncode = self.d["rerun_exit"]
            output = ("This request is already reconciled; current DNS health is not "
                      "checked. Nothing was changed now.\n")
            if self.d["rerun_mutates"]:
                self.ledger_sha = "7" * 64
        return {"ran": True, "argv": list(argv), "returncode": returncode,
                "output": output, "_raw_output": output.encode()}

    GENERATION = "a" * 64

    def summary_text(self) -> str:
        if self.d["summary"] is not None:
            return self.d["summary"]
        return "\n".join([
            "The accepted BIND switch rollback reached its terminal verdict for request "
            + self.REQUEST + ".",
            "Restored: the PowerDNS service, its DNS state receipt, the BIND configuration "
            "files this switch changed and the managed BIND pointer.",
            "BIND units: named.service and bind9.service are under the package guard's "
            "persistent mask, inactive and not enabled, so BIND cannot start by accident.",
            f"Removed: the staged BIND generation {self.GENERATION} that this switch created.",
            "Intentionally kept as rollback standby: the installed bind9 packages, the rndc "
            "key and the BIND install-ownership record. This command does not remove packages.",
            "Left in BIND's working directory /var/cache/bind, which is outside the managed "
            "BIND root, so they were not removed: managed-keys.bind.",
            "Local port-53 listeners accepted as the systemd-resolved stub resolver (unit "
            "cgroup and address proved; not DNS authority): 127.0.0.53:53, 127.0.0.54:53.",
            "",
        ])

    def rollback_facts(self, journal_path: str) -> dict[str, object]:
        return {
            "target_generation": self.GENERATION,
            "target_units_before": [
                {"name": "named.service", "load_state": "not-found"},
                {"name": "bind9.service", "load_state": "not-found"},
            ],
            "sealed_end_state_expected": True,
            "generation_tree": "/var/cache/bind/celikpanel/generations/" + self.GENERATION,
            "generation_tree_present_at_step2": self.d["tree_at_step2"],
        }

    def guard_masks(self, settings: object, environment: object) -> dict[str, object]:
        units = {}
        for unit in ("named.service", "bind9.service"):
            sealed = self.d["bind_sealed"]
            units[unit] = {
                "properties": {
                    "LoadState": "masked" if sealed else "loaded",
                    "ActiveState": "inactive",
                    "SubState": "dead",
                    "UnitFileState": "masked" if sealed else "disabled",
                    "MainPID": "0",
                },
                "persistent_link": (
                    {"path": "/etc/systemd/system/" + unit, "exists": True, "symlink": True,
                     "uid": 0, "target": "/dev/null"}
                    if sealed else {"path": "/etc/systemd/system/" + unit, "exists": False}
                ),
                "runtime_link": {"path": "/run/systemd/system/" + unit, "exists": False},
            }
        return {"units": units, "unknown": []}

    def source(self, settings, environment, *, expected_pid=None) -> dict[str, object]:
        pid = self.d["pid_after_owner"] if self.after_owner else 600
        errors: list[str] = []
        report: dict[str, object] = {self.pid_key: pid}
        if self.after_owner and not self.d["dns_after_owner_ok"]:
            errors.append("authoritative UDP/TCP DNS: timed out")
            errors.append("BIND target unit is active: {'bind9.service': 'active'}")
        elif self.after_owner and not self.d["pdns_after_owner_active"]:
            errors.append("PowerDNS source unit is not active: {'pdns.service': 'inactive'}")
            errors.append("authoritative UDP/TCP DNS: connection refused")
        elif self.after_owner and self.d["bind_after_owner_active"]:
            errors.append("BIND target unit is active: {'named.service': 'active'}")
            errors.append(
                f"udp port-53 listeners are not owned only by PowerDNS MainPID {pid}: [812]"
            )
        else:
            report["dns"] = {"udp": {"answers": 1}, "tcp": {"answers": 1}}
            report["dns_answered_at"] = "2026-09-29T10:00:40Z"
        report.update({
            "errors": errors, "unknown": [],
            self.pid_key + "_expected": expected_pid,
            self.pid_key + "_changed": expected_pid is not None and pid != expected_pid,
            "ok": not errors,
        })
        return report

    def native(self, settings, environment) -> dict[str, object]:
        def unit(active: str, load: str = "loaded", file_state: str = "disabled",
                 pid: int = 0) -> dict[str, str]:
            return {"LoadState": load, "ActiveState": active, "SubState": "dead",
                    "UnitFileState": file_state, "MainPID": str(pid)}

        if not self.after_owner:
            bind_active = self.d["bind_step2_active"]
            pdns_active = self.d["pdns_step2_state"] == "active"
            named = [812] if bind_active else []
        else:
            bind_active = self.d["bind_after_owner_active"]
            pdns_active = self.d["pdns_after_owner_active"]
            named = list(self.d["named_after_owner"]) or ([812] if bind_active else [])
        bind_state = "active" if bind_active else "inactive"
        pdns_state = "active" if pdns_active else (
            "inactive" if self.after_owner else self.d["pdns_step2_state"]
        )
        # The rollback standby: both BIND names under the guard's mask.
        sealed = self.after_owner and not bind_active and self.d["bind_sealed"]
        bind_load, bind_file = ("masked", "masked") if sealed else ("loaded", "disabled")
        return {
            "at": "2026-09-29T10:00:20Z",
            "units": {
                "pdns.service": unit(
                    pdns_state, file_state="enabled", pid=700 if pdns_active else 0,
                ),
                "named.service": unit(bind_state, load=bind_load, file_state=bind_file,
                                      pid=812 if bind_active else 0),
                "bind9.service": unit(bind_state, load=bind_load, file_state=bind_file,
                                      pid=812 if bind_active else 0),
            },
            "named_processes": named,
            "port53_listeners": {"tcp": [], "udp": []},
            "dns": {"answered": bind_active or pdns_active, "at": "2026-09-29T10:00:20Z"},
            "unknown": [],
        }

    def unit_journal(self, settings, environment, since_epoch) -> dict[str, object]:
        if not self.d["journal_readable"]:
            return {"read": False, "error": "journalctl exited 1"}
        return {
            "read": True,
            "stopping_at": "2026-09-29T10:00:00Z",
            "stopped_at": "2026-09-29T10:00:01Z",
            "started_after_stop_at": "2026-09-29T10:00:30Z" if self.after_owner else None,
        }

    def probe(self, settings, environment, transcript, ordinal) -> dict[str, object]:
        if not self.after_owner or self.journal_exists:
            return {"ordinal": ordinal, "valid": True, "converged": False,
                    "recovery_outcome": "indeterminate", "active_dns_engine": "pdns",
                    "fingerprint": "e" * 64, "detail": "journal remains"}
        return {"ordinal": ordinal, "valid": True, "converged": False,
                "recovery_outcome": "rolled_back_source_active",
                "active_dns_engine": "pdns", "fingerprint": "f" * 64,
                "detail": "rolled back"}


PDNS_ROLLED_BACK_CELL = "pdns-adopt__rolled-back__after-write__standalone__peer-reachable"


def complete_rpc_retry_pass(**extra: object) -> dict:
    """A complete passing rpc-retry result: the pre-reboot verdict passes."""

    probe_value = {"valid": True, "recovery_outcome": "target_converged",
                   "fingerprint": "f" * 64}
    return {
        "status": "passed", "safety_status": "passed",
        "recovery_outcome": {"classification": "target_converged"},
        "recovery": {"attempts": [
            {"ordinal": 1, "command": {"returncode": 0}, "error": None},
            {"ordinal": 2, "command": {"returncode": 0}, "error": None},
        ]},
        "recovery_probes": [dict(probe_value, ordinal=1), dict(probe_value, ordinal=2)],
        **extra,
    }


def hypothetical_pdns_owner_admission():
    """Admit the retained pdns-adoption-v1 owner profile inside one test only.

    Since 3cc2de22 no cell is admitted with this profile (the restarted Agent
    finishes the V1 adoption at rolled-back itself); the profile code stays as
    the owner path for a host whose Agent re-proof fails, so its judgements are
    still exercised offline under this hypothetical admission.
    """

    cell_id, admission = run_cell._admission(
        "pdns-adoption-v1", "pdns-adopt", "rolled-back", "after-write", "reachable",
        "driver-specific", "pre-start", step2="rolled-back",
    )
    return mock.patch.dict(run_cell.OWNER_INVERSE_ADMISSIONS, {cell_id: admission})


class OwnerInverseAfterRestartTest(unittest.TestCase):
    NORMALIZATION = {
        "configuration": {"main": {"sha256": "8" * 64}, "managed": {"sha256": "9" * 64}},
        "database": {"device": 1, "inode": 2, "quick_check": "ok"},
    }

    def test_owner_inverse_cells_are_exact(self) -> None:
        manifest = json.loads(
            Path(__file__).with_name("manifest.json").read_text(encoding="utf-8")
        )
        expected_variants = {
            "bind__intent__after-write__standalone__peer-reachable": "pre-start",
            "bind__target-staged__after-write__standalone__peer-reachable": "pre-start",
            "bind__target-staged__before-write__standalone__peer-unreachable": "pre-start",
            "bind__source-stopped__after-write__standalone__peer-reachable": "critical",
            "bind__source-stopped__before-write__standalone__peer-reachable": "critical",
            "bind__target-started__after-write__standalone__peer-reachable": "critical",
            "bind__target-started__before-write__standalone__peer-reachable": "critical",
            "bind__rolled-back__before-write__standalone__peer-reachable": "pre-start",
            "bind__rolled-back__after-write__standalone__peer-reachable": "pre-start",
            "bind__rolling-back__after-write__standalone__peer-reachable": "pre-start",
        }
        self.assertEqual(set(run_cell.OWNER_INVERSE_CELLS), set(expected_variants))
        self.assertEqual(set(guest_bootstrap.OWNER_INVERSE_CELLS), set(expected_variants))
        for cell_id, variant in expected_variants.items():
            selected = run_cell.CellSpec.from_manifest(manifest, cell_id)
            admission = run_cell.OWNER_INVERSE_ADMISSIONS[cell_id]
            profile = run_cell.OWNER_INVERSE_PROFILES[admission.profile]
            with self.subTest(cell_id=cell_id):
                self.assertTrue(run_cell.is_owner_inverse_cell(selected))
                self.assertEqual(run_cell.owner_inverse_expectation(selected).variant, variant)
                # The host-side table names the same fixture, policy and precursor.
                self.assertEqual(
                    guest_bootstrap.OWNER_INVERSE_ADMISSIONS[cell_id],
                    (profile.source_fixture, admission.source_fixture_policy,
                     admission.requires_target_started_precursor),
                )
                self.assertEqual(
                    run_cell.expected_journal_schema(
                        selected, owner_inverse_after_restart=True
                    ),
                    profile.journal_schema,
                )
        self.assertTrue(
            run_cell.owner_inverse_expectation(owner_cell("target-started")).bind_active_after_restart
        )
        self.assertFalse(
            run_cell.owner_inverse_expectation(owner_cell("source-stopped")).bind_active_after_restart
        )
        before_started = run_cell.CellSpec.from_manifest(
            manifest, "bind__target-started__before-write__standalone__peer-reachable"
        )
        self.assertTrue(run_cell.owner_inverse_expectation(before_started).bind_active_after_restart)
        # The PowerDNS adoption rolled-back cut left the owner flow (3cc2de22).
        pdns_rolled = run_cell.CellSpec.from_manifest(manifest, PDNS_ROLLED_BACK_CELL)
        self.assertIsNone(run_cell.owner_inverse_profile(pdns_rolled))
        self.assertIn(PDNS_ROLLED_BACK_CELL, run_cell.PDNS_ADOPTION_STARTUP_ROLLBACK_CELLS)
        with hypothetical_pdns_owner_admission():
            self.assertEqual(
                run_cell.owner_inverse_profile(pdns_rolled).journal_schema,
                run_cell.JOURNAL_SCHEMA,
            )
        self.assertEqual(
            run_cell.V2_SWITCH_OWNER_CELLS,
            frozenset(c for c in expected_variants if c != run_cell.BIND_HANDOFF_CELL),
        )
        for changed in (
            owner_cell("committed"),
            owner_cell("source-stopped", source_fixture_policy="driver-specific"),
            owner_cell("target-staged", source_fixture_policy="managed-pdns-required"),
            owner_cell("target-staged", edge="before-write", point="before_write"),
            owner_cell("target-staged", peer_reachability="unreachable"),
            owner_cell("target-staged", role="paired-primary"),
            owner_cell("rolled-back", point="before_write"),
            replace(owner_cell("target-staged"), phase="intent"),
            replace(owner_cell("source-stopped"), phase="target-started"),
            owner_cell("target-staged", driver="pdns-switch"),
            replace(cell("rolling-back", "after-write"), cell_id=run_cell.BIND_HANDOFF_CELL,
                    source_fixture_policy="managed-pdns-required"),
            replace(cell("intent", "before-write", peer="unreachable"),
                    cell_id="bind__intent__before-write__standalone__peer-unreachable"),
            replace(cell("rolling-back", "after-write", driver="pdns-adopt"),
                    cell_id=run_cell.PDNS_HANDOFF_CELL),
        ):
            with self.subTest(changed=changed):
                self.assertFalse(run_cell.is_owner_inverse_cell(changed))
                with self.assertRaises(run_cell.ControllerError):
                    run_cell.owner_inverse_expectation(changed)

    def test_critical_cells_keep_stale_v1_without_the_flag(self) -> None:
        manifest = json.loads(
            Path(__file__).with_name("manifest.json").read_text(encoding="utf-8")
        )
        for phase in sorted(run_cell.OWNER_INVERSE_CRITICAL_PHASES):
            selected = run_cell.CellSpec.from_manifest(
                manifest, f"bind__{phase}__after-write__standalone__peer-reachable"
            )
            with self.subTest(phase=phase):
                self.assertTrue(run_cell.is_owner_inverse_cell(selected))
                self.assertEqual(
                    run_cell.expected_journal_schema(selected), run_cell.JOURNAL_SCHEMA
                )
                self.assertEqual(
                    run_cell.expected_journal_schema(
                        selected, owner_inverse_after_restart=True
                    ),
                    run_cell.BIND_HANDOFF_JOURNAL_SCHEMA,
                )
                identity = boundary_identity()
                observed = observed_journal_value(selected, phase, "/tmp/j.json", identity)
                run_cell.validate_observed_journal(selected, observed, "1" * 32, identity)
                with self.assertRaises(run_cell.BoundaryUnverified):
                    run_cell.validate_observed_journal(
                        selected, dict(observed, schema=run_cell.BIND_HANDOFF_JOURNAL_SCHEMA),
                        "1" * 32, identity,
                    )
        # Paired critical cells never join the flow.
        paired = run_cell.CellSpec.from_manifest(
            manifest, "bind__source-stopped__after-write__paired-primary__peer-reachable"
        )
        self.assertFalse(run_cell.is_owner_inverse_cell(paired))

    def test_boundary_requires_v2_journal_only_in_owner_inverse_mode(self) -> None:
        selected = owner_cell("target-staged")
        identity = boundary_identity()
        observed = observed_journal_value(selected, "target-staged", "/tmp/j.json", identity)
        v2 = dict(observed, schema=run_cell.BIND_HANDOFF_JOURNAL_SCHEMA)
        run_cell.validate_observed_journal(
            selected, v2, "1" * 32, identity, owner_inverse_after_restart=True
        )
        run_cell.validate_observed_journal(selected, observed, "1" * 32, identity)
        with self.assertRaises(run_cell.BoundaryUnverified):
            run_cell.validate_observed_journal(selected, v2, "1" * 32, identity)
        with self.assertRaises(run_cell.BoundaryUnverified):
            run_cell.validate_observed_journal(
                selected, observed, "1" * 32, identity, owner_inverse_after_restart=True
            )
        journal = dict(v2, inverse_plan=owner_v2_plan())
        status = mock.Mock(st_dev=1, st_ino=2, st_mode=0o100600)
        with mock.patch.object(
            run_cell, "secure_read_json", return_value=(journal, status)
        ), mock.patch.object(run_cell, "sha256_file", return_value="c" * 64):
            proof = run_cell.validate_journal_disk_state(
                "/tmp/j.json", "target-staged", "1" * 32, identity,
                cell=selected, owner_inverse_after_restart=True,
            )
            self.assertEqual(proof["observed_phase"], "target-staged")
            with self.assertRaises(run_cell.BoundaryUnverified):
                run_cell.validate_journal_disk_state(
                    "/tmp/j.json", "target-staged", "1" * 32, identity, cell=selected,
                )
            with self.assertRaises(run_cell.ControllerError):
                run_cell.validate_journal_disk_state(
                    "/tmp/j.json", "target-staged", "1" * 32, identity,
                    cell=cell("target-staged", "after-write"),
                    owner_inverse_after_restart=True,
                )
        plan = owner_v2_plan()
        bad_plans = (
            None,
            {**plan, "kind": "bind-switch-config/v0"},
            {**plan, "host_layout": "arch"},
            {**plan, "source_bind": {"kind": "bind-adoption-source/v1"}},
            {**plan, "source_pdns": {**plan["source_pdns"], "kind": "other"}},
            {**plan, "source_pdns": {**plan["source_pdns"], "database": {"path": "/x"}}},
            {**plan, "source_pdns": {**plan["source_pdns"], "config_before": []}},
        )
        for bad in bad_plans:
            with self.subTest(bad=bad), self.assertRaises(run_cell.BoundaryUnverified):
                run_cell.validate_owner_inverse_frozen_source(dict(journal, inverse_plan=bad))
        with self.assertRaises(run_cell.BoundaryUnverified):
            run_cell.validate_owner_inverse_frozen_source(dict(journal, source_engine=""))

    @unittest.skipUnless(
        sys.platform == "linux" and hasattr(os, "geteuid") and os.geteuid() == 0,
        "owner executable ownership proof requires a root Linux host",
    )
    def test_settings_gate_the_owner_inverse_mode(self) -> None:
        executable = os.path.realpath(sys.executable)
        with tempfile.TemporaryDirectory() as root:
            scenario = os.path.join(root, "scenario.json")
            receipt = os.path.join(root, "identity.json")
            trigger = (executable, "rpc-switch", "--scenario", scenario,
                       "--identity-receipt", receipt, "--timeout", "45m")
            settings = run_cell.Settings(
                cell=owner_cell("intent"), request_id="1" * 32, nonce="a" * 32,
                tagged_agent_command=(executable,), trigger_mode="socket",
                trigger_command=trigger,
                recovery_command=(executable, "rpc-retry", *trigger[2:]),
                source_proof_path=os.path.join(root, "source-proof.json"),
                agent_restart_command=(executable, "-V"),
                panel_restart_command=(executable, "-V"),
                recovery_probe_command=(executable, "-V"),
                peer_partition_command=None, command_cwd=root, state_dir=root,
                mutation_lock=os.path.join(root, "mutation.lock"),
                agent_socket=os.path.join(root, "agent.sock"),
                agent_token_file=os.path.join(root, "agent.token"),
                journal_path=os.path.join(root, "journal.json"),
                marker_path=os.path.join(root, "marker.json"),
                proof_path=os.path.join(root, "proof.json"),
                result_path=os.path.join(root, "result.json"),
                transcript_path=os.path.join(root, "transcript.log"),
                dns_address="192.0.2.10", dns_port=53, dns_name="www.s1-kill.test",
                dns_type="A", panel_address="127.0.0.1", panel_port=2083,
                startup_timeout=1, boundary_timeout=1, stop_timeout=1, kill_timeout=1,
                command_timeout=1, recovery_timeout=1, endpoint_timeout=1,
                dns_timeout=1, stability_seconds=1, stability_interval=1,
                owner_inverse_after_restart=True,
            )
            with mock.patch.multiple(
                run_cell, OWNER_RECOVERY_EXECUTABLE=executable,
                JOURNALCTL_EXECUTABLE=executable, SS_EXECUTABLE=executable,
            ):
                evidence = run_cell.validate_settings(settings)
                self.assertEqual(evidence["owner_recovery"]["path"], executable)
                self.assertIn("journalctl", evidence)
                for changed in (
                    replace(settings, cell=owner_cell("rolled-back")),
                    replace(settings, cell=owner_cell("intent", peer_reachability="unreachable")),
                    replace(settings, stop_after_kill_for_independent_recovery=True),
                    replace(settings, bind_rollback_after_target_started=True),
                    replace(settings, source_proof_path=None),
                ):
                    with self.subTest(changed=changed), self.assertRaises(
                        run_cell.ControllerError
                    ):
                        run_cell.validate_settings(changed)
                manifest = json.loads(
                    Path(__file__).with_name("manifest.json").read_text(encoding="utf-8")
                )
                adoption = replace(settings, cell=run_cell.CellSpec.from_manifest(
                    manifest, run_cell.BIND_HANDOFF_CELL))
                with self.assertRaisesRegex(run_cell.ControllerError, "admitted owner-inverse"):
                    run_cell.validate_settings(adoption)
                run_cell.validate_settings(
                    replace(adoption, bind_rollback_after_target_started=True)
                )
                startup = replace(
                    settings, owner_inverse_after_restart=False,
                    expect_agent_startup_rollback=True,
                    cell=run_cell.CellSpec.from_manifest(
                        manifest, "pdns-adopt__intent__after-write__standalone__peer-reachable"
                    ),
                )
                self.assertIn("journalctl", run_cell.validate_settings(startup))
                for wrong in (
                    replace(startup, cell=settings.cell),
                    replace(startup, owner_inverse_after_restart=True),
                ):
                    with self.assertRaises(run_cell.ControllerError):
                        run_cell.validate_settings(wrong)
                writable = os.path.join(root, "recovery")
                Path(writable).write_text("#!/bin/sh\n", encoding="utf-8")
                os.chmod(writable, 0o775)
                with mock.patch.object(run_cell, "OWNER_RECOVERY_EXECUTABLE", writable):
                    with self.assertRaisesRegex(run_cell.ControllerError, "world writable"):
                        run_cell.validate_settings(settings)
            self.assertNotIn(
                "owner_recovery",
                run_cell.validate_settings(replace(settings, owner_inverse_after_restart=False)),
            )

    def test_owner_inverse_source_proof_requires_managed_pdns(self) -> None:
        scenario = {"source_fixture": "uninitialized"}
        with mock.patch.object(
            run_cell, "validate_source_scenario", return_value=(scenario, {"sha256": "0"})
        ), mock.patch.object(run_cell, "secure_json_with_digest") as read:
            with self.assertRaisesRegex(run_cell.ControllerError, "managed PowerDNS"):
                run_cell.validate_socket_source_proof(
                    "/p", owner_cell(), "/s", "/i", "/state", "/j", "192.0.2.10", 53,
                    "www.s1-kill.test", "A", owner_inverse_after_restart=True,
                )
            read.assert_not_called()

    def test_port53_listener_parser_attributes_answers_to_exact_owner(self) -> None:
        output = "\n".join([
            'udp UNCONN 0 0 127.0.0.54:53 0.0.0.0:* users:(("systemd-resolve",pid=300,fd=20))',
            'udp UNCONN 0 0 127.0.0.53%lo:53 0.0.0.0:* users:(("systemd-resolve",pid=300,fd=18))',
            'udp UNCONN 0 0 192.0.2.10:53 0.0.0.0:* users:(("pdns_server",pid=600,fd=5))',
            'tcp LISTEN 0 128 192.0.2.10:53 0.0.0.0:* users:(("pdns_server",pid=600,fd=6))',
            'tcp LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=10,fd=3))',
            'tcp LISTEN 0 4096 [::]:5353 [::]:* users:(("x",pid=11,fd=3))',
        ])
        listeners = run_cell.parse_port53_listeners(output, "192.0.2.10")
        self.assertEqual([item["pids"] for item in listeners["udp"]], [[600]])
        self.assertEqual([item["pids"] for item in listeners["tcp"]], [[600]])
        wildcard = output + '\ntcp LISTEN 0 10 [::]:53 [::]:* users:(("named",pid=700,fd=9))'
        self.assertEqual(
            [item["pids"] for item in run_cell.parse_port53_listeners(wildcard, "192.0.2.10")["tcp"]],
            [[600], [700]],
        )
        for bad in ("garbage", "sctp LISTEN 0 1 1.2.3.4:53 *:*",
                    "tcp LISTEN 0 128 192.0.2.10:53 0.0.0.0:*"):
            with self.subTest(bad=bad), self.assertRaises(run_cell.ControllerError):
                run_cell.parse_port53_listeners(bad, "192.0.2.10")

    def serving(self, *, units=None, pid=600, listeners=None, dns_error=None, expected=None):
        settings = mock.Mock(
            endpoint_timeout=1.0, dns_address="192.0.2.10", dns_port=53,
            dns_name="www.s1-kill.test", dns_type="A", dns_timeout=1.0,
        )
        units = units or {"bind9.service": "inactive", "named.service": "inactive",
                          "pdns.service": "active"}
        listeners = listeners or {"tcp": [{"pids": [600]}], "udp": [{"pids": [600]}]}
        dns = mock.Mock(side_effect=dns_error) if dns_error else mock.Mock(
            return_value={"udp": {}, "tcp": {}}
        )
        with mock.patch.object(run_cell, "inspect_dns_unit_states", return_value=units), \
                mock.patch.object(run_cell, "read_unit_main_pid", return_value=pid), \
                mock.patch.object(run_cell, "observe_port53_listeners", return_value=listeners), \
                mock.patch.object(run_cell, "query_authoritative_dns", dns):
            return run_cell.observe_pdns_source_serving(
                settings, {"PATH": "/usr/bin"}, expected_pid=expected
            )

    def test_source_serving_requires_exclusive_powerdns_authority(self) -> None:
        self.assertTrue(self.serving()["ok"])
        self.assertFalse(self.serving(expected=600)["pdns_main_pid_changed"])
        changed = self.serving(pid=601, expected=600,
                               listeners={"tcp": [{"pids": [601]}], "udp": [{"pids": [601]}]})
        self.assertTrue(changed["ok"])
        self.assertTrue(changed["pdns_main_pid_changed"])
        for report in (
            self.serving(units={"bind9.service": "active", "named.service": "active",
                                "pdns.service": "active"}),
            self.serving(units={"bind9.service": "inactive", "named.service": "inactive",
                                "pdns.service": "inactive"}),
            self.serving(listeners={"tcp": [{"pids": [600]}], "udp": [{"pids": [700]}]}),
            self.serving(listeners={"tcp": [], "udp": [{"pids": [600]}]}),
            self.serving(dns_error=run_cell.ControllerError("not authoritative")),
            self.serving(pid=0),
        ):
            with self.subTest(errors=report["errors"]):
                self.assertFalse(report["ok"])
                self.assertTrue(report["errors"])

    def test_release_and_verdict_classification_are_exact(self) -> None:
        guest = FakeOwnerGuest()
        identity = {"request_id": guest.REQUEST, "owner_id": guest.OWNER,
                    "manifest_qualifier": guest.QUALIFIER}
        ledger = {"active_request_id": "", "sha256": "0" * 64}
        release = guest.job_for(run_cell.AGENT_RELEASED_NATIVE_UNKNOWN)
        self.assertEqual(run_cell.classify_agent_release(ledger, release, identity), [])
        self.assertFalse(hasattr(run_cell, "classify_owner_verdict"))
        for label, job, active in (
            ("wrong reason", guest.job_for("host_unsupported_after_restart"), ""),
            ("still active", release, guest.REQUEST),
            ("other owner", dict(release, owner_id="4" * 32), ""),
            ("worker", dict(release, worker_pid=44), ""),
            ("lease", dict(release, lease_expires_at="2026-09-29T11:00:00Z"), ""),
            ("running", dict(release, status="running"), ""),
            ("absent", None, ""),
        ):
            with self.subTest(label=label):
                self.assertTrue(run_cell.classify_agent_release(
                    dict(ledger, active_request_id=active), job, identity
                ))
        self.assertIn(
            "error_code",
            " ".join(run_cell.classify_agent_release(
                ledger, guest.job_for("host_unsupported_after_restart"), identity
            )),
        )

    def test_status_naming_needs_exact_command_and_request(self) -> None:
        request = "1" * 32
        self.assertTrue(run_cell.owner_status_names_command(
            f"with: /usr/libexec/celikpanel/recovery recover-dns-bind-switch --request-id {request}. ".encode(),
            request,
        ))
        for text in (
            "No owner recovery command applies",
            f"recover-dns-bind-adoption --request-id {request}",
            "recover-dns-bind-switch --request-id " + "2" * 32,
        ):
            self.assertFalse(run_cell.owner_status_names_command(text.encode(), request))

    def flow_settings(self, guest: FakeOwnerGuest, selected: object, **attrs: object):
        return mock.Mock(
            cell=selected, request_id=guest.REQUEST,
            state_dir="/state", journal_path="/state/journal.json",
            agent_restart_command=("/bin/systemctl", "restart", "celikpanel-agent"),
            panel_restart_command=("/bin/systemctl", "restart", "celikpanel-panel"),
            agent_socket="/run/celikpanel/agent.sock", command_timeout=1.0,
            endpoint_timeout=1.0, recovery_timeout=1.0, panel_address="127.0.0.1",
            panel_port=2083, dns_address="192.0.2.10", dns_port=53,
            dns_name="www.s1-kill.test", dns_type="A", dns_timeout=1.0,
            command_cwd="/", **attrs,
        )

    def flow_patches(self, guest: FakeOwnerGuest, extra: dict | None = None):
        files = (
            {"configuration": {"main": {"sha256": "0" * 64}}, "database": {}}
            if guest.d["owner_files_changed"] else dict(self.NORMALIZATION)
        )
        ok = {"ok": True}
        sample = {"agent": ok, "panel": ok, "dns": ok}
        patches = dict(
            checked_command=mock.Mock(side_effect=lambda *a, **k: ({"argv": list(a[1])}, None)),
            wait_for_unix_socket=mock.Mock(return_value=(1, 2)),
            assert_unix_socket_stable=mock.Mock(return_value={"device": 1, "inode": 2}),
            inspect_restarted_agent_process=mock.Mock(return_value={"pid": 777}),
            wait_for_agent_release=mock.Mock(side_effect=guest.wait_release),
            validate_journal_disk_state=mock.Mock(side_effect=guest.journal_state),
            find_agent_owner_refusal=mock.Mock(
                side_effect=lambda *a, **k: {"observed": guest.d["refusal_logged"], "attempts": 1}
            ),
            observe_pdns_source_serving=mock.Mock(side_effect=guest.source),
            observe_native_dns_state=mock.Mock(side_effect=guest.native),
            read_pdns_unit_journal=mock.Mock(side_effect=guest.unit_journal),
            capture_pdns_journal=mock.Mock(return_value={"captured": True}),
            run_recovery_probe=mock.Mock(side_effect=guest.probe),
            snapshot_private_evidence=mock.Mock(side_effect=guest.snapshot),
            run_owner_command=mock.Mock(side_effect=guest.run_owner),
            read_request_ledger=mock.Mock(side_effect=guest.read_ledger),
            read_dns_state_semantic=mock.Mock(
                return_value={"sha256": "s" * 64, "semantic": {"engine": "pdns"}}
            ),
            owner_pdns_files=mock.Mock(return_value=files),
            wait_for_tcp=mock.Mock(return_value=None),
            query_authoritative_dns=mock.Mock(return_value={"udp": {}, "tcp": {}}),
            run_stability_window=mock.Mock(
                return_value=({"samples": [sample] * 31}, [], [])
            ),
            observe_bind_source_serving=mock.Mock(side_effect=guest.source),
            read_dns_state_optional=mock.Mock(return_value={"exists": False}),
            read_v2_rollback_facts=mock.Mock(side_effect=guest.rollback_facts),
            observe_bind_guard_masks=mock.Mock(side_effect=guest.guard_masks),
            generation_tree_present=mock.Mock(side_effect=lambda path: guest.d["tree_after"]),
            record_bind_rollback_leftovers=mock.Mock(return_value={"judged": False}),
            record_native_versions=mock.Mock(return_value={"packages": {}, "daemons": {}}),
        )
        patches.update(extra or {})
        return mock.patch.multiple(run_cell, **patches)

    def flow_result(
        self,
        guest: FakeOwnerGuest,
        state_before: dict | None = None,
        source_proof: dict | None = None,
    ) -> dict:
        return {
            "owner_inverse_preflight": {
                "source": {guest.pid_key: 600, "dns_answered_at": "2026-09-29T09:59:50Z"},
                "observed_at_epoch": 1,
                "state": state_before or {"sha256": "s" * 64, "semantic": {"engine": "pdns"}},
            },
            "source_proof": source_proof or {"source_normalization": dict(self.NORMALIZATION)},
        }

    def run_flow(
        self,
        guest: FakeOwnerGuest,
        phase: str = "target-staged",
        *,
        selected: object | None = None,
        state_before: dict | None = None,
        source_proof: dict | None = None,
        extra: dict | None = None,
        settings_attrs: dict | None = None,
    ) -> tuple[dict, list, list, list]:
        settings = self.flow_settings(
            guest, selected or owner_cell(phase), **(settings_attrs or {})
        )
        result = self.flow_result(guest, state_before, source_proof)
        safety: list = []
        verification: list = []
        diagnostic: list = []
        with self.flow_patches(guest, extra):
            run_cell.run_owner_inverse_after_restart(
                settings,
                result=result,
                transcript=mock.Mock(),
                ordinary={"PATH": "/usr/bin"},
                owner_environment={"PATH": "/usr/bin"},
                controller_identity={"effective_gid": 999},
                identity_receipt={"owner_id": guest.OWNER,
                                  "manifest_qualifier": guest.QUALIFIER},
                boundary_identity=boundary_identity(),
                old_socket_identity=(1, 1),
                kill_proven=True,
                safety_failures=safety,
                verification_failures=verification,
                diagnostic_failures=diagnostic,
            )
        return result, safety, verification, diagnostic

    def test_agent_decides_owner_executes_passes(self) -> None:
        guest = FakeOwnerGuest()
        result, safety, verification, _ = self.run_flow(guest)
        flow = result["owner_inverse_after_restart"]
        self.assertEqual((result["status"], result["safety_status"]), ("passed", "passed"))
        self.assertEqual(flow["status"], "passed", flow["failures"] + flow["ambiguities"])
        self.assertEqual((safety, verification), ([], []))
        self.assertEqual(
            guest.owner_calls,
            ["owner-recover-dns-bind-switch", "owner-recover-dns-bind-switch-rerun"],
        )
        self.assertEqual((guest.status_calls, guest.told_calls), (1, 1))
        self.assertEqual(result["recovery_outcome"]["classification"],
                         "rolled_back_source_serving")
        step2 = flow["steps"]["agent_restarted"]
        step5 = flow["steps"]["after_owner_command"]
        self.assertEqual(step2["ledger_after_release"]["sha256"], "4" * 64)
        self.assertTrue(step5["ledger_unchanged_since_release"])
        self.assertEqual(step5["job"]["error_code"], run_cell.AGENT_RELEASED_NATIVE_UNKNOWN)
        self.assertFalse(step5["evidence"]["journal"]["exists"])
        told = flow["steps"]["status_after_owner_command"]
        self.assertIs(told["judged"], False)
        self.assertIn("No DNS switch journal", told["command"]["output"])
        self.assertEqual(flow["steps"]["status"]["changed_evidence"], [])
        self.assertEqual(flow["steps"]["rerun"]["changed_evidence"], [])
        self.assertTrue(flow["steps"]["after_owner_command"]["owner_files_unchanged"])
        self.assertEqual(flow["owner_command"], [
            "/usr/libexec/celikpanel/recovery", "recover-dns-bind-switch",
            "--request-id", guest.REQUEST,
        ])
        self.assertNotIn("_raw_output", json.dumps(result))
        self.assertEqual(len(result["stability"]["samples"]), 31)
        self.assertTrue(result["complete_verdict"]["passed"], result["complete_verdict"])
        failed, _, _, _ = self.run_flow(FakeOwnerGuest(rerun_exit=3))
        self.assertFalse(failed["complete_verdict"]["passed"])

    def assert_failed_before_owner(self, guest: FakeOwnerGuest, text: str) -> dict:
        result, _, _, _ = self.run_flow(guest)
        flow = result["owner_inverse_after_restart"]
        self.assertEqual(result["status"], "failed")
        self.assertEqual(flow["status"], "failed")
        self.assertEqual(guest.owner_calls, [])
        self.assertIn("skipped", flow["steps"]["owner_command"])
        self.assertIn(text, " ".join(flow["failures"]))
        return flow

    def test_wrong_release_reason_fails_before_owner_command(self) -> None:
        guest = FakeOwnerGuest(release_code="host_not_ready_within_recovery_window")
        self.assert_failed_before_owner(guest, "error_code")
        self.assertEqual(guest.status_calls, 0)

    def test_journal_not_rolling_back_after_restart_fails(self) -> None:
        guest = FakeOwnerGuest(journal_phase="target-staged")
        self.assert_failed_before_owner(guest, "not V2 rolling-back")
        self.assertEqual(guest.status_calls, 0)

    def test_status_not_naming_the_command_fails(self) -> None:
        guest = FakeOwnerGuest(status_names=False)
        self.assert_failed_before_owner(guest, "does not name recover-dns-bind-switch")
        self.assertEqual(guest.status_calls, 1)

    def test_status_mutation_fails_and_blocks_owner_command(self) -> None:
        guest = FakeOwnerGuest(status_mutates=True)
        flow = self.assert_failed_before_owner(guest, "read-only status changed")
        self.assertEqual(flow["steps"]["status"]["changed_evidence"], ["ledger"])

    def test_dns_not_served_by_powerdns_fails(self) -> None:
        result, _, _, _ = self.run_flow(FakeOwnerGuest(dns_after_owner_ok=False))
        flow = result["owner_inverse_after_restart"]
        self.assertEqual(result["status"], "failed")
        self.assertIn("after owner command: authoritative", " ".join(flow["failures"]))
        self.assertIn("BIND target unit is active", " ".join(flow["failures"]))

    def test_owner_command_without_terminal_rollback_fails(self) -> None:
        result, _, _, _ = self.run_flow(FakeOwnerGuest(owner_retires=False))
        flow = result["owner_inverse_after_restart"]
        self.assertEqual(result["status"], "failed")
        joined = " ".join(flow["failures"])
        self.assertIn("left the switch journal in place", joined)
        self.assertEqual(
            flow["steps"]["owner_command"]["command"]["returncode"], 1
        )

    def test_rerun_mutation_and_owner_file_drift_fail(self) -> None:
        for deviation, text in (
            ({"rerun_mutates": True}, "re-run changed private evidence"),
            ({"owner_files_changed": True}, "owner PowerDNS configuration or database changed"),
            ({"refusal_logged": False}, "Agent journal lacks the refusal"),
        ):
            with self.subTest(deviation=deviation):
                result, _, _, _ = self.run_flow(FakeOwnerGuest(**deviation))
                self.assertEqual(result["status"], "failed")
                self.assertIn(text, " ".join(
                    result["owner_inverse_after_restart"]["failures"]
                ))

    def test_ledger_changed_by_owner_command_fails(self) -> None:
        # Even the former owner-recovery verdict is a deviation for a job the
        # Agent released: the admitted inverse must leave that ledger as is.
        for code in (
            "dns_engine_switch_rolled_back_by_owner_recovery",
            run_cell.AGENT_RELEASED_NATIVE_UNKNOWN,
        ):
            with self.subTest(code=code):
                result, _, _, _ = self.run_flow(FakeOwnerGuest(owner_changes_ledger=code))
                flow = result["owner_inverse_after_restart"]
                self.assertEqual((result["status"], flow["status"]), ("failed", "failed"))
                self.assertFalse(
                    flow["steps"]["after_owner_command"]["ledger_unchanged_since_release"]
                )
                self.assertIn("changed the ledger the Agent wrote", " ".join(flow["failures"]))
        result, _, _, _ = self.run_flow(FakeOwnerGuest(
            owner_changes_ledger="dns_engine_switch_rolled_back_by_owner_recovery"
        ))
        self.assertIn(
            "after owner command: Agent release: ledger job error_code",
            " ".join(result["owner_inverse_after_restart"]["failures"]),
        )

    def test_unknown_results_are_unverified_not_passed(self) -> None:
        result, _, verification, _ = self.run_flow(FakeOwnerGuest(pid_after_owner=601))
        flow = result["owner_inverse_after_restart"]
        self.assertEqual((result["status"], flow["status"]), ("unverified", "ambiguous"))
        self.assertIn("MainPID changed from 600 to 601", " ".join(verification))
        self.assertIn("pdns_journal", flow["steps"]["after_owner_command"]["source"])
        # A lease never released within the bound is an unknown result: no
        # status or owner command is run, and the cell stays unverified.
        guest = FakeOwnerGuest(released=False)
        result, _, verification, _ = self.run_flow(guest)
        self.assertEqual(result["status"], "unverified")
        self.assertIn("did not release the request", " ".join(verification))
        self.assertEqual((guest.status_calls, guest.owner_calls), (0, []))

    @staticmethod
    def critical_guest(phase: str, **deviations: object) -> FakeOwnerGuest:
        values: dict[str, object] = {
            "pid_after_owner": 700,
            "bind_step2_active": phase == "target-started",
        }
        values.update(deviations)
        return FakeOwnerGuest(**values)

    def test_critical_cells_pass_with_new_pid_and_recorded_outage(self) -> None:
        for phase in ("source-stopped", "target-started"):
            guest = self.critical_guest(phase)
            result, safety, verification, _ = self.run_flow(guest, phase)
            flow = result["owner_inverse_after_restart"]
            with self.subTest(phase=phase):
                self.assertEqual((result["status"], flow["status"]), ("passed", "passed"),
                                 flow["failures"] + flow["ambiguities"])
                self.assertEqual((safety, verification), ([], []))
                self.assertEqual(flow["variant"], "critical")
                self.assertEqual(result["recovery_outcome"]["classification"],
                                 "rolled_back_source_serving")
                step2 = flow["steps"]["agent_restarted"]
                self.assertNotIn("source", step2)
                self.assertEqual(step2["judged"]["pdns_active_state"], "inactive")
                self.assertEqual(
                    step2["native"]["units"]["named.service"]["ActiveState"],
                    "active" if phase == "target-started" else "inactive",
                )
                step5 = flow["steps"]["after_owner_command"]
                self.assertEqual(step5["pdns_main_pid"], {
                    "judged": False, "pre_cut": 600, "after_owner_command": 700,
                    "changed": True,
                    "reason": "the source was stopped before the cut; the inverse starts it again",
                })
                self.assertEqual(step5["bind_unit_files"]["named.service"],
                                 {"LoadState": "masked", "UnitFileState": "masked"})
                end_state = step5["rollback_end_state"]
                self.assertEqual(end_state["failures"], [])
                self.assertEqual(len(end_state["accepted_stub_listener_lines"]), 1)
                self.assertEqual(step5["native"]["named_processes"], [])
                self.assertFalse(
                    flow["steps"]["after_stability"]["source"]["pdns_main_pid_changed"]
                )
                self.assertEqual(result["dns_outage"], {
                    "judged": False,
                    "source_stopped_at": "2026-09-29T10:00:00Z",
                    "source_serving_again_at": "2026-09-29T10:00:40Z",
                    "seconds": 40.0,
                    "measured_from": result["dns_outage"]["measured_from"],
                    "last_source_answer_before_cut_at": "2026-09-29T09:59:50Z",
                    "pdns_started_after_stop_at": "2026-09-29T10:00:30Z",
                })
                self.assertIn("stopping entry",
                              result["dns_outage"]["measured_from"]["source_stopped_at"])
        # The pre-start variant keeps treating a PowerDNS PID change as unknown
        # and records no outage.
        result, _, _, _ = self.run_flow(FakeOwnerGuest())
        self.assertNotIn("dns_outage", result)
        self.assertEqual(result["owner_inverse_after_restart"]["variant"], "pre-start")

    def test_source_stopped_bind_state_is_recorded_not_judged(self) -> None:
        for bind_active in (False, True):
            guest = self.critical_guest("source-stopped", bind_step2_active=bind_active)
            result, _, _, _ = self.run_flow(guest, "source-stopped")
            with self.subTest(bind_active=bind_active):
                self.assertEqual(result["status"], "passed")
                self.assertNotIn(
                    "bind_active_states",
                    result["owner_inverse_after_restart"]["steps"]["agent_restarted"]["judged"],
                )

    def test_critical_deviations_fail(self) -> None:
        cases = (
            ("target-started", {"pdns_after_owner_active": False},
             ["PowerDNS source unit is not active", "not active and enabled"], True),
            ("source-stopped", {"bind_after_owner_active": True},
             ["named.service is active, want inactive", "port-53 listeners"], True),
            ("target-started", {"named_after_owner": [913]},
             ["named process remains: [913]"], True),
            ("target-started", {"owner_changes_ledger": run_cell.AGENT_RELEASED_NATIVE_UNKNOWN},
             ["changed the ledger the Agent wrote"], True),
            ("source-stopped", {"owner_retires": False},
             ["left the switch journal in place"], True),
            ("target-started", {"status_mutates": True},
             ["read-only status changed private evidence"], False),
            ("source-stopped", {"pdns_step2_state": "active"},
             ["PowerDNS is active, but this cut stopped the source"], True),
            ("target-started", {"bind_step2_active": False},
             ["BIND is not active, but this cut started the target"], True),
        )
        for phase, deviation, texts, owner_ran in cases:
            guest = self.critical_guest(phase, **deviation)
            result, _, _, _ = self.run_flow(guest, phase)
            flow = result["owner_inverse_after_restart"]
            with self.subTest(phase=phase, deviation=deviation):
                self.assertEqual((result["status"], flow["status"]), ("failed", "failed"))
                joined = " ".join(flow["failures"])
                for text in texts:
                    self.assertIn(text, joined)
                self.assertEqual(bool(guest.owner_calls), owner_ran)
                self.assertIs(result["dns_outage"]["judged"], False)

    def test_unavailable_outage_timestamps_are_explicit(self) -> None:
        guest = self.critical_guest("target-started", journal_readable=False)
        result, _, _, _ = self.run_flow(guest, "target-started")
        outage = result["dns_outage"]
        self.assertEqual(result["status"], "passed")
        self.assertIsNone(outage["source_stopped_at"])
        self.assertIsNone(outage["seconds"])
        self.assertTrue(outage["measured_from"]["source_stopped_at"].startswith("unavailable"))
        self.assertTrue(outage["measured_from"]["seconds"].startswith("unavailable"))
        guest = self.critical_guest("target-started", status_names=False)
        result, _, _, _ = self.run_flow(guest, "target-started")
        self.assertEqual(result["status"], "failed")
        self.assertIsNone(result["dns_outage"]["source_serving_again_at"])
        self.assertTrue(result["dns_outage"]["measured_from"]["source_serving_again_at"]
                        .startswith("unavailable"))

    def test_pdns_unit_journal_and_outage_parsing(self) -> None:
        def line(message: str, micros: int, message_id: str | None = None) -> str:
            entry = {"MESSAGE": message, "__REALTIME_TIMESTAMP": str(micros)}
            if message_id:
                entry["MESSAGE_ID"] = message_id
            return json.dumps(entry)

        base = 1_790_000_000_000_000
        output = "\n".join([
            line("Started pdns.service - PowerDNS Authoritative Server.", base - 5_000_000),
            line("PowerDNS shutting down", base - 1),
            line("Stopping pdns.service - PowerDNS Authoritative Server...", base,
                 run_cell.SYSTEMD_UNIT_STOPPING_ID),
            line("Stopped pdns.service - PowerDNS Authoritative Server.", base + 400_000),
            line("Started pdns.service - PowerDNS Authoritative Server.", base + 30_000_000,
                 run_cell.SYSTEMD_UNIT_STARTED_ID),
            "",
        ])
        events = run_cell.parse_pdns_unit_journal(output)
        self.assertEqual(events["stopping_at"], "2026-09-21T14:13:20Z")
        self.assertEqual(events["stopped_at"], "2026-09-21T14:13:20.400000Z")
        self.assertEqual(events["started_after_stop_at"], "2026-09-21T14:13:50Z")
        with self.assertRaises(run_cell.ControllerError):
            run_cell.parse_pdns_unit_journal("not json")
        outage = run_cell.build_dns_outage(
            last_answer_before_cut_at=None,
            unit_journal={"read": True, **events},
            serving_again_at="2026-09-21T14:14:00Z",
        )
        self.assertEqual(outage["seconds"], 40.0)
        empty = run_cell.build_dns_outage(
            last_answer_before_cut_at=None,
            unit_journal={"read": True, "stopping_at": None, "stopped_at": None},
            serving_again_at="2026-09-21T14:14:00Z",
        )
        self.assertIsNone(empty["seconds"])
        self.assertIn("no stop entry", empty["measured_from"]["source_stopped_at"])

    def test_named_process_scan_uses_exact_comm(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            for pid, comm in ((10, "named\n"), (11, "named-checkconf\n"), (12, "pdns_server\n")):
                os.mkdir(os.path.join(root, str(pid)))
                Path(root, str(pid), "comm").write_text(comm, encoding="utf-8")
            os.mkdir(os.path.join(root, "self"))
            os.mkdir(os.path.join(root, "13"))  # exited: no comm
            self.assertEqual(run_cell.find_named_processes(root), [10])

    # -- deliverable 1/3: profiles and newly admitted cells -------------------

    @staticmethod
    def manifest_cell(cell_id: str) -> object:
        manifest = json.loads(
            Path(__file__).with_name("manifest.json").read_text(encoding="utf-8")
        )
        return run_cell.CellSpec.from_manifest(manifest, cell_id)

    def test_rolled_back_switch_cells_use_their_step2_phase(self) -> None:
        for cell_id, phase_on_disk in (
            ("bind__rolled-back__after-write__standalone__peer-reachable", "rolled-back"),
            ("bind__rolled-back__before-write__standalone__peer-reachable", "rolling-back"),
            ("bind__target-staged__before-write__standalone__peer-unreachable", "rolling-back"),
        ):
            selected = self.manifest_cell(cell_id)
            with self.subTest(cell_id=cell_id):
                guest = FakeOwnerGuest(journal_phase=phase_on_disk)
                result, _, _, _ = self.run_flow(guest, selected=selected)
                flow = result["owner_inverse_after_restart"]
                self.assertEqual((result["status"], flow["status"]), ("passed", "passed"),
                                 flow["failures"] + flow["ambiguities"])
                self.assertEqual(flow["variant"], "pre-start")
                self.assertNotIn("profile", flow)
                other = "rolling-back" if phase_on_disk == "rolled-back" else "rolled-back"
                guest = FakeOwnerGuest(journal_phase=other)
                result, _, _, _ = self.run_flow(guest, selected=selected)
                self.assertEqual(result["status"], "failed")
                self.assertIn(f"is not V2 {phase_on_disk}",
                              " ".join(result["owner_inverse_after_restart"]["failures"]))
                self.assertEqual(guest.owner_calls, [])

    def test_before_write_critical_cells_pass(self) -> None:
        for cell_id, bind_active in (
            ("bind__source-stopped__before-write__standalone__peer-reachable", False),
            ("bind__target-started__before-write__standalone__peer-reachable", True),
        ):
            guest = self.critical_guest("x", bind_step2_active=bind_active)
            result, _, _, _ = self.run_flow(guest, selected=self.manifest_cell(cell_id))
            flow = result["owner_inverse_after_restart"]
            with self.subTest(cell_id=cell_id):
                self.assertEqual((result["status"], flow["variant"]), ("passed", "critical"),
                                 flow["failures"] + flow["ambiguities"])
                self.assertIn("dns_outage", result)
        guest = self.critical_guest("x", bind_step2_active=False)
        result, _, _, _ = self.run_flow(guest, selected=self.manifest_cell(
            "bind__target-started__before-write__standalone__peer-reachable"
        ))
        self.assertIn("BIND is not active, but this cut started the target",
                      " ".join(result["owner_inverse_after_restart"]["failures"]))

    OWNER_BIND_EVIDENCE = {
        label: {"path": "/etc/bind/" + label, "sha256": char * 64, "device": 1, "inode": n}
        for n, (label, char) in enumerate(
            (("main", "1"), ("options", "2"), ("local", "3"), ("leaf", "4"), ("zone", "5"))
        )
    }

    def run_adoption(self, **deviations: object) -> tuple[dict, FakeOwnerGuest]:
        changed_files = deviations.pop("files_changed", False)
        state_after = deviations.pop("state_after", {"exists": False})
        guest = FakeOwnerGuest(
            command=run_cell.OWNER_BIND_ADOPTION_COMMAND, pid_key="named_main_pid",
            **deviations,
        )
        evidence = {**self.OWNER_BIND_EVIDENCE, "inventory": ["b", "a"]}
        comparable = run_cell.owner_bind_comparable(evidence)
        if changed_files:
            comparable = dict(comparable, files=dict(comparable["files"], zone="9" * 64))
        result, _, _, _ = self.run_flow(
            guest,
            selected=self.manifest_cell(run_cell.BIND_HANDOFF_CELL),
            state_before={"exists": False},
            source_proof={"owner_bind_native_source": evidence},
            extra={
                "owner_bind_files": mock.Mock(return_value=comparable),
                "read_dns_state_optional": mock.Mock(return_value=state_after),
            },
        )
        return result, guest

    def test_running_bind_adoption_owner_flow(self) -> None:
        result, guest = self.run_adoption()
        flow = result["owner_inverse_after_restart"]
        self.assertEqual((result["status"], flow["status"]), ("passed", "passed"),
                         flow["failures"] + flow["ambiguities"])
        self.assertEqual(flow["profile"], "bind-adoption-v2")
        self.assertEqual(flow["pre_cut_named_main_pid"], 600)
        self.assertNotIn("pre_cut_pdns_main_pid", flow)
        self.assertEqual(flow["owner_command"][1], "recover-dns-bind-adoption")
        self.assertEqual(guest.owner_calls, [
            "owner-recover-dns-bind-adoption", "owner-recover-dns-bind-adoption-rerun",
        ])
        step5 = flow["steps"]["after_owner_command"]
        self.assertEqual(step5["state"], {"exists": False})
        self.assertTrue(step5["owner_files_unchanged"])
        self.assertIs(step5["adoption_zone_answered"]["judged"], False)
        for deviation, text in (
            ({"pid_after_owner": 601}, "must never stop or restart the owner's named"),
            ({"files_changed": True}, "owner BIND configuration, zone file"),
            ({"state_after": {"sha256": "x" * 64, "semantic": {}}},
             "pre-cut source had none"),
            ({"status_names": False}, "does not name recover-dns-bind-adoption"),
        ):
            with self.subTest(deviation=deviation):
                result, _ = self.run_adoption(**deviation)
                self.assertEqual(result["status"], "failed")
                self.assertIn(text, " ".join(result["owner_inverse_after_restart"]["failures"]))

    def run_pdns_adoption(self, **deviations: object) -> tuple[dict, FakeOwnerGuest]:
        drift = deviations.pop("files_changed", False)
        guest = FakeOwnerGuest(
            command=run_cell.OWNER_PDNS_ADOPTION_COMMAND,
            **{"target": "pdns", "journal_phase": "rolled-back", **deviations},
        )
        preimage = {
            "configuration": {
                "main": {"path": "/etc/powerdns/pdns.conf", "sha256": "1" * 64},
                "managed": {"path": "/etc/powerdns/pdns.d/celikpanel.conf", "sha256": "2" * 64},
            },
            "database": {"path": "/var/lib/powerdns/pdns.sqlite3", "sha256": "3" * 64},
        }
        proof = {"external_pdns_preimage": preimage}
        current = run_cell.owner_pdns_adoption_expected(proof)
        if drift:
            current = dict(current, database={"sha256": "4" * 64})
        with hypothetical_pdns_owner_admission():
            result, _, _, _ = self.run_flow(
                guest,
                selected=self.manifest_cell(PDNS_ROLLED_BACK_CELL),
                state_before={"exists": False},
                source_proof=proof,
                extra={"owner_pdns_adoption_files": mock.Mock(return_value=current)},
            )
        return result, guest

    def test_pdns_adoption_rolled_back_owner_flow(self) -> None:
        # The retained owner profile under a hypothetical admission; the real
        # cell runs with --expect-agent-startup-rollback since 3cc2de22.
        result, guest = self.run_pdns_adoption()
        flow = result["owner_inverse_after_restart"]
        self.assertEqual((result["status"], flow["status"]), ("passed", "passed"),
                         flow["failures"] + flow["ambiguities"])
        self.assertEqual(flow["profile"], "pdns-adoption-v1")
        self.assertEqual(flow["step2_journal_phase"], "rolled-back")
        self.assertEqual(guest.owner_calls[0], "owner-recover-dns-pdns-adoption")
        for deviation, text in (
            ({"files_changed": True}, "external PowerDNS configuration or database changed"),
            ({"target": "bind"}, "ledger job target"),
            ({"journal_phase": "rolling-back"}, "is not V1 rolled-back"),
        ):
            with self.subTest(deviation=deviation):
                result, _ = self.run_pdns_adoption(**deviation)
                self.assertEqual(result["status"], "failed")
                self.assertIn(text, " ".join(result["owner_inverse_after_restart"]["failures"]))
        # A PowerDNS PID change stays an unknown for PowerDNS adoption.
        result, _ = self.run_pdns_adoption(pid_after_owner=601)
        self.assertEqual(result["status"], "unverified")

    def test_journal_source_is_checked_by_profile(self) -> None:
        adoption = self.manifest_cell(run_cell.BIND_HANDOFF_CELL)
        plan = {"kind": "bind-switch-config/v1", "host_layout": "apt", "digest": "a" * 64,
                "source_bind": {"kind": "bind-adoption-source/v1"}}
        journal = {"source_engine": "", "target_engine": "bind", "inverse_plan": plan}
        run_cell.validate_owner_inverse_journal_source(journal, adoption)
        for bad in (
            dict(journal, source_engine="pdns"),
            dict(journal, inverse_plan={**plan, "source_pdns": {}}),
            dict(journal, inverse_plan={**plan, "source_bind": {"kind": "pdns-source/v1"}}),
        ):
            with self.subTest(bad=bad), self.assertRaises(run_cell.BoundaryUnverified):
                run_cell.validate_owner_inverse_journal_source(bad, adoption)
        pdns = self.manifest_cell(PDNS_ROLLED_BACK_CELL)
        v1 = {"mode": "adopt", "source_engine": "", "target_engine": "pdns",
              "state_before": {"exists": False}}
        with self.assertRaisesRegex(run_cell.ControllerError, "exact owner-inverse cell"):
            run_cell.validate_owner_inverse_journal_source(v1, pdns)
        with hypothetical_pdns_owner_admission():
            run_cell.validate_owner_inverse_journal_source(v1, pdns)
            for bad in (dict(v1, mode="switch"), dict(v1, state_before={"exists": True})):
                with self.subTest(bad=bad), self.assertRaises(run_cell.BoundaryUnverified):
                    run_cell.validate_owner_inverse_journal_source(bad, pdns)
        with self.assertRaises(run_cell.ControllerError):
            run_cell.validate_owner_inverse_journal_source(v1, cell("intent", "after-write"))

    def test_preconditions_use_each_profile_capability_and_source(self) -> None:
        settings = mock.Mock(command_timeout=1.0, command_cwd="/", state_dir="/state")
        capability = mock.Mock(
            returncode=0, truncated=False,
            output=(run_cell.OWNER_BIND_ADOPTION_CAPABILITY_MARKER + "\n").encode(),
        )
        capability.report.return_value = {"argv": []}
        serving = {"ok": True, "errors": [], "unknown": [], "named_main_pid": 700}
        settings.cell = self.manifest_cell(run_cell.BIND_HANDOFF_CELL)
        with mock.patch.object(run_cell, "run_bounded_command", return_value=capability) as run, \
                mock.patch.object(run_cell, "observe_bind_source_serving", return_value=serving), \
                mock.patch.object(run_cell, "read_dns_state_optional",
                                  return_value={"exists": False}):
            preflight = run_cell.prove_owner_inverse_preconditions(settings, {}, mock.Mock())
        self.assertEqual(run.call_args.args[0][1], "check-bind-adoption-inverse-v1")
        self.assertEqual(preflight["state"], {"exists": False})
        settings.cell = self.manifest_cell(PDNS_ROLLED_BACK_CELL)
        pdns = {"ok": True, "errors": [], "unknown": [], "pdns_main_pid": 600}
        with self.assertRaisesRegex(run_cell.ControllerError, "admitted owner-inverse cell"):
            run_cell.prove_owner_inverse_preconditions(settings, {}, mock.Mock())
        with hypothetical_pdns_owner_admission():
            with mock.patch.object(run_cell, "run_bounded_command") as run, \
                    mock.patch.object(run_cell, "observe_pdns_source_serving",
                                      return_value=pdns), \
                    mock.patch.object(run_cell, "read_dns_state_optional",
                                      return_value={"exists": False}):
                preflight = run_cell.prove_owner_inverse_preconditions(
                    settings, {}, mock.Mock())
                run.assert_not_called()
            self.assertIs(preflight["capability"]["probed"], False)
            with mock.patch.object(run_cell, "run_bounded_command"), \
                    mock.patch.object(run_cell, "observe_pdns_source_serving",
                                      return_value=dict(pdns, ok=False, errors=["x"])), \
                    self.assertRaisesRegex(run_cell.ControllerError, "external PowerDNS"):
                run_cell.prove_owner_inverse_preconditions(settings, {}, mock.Mock())

    # -- deliverable 2: reboot before the owner command ------------------------

    BOOT = {"boot_id": "1" * 8 + "-1111-1111-1111-" + "1" * 12,
            "product_uuid": "2" * 8 + "-2222-2222-2222-" + "2" * 12,
            "fixture_marker": {}}

    def suspend(self, guest: FakeOwnerGuest, selected: object | None = None):
        settings = self.flow_settings(
            guest, selected or owner_cell("target-staged"),
            reboot_before_owner_command=True,
        )
        result = self.flow_result(guest)
        lists: tuple[list, list, list] = ([], [], [])
        with self.flow_patches(guest, {
            "read_guest_boot_identity": mock.Mock(return_value=self.BOOT),
        }), self.assertRaises(run_cell.RebootRequested) as raised:
            run_cell.run_owner_inverse_after_restart(
                settings, result=result, transcript=mock.Mock(),
                ordinary={}, owner_environment={},
                controller_identity={"effective_gid": 999},
                identity_receipt={"owner_id": guest.OWNER,
                                  "manifest_qualifier": guest.QUALIFIER},
                boundary_identity=boundary_identity(), old_socket_identity=(1, 1),
                kill_proven=True, safety_failures=lists[0],
                verification_failures=lists[1], diagnostic_failures=lists[2],
            )
        # The checkpoint is JSON: the resumed controller gets the same values.
        state = json.loads(json.dumps(raised.exception.state))
        return settings, json.loads(json.dumps(result)), state, lists, raised.exception

    def resume(self, guest, settings, result, state, lists, *, units_active=True,
               extra=None):
        management = {"unknown": [], "units": {
            unit: {"ActiveState": "active" if units_active else "failed"}
            for unit in run_cell.MANAGEMENT_UNITS
        }}
        with self.flow_patches(guest, {
            "observe_management_units": mock.Mock(return_value=management),
            **(extra or {}),
        }):
            run_cell.resume_owner_inverse_before_owner_command(
                settings, state, result=result, transcript=mock.Mock(),
                ordinary={}, owner_environment={},
                controller_identity={"effective_gid": 999},
                boot={"boot_id_before": "a", "boot_id_after": "b"},
                kill_proven=True, safety_failures=lists[0],
                verification_failures=lists[1], diagnostic_failures=lists[2],
            )
        return result

    def test_reboot_before_owner_command_suspends_then_continues(self) -> None:
        guest = FakeOwnerGuest()
        settings, result, state, lists, request = self.suspend(guest)
        self.assertEqual(request.stage, run_cell.REBOOT_BEFORE_OWNER_COMMAND)
        self.assertEqual(state["flow"], "owner-inverse")
        self.assertEqual(state["boot"], self.BOOT)
        self.assertEqual((guest.status_calls, guest.owner_calls), (0, []))
        self.assertIn("evidence_before_reboot",
                      result["owner_inverse_after_restart"]["steps"]["agent_restarted"])
        result = self.resume(guest, settings, result, state, lists)
        flow = result["owner_inverse_after_restart"]
        self.assertEqual((result["status"], flow["status"]), ("passed", "passed"),
                         flow["failures"] + flow["ambiguities"])
        after = flow["steps"]["after_reboot"]
        self.assertTrue(after["ledger_unchanged_since_release"])
        self.assertIs(after["source_main_pid"]["judged"], False)
        self.assertEqual(guest.owner_calls, [
            "owner-recover-dns-bind-switch", "owner-recover-dns-bind-switch-rerun",
        ])

    def test_reboot_deviations_fail_before_the_owner_command(self) -> None:
        guest = FakeOwnerGuest()
        settings, result, state, lists, _ = self.suspend(guest)
        guest.ledger_sha = "8" * 64
        result = self.resume(guest, settings, result, state, lists)
        self.assertEqual(result["status"], "failed")
        self.assertIn("the reboot changed the ledger",
                      " ".join(result["owner_inverse_after_restart"]["failures"]))
        self.assertEqual(guest.owner_calls, [])
        guest = FakeOwnerGuest()
        settings, result, state, lists, _ = self.suspend(guest)
        result = self.resume(guest, settings, result, state, lists, units_active=False)
        self.assertEqual(result["status"], "failed")
        self.assertIn("celikpanel-panel.service did not come up",
                      " ".join(result["owner_inverse_after_restart"]["failures"]))
        guest = FakeOwnerGuest()
        settings, result, state, lists, _ = self.suspend(guest)
        guest.d["journal_phase"] = "rolled-back"
        result = self.resume(guest, settings, result, state, lists)
        self.assertIn("journal after reboot is not V2 rolling-back",
                      " ".join(result["owner_inverse_after_restart"]["failures"]))
        self.assertEqual(guest.owner_calls, [])

    def test_reboot_refused_outside_the_fixture_guest(self) -> None:
        guest = FakeOwnerGuest()
        settings = self.flow_settings(guest, owner_cell("target-staged"),
                                      reboot_before_owner_command=True)
        result = self.flow_result(guest)
        with self.flow_patches(guest, {"read_guest_boot_identity": mock.Mock(
            side_effect=run_cell.ControllerError("refusing reboot: marker"))}), \
                self.assertRaisesRegex(run_cell.ControllerError, "refusing reboot"):
            run_cell.run_owner_inverse_after_restart(
                settings, result=result, transcript=mock.Mock(), ordinary={},
                owner_environment={}, controller_identity={"effective_gid": 999},
                identity_receipt={"owner_id": guest.OWNER,
                                  "manifest_qualifier": guest.QUALIFIER},
                boundary_identity=boundary_identity(), old_socket_identity=(1, 1),
                kill_proven=True, safety_failures=[], verification_failures=[],
                diagnostic_failures=[],
            )
        self.assertEqual(result["reboots_refused"][0]["stage"], "before-owner-command")
        self.assertEqual(guest.owner_calls, [])

    def test_reboot_after_recovery_only_for_a_passing_flow(self) -> None:
        guest = FakeOwnerGuest()
        with self.flow_patches(guest, {
            "observe_serving_authority": mock.Mock(return_value={"authority": {}}),
            "observe_management_units": mock.Mock(return_value={"units": {}, "unknown": []}),
            "read_guest_boot_identity": mock.Mock(return_value=self.BOOT),
        }), self.assertRaises(run_cell.RebootRequested) as whole:
            run_cell.run_owner_inverse_after_restart(
                self.flow_settings(guest, owner_cell("target-staged"),
                                   reboot_after_recovery=True),
                result=self.flow_result(guest), transcript=mock.Mock(), ordinary={},
                owner_environment={}, controller_identity={"effective_gid": 999},
                identity_receipt={"owner_id": guest.OWNER,
                                  "manifest_qualifier": guest.QUALIFIER},
                boundary_identity=boundary_identity(), old_socket_identity=(1, 1),
                kill_proven=True, safety_failures=[], verification_failures=[],
                diagnostic_failures=[],
            )
        # The whole owner flow ran first; the reboot follows the final samples.
        self.assertEqual(len(guest.owner_calls), 2)
        self.assertEqual(whole.exception.state["flow"], "owner-inverse")
        settings = self.flow_settings(guest, owner_cell("target-staged"),
                                      reboot_after_recovery=True)
        failed = {"status": "failed"}
        run_cell.maybe_request_reboot_after_recovery(settings, failed, {}, {})
        self.assertIs(failed["reboot_after_recovery"]["run"], False)
        passed = complete_rpc_retry_pass()
        with mock.patch.multiple(
            run_cell,
            observe_serving_authority=mock.Mock(return_value={"authority": {"answered": True}}),
            observe_management_units=mock.Mock(return_value={"units": {}, "unknown": []}),
            read_guest_boot_identity=mock.Mock(return_value=self.BOOT),
        ), self.assertRaises(run_cell.RebootRequested) as raised:
            run_cell.maybe_request_reboot_after_recovery(settings, passed, {}, {"flow": "rpc-retry"})
        self.assertEqual(raised.exception.stage, run_cell.REBOOT_AFTER_RECOVERY)
        self.assertEqual(raised.exception.state["authority_before"],
                         {"authority": {"answered": True}})
        # A Mock without the flag never reboots.
        run_cell.maybe_request_reboot_after_recovery(mock.Mock(), passed, {}, {})

    def test_run_cell_dispatches_owner_flow_after_proven_kill(self) -> None:
        source = MODULE_PATH.read_text(encoding="utf-8")
        flow = source[source.index("def run_cell(settings:"):]
        dispatch = flow.index("run_owner_inverse_after_restart(")
        self.assertLess(flow.index("atomic_write_new_json(settings.proof_path, proof)"), dispatch)
        self.assertLess(flow.index('result["native_post_kill_status"]'), dispatch)
        self.assertLess(dispatch, flow.index("recovery_attempts: list"))
        self.assertLess(dispatch, flow.index("raise OwnerInverseFlowFinished()"))
        self.assertIn("except OwnerInverseFlowFinished:", flow)
        self.assertLess(
            flow.index("prove_owner_inverse_preconditions("),
            flow.index("tagged = start_tagged_agent("),
        )
        parser = run_cell.build_argument_parser()
        self.assertIn("--owner-inverse-after-restart", parser._option_string_actions)


def plain_settings(root: str, selected: object | None = None, **changes: object) -> object:
    trigger = ("/opt/t", "rpc-switch", "--scenario", root + "/scenario.json",
               "--identity-receipt", root + "/identity.json", "--timeout", "45m")
    settings = run_cell.Settings(
        cell=selected or owner_cell("target-staged"), request_id="1" * 32, nonce="a" * 32,
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
        recovery_timeout=1, endpoint_timeout=1, dns_timeout=1,
        stability_seconds=30, stability_interval=1,
        owner_inverse_after_restart=True, reboot_before_owner_command=True,
        reboot_dir=root,
    )
    return replace(settings, **changes) if changes else settings


class RebootCheckpointTest(unittest.TestCase):
    BOOT = {"boot_id": "1" * 8 + "-1111-1111-1111-" + "1" * 12,
            "product_uuid": "2" * 8 + "-2222-2222-2222-" + "2" * 12,
            "fixture_marker": {}}

    def checkpoint(self, root: str, settings: object) -> dict:
        Path(settings.transcript_path).write_text("first\n", encoding="utf-8")
        Path(settings.proof_path).write_text("{}\n", encoding="utf-8")
        request = run_cell.RebootRequested(
            run_cell.REBOOT_BEFORE_OWNER_COMMAND, {"flow": "owner-inverse", "boot": self.BOOT}
        )
        return run_cell.write_reboot_checkpoint(
            settings, 1, request, {"status": "unverified"}, kill_proven=True,
            safety_failures=[], verification_failures=["v"], diagnostic_failures=[],
            boot=self.BOOT,
            transcripts=[{"path": settings.transcript_path,
                          "sha256": run_cell.sha256_file(settings.transcript_path)}],
        )

    def test_checkpoint_roundtrip_binds_settings_proof_and_transcript(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            settings = plain_settings(root)
            written = self.checkpoint(root, settings)
            self.assertEqual(stat_mode(written["path"]), 0o600)
            resumed = replace(settings, resume_after_reboot=True)
            ordinal, checkpoint = run_cell.load_pending_reboot_checkpoint(resumed)
            self.assertEqual((ordinal, checkpoint["stage"]), (1, "before-owner-command"))
            self.assertEqual(checkpoint["failures"]["verification"], ["v"])
            # The resume switch is the only setting outside the fingerprint.
            with self.assertRaisesRegex(run_cell.ControllerError, "settings_sha256"):
                run_cell.load_pending_reboot_checkpoint(replace(resumed, dns_name="x.test"))
            Path(settings.transcript_path).write_text("tampered\n", encoding="utf-8")
            with self.assertRaisesRegex(run_cell.ControllerError, "transcript changed"):
                run_cell.load_pending_reboot_checkpoint(resumed)
            Path(settings.transcript_path).write_text("first\n", encoding="utf-8")
            Path(run_cell.reboot_checkpoint_paths(root, 1)["resumed"]).write_text(
                "{}", encoding="utf-8"
            )
            with self.assertRaisesRegex(run_cell.ControllerError, "exactly one unconsumed"):
                run_cell.load_pending_reboot_checkpoint(resumed)
            with self.assertRaises(run_cell.ControllerError):
                self.checkpoint(root, settings)  # create-new, never replaced

    def test_resume_refuses_an_unrebooted_or_foreign_guest(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            settings = replace(plain_settings(root), resume_after_reboot=True)
            self.checkpoint(root, settings)
            for boot, text in (
                (self.BOOT, "was not rebooted"),
                (dict(self.BOOT, boot_id="3" * 8 + "-3333-3333-3333-" + "3" * 12,
                      product_uuid="4" * 8 + "-4444-4444-4444-" + "4" * 12),
                 "another SMBIOS machine UUID"),
            ):
                with self.subTest(text=text), mock.patch.multiple(
                    run_cell,
                    validate_controller_identity=mock.Mock(return_value={"effective_gid": 1}),
                    minimal_command_environment=mock.Mock(return_value={}),
                    validate_settings=mock.Mock(return_value={}),
                    read_guest_boot_identity=mock.Mock(return_value=boot),
                ), self.assertRaisesRegex(run_cell.ControllerError, text):
                    run_cell.resume_cell(settings)
                self.assertFalse(os.path.exists(
                    run_cell.reboot_checkpoint_paths(root, 1)["resumed"]
                ))

    def test_resume_consumes_the_checkpoint_once_and_writes_the_result(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            settings = replace(plain_settings(root), resume_after_reboot=True)
            self.checkpoint(root, settings)
            after = dict(self.BOOT, boot_id="5" * 8 + "-5555-5555-5555-" + "5" * 12)

            def finish(*args, **kwargs):
                kwargs["result"]["status"] = "passed"
                kwargs["result"]["safety_status"] = "passed"

            with mock.patch.multiple(
                run_cell,
                validate_controller_identity=mock.Mock(return_value={"effective_gid": 1}),
                minimal_command_environment=mock.Mock(return_value={}),
                validate_settings=mock.Mock(return_value={}),
                read_guest_boot_identity=mock.Mock(return_value=after),
                ordinary_environment=mock.Mock(return_value={}),
                resume_owner_inverse_before_owner_command=mock.Mock(side_effect=finish),
            ):
                self.assertEqual(run_cell.resume_cell(settings), 0)
                with self.assertRaises(run_cell.ControllerError):
                    run_cell.resume_cell(settings)
            result = json.loads(Path(settings.result_path).read_text(encoding="utf-8"))
            self.assertEqual(result["reboots"][0]["boot_id_after"], after["boot_id"])
            self.assertEqual(len(result["transcripts_after_reboot"]), 1)
            self.assertTrue(os.path.exists(run_cell.reboot_checkpoint_paths(root, 1)["resumed"]))

    def test_boot_identity_requires_this_cells_fixture_marker(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            boot = Path(root, "boot_id")
            boot.write_text(self.BOOT["boot_id"] + "\n", encoding="ascii")
            product = Path(root, "product_uuid")
            product.write_text(self.BOOT["product_uuid"].upper() + "\n", encoding="ascii")
            marker = (
                "schema=celikpanel/dns-kill-fixture-plan/v1\n"
                "cell_id=bind__x\nnode=debian13\n"
            ).encode()
            with mock.patch.multiple(
                run_cell, BOOT_ID_PATH=str(boot), PRODUCT_UUID_PATH=str(product),
                secure_read_bytes=mock.Mock(return_value=(marker, None)),
            ):
                identity = run_cell.read_guest_boot_identity("bind__x")
                self.assertEqual(identity["product_uuid"], self.BOOT["product_uuid"])
                with self.assertRaisesRegex(run_cell.ControllerError, "refusing reboot"):
                    run_cell.read_guest_boot_identity("bind__other")

    def test_reboot_settings_are_gated(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            settings = plain_settings(root)
            run_cell.validate_reboot_settings(settings)
            run_cell.validate_reboot_settings(replace(
                settings, owner_inverse_after_restart=False,
                reboot_before_owner_command=False, reboot_after_recovery=False,
                reboot_dir=None,
            ))
            for changed, text in (
                ({"reboot_dir": None}, "explicit --reboot-dir"),
                ({"owner_inverse_after_restart": False}, "requires --owner-inverse"),
                ({"stop_after_kill_for_independent_recovery": True}, "handoff"),
                ({"cell": owner_cell("target-staged", role="paired-primary")}, "standalone"),
                ({"reboot_before_owner_command": False, "reboot_dir": None,
                  "resume_after_reboot": True}, "reboot flag"),
            ):
                with self.subTest(changed=changed), self.assertRaisesRegex(
                    run_cell.ControllerError, text
                ):
                    run_cell.validate_reboot_settings(replace(settings, **changed))
            Path(run_cell.reboot_checkpoint_paths(root, 2)["checkpoint"]).write_text("x")
            with self.assertRaisesRegex(run_cell.ControllerError, "already exists"):
                run_cell.validate_reboot_settings(settings)
            run_cell.validate_reboot_settings(replace(settings, resume_after_reboot=True))

    def authority(self, engines, *, answers=1, semantic=None, journal=False):
        return {
            "unknown": [],
            "authority": {"engines": engines, "answered": True,
                          "answer_counts": {"udp": answers, "tcp": answers}},
            "state": {"sha256": "s", "semantic": semantic or {"engine": "pdns"}},
            "evidence": {"journal": {"exists": journal}},
            "named_processes": [],
        }

    def verify(self, state, after, *, units="active", source=None, flow_cell=None):
        settings = mock.Mock(
            cell=flow_cell or owner_cell("target-staged"), agent_socket="/s",
            endpoint_timeout=1.0, panel_address="127.0.0.1", panel_port=2083,
        )
        ok = {"ok": True}
        result: dict = {"status": "passed"}
        safety: list = []
        verification: list = []
        with mock.patch.multiple(
            run_cell,
            observe_management_units=mock.Mock(return_value={"unknown": [], "units": {
                unit: {"ActiveState": units} for unit in run_cell.MANAGEMENT_UNITS}}),
            wait_for_unix_socket=mock.Mock(return_value=(1, 2)),
            inspect_restarted_agent_process=mock.Mock(return_value={"pid": 9}),
            wait_for_tcp=mock.Mock(return_value=None),
            observe_serving_authority=mock.Mock(return_value=after),
            observe_owner_source_serving=mock.Mock(
                return_value=source or {"errors": [], "unknown": []}
            ),
            run_stability_window=mock.Mock(return_value=(
                {"samples": [{"agent": ok, "panel": ok, "dns": ok}] * 31}, [], []
            )),
        ):
            run_cell.verify_after_recovery_reboot(
                settings, state, result=result, transcript=mock.Mock(), ordinary={},
                owner_environment={}, controller_identity={"effective_gid": 1},
                boot={"boot_id_after": "b"}, safety_failures=safety,
                verification_failures=verification,
            )
        return result

    def test_after_recovery_reboot_needs_same_authority_state_and_units(self) -> None:
        before = self.authority({"tcp": ["pdns"], "udp": ["pdns"]})
        state = {"flow": "rpc-retry", "authority_before": before}
        result = self.verify(state, before)
        report = result["reboot_after_recovery"]
        self.assertEqual((result["status"], report["status"]), ("passed", "passed"))
        self.assertEqual(len(report["stability"]["samples"]), 31)
        for after, kwargs, text in (
            (self.authority({"tcp": ["bind"], "udp": ["bind"]}), {}, "authority changed"),
            (self.authority({"tcp": ["pdns"], "udp": ["pdns"]}, semantic={"engine": "bind"}),
             {}, "state receipt changed"),
            (self.authority({"tcp": ["pdns"], "udp": ["pdns"]}, journal=True), {},
             "journal presence changed"),
            (self.authority({"tcp": ["pdns"], "udp": ["pdns"]}, answers=2), {},
             "answer counts changed"),
            (before, {"units": "inactive"}, "did not come up after the reboot"),
        ):
            with self.subTest(text=text):
                result = self.verify(state, after, **kwargs)
                self.assertEqual(result["status"], "failed")
                self.assertIn(text, " ".join(result["reboot_after_recovery"]["failures"]))
        owner_state = {"flow": "owner-inverse", "authority_before": before}
        serving_bind = dict(before, named_processes=[812])
        result = self.verify(owner_state, serving_bind)
        self.assertIn("BIND is serving again", " ".join(
            result["reboot_after_recovery"]["failures"]))
        result = self.verify(owner_state, before, source={
            "errors": ["BIND target unit is active"], "unknown": []})
        self.assertEqual(result["status"], "failed")

    def test_serving_authority_maps_listener_pids_to_engines(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            settings = mock.Mock(state_dir=root, journal_path=root + "/journal.json")
            native = {
                "unknown": [],
                "units": {
                    "pdns.service": {"MainPID": "600"},
                    "named.service": {"MainPID": "0"},
                    "bind9.service": {"MainPID": "0"},
                },
                "port53_listeners": {"tcp": [{"pids": [600]}], "udp": [{"pids": [600, 7]}]},
                "dns": {"answered": True,
                        "result": {"udp": {"answers": 1}, "tcp": {"answers": 1}}},
            }
            with mock.patch.multiple(
                run_cell,
                observe_native_dns_state=mock.Mock(return_value=native),
                snapshot_private_evidence=mock.Mock(return_value={"journal": {"exists": False}}),
            ):
                report = run_cell.observe_serving_authority(settings, {})
        self.assertEqual(report["authority"]["engines"],
                         {"tcp": ["pdns"], "udp": ["other", "pdns"]})
        self.assertEqual(report["state"], {"exists": False})


class V2RefusalAndStartupRollbackTest(unittest.TestCase):
    @staticmethod
    def manifest_cell(cell_id: str) -> object:
        manifest = json.loads(
            Path(__file__).with_name("manifest.json").read_text(encoding="utf-8")
        )
        return run_cell.CellSpec.from_manifest(manifest, cell_id)

    def refuse(self, cell_id: str, fixture: str = "managed-pdns", **changes: object):
        with tempfile.TemporaryDirectory() as root:
            settings = plain_settings(root, self.manifest_cell(cell_id), **{
                "owner_inverse_after_restart": False,
                "reboot_before_owner_command": False, "reboot_dir": None, **changes,
            })
            with mock.patch.object(
                run_cell, "validate_source_scenario",
                return_value=({"source_fixture": fixture}, {}),
            ):
                return run_cell.refuse_unrunnable_v2_cells(settings)

    def test_v2_cells_are_refused_before_any_mutation_without_the_flag(self) -> None:
        for cell_id in sorted(
            c for c in run_cell.OWNER_INVERSE_CELLS
            if run_cell.OWNER_INVERSE_ADMISSIONS[c].profile == "bind-switch-v2"
        ):
            with self.subTest(cell_id=cell_id), self.assertRaisesRegex(
                run_cell.ControllerError, "Run it with --owner-inverse-after-restart"
            ):
                self.refuse(cell_id)
        for cell_id, text in (
            ("bind__intent__before-write__standalone__peer-unreachable", "no journal exists"),
            ("bind__source-stopped__after-write__standalone__peer-unreachable",
             "not admitted"),
            ("bind__rolled-back__after-write__standalone__peer-unreachable", "not admitted"),
            (run_cell.BIND_HANDOFF_CELL, "not admitted"),
        ):
            with self.subTest(cell_id=cell_id), self.assertRaisesRegex(
                run_cell.ControllerError, text
            ):
                self.refuse(cell_id)
        # Other sources, the flag, the handoff and other drivers are unchanged.
        self.assertIsNone(self.refuse(
            "bind__target-staged__after-write__standalone__peer-reachable", "uninitialized"))
        self.assertIsNone(self.refuse(
            "bind__target-staged__after-write__standalone__peer-reachable",
            owner_inverse_after_restart=True))
        self.assertIsNone(self.refuse(
            run_cell.BIND_HANDOFF_CELL, stop_after_kill_for_independent_recovery=True))
        with mock.patch.object(run_cell, "validate_source_scenario",
                               side_effect=run_cell.ControllerError("unreadable")):
            with tempfile.TemporaryDirectory() as root:
                self.assertIsNone(run_cell.refuse_unrunnable_v2_cells(plain_settings(
                    root, owner_inverse_after_restart=False,
                    reboot_before_owner_command=False, reboot_dir=None,
                )))

    def test_refusal_precedes_every_controller_artifact(self) -> None:
        source = MODULE_PATH.read_text(encoding="utf-8")
        flow = source[source.index("def run_cell(settings:"):]
        self.assertLess(flow.index("refuse_unrunnable_v2_cells(settings)"),
                        flow.index("transcript = Transcript("))
        self.assertLess(flow.index("except RebootRequested as request:"),
                        flow.index("except OwnerInverseFlowFinished:"))
        self.assertLess(flow.index("write_reboot_checkpoint("),
                        flow.index("atomic_write_new_json(settings.result_path, result)"))
        main = source[source.index("def main(argv"):]
        self.assertIn(
            "if settings.resume_after_reboot:\n            exit_code = resume_cell(settings)", main)
        self.assertIn("exit_code = continue_after_zone_lifecycle(settings)", main)
        parser = run_cell.build_argument_parser()
        for flag in ("--reboot-before-owner-command", "--reboot-after-recovery",
                     "--resume-after-reboot", "--reboot-dir",
                     "--expect-agent-startup-rollback"):
            self.assertIn(flag, parser._option_string_actions)

    def startup(self, *, job_code=run_cell.AGENT_ROLLED_BACK_AFTER_RESTART,
                journal=False, named=False, released=True, pid=600):
        guest = FakeOwnerGuest(release_code=job_code, target="pdns", released=released)
        with tempfile.TemporaryDirectory() as root:
            settings = mock.Mock(request_id=guest.REQUEST, journal_path=root + "/j.json",
                                 recovery_timeout=1.0)
            if journal:
                Path(settings.journal_path).write_text("{}", encoding="utf-8")
            with mock.patch.multiple(
                run_cell,
                wait_for_agent_release=mock.Mock(side_effect=guest.wait_release),
                observe_pdns_source_serving=mock.Mock(return_value={
                    "errors": [], "unknown": [], "pdns_main_pid": pid,
                    "pdns_main_pid_changed": pid != 600,
                }),
                find_agent_owner_refusal=mock.Mock(return_value={"observed": named}),
            ):
                return run_cell.observe_agent_startup_rollback(
                    settings, {}, mock.Mock(),
                    {"owner_id": guest.OWNER, "manifest_qualifier": guest.QUALIFIER},
                    600, 1,
                )

    def test_agent_startup_rollback_is_judged_before_any_retry(self) -> None:
        report = self.startup()
        self.assertTrue(report["retry_allowed"], report)
        for kwargs, text in (
            ({"job_code": run_cell.AGENT_RELEASED_NATIVE_UNKNOWN}, "error_code"),
            ({"journal": True}, "kept the switch journal"),
            ({"named": True}, "did not roll back by itself"),
        ):
            with self.subTest(kwargs=kwargs):
                report = self.startup(**kwargs)
                self.assertFalse(report["retry_allowed"])
                self.assertIn(text, " ".join(report["failures"]))
        for kwargs in ({"released": False}, {"pid": 601}):
            with self.subTest(kwargs=kwargs):
                report = self.startup(**kwargs)
                self.assertFalse(report["retry_allowed"])
                self.assertEqual(report["failures"], [])
                self.assertTrue(report["unknown"])
        verification: list = []
        result = {"status": "passed",
                  "recovery_outcome": {"classification": "target_converged"}}
        run_cell.judge_agent_startup_rollback(result, self.startup(), verification)
        self.assertEqual(result["status"], "passed")
        result = {"status": "passed",
                  "recovery_outcome": {"classification": "rolled_back_source_serving"}}
        run_cell.judge_agent_startup_rollback(result, self.startup(), verification)
        self.assertEqual(result["status"], "failed")
        result = {"status": "passed"}
        run_cell.judge_agent_startup_rollback(result, None, verification)
        self.assertEqual(result["status"], "unverified")


def stat_mode(path: str) -> int:
    import stat as stat_module

    return stat_module.S_IMODE(os.stat(path).st_mode)


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


    @unittest.skipUnless(hasattr(os, "geteuid"), "secure fixture files require POSIX ownership")
    def test_managed_bind_paired_setup_binds_role_addresses_and_zone(self) -> None:
        selected = cell(driver="pdns-switch", role="paired-primary")
        measured = guest_bootstrap.pdns_switch_scenario(role="paired-primary")
        source = guest_bootstrap.bind_scenario(
            "uninitialized", role="paired-primary", node="debian13",
            allow_debian_paired_source=True,
        )
        request_id = hashlib.sha256(
            (selected.cell_id + "\0source-bind-switch").encode()
        ).hexdigest()[:32]
        owner_id = run_cell.deterministic_trigger_owner(selected.cell_id, request_id)
        qualifier = "dns-engine-switch/v1:sha256:" + "a" * 64
        state, state_raw = v2_state_document(
            engine="bind", engine_epoch=1, qualifier=qualifier,
            request_id=request_id, owner_id=owner_id,
        )
        receipt = {
            "schema": run_cell.TRIGGER_IDENTITY_RECEIPT_SCHEMA,
            "cell_id": selected.cell_id, "driver": "bind",
            "source_fixture": "uninitialized", "request_id": request_id,
            "owner_id": owner_id, "manifest_qualifier": qualifier,
        }
        with tempfile.TemporaryDirectory() as directory:
            source_path = Path(directory, "source.json")
            receipt_path = Path(directory, "identity.json")
            source_raw = (json.dumps(source, indent=2, sort_keys=True) + "\n").encode()
            source_path.write_bytes(source_raw)
            receipt_raw = (json.dumps(receipt, separators=(",", ":")) + "\n").encode()
            receipt_path.write_bytes(receipt_raw)
            source_path.chmod(0o600)
            receipt_path.chmod(0o600)
            proof = {
                "source_setup_scenario_sha256": hashlib.sha256(source_raw).hexdigest(),
                "source_setup_identity_receipt_sha256": hashlib.sha256(receipt_raw).hexdigest(),
            }
            with mock.patch.object(run_cell, "MANAGED_BIND_SETUP_SCENARIO_PATH", str(source_path)), mock.patch.object(
                run_cell, "MANAGED_BIND_SETUP_IDENTITY_PATH", str(receipt_path)
            ):
                run_cell.validate_managed_bind_setup(proof, selected, measured, state, state_raw)
                forged = dict(source, peer_ip="192.0.2.12")
                forged_raw = (json.dumps(forged, indent=2, sort_keys=True) + "\n").encode()
                source_path.write_bytes(forged_raw)
                proof["source_setup_scenario_sha256"] = hashlib.sha256(forged_raw).hexdigest()
                with self.assertRaises(run_cell.ControllerError):
                    run_cell.validate_managed_bind_setup(proof, selected, measured, state, state_raw)

    @unittest.skipUnless(hasattr(os, "geteuid"), "secure fixture files require POSIX ownership")
    def test_managed_bind_setup_refuses_forged_operation_identity(self) -> None:
        selected = cell(driver="pdns-switch")
        measured = guest_bootstrap.pdns_switch_scenario()
        source = guest_bootstrap.bind_scenario("uninitialized", node="debian13")
        request_id = hashlib.sha256(
            (selected.cell_id + "\0source-bind-switch").encode()
        ).hexdigest()[:32]
        owner_id = run_cell.deterministic_trigger_owner(selected.cell_id, request_id)
        qualifier = "dns-engine-switch/v1:sha256:" + "a" * 64
        state, state_raw = v2_state_document(
            engine="bind", engine_epoch=1, qualifier=qualifier,
            request_id=request_id, owner_id=owner_id,
        )
        receipt = {
            "schema": run_cell.TRIGGER_IDENTITY_RECEIPT_SCHEMA,
            "cell_id": selected.cell_id,
            "driver": "bind",
            "source_fixture": "uninitialized",
            "request_id": request_id,
            "owner_id": owner_id,
            "manifest_qualifier": qualifier,
        }
        with tempfile.TemporaryDirectory() as directory:
            source_path = Path(directory, "source.json")
            receipt_path = Path(directory, "identity.json")
            source_raw = (json.dumps(source, indent=2, sort_keys=True) + "\n").encode()
            source_path.write_bytes(source_raw)
            receipt_raw = (json.dumps(receipt, separators=(",", ":")) + "\n").encode()
            receipt_path.write_bytes(receipt_raw)
            source_path.chmod(0o600)
            receipt_path.chmod(0o600)
            proof = {
                "source_setup_scenario_sha256": hashlib.sha256(source_raw).hexdigest(),
                "source_setup_identity_receipt_sha256": hashlib.sha256(receipt_raw).hexdigest(),
            }
            with mock.patch.object(run_cell, "MANAGED_BIND_SETUP_SCENARIO_PATH", str(source_path)), mock.patch.object(
                run_cell, "MANAGED_BIND_SETUP_IDENTITY_PATH", str(receipt_path)
            ):
                run_cell.validate_managed_bind_setup(proof, selected, measured, state, state_raw)
                forged = dict(receipt, owner_id="f" * 32)
                forged_raw = (json.dumps(forged, separators=(",", ":")) + "\n").encode()
                receipt_path.write_bytes(forged_raw)
                proof["source_setup_identity_receipt_sha256"] = hashlib.sha256(forged_raw).hexdigest()
                with self.assertRaises(run_cell.ControllerError):
                    run_cell.validate_managed_bind_setup(proof, selected, measured, state, state_raw)


BATCH5_C6 = (Path(__file__).with_name("evidence") / "batch5-paired-first-20260929"
             / "c6-reinstall" / "raw")


class ManagedBindSetupV2ReceiptTest(unittest.TestCase):
    """Batch 5 cell c6: the reinstall setup reads the product's v2 state receipt."""

    def copy(self, directory: str, name: str, source: Path) -> Path:
        target = Path(directory, name)
        target.write_bytes(source.read_bytes())
        target.chmod(0o600)
        return target

    def test_real_v2_receipt_from_the_reinstall_cell(self) -> None:
        manifest = json.loads(MANIFEST_TEXT)
        selected = run_cell.CellSpec.from_manifest(
            manifest, "bind__target-staged__after-write__standalone__peer-reachable")
        fixture = BATCH5_C6 / "fixture"
        state_raw = (BATCH5_C6 / "state" / "dns-engine-state.json").read_bytes()
        state = json.loads(state_raw)
        self.assertEqual(state["schema"], "celikpanel-dns-engine-state/v2")
        self.assertNotIn("manifest_qualifier", state)  # the flat read raised KeyError
        proof = json.loads((fixture / "source-proof.json").read_bytes())
        measured = json.loads((fixture / "scenario.json").read_bytes())
        self.assertEqual(measured["source_fixture"], run_cell.MANAGED_BIND_ABSENT)
        with tempfile.TemporaryDirectory() as directory:
            scenario_path = self.copy(directory, "setup.json", fixture / "source-setup-bind.json")
            identity_path = self.copy(directory, "identity.json",
                                      fixture / "source-setup-bind-identity.json")
            with mock.patch.multiple(
                run_cell, MANAGED_BIND_SETUP_SCENARIO_PATH=str(scenario_path),
                MANAGED_BIND_SETUP_IDENTITY_PATH=str(identity_path),
            ):
                run_cell.validate_managed_bind_setup(proof, selected, measured, state, state_raw)
                # Any change of the receipt bytes is refused, not a KeyError.
                tampered = state_raw.replace(b'"engine_epoch":1', b'"engine_epoch":2')
                with self.assertRaises(run_cell.ControllerError):
                    run_cell.validate_managed_bind_setup(
                        proof, selected, measured, json.loads(tampered), tampered)
                with self.assertRaises(run_cell.ControllerError):
                    run_cell.validate_managed_bind_setup(
                        proof, selected, measured, state, state_raw.rstrip(b"\n"))
                flat = run_cell.decode_dns_document(state, state_raw)
                self.assertEqual(flat["engine"], "bind")
                with self.assertRaises(run_cell.ControllerError):
                    run_cell.validate_managed_bind_setup(
                        proof, selected, measured, flat,
                        (json.dumps(flat) + "\n").encode())
        # The same shared projection is what validate_managed_source_state reads.
        run_cell.validate_managed_source_state(
            state, state_raw, dict(measured, source_epoch=1, source_revision=0), "bind")

    def test_no_flat_state_receipt_reads_remain(self) -> None:
        source = MODULE_PATH.read_text(encoding="utf-8")
        body = source[source.index("def validate_managed_bind_setup("):
                      source.index("def validate_source_scenario(")]
        self.assertIn("state = decode_dns_document(state_document, state_raw)", body)
        self.assertNotIn('state["', body)
        shell = Path(__file__).with_name("guest_bootstrap.sh").read_text(encoding="utf-8")
        # Every shell read of the state receipt's identity goes through the
        # probe's shared decoder.
        self.assertEqual(shell.count("['decode_dns_document'](json.loads(raw),raw)"), 2)


class NativeVersionsTest(unittest.TestCase):
    def test_versions_are_recorded_before_and_after(self) -> None:
        outputs = {
            "bind9": "ii  1:9.20.29-1~deb13u1", "bind9-libs": "ii  1:9.20.29-1~deb13u1",
            "bind9-host": "ii  1:9.20.29-1~deb13u1", "pdns-server": "ii  4.9.17-0+deb13u1",
        }

        def run(argv, **kwargs):
            if argv[0] == "/usr/bin/dpkg-query":
                text = outputs.get(argv[-1])
                if text is None:
                    return mock.Mock(returncode=1, stdout=b"dpkg-query: no packages found")
                return mock.Mock(returncode=0, stdout=text.encode())
            return mock.Mock(returncode=0, stdout=b"BIND 9.20.29-1~deb13u1-Debian (Stable Release)")

        exists = {"/usr/bin/dpkg-query": True, "/usr/sbin/named": True,
                  "/usr/sbin/pdns_server": False, "/usr/bin/pacman": False}
        with mock.patch.object(run_cell.subprocess, "run", side_effect=run), \
                mock.patch.object(run_cell.os.path, "exists",
                                  side_effect=lambda path: exists.get(path, False)):
            report = run_cell.record_native_versions({}, 5.0)
        self.assertEqual(report["package_manager"], "dpkg")
        self.assertEqual(report["packages"]["pdns-server"], "4.9.17-0+deb13u1")
        self.assertIsNone(report["packages"]["pdns-backend-sqlite3"])
        self.assertIn("9.20.29", report["daemons"]["named"]["output"])
        self.assertNotIn("pdns_server", report["daemons"])
        source = MODULE_PATH.read_text(encoding="utf-8")
        flow = source[source.index("def run_cell(settings:"):]
        self.assertLess(flow.index('"before": record_native_versions('),
                        flow.index("tagged = start_tagged_agent("))
        # The rpc-retry tail runs after every judgement of that flow.
        tail = flow.index("finish_rpc_retry_flow(")
        for judgement in ("judge_agent_startup_rollback(", "judge_fixture_pass_definition(",
                          "judge_fresh_primary_pass_definition(",
                          "judge_recovery_status_reads(result)"):
            self.assertLess(flow.index(judgement), tail, judgement)
        self.assertLess(tail, flow.index("except RebootRequested as request:"))
        # One writer of ``after``: every flow-finishing path goes through it.
        self.assertEqual(source.count('["after"] = record_native_versions('), 1)
        for function in ("def finish_rpc_retry_flow(", "def run_owner_inverse_after_restart(",
                         "def resume_owner_inverse_before_owner_command(",
                         "def run_fresh_primary_hold_flow(", "def resume_cell("):
            body = source[source.index(function):]
            body = body[:body.index("\ndef ", 1)]
            self.assertIn("record_native_versions_after(", body, function)


class NativeVersionsEveryFlowTest(unittest.TestCase):
    """Batch 7 c05: every path that finishes a flow records before and after.

    ``before`` is written by run_cell before the tagged Agent; these tests
    start from a result that already holds it, as run_cell leaves it, and
    assert that the path adds ``after`` without replacing ``before``.
    """

    BEFORE = {"at": "before", "packages": {"bind9": "1"}, "daemons": {}}
    BOOT = OwnerInverseAfterRestartTest.BOOT

    def setUp(self) -> None:
        # Reuse the owner-flow scaffolding without re-running its tests; its
        # results start with ``before`` as run_cell leaves it.
        self.owner = OwnerInverseAfterRestartTest("test_agent_decides_owner_executes_passes")
        plain_result = self.owner.flow_result

        def flow_result(*args, **kwargs) -> dict:
            result = plain_result(*args, **kwargs)
            result["native_versions"] = {"before": dict(self.BEFORE)}
            return result

        self.owner.flow_result = flow_result

    def assert_before_and_after(self, result: dict) -> None:
        versions = result.get("native_versions") or {}
        self.assertEqual(versions.get("before"), self.BEFORE, versions)
        self.assertIn("after", versions, versions)
        self.assertIsNot(versions["after"], None)

    def test_owner_inverse_flow(self) -> None:
        result, _, _, _ = self.owner.run_flow(FakeOwnerGuest())
        self.assertEqual(result["status"], "passed")
        self.assert_before_and_after(result)

    def test_owner_inverse_resume_after_reboot_before_owner_command(self) -> None:
        guest = FakeOwnerGuest()
        settings, result, state, lists, _ = self.owner.suspend(guest)
        # The suspended flow stopped before the owner command: no ``after`` yet.
        self.assertNotIn("after", result["native_versions"])
        result = self.owner.resume(guest, settings, result, state, lists)
        self.assertEqual(result["status"], "passed")
        self.assert_before_and_after(result)

    def test_post_reboot_resume(self) -> None:
        for stage_after in (True, False):
            with self.subTest(after_recorded_before_reboot=stage_after), \
                    tempfile.TemporaryDirectory() as root:
                settings = replace(plain_settings(root, reboot_before_owner_command=False,
                                                  reboot_after_recovery=True),
                                   resume_after_reboot=True)
                Path(settings.transcript_path).write_text("first\n", encoding="utf-8")
                Path(settings.proof_path).write_text("{}\n", encoding="utf-8")
                versions: dict = {"before": dict(self.BEFORE)}
                if stage_after:
                    versions["after"] = {"at": "after-recovery"}
                run_cell.write_reboot_checkpoint(
                    settings, 1,
                    run_cell.RebootRequested(run_cell.REBOOT_AFTER_RECOVERY,
                                             {"flow": "rpc-retry", "boot": self.BOOT}),
                    {"status": "passed", "safety_status": "passed",
                     "native_versions": versions},
                    kill_proven=True, safety_failures=[], verification_failures=[],
                    diagnostic_failures=[], boot=self.BOOT,
                    transcripts=[{"path": settings.transcript_path,
                                  "sha256": run_cell.sha256_file(settings.transcript_path)}],
                )
                rebooted = dict(self.BOOT, boot_id="5" * 8 + "-5555-5555-5555-" + "5" * 12)
                with mock.patch.multiple(
                    run_cell,
                    validate_controller_identity=mock.Mock(return_value={"effective_gid": 1}),
                    minimal_command_environment=mock.Mock(return_value={}),
                    validate_settings=mock.Mock(return_value={}),
                    read_guest_boot_identity=mock.Mock(return_value=rebooted),
                    ordinary_environment=mock.Mock(return_value={}),
                    verify_after_recovery_reboot=mock.Mock(),
                    record_native_versions=mock.Mock(return_value={"at": "post-reboot"}),
                ):
                    self.assertEqual(run_cell.resume_cell(settings), 0)
                result = json.loads(Path(settings.result_path).read_text(encoding="utf-8"))
                self.assert_before_and_after(result)
                # An ``after`` taken when recovery finished is kept, not re-read.
                self.assertEqual(result["native_versions"]["after"],
                                 {"at": "after-recovery" if stage_after else "post-reboot"})

    def rpc_retry_tail(self, extra: dict | None = None, **changes: object) -> dict:
        with tempfile.TemporaryDirectory() as root:
            settings = plain_settings(root, owner_inverse_after_restart=False,
                                      reboot_before_owner_command=False, **changes)
            result = complete_rpc_retry_pass(**(extra or {}))
            result["native_versions"] = {"before": dict(self.BEFORE)}
            with mock.patch.object(run_cell, "record_native_versions",
                                   return_value={"at": "after"}):
                run_cell.finish_rpc_retry_flow(settings, result, {}, peer_ip="",
                                               agent_identity=(1, 2))
        self.assertTrue(result["complete_verdict"]["passed"], result["complete_verdict"])
        return result

    def test_socket_recovery_flow(self) -> None:
        self.assert_before_and_after(self.rpc_retry_tail(trigger_mode="socket"))

    def test_agent_startup_rollback_flow(self) -> None:
        self.assert_before_and_after(self.rpc_retry_tail(
            {"agent_startup_rollback": {"observed": True, "failures": [], "unknown": []}},
            expect_agent_startup_rollback=True,
        ))

    def test_rpc_retry_tail_with_reboot_after_recovery_records_before_the_request(self) -> None:
        # The reboot request carries the result into the checkpoint, so
        # ``after`` must already be in it.
        with tempfile.TemporaryDirectory() as root:
            settings = plain_settings(root, owner_inverse_after_restart=False,
                                      reboot_before_owner_command=False,
                                      reboot_after_recovery=True)
            result = complete_rpc_retry_pass()
            result["native_versions"] = {"before": dict(self.BEFORE)}
            with mock.patch.multiple(
                run_cell,
                record_native_versions=mock.Mock(return_value={"at": "after"}),
                observe_serving_authority=mock.Mock(return_value={"authority": {}}),
                observe_management_units=mock.Mock(return_value={"units": {}, "unknown": []}),
                read_guest_boot_identity=mock.Mock(return_value=self.BOOT),
            ), self.assertRaises(run_cell.RebootRequested):
                run_cell.finish_rpc_retry_flow(settings, result, {}, peer_ip="",
                                               agent_identity=(1, 2))
        self.assert_before_and_after(result)


class OwnerFlowHelpers:
    """Reuse the owner-flow scaffolding without re-running its tests."""

    def setUp(self) -> None:
        self.owner = OwnerInverseAfterRestartTest("test_agent_decides_owner_executes_passes")

    def run_flow(self, *args, **kwargs):
        return self.owner.run_flow(*args, **kwargs)

    def run_adoption(self, **deviations):
        return self.owner.run_adoption(**deviations)

    critical_guest = staticmethod(OwnerInverseAfterRestartTest.critical_guest)


class V2RollbackStandbyEndStateTest(OwnerFlowHelpers, unittest.TestCase):
    """Step 5 judges the rollback standby; step 6 expects a completed re-run (f7a844f7)."""

    def end_state(self, phase: str = "target-staged", **deviations: object):
        guest = FakeOwnerGuest(**deviations)
        if phase in run_cell.OWNER_INVERSE_CRITICAL_PHASES:
            guest = self.critical_guest(phase, **deviations)
        result, _, _, _ = self.run_flow(guest, phase)
        flow = result["owner_inverse_after_restart"]
        return result, flow, flow["steps"]["after_owner_command"].get("rollback_end_state")

    def test_sealed_standby_passes_in_both_variants(self) -> None:
        for phase in ("intent", "target-staged", "source-stopped", "target-started"):
            with self.subTest(phase=phase):
                result, flow, end = self.end_state(phase)
                self.assertEqual((result["status"], flow["status"]), ("passed", "passed"),
                                 flow["failures"] + flow["ambiguities"])
                self.assertEqual(end["failures"], [])
                self.assertEqual(len(end["summary"]["restored"]), 1)
                self.assertEqual(len(end["summary"]["removed"]), 1)
                self.assertEqual(len(end["summary"]["intentionally_kept"]), 1)
                self.assertEqual(len(end["summary"]["working_directory"]), 1)
                self.assertTrue(end["removed_line_required"])
                self.assertIs(end["generation_tree_present_after"], False)
                self.assertEqual(end["leftovers"], {"judged": False})
                rerun = flow["steps"]["rerun"]
                self.assertEqual((rerun["expected_returncode"],
                                  rerun["command"]["returncode"]), (0, 0))
                self.assertIn("already reconciled", rerun["stdout"])

    def test_standby_deviations_fail(self) -> None:
        no_removed = FakeOwnerGuest().summary_text().replace("Removed:", "Gone:")
        no_kept = FakeOwnerGuest().summary_text().replace("Intentionally kept", "Kept")
        no_restored = FakeOwnerGuest().summary_text().replace("Restored:", "Back:")
        kept_changed = FakeOwnerGuest().summary_text() + (
            "Not removed: the BIND generation " + "a" * 64 + " under "
            "/var/cache/bind/celikpanel/generations, because it is not exactly what this "
            "switch staged (content differs). Check it before removing it yourself.\n")
        for phase, deviation, text in (
            ("target-staged", {"bind_sealed": False}, "not under a persistent mask"),
            ("target-started", {"bind_sealed": False}, "persistent mask is not a root-owned link"),
            ("target-staged", {"tree_after": True}, "staged BIND generation tree"),
            ("source-stopped", {"summary": no_removed}, "no 'Removed:' line"),
            ("target-staged", {"summary": no_kept}, "Intentionally kept as rollback standby"),
            ("target-staged", {"summary": no_restored}, "no 'Restored:' line"),
            ("target-staged", {"summary": kept_changed}, "these cells do not edit it"),
            ("target-staged", {"rerun_exit": 3}, "re-run exited 3, want 0"),
            ("target-started", {"rerun_exit": 3}, "re-run exited 3, want 0"),
        ):
            with self.subTest(phase=phase, deviation=deviation):
                result, flow, _ = self.end_state(phase, **deviation)
                self.assertEqual((result["status"], flow["status"]), ("failed", "failed"))
                self.assertIn(text, " ".join(flow["failures"]))

    def test_removed_line_needs_a_tree_present_at_step2(self) -> None:
        # The in-process rollback already removed the tree (rolled-back cells).
        summary = FakeOwnerGuest().summary_text().replace("Removed:", "Gone:")
        result, flow, end = self.end_state(tree_at_step2=False, summary=summary)
        self.assertEqual(result["status"], "passed", flow["failures"])
        self.assertFalse(end["removed_line_required"])
        self.assertIn("already absent at step 2", end["removed_line_absent_reason"])

    def test_adoption_profile_has_no_switch_standby_judgement(self) -> None:
        result, _ = self.run_adoption()
        step5 = result["owner_inverse_after_restart"]["steps"]["after_owner_command"]
        self.assertNotIn("rollback_end_state", step5)
        self.assertEqual(result["status"], "passed")
        result, _ = self.run_adoption(rerun_exit=3)
        self.assertIn("re-run exited 3", " ".join(
            result["owner_inverse_after_restart"]["failures"]))

    def test_pure_standby_judgement(self) -> None:
        guest = FakeOwnerGuest()
        masks = guest.guard_masks(None, None)
        facts = guest.rollback_facts("/j")
        text = guest.summary_text()
        report = run_cell.judge_v2_bind_rollback_end_state(facts, masks, text, False)
        self.assertEqual((report["failures"], report["unknown"]), ([], []))
        # Unreadable facts are an unknown, not a pass.
        report = run_cell.judge_v2_bind_rollback_end_state({"error": "gone"}, masks, text, None)
        self.assertIn("rollback facts were not read", " ".join(report["unknown"]))
        # A journal that froze an existing BIND is restored exactly: recorded.
        report = run_cell.judge_v2_bind_rollback_end_state(
            dict(facts, sealed_end_state_expected=False),
            FakeOwnerGuest(bind_sealed=False).guard_masks(None, None), text, False)
        self.assertEqual(report["failures"], [])
        self.assertIs(report["sealed_end_state"]["judged"], False)
        # Only names that exist are judged; none existing is a failure.
        absent = {"units": {unit: {
            "properties": {"LoadState": "not-found", "ActiveState": "inactive",
                           "UnitFileState": ""},
            "persistent_link": {"exists": False}, "runtime_link": {"exists": False},
        } for unit in run_cell.BIND_TARGET_UNITS}, "unknown": []}
        failures, _ = run_cell.judge_bind_guard_sealed(absent)
        self.assertIn("no BIND unit name exists", failures[0])
        one = json.loads(json.dumps(masks))
        one["units"]["bind9.service"] = absent["units"]["bind9.service"]
        self.assertEqual(run_cell.judge_bind_guard_sealed(one), ([], []))
        runtime = json.loads(json.dumps(masks))
        runtime["units"]["named.service"]["runtime_link"] = {
            "path": "/run/systemd/system/named.service", "exists": True, "symlink": True,
            "uid": 0, "target": "/dev/null"}
        self.assertIn("runtime mask", " ".join(run_cell.judge_bind_guard_sealed(runtime)[0]))
        active = json.loads(json.dumps(masks))
        active["units"]["named.service"]["properties"]["ActiveState"] = "active"
        self.assertIn("want inactive", " ".join(run_cell.judge_bind_guard_sealed(active)[0]))
        lines = run_cell.parse_bind_rollback_summary(text)
        self.assertEqual(lines["accepted_stub_listeners"][0][:20], "Local port-53 listen")
        self.assertEqual(lines["bind_units"][0][:11], "BIND units:")

    def test_rollback_facts_are_read_from_the_v2_journal(self) -> None:
        if not (hasattr(os, "geteuid") and os.geteuid() == 0):
            self.skipTest("the journal proof requires root ownership")
        with tempfile.TemporaryDirectory() as root:
            journal = Path(root, "journal.json")
            generation = "b" * 64
            journal.write_text(json.dumps({
                "target_generation": generation,
                "target_units_before": [
                    {"name": "named.service", "load_state": "not-found"},
                    {"name": "bind9.service", "load_state": "masked"},
                ],
            }), encoding="utf-8")
            journal.chmod(0o600)
            with mock.patch.object(run_cell, "generation_tree_present", return_value=True):
                facts = run_cell.read_v2_rollback_facts(str(journal))
            self.assertEqual(facts["generation_tree"],
                             "/var/cache/bind/celikpanel/generations/" + generation)
            self.assertTrue(facts["sealed_end_state_expected"])
            self.assertTrue(facts["generation_tree_present_at_step2"])
            journal.write_text(json.dumps({
                "target_generation": "../escape",
                "target_units_before": [{"name": "named.service", "load_state": "loaded"}],
            }), encoding="utf-8")
            facts = run_cell.read_v2_rollback_facts(str(journal))
            self.assertIsNone(facts["generation_tree"])
            self.assertFalse(facts["sealed_end_state_expected"])


class RetrySwitchAfterRollbackTest(unittest.TestCase):
    """--retry-switch-after-rollback: a NEW request must complete forward."""

    def retry_settings(self, root: str, **changes: object) -> object:
        settings = plain_settings(
            root, owner_cell("target-started"), reboot_before_owner_command=False,
            reboot_dir=None, retry_switch_after_rollback=True,
            recovery_probe_command=(
                "/opt/probe", "--cell-id", "x", "--identity-receipt", root + "/identity.json",
            ),
        )
        return replace(settings, **changes) if changes else settings

    def passing_result(self) -> dict:
        return {
            "status": "passed", "safety_status": "passed",
            "owner_inverse_after_restart": {"status": "passed"},
            "recovery_outcome": {"classification": "rolled_back_source_serving"},
        }

    def run_retry(self, settings, result, **deviations: object) -> dict:
        d = {"returncode": 0, "status": "succeeded", "masked": False, "pdns": "inactive",
             "bind_errors": [], "journal": False, "old_changes": False,
             "probe": "target_converged", **deviations}
        retry_id = run_cell.retry_switch_request_id(settings.request_id)
        old_job = {"request_id": settings.request_id, "status": "failed"}
        reads = {"count": 0}

        def ledger(state_dir, request_id):
            if request_id == retry_id:
                return ({"active_request_id": ""}, {
                    "request_id": retry_id, "status": d["status"], "target": "bind",
                    "phase": "commit/dns-engine-switch/v2/finalized/" + retry_id + "/q"})
            reads["count"] += 1
            job = dict(old_job, touched=True) if d["old_changes"] and reads["count"] > 1 \
                else old_job
            return ({"active_request_id": ""}, job)

        def masks(settings, environment):
            state = "masked" if d["masked"] else "loaded"
            return {"units": {unit: {
                "properties": {"LoadState": state, "ActiveState": "active",
                               "UnitFileState": "masked" if d["masked"] else "enabled"},
                "persistent_link": {"exists": True, "symlink": True, "uid": 0,
                                    "target": "/dev/null" if d["masked"] else
                                    "/usr/lib/systemd/system/named.service",
                                    "path": "/etc/systemd/system/" + unit},
                "runtime_link": {"exists": False, "path": "/run/systemd/system/" + unit},
            } for unit in run_cell.BIND_TARGET_UNITS}, "unknown": []}

        command = mock.Mock(returncode=d["returncode"], output=b"rpc-switch-complete\n",
                            truncated=False)
        command.report.return_value = {"returncode": d["returncode"]}
        probe = {"valid": True, "recovery_outcome": d["probe"], "fingerprint": "c" * 64,
                 "active_dns_engine": "bind"}
        ok = {"ok": True}
        with mock.patch.multiple(
            run_cell,
            read_request_ledger=mock.Mock(side_effect=ledger),
            wait_for_unix_socket=mock.Mock(return_value=(1, 2)),
            run_bounded_command=mock.Mock(return_value=command),
            validate_trigger_identity_receipt=mock.Mock(return_value={"request_id": retry_id}),
            read_dns_state_semantic=mock.Mock(return_value={"semantic": {"engine": "bind"}}),
            observe_bind_source_serving=mock.Mock(return_value={
                "errors": list(d["bind_errors"]), "unknown": []}),
            observe_bind_guard_masks=mock.Mock(side_effect=masks),
            observe_native_dns_state=mock.Mock(return_value={
                "units": {"pdns.service": {"ActiveState": d["pdns"]}}}),
            decode_recovery_probe=mock.Mock(side_effect=lambda c, ordinal: dict(
                probe, ordinal=ordinal)),
            run_stability_window=mock.Mock(return_value=(
                {"samples": [{"agent": ok, "panel": ok, "dns": ok}] * 31}, [], [])),
            require_new_output_path=mock.Mock(),
        ), mock.patch.object(run_cell.os.path, "lexists", return_value=d["journal"]):
            run_cell.maybe_retry_switch_after_rollback(
                settings, result, transcript=mock.Mock(), ordinary={"PATH": "/usr/bin"},
                owner_environment={}, verification_failures=[],
            )
            self.last_trigger = run_cell.run_bounded_command.call_args_list[0]
        return result

    def test_new_request_identity_and_commands(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            settings = self.retry_settings(root)
            retry_id = run_cell.retry_switch_request_id(settings.request_id)
            self.assertRegex(retry_id, "^[0-9a-f]{32}$")
            self.assertNotEqual(retry_id, settings.request_id)
            trigger, probe, receipt = run_cell.retry_switch_commands(settings, retry_id)
            self.assertEqual(trigger[1], "rpc-switch")
            self.assertEqual(receipt, root + "/" + run_cell.RETRY_SWITCH_IDENTITY_NAME)
            self.assertEqual(trigger[5], receipt)
            self.assertEqual(probe[probe.index("--identity-receipt") + 1], receipt)
            self.assertEqual(trigger[3], settings.trigger_command[3])  # same scenario
            with self.assertRaises(run_cell.ControllerError):
                run_cell.retry_switch_commands(replace(settings, trigger_command=None), retry_id)

    def test_forward_retry_passes_and_uses_a_new_request(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            settings = self.retry_settings(root)
            result = self.run_retry(settings, self.passing_result())
            report = result["retry_switch_after_rollback"]
            self.assertEqual((result["status"], report["status"]), ("passed", "passed"),
                             report["failures"] + report["unknown"])
            self.assertEqual(report["request_id"],
                             run_cell.retry_switch_request_id(settings.request_id))
            argv, label = self.last_trigger.args[0], self.last_trigger.args[1]
            self.assertEqual(label, "retry-switch-after-rollback")
            self.assertEqual(argv[1], "rpc-switch")
            environment = self.last_trigger.args[3]
            self.assertEqual(environment["CELIKPANEL_S1_REQUEST_ID"], report["request_id"])
            self.assertEqual(report["recovery_outcome"]["classification"], "target_converged")
            self.assertEqual(len(report["stability"]["samples"]), 31)

    def test_forward_retry_deviations_fail(self) -> None:
        for deviation, text in (
            ({"returncode": 1}, "exited 1, want 0"),
            ({"status": "failed"}, "not a finalized succeeded BIND switch"),
            ({"masked": True}, "still masked after the forward switch"),
            ({"pdns": "active"}, "PowerDNS is active"),
            ({"bind_errors": ["owner BIND unit is not active"]}, "BIND serving"),
            ({"journal": True}, "journal remains"),
            ({"old_changes": True}, "changed the rolled-back job"),
            ({"probe": "indeterminate"}, "want target_converged"),
        ):
            with self.subTest(deviation=deviation), tempfile.TemporaryDirectory() as root:
                result = self.run_retry(self.retry_settings(root), self.passing_result(),
                                        **deviation)
                report = result["retry_switch_after_rollback"]
                self.assertEqual((result["status"], report["status"]), ("failed", "failed"))
                self.assertIn(text, " ".join(report["failures"]))

    def test_retry_runs_only_after_a_passed_rollback(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            settings = self.retry_settings(root)
            for result, text in (
                (dict(self.passing_result(), status="failed"), "status is 'failed'"),
                (dict(self.passing_result(),
                      recovery_outcome={"classification": "repeated_nonconvergence"}),
                 "classification"),
                (dict(self.passing_result(), reboot_after_recovery={
                    "run": True, "judged": False, "observed_status": "passed"}),
                 "reboot was not a judged pass"),
            ):
                with self.subTest(text=text), mock.patch.object(
                    run_cell, "run_bounded_command") as run:
                    run_cell.maybe_retry_switch_after_rollback(
                        settings, result, transcript=mock.Mock(), ordinary={},
                        owner_environment={}, verification_failures=[],
                    )
                    run.assert_not_called()
                    report = result["retry_switch_after_rollback"]
                    self.assertIs(report["run"], False)
                    self.assertIn(text, report["reason"])
            untouched = self.passing_result()
            run_cell.maybe_retry_switch_after_rollback(
                replace(settings, retry_switch_after_rollback=False), untouched,
                transcript=mock.Mock(), ordinary={}, owner_environment={},
                verification_failures=[],
            )
            self.assertNotIn("retry_switch_after_rollback", untouched)

    def test_owner_flow_calls_the_retry_after_the_rollback(self) -> None:
        source = MODULE_PATH.read_text(encoding="utf-8")
        flow = source[source.index("def run_owner_inverse_after_restart("):]
        flow = flow[:flow.index("\ndef ")]
        self.assertLess(flow.index("flow.finish()"),
                        flow.index("maybe_request_reboot_after_recovery("))
        self.assertLess(flow.index("maybe_request_reboot_after_recovery("),
                        flow.index("maybe_retry_switch_after_rollback("))
        resume = source[source.index("def resume_cell("):]
        self.assertLess(resume.index("verify_after_recovery_reboot("),
                        resume.index("maybe_retry_switch_after_rollback("))


class PreRebootVerdictTest(unittest.TestCase):
    BOOT = RebootCheckpointTest.BOOT

    def test_c6_shape_is_not_rebooted(self) -> None:
        # Batch cell c6: status "passed" (D-021 safety), both retries exited 1,
        # both probes indeterminate, classification repeated_nonconvergence.
        indeterminate = {"valid": True, "recovery_outcome": "indeterminate",
                         "fingerprint": "e" * 64}
        c6 = {
            "status": "passed", "safety_status": "passed",
            "source_proof": {"source_fixture": "uninitialized"},
            "recovery_outcome": {"classification": "repeated_nonconvergence"},
            "recovery": {"attempts": [
                {"ordinal": 1, "command": {"returncode": 1}, "error": "exited 1"},
                {"ordinal": 2, "command": {"returncode": 1}, "error": "exited 1"},
            ]},
            "recovery_probes": [dict(indeterminate, ordinal=1), dict(indeterminate, ordinal=2)],
        }
        verdict = run_cell.pre_reboot_verdict(c6, {"flow": "rpc-retry"})
        self.assertFalse(verdict["passed"])
        joined = " ".join(verdict["reasons"])
        for text in ("retry 1: exited 1", "first recovery probe is indeterminate",
                     "classification is 'repeated_nonconvergence'"):
            self.assertIn(text, joined)
        self.assertEqual(verdict["required_classification"], ["target_converged"])
        settings = mock.Mock(reboot_after_recovery=True, reboot_even_if_failed=False,
                             disable_management_before_reboot=False)
        result = json.loads(json.dumps(c6))
        with mock.patch.object(run_cell, "read_guest_boot_identity") as boot:
            run_cell.maybe_request_reboot_after_recovery(settings, result, {}, {"flow": "rpc-retry"})
            boot.assert_not_called()
        self.assertIs(result["reboot_after_recovery"]["run"], False)
        self.assertIn("--reboot-even-if-failed", result["reboot_after_recovery"]["reason"])
        self.assertEqual(result["status"], "passed")  # D-021 status is unchanged
        # The diagnostic flag reboots anyway and marks the state.
        settings.reboot_even_if_failed = True
        with mock.patch.multiple(
            run_cell,
            observe_serving_authority=mock.Mock(return_value={"authority": {}}),
            observe_management_units=mock.Mock(return_value={"units": {}, "unknown": []}),
            read_guest_boot_identity=mock.Mock(return_value=self.BOOT),
        ), self.assertRaises(run_cell.RebootRequested) as raised:
            run_cell.maybe_request_reboot_after_recovery(
                settings, json.loads(json.dumps(c6)), {}, {"flow": "rpc-retry"})
        self.assertIs(raised.exception.state["diagnostic_reboot"], True)

    def test_required_classification_follows_the_pass_definition(self) -> None:
        self.assertEqual(run_cell.required_rpc_retry_classifications(
            {"source_proof": {"source_fixture": "managed-pdns"}}),
            frozenset({"target_converged", "rolled_back_source_serving"}))
        for result in ({"source_proof": {"source_fixture": "uninitialized"}},
                       {"fixture_pass_definition": {}}, {"agent_startup_rollback": {}}):
            with self.subTest(result=result):
                self.assertEqual(run_cell.required_rpc_retry_classifications(result),
                                 frozenset({"target_converged"}))
        owner = {"status": "passed", "safety_status": "passed",
                 "owner_inverse_after_restart": {"status": "passed"},
                 "recovery_outcome": {"classification": "rolled_back_source_serving"}}
        self.assertTrue(run_cell.pre_reboot_verdict(owner, {"flow": "owner-inverse"})["passed"])
        failed_owner = dict(owner, owner_inverse_after_restart={"status": "failed"})
        self.assertFalse(run_cell.pre_reboot_verdict(failed_owner,
                                                     {"flow": "owner-inverse"})["passed"])
        self.assertFalse(run_cell.pre_reboot_verdict(
            dict(owner, safety_status="failed"), {"flow": "owner-inverse"})["passed"])
        self.assertTrue(run_cell.pre_reboot_verdict(complete_rpc_retry_pass(),
                                                    {"flow": "rpc-retry"})["passed"])

    def test_diagnostic_reboot_records_unjudged_and_keeps_the_status(self) -> None:
        before = RebootCheckpointTest().authority({"tcp": ["bind"], "udp": ["bind"]})
        dead = RebootCheckpointTest().authority({"tcp": [], "udp": []})
        dead["authority"]["answered"] = False
        state = {"flow": "rpc-retry", "authority_before": before, "diagnostic_reboot": True}
        settings = mock.Mock(cell=owner_cell("target-staged"), agent_socket="/s",
                             endpoint_timeout=1.0, panel_address="127.0.0.1", panel_port=2083)
        result = {"status": "passed", "safety_status": "passed"}
        safety: list = []
        verification: list = []
        bad = {"ok": False}
        with mock.patch.multiple(
            run_cell,
            observe_management_units=mock.Mock(return_value={"unknown": [], "units": {
                unit: {"ActiveState": "active"} for unit in run_cell.MANAGEMENT_UNITS}}),
            wait_for_unix_socket=mock.Mock(return_value=(1, 2)),
            inspect_restarted_agent_process=mock.Mock(return_value={"pid": 9}),
            wait_for_tcp=mock.Mock(return_value=None),
            observe_serving_authority=mock.Mock(return_value=dead),
            run_stability_window=mock.Mock(return_value=(
                {"samples": [{"agent": bad, "panel": bad, "dns": bad}] * 31},
                ["stability sample 1: refused"], [])),
        ):
            run_cell.verify_after_recovery_reboot(
                settings, state, result=result, transcript=mock.Mock(), ordinary={},
                owner_environment={}, controller_identity={"effective_gid": 1},
                boot={"boot_id_after": "b"}, safety_failures=safety,
                verification_failures=verification,
            )
        report = result["reboot_after_recovery"]
        self.assertIs(report["judged"], False)
        self.assertEqual(report["observed_status"], "failed")
        self.assertIn("authority changed", " ".join(report["failures"]))
        self.assertTrue(report["observed_safety_failures"])
        self.assertEqual((result["status"], result["safety_status"]), ("passed", "passed"))
        self.assertEqual((safety, verification), ([], []))

    def test_reboot_settings_gate_the_diagnostic_and_adoption_flags(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            base = plain_settings(root, reboot_before_owner_command=False,
                                  reboot_after_recovery=True)
            run_cell.validate_reboot_settings(replace(base, reboot_even_if_failed=True))
            with self.assertRaisesRegex(run_cell.ControllerError, "--reboot-even-if-failed"):
                run_cell.validate_reboot_settings(replace(
                    base, reboot_after_recovery=False, reboot_before_owner_command=True,
                    reboot_even_if_failed=True))
            manifest = json.loads(MANIFEST_TEXT)
            adoption = run_cell.CellSpec.from_manifest(manifest, run_cell.BIND_HANDOFF_CELL)
            for before, after in ((True, False), (False, True), (True, True)):
                with self.subTest(before=before, after=after):
                    run_cell.validate_reboot_settings(replace(
                        base, cell=adoption, bind_rollback_after_target_started=True,
                        reboot_before_owner_command=before, reboot_after_recovery=after))
                    self.assertTrue(run_cell.reboot_keeps_target_started_precursor(replace(
                        base, cell=adoption, bind_rollback_after_target_started=True)))
            for changed in (
                {"cell": adoption, "bind_rollback_after_target_started": True,
                 "owner_inverse_after_restart": False,
                 "stop_after_kill_for_independent_recovery": True},
                {"bind_rollback_after_target_started": True},
            ):
                with self.subTest(changed=changed), self.assertRaisesRegex(
                    run_cell.ControllerError, "handoff"
                ):
                    run_cell.validate_reboot_settings(replace(base, **changed))


class AfterRecoveryRebootOwnerFilesTest(unittest.TestCase):
    def owner_verify(self, files, expected, *, profile_cell=None):
        helper = RebootCheckpointTest()
        before = helper.authority({"tcp": ["bind"], "udp": ["bind"]})
        state = {"flow": "owner-inverse", "authority_before": before, "pid_reference": 600}
        manifest = json.loads(MANIFEST_TEXT)
        selected = profile_cell or run_cell.CellSpec.from_manifest(
            manifest, run_cell.BIND_HANDOFF_CELL)
        with mock.patch.object(run_cell, "owner_inverse_owner_files",
                               return_value=(files, expected, "owner BIND files changed")):
            return helper.verify(state, before, flow_cell=selected,
                                 source={"errors": [], "unknown": [], "named_main_pid": 700})

    def test_owner_files_are_judged_and_the_new_pid_recorded(self) -> None:
        result = self.owner_verify({"a": 1}, {"a": 1})
        report = result["reboot_after_recovery"]
        self.assertEqual(result["status"], "passed", report.get("failures"))
        self.assertTrue(report["owner_files_unchanged"])
        self.assertEqual(report["source_main_pid"], {
            "judged": False, "before_reboot": 600, "after_reboot": 700,
            "reason": "a reboot restarts the source service"})
        result = self.owner_verify({"a": 2}, {"a": 1})
        self.assertEqual(result["status"], "failed")
        self.assertIn("owner BIND files changed",
                      " ".join(result["reboot_after_recovery"]["failures"]))

    def test_after_boot_records_owner_files_unjudged(self) -> None:
        source = MODULE_PATH.read_text(encoding="utf-8")
        body = source[source.index("    def after_boot("):source.index("    def owner_steps(")]
        self.assertIn('step["owner_files"] = {', body)
        self.assertIn('"judged": False', body)


class StabilityWindowCountTest(unittest.TestCase):
    def test_slow_samples_still_give_31_samples(self) -> None:
        clock = {"now": 100.0}

        def monotonic() -> float:
            return clock["now"]

        def sleep(seconds: float) -> None:
            clock["now"] += seconds

        def slow_dns(*args, **kwargs):
            clock["now"] += 0.05  # each sample costs 50 ms
            return {"udp": {}, "tcp": {}}

        settings = mock.Mock(cell=owner_cell("target-staged"), stability_seconds=30,
                             stability_interval=1, dns_address="10.0.2.15", dns_port=53,
                             dns_name="www.s1-kill.test", dns_type="A", dns_timeout=1)
        with mock.patch.object(run_cell.time, "monotonic", side_effect=monotonic), \
                mock.patch.object(run_cell.time, "sleep", side_effect=sleep), \
                mock.patch.object(run_cell, "query_authoritative_dns", side_effect=slow_dns):
            report, failures, _ = run_cell.run_stability_window(
                settings, None, mock.Mock(), "", dns_only=True)
        self.assertEqual(len(report["samples"]), 31)
        self.assertEqual(report["sample_count"], 31)
        self.assertEqual(failures, [])
        self.assertEqual(run_cell.stability_samples_count(30, 1), 31)
        self.assertEqual(run_cell.stability_samples_count(1, 1), 2)
        with self.assertRaises(run_cell.ControllerError):
            run_cell.stability_samples_count(0, 1)


class RecoveryStatusReadTest(unittest.TestCase):
    def test_absent_launcher_is_recorded_not_failed(self) -> None:
        with tempfile.TemporaryDirectory() as root, mock.patch.object(
            run_cell, "OWNER_RECOVERY_EXECUTABLE", root + "/absent"
        ):
            report = run_cell.record_recovery_status(
                mock.Mock(request_id="1" * 32), {}, mock.Mock(), "before-recovery")
        self.assertIs(report["available"], False)
        self.assertIn("enroll-recovery-runtime", report["reason"])

    def test_status_read_is_judged_only_for_no_mutation(self) -> None:
        if not (hasattr(os, "geteuid") and os.geteuid() == 0):
            self.skipTest("the launcher proof requires a root-owned file")
        with tempfile.TemporaryDirectory() as root:
            launcher = Path(root, "recovery")
            launcher.write_text("#!/bin/sh\n", encoding="utf-8")
            launcher.chmod(0o755)
            settings = mock.Mock(request_id="1" * 32, state_dir=root,
                                 journal_path=root + "/j.json")
            snapshots = iter([{"ledger": {"sha256": "a"}}, {"ledger": {"sha256": "b"}}])
            status = {"ran": True, "returncode": 3, "output": "unknown\n",
                      "_raw_output": b"unknown\n"}
            runner = mock.Mock(return_value=status)
            with mock.patch.multiple(
                run_cell, OWNER_RECOVERY_EXECUTABLE=str(launcher),
                snapshot_private_evidence=mock.Mock(side_effect=lambda *a: next(snapshots)),
                run_owner_command=runner,
            ):
                report = run_cell.record_recovery_status(settings, {}, mock.Mock(), "after-recovery")
            self.assertTrue(report["mutated"])
            self.assertEqual(runner.call_args.args[1], [
                str(launcher), "dns-switch-status", "--quiesced", "--request-id", "1" * 32])
            self.assertEqual(runner.call_args.args[2], "recovery-status-after-recovery")
            self.assertNotIn("_raw_output", report["command"])
            result = {"status": "passed", "recovery_status_reads": {"after-recovery": report}}
            run_cell.judge_recovery_status_reads(result)
            self.assertEqual(result["status"], "failed")
            self.assertIn("changed private evidence", result["recovery_status_read_failures"][0])
            quiet = {"status": "passed", "recovery_status_reads": {
                "before-recovery": {"available": False},
                "after-recovery": dict(report, mutated=False, changed_evidence=[])}}
            run_cell.judge_recovery_status_reads(quiet)
            self.assertEqual(quiet["status"], "passed")
            launcher.chmod(0o777)
            with mock.patch.object(run_cell, "OWNER_RECOVERY_EXECUTABLE", str(launcher)):
                report = run_cell.record_recovery_status(settings, {}, mock.Mock(), "x")
            self.assertIs(report["available"], False)

    def test_rpc_retry_flow_reads_status_before_and_after_recovery(self) -> None:
        source = MODULE_PATH.read_text(encoding="utf-8")
        flow = source[source.index("def run_cell(settings:"):]
        before = flow.index('"before-recovery": record_recovery_status(')
        self.assertLess(flow.index("raise OwnerInverseFlowFinished()"), before)
        self.assertLess(before, flow.index('"agent-restart",'))
        after = flow.index('["after-recovery"] = (')
        self.assertLess(flow.index("assess_recovery_probes(recovery_probes[0]"), after)
        self.assertLess(after, flow.index('"panel-restart",'))
        self.assertLess(flow.index("judge_fixture_pass_definition("),
                        flow.index("judge_recovery_status_reads(result)"))
        self.assertLess(flow.index("judge_recovery_status_reads(result)"),
                        flow.index("finish_rpc_retry_flow("))
        tail = source[source.index("def finish_rpc_retry_flow("):source.index("def run_cell(settings:")]
        self.assertLess(tail.index('result["complete_verdict"] = pre_reboot_verdict('),
                        tail.index("maybe_request_reboot_after_recovery("))


class AdoptionRolledBackStartupCellTest(unittest.TestCase):
    def test_cell_is_in_the_startup_family_and_refused_under_the_owner_flag(self) -> None:
        manifest = json.loads(MANIFEST_TEXT)
        selected = run_cell.CellSpec.from_manifest(manifest, PDNS_ROLLED_BACK_CELL)
        self.assertIn(PDNS_ROLLED_BACK_CELL, run_cell.PDNS_ADOPTION_STARTUP_ROLLBACK_CELLS)
        self.assertNotIn(PDNS_ROLLED_BACK_CELL, run_cell.OWNER_INVERSE_CELLS)
        self.assertEqual(run_cell.PDNS_ADOPTION_STARTUP_ROLLBACK_CELLS,
                         guest_bootstrap.STARTUP_ROLLBACK_CELLS)
        with tempfile.TemporaryDirectory() as root:
            settings = plain_settings(root, selected, reboot_before_owner_command=False,
                                      reboot_dir=None)
            with mock.patch.object(run_cell, "validate_reboot_settings"), \
                    self.assertRaisesRegex(run_cell.ControllerError,
                                           "run it with --expect-agent-startup-rollback"):
                run_cell.validate_settings(settings)
        # The startup judgement the cell now gets: the Agent publishes its own
        # rollback verdict, no lease, journal retired, PowerDNS on its pre-cut
        # PID and no owner command named (observe_agent_startup_rollback).
        guest = FakeOwnerGuest(release_code=run_cell.AGENT_ROLLED_BACK_AFTER_RESTART,
                               target="pdns")
        identity = {"request_id": guest.REQUEST, "owner_id": guest.OWNER,
                    "manifest_qualifier": guest.QUALIFIER}
        ledger, job = guest.read_ledger("", guest.REQUEST)
        self.assertEqual(run_cell.classify_agent_startup_rollback(
            ledger, job, identity, target="pdns"), [])
        released = guest.job_for(run_cell.AGENT_RELEASED_NATIVE_UNKNOWN)
        self.assertTrue(run_cell.classify_agent_startup_rollback(
            ledger, released, identity, target="pdns"))

    def test_peer_catalog_flags_are_parsed_and_scoped(self) -> None:
        parser = run_cell.build_argument_parser()
        base = ["--manifest", "/m", "--cell-id", "c", "--request-id", "r", "--nonce", "n",
                "--tagged-agent-command", "[]", "--trigger-mode", "socket",
                "--recovery-command", "[]", "--agent-restart-command", "[]",
                "--panel-restart-command", "[]", "--recovery-probe-command", "[]",
                "--command-cwd", "/", "--state-dir", "/", "--mutation-lock", "/l",
                "--agent-socket", "/s", "--agent-token-file", "/t", "--journal", "/j",
                "--marker", "/k", "--proof", "/p", "--result", "/r", "--transcript", "/x",
                "--dns-address", "1", "--dns-port", "53", "--dns-name", "n",
                "--panel-address", "a", "--panel-port", "1", "--startup-timeout", "1",
                "--boundary-timeout", "1", "--stop-timeout", "1", "--kill-timeout", "1",
                "--command-timeout", "1", "--recovery-timeout", "1", "--endpoint-timeout", "1",
                "--dns-timeout", "1", "--stability-seconds", "1", "--stability-interval", "1"]
        self.assertIsNone(parser.parse_args(base).peer_catalog_format)
        self.assertEqual(parser.parse_args(base + ["--peer-catalog-format-bind"])
                         .peer_catalog_format, "bind")
        args = parser.parse_args(base + ["--peer-catalog-format-pdns-native",
                                         "--retry-switch-after-rollback",
                                         "--reboot-even-if-failed"])
        self.assertEqual(args.peer_catalog_format, "pdns-native")
        self.assertTrue(args.retry_switch_after_rollback and args.reboot_even_if_failed)
        with mock.patch("sys.stderr"), self.assertRaises(SystemExit):
            parser.parse_args(base + ["--peer-catalog-format-bind",
                                      "--peer-catalog-format-pdns-native"])


MANIFEST_TEXT = Path(__file__).with_name("manifest.json").read_text(encoding="utf-8")


if __name__ == "__main__":
    unittest.main()
