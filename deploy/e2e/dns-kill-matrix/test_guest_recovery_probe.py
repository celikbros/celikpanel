#!/usr/bin/env python3

from __future__ import annotations

import argparse
from contextlib import redirect_stdout
import hashlib
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import guest_recovery_probe as probe


CELL = "pdns-switch__committed__after-write__standalone__peer-reachable"
REQUEST = "1" * 32
OWNER = hashlib.sha256(
    ("celikpanel/dns-kill-matrix-owner/v1\x00" + REQUEST + "\x00" + CELL).encode()
).hexdigest()[:32]
QUALIFIER = "dns-engine-switch/v1:sha256:" + "3" * 64


def write_json(path: Path, value: object) -> None:
    path.write_text(json.dumps(value, separators=(",", ":")) + "\n", encoding="utf-8")
    path.chmod(0o600)


def write_state(path: Path, value: dict) -> None:
    path.write_bytes(probe.canonical_state_bytes(value))
    path.chmod(0o600)


class GuestRecoveryProbeTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name).resolve()
        self.scenario = self.root / "scenario.json"
        self.identity = self.root / "identity.json"
        self.ledger = self.root / "ledger.json"
        self.state = self.root / "state.json"
        self.journal = self.root / "journal.json"
        self.scenario_value = {
            "schema": probe.SCENARIO_SCHEMA,
            "driver": "pdns-switch",
            "source_fixture": "uninitialized",
            "mode": "switch",
            "source_engine": "",
            "target_engine": "pdns",
            "source_epoch": 0,
            "target_epoch": 1,
            "source_revision": 0,
            "topology": "standalone",
            "zones": [{
                "ordinal": 0, "domain": "s1-kill.test", "desired_generation": 1,
                "delete": False, "zone_type": "NATIVE", "records": [],
                "zone_qualifier": "",
            }],
        }
        self.identity_value = {
            "schema": probe.IDENTITY_SCHEMA, "cell_id": CELL,
            "driver": "pdns-switch", "source_fixture": "uninitialized",
            "request_id": REQUEST, "owner_id": OWNER,
            "manifest_qualifier": QUALIFIER,
        }
        self.state_value = {
            "schema": probe.STATE_SCHEMA, "mode": "switch", "engine": "pdns",
            "engine_epoch": 1, "source_revision": 0,
            "manifest_qualifier": QUALIFIER, "mutation_request_id": REQUEST,
            "mutation_owner_id": OWNER,
        }
        self.job = {
            "request_id": REQUEST, "owner_id": OWNER, "kind": "dns_engine_switch",
            "target": "pdns", "package_name": QUALIFIER, "status": "succeeded",
            "phase": "commit/dns-engine-switch/v2/finalized/" + REQUEST + "/" + QUALIFIER,
            "attempt": 1, "started_at": "2026-08-31T00:00:00Z",
            "updated_at": "2026-08-31T00:00:01Z",
            "lease_expires_at": "0001-01-01T00:00:00Z",
            "deadline_at": "2026-08-31T00:45:00Z",
            "finished_at": "2026-08-31T00:00:01Z",
        }
        write_json(self.scenario, self.scenario_value)
        write_json(self.identity, self.identity_value)
        write_state(self.state, self.state_value)
        write_state(self.root / "dns-engine-ownership-pdns.json", self.state_value)
        write_json(self.ledger, {"version": 1, "jobs": {REQUEST: self.job}})
        self.args = argparse.Namespace(
            cell_id=CELL, scenario=self.scenario, identity_receipt=self.identity,
            ledger=self.ledger, state=self.state, journal=self.journal,
        )

    def tearDown(self) -> None:
        self.temporary.cleanup()

    @staticmethod
    def units(unit: str) -> str:
        return "active" if unit == "pdns.service" else "inactive"

    def test_exact_converged_state_is_repeatable(self) -> None:
        first = probe.probe(self.args, self.units)
        second = probe.probe(self.args, self.units)
        self.assertTrue(first["converged"])
        self.assertEqual(first["recovery_outcome"], "target_converged")
        self.assertEqual(first["active_dns_engine"], "pdns")
        self.assertEqual(first["fingerprint"], second["fingerprint"])

    def test_timestamp_changes_do_not_change_fingerprint(self) -> None:
        first = probe.probe(self.args, self.units)
        self.job["updated_at"] = "2026-08-31T00:00:02Z"
        self.job["finished_at"] = "2026-08-31T00:00:02Z"
        write_json(self.ledger, {"version": 1, "jobs": {REQUEST: self.job}})
        second = probe.probe(self.args, self.units)
        self.assertTrue(second["converged"])
        self.assertEqual(first["fingerprint"], second["fingerprint"])

    def test_finalized_job_rejects_updated_finished_mismatch(self) -> None:
        self.job["finished_at"] = "2026-08-31T00:00:02Z"
        write_json(self.ledger, {"version": 1, "jobs": {REQUEST: self.job}})
        result = probe.probe(self.args, self.units)
        self.assertFalse(result["converged"])
        self.assertEqual(result["recovery_outcome"], "indeterminate")
        self.assertIn("invalid worker/lease/error/time state", result["detail"])

    def test_remaining_journal_is_stable_nonconvergence(self) -> None:
        write_json(self.journal, {
            "schema": "celikpanel-dns-engine-switch-journal/v1",
            "phase": "committed", "mode": "switch", "mutation_request_id": REQUEST,
            "mutation_owner_id": OWNER, "manifest_qualifier": QUALIFIER,
            "source_engine": "", "target_engine": "pdns", "source_epoch": 0,
            "target_epoch": 1, "source_revision": 0, "topology": "standalone",
        })
        first = probe.probe(self.args, self.units)
        second = probe.probe(self.args, self.units)
        self.assertFalse(first["converged"])
        self.assertEqual(first["fingerprint"], second["fingerprint"])

    def test_wrong_target_receipt_fails(self) -> None:
        self.state_value["engine_epoch"] = 2
        write_state(self.state, self.state_value)
        result = probe.probe(self.args, self.units)
        self.assertFalse(result["converged"])
        self.assertIn("state differs from target", result["detail"])

    def test_standalone_rejects_stale_pair_identity(self) -> None:
        self.state_value["pair_role"] = "secondary"
        self.state_value["pair_local_ip"] = "192.0.2.10"
        self.state_value["pair_peer_ip"] = "192.0.2.20"
        write_state(self.state, self.state_value)
        result = probe.probe(self.args, self.units)
        self.assertFalse(result["converged"])
        self.assertIn("retains paired identity", result["detail"])

    def test_bind_requires_canonical_generation(self) -> None:
        self.scenario_value["driver"] = "bind"
        self.scenario_value["source_fixture"] = "managed-pdns"
        self.scenario_value["source_engine"] = "pdns"
        self.scenario_value["source_epoch"] = 1
        self.scenario_value["target_engine"] = "bind"
        self.scenario_value["target_epoch"] = 2
        self.identity_value["driver"] = "bind"
        self.identity_value["source_fixture"] = "managed-pdns"
        self.state_value["engine"] = "bind"
        self.state_value["engine_epoch"] = 2
        write_json(self.scenario, self.scenario_value)
        write_json(self.identity, self.identity_value)
        write_state(self.state, self.state_value)
        write_state(self.root / "dns-engine-ownership-bind.json", self.state_value)
        result = probe.probe(
            self.args,
            lambda unit: "active" if unit == "bind9.service" else "inactive",
        )
        self.assertFalse(result["converged"])
        self.assertIn("canonical generation", result["detail"])

    def test_target_install_ownership_residue_is_a_stable_failure(self) -> None:
        install = {
            "schema": "celikpanel-dns-engine-install-ownership/v1",
            "engine": "pdns",
            "package_manager": "apt",
            "packages": ["pdns-backend-sqlite3", "pdns-server"],
            "missing_before": ["pdns-server"],
            "manifest_qualifier": QUALIFIER,
            "mutation_request_id": REQUEST,
            "mutation_owner_id": OWNER,
        }
        write_json(self.root / "dns-engine-install-ownership-pdns.json", install)
        first = probe.probe(self.args, self.units)
        second = probe.probe(self.args, self.units)
        self.assertFalse(first["converged"])
        self.assertEqual(first["recovery_outcome"], "indeterminate")
        self.assertIn("remains after successful finalization", first["detail"])
        self.assertEqual(first["fingerprint"], second["fingerprint"])

    def test_prior_source_ownership_is_bound_to_source_tuple(self) -> None:
        self.scenario_value.update({
            "driver": "bind",
            "source_fixture": "managed-pdns",
            "source_engine": "pdns",
            "source_epoch": 1,
            "target_engine": "bind",
            "target_epoch": 2,
        })
        self.identity_value.update({
            "driver": "bind",
            "source_fixture": "managed-pdns",
        })
        self.state_value.update({
            "engine": "bind",
            "engine_epoch": 2,
            "generation": "4" * 64,
        })
        prior_source = {
            "schema": probe.STATE_SCHEMA,
            "mode": "switch",
            "engine": "pdns",
            "engine_epoch": 99,
            "source_revision": 0,
            "manifest_qualifier": "dns-engine-switch/v1:sha256:" + "5" * 64,
            "mutation_request_id": "6" * 32,
            "mutation_owner_id": "7" * 32,
        }
        write_json(self.scenario, self.scenario_value)
        write_json(self.identity, self.identity_value)
        write_state(self.state, self.state_value)
        write_state(self.root / "dns-engine-ownership-bind.json", self.state_value)
        write_state(self.root / "dns-engine-ownership-pdns.json", prior_source)
        result = probe.probe(
            self.args,
            lambda unit: "active" if unit == "bind9.service" else "inactive",
        )
        self.assertFalse(result["converged"])
        self.assertEqual(result["active_dns_engine"], "bind")
        self.assertIn("differs from measured source", result["detail"])

    def test_exact_source_state_and_unit_activity_classifies_rollback(self) -> None:
        self.scenario_value.update({
            "driver": "bind",
            "source_fixture": "managed-pdns",
            "source_engine": "pdns",
            "source_epoch": 1,
            "target_engine": "bind",
            "target_epoch": 2,
        })
        self.identity_value.update({
            "driver": "bind",
            "source_fixture": "managed-pdns",
        })
        source_state = {
            "schema": probe.STATE_SCHEMA,
            "mode": "switch",
            "engine": "pdns",
            "engine_epoch": 1,
            "source_revision": 0,
            "manifest_qualifier": "dns-engine-switch/v1:sha256:" + "5" * 64,
            "mutation_request_id": "6" * 32,
            "mutation_owner_id": "7" * 32,
        }
        write_json(self.scenario, self.scenario_value)
        write_json(self.identity, self.identity_value)
        write_state(self.state, source_state)
        write_state(self.root / "dns-engine-ownership-pdns.json", source_state)
        self.job.update({
            "target": "bind", "status": "failed", "phase": "interrupted",
            "error_code": "dns_engine_switch_rolled_back_after_restart",
            "error_message": "The interrupted switch was rolled back.",
        })
        write_json(self.ledger, {"version": 1, "jobs": {REQUEST: self.job}})
        result = probe.probe(self.args, self.units)
        self.assertFalse(result["converged"])
        self.assertEqual(result["active_dns_engine"], "pdns")
        self.assertEqual(result["recovery_outcome"], "rolled_back_source_active", result["detail"])

        self.job["status"] = "running"
        write_json(self.ledger, {"version": 1, "jobs": {REQUEST: self.job}})
        self.assertEqual(probe.probe(self.args, self.units)["recovery_outcome"], "indeterminate")
        self.job["status"] = "failed"
        write_json(self.ledger, {"version": 1, "jobs": {REQUEST: self.job}})

        write_json(self.journal, {"phase": "rolled-back"})
        self.assertEqual(probe.probe(self.args, self.units)["recovery_outcome"], "indeterminate")
        self.journal.unlink()

        write_state(self.root / "dns-engine-ownership-bind.json", self.state_value)
        self.assertEqual(probe.probe(self.args, self.units)["recovery_outcome"], "indeterminate")
        (self.root / "dns-engine-ownership-bind.json").unlink()

        changed_source = dict(source_state, mutation_request_id="8" * 32)
        write_state(self.root / "dns-engine-ownership-pdns.json", changed_source)
        self.assertEqual(probe.probe(self.args, self.units)["recovery_outcome"], "indeterminate")

    def test_owner_bind_rollback_requires_absent_receipts_failed_ledger_and_active_units(self) -> None:
        self.scenario_value.update({
            "driver": "bind", "source_fixture": "owner-bind", "target_engine": "bind",
        })
        self.identity_value.update({"driver": "bind", "source_fixture": "owner-bind"})
        write_json(self.scenario, self.scenario_value)
        write_json(self.identity, self.identity_value)
        self.state.unlink()
        (self.root / "dns-engine-ownership-pdns.json").unlink()
        self.job.update({
            "target": "bind", "status": "failed", "phase": "interrupted",
            "error_code": "dns_engine_switch_rolled_back_after_restart",
            "error_message": "The interrupted switch was rolled back.",
        })
        write_json(self.ledger, {"version": 1, "jobs": {REQUEST: self.job}})
        units = lambda unit: "active" if unit in {"named.service", "bind9.service"} else "inactive"
        first = probe.probe(self.args, units)
        self.assertFalse(first["converged"])
        self.assertEqual(first["recovery_outcome"], "rolled_back_source_active", first["detail"])
        self.assertEqual(first["active_dns_engine"], "bind")
        self.assertEqual(first["fingerprint"], probe.probe(self.args, units)["fingerprint"])
        self.assertIn("must prove DNS serving", first["detail"])

        for label, change, restore in (
            ("state", lambda: write_state(self.state, self.state_value), lambda: self.state.unlink()),
            ("owner receipt", lambda: write_state(self.root / "dns-engine-ownership-bind.json", self.state_value),
             lambda: (self.root / "dns-engine-ownership-bind.json").unlink()),
            ("journal", lambda: write_json(self.journal, {"phase": "rolling-back"}), lambda: self.journal.unlink()),
        ):
            with self.subTest(label=label):
                change()
                self.assertEqual(probe.probe(self.args, units)["recovery_outcome"], "indeterminate")
                restore()
        self.job["status"] = "running"
        write_json(self.ledger, {"version": 1, "jobs": {REQUEST: self.job}})
        self.assertEqual(probe.probe(self.args, units)["recovery_outcome"], "indeterminate")
        self.job["status"] = "failed"
        write_json(self.ledger, {"version": 1, "jobs": {REQUEST: self.job}})
        self.assertEqual(probe.probe(self.args, lambda unit: "inactive")["recovery_outcome"], "indeterminate")

    def test_unexpected_error_still_emits_the_exact_probe_shape(self) -> None:
        argv = [
            "guest-recovery-probe",
            "--cell-id", CELL,
            "--scenario", str(self.scenario),
            "--identity-receipt", str(self.identity),
            "--ledger", str(self.ledger),
            "--state", str(self.state),
            "--journal", str(self.journal),
        ]
        output = io.StringIO()
        with mock.patch.object(probe, "probe", side_effect=RuntimeError("boom")), \
             mock.patch("sys.argv", argv), redirect_stdout(output):
            self.assertEqual(probe.main(), 0)
        value = json.loads(output.getvalue())
        self.assertEqual(set(value), {
            "schema", "converged", "recovery_outcome", "active_dns_engine",
            "fingerprint", "detail",
        })
        self.assertFalse(value["converged"])
        self.assertEqual(value["recovery_outcome"], "indeterminate")
        self.assertEqual(value["active_dns_engine"], "")


if __name__ == "__main__":
    unittest.main()

class DNSDocumentCompatibilityTest(unittest.TestCase):
    def test_current_go_producer_goldens(self):
        root=Path(__file__).resolve().parents[3]/"internal/dnsengineartifact/testdata"
        for name in ("bind-zone-add", "pdns-adopted"):
            old=json.loads((root/f"alpha81-{name}.json").read_bytes())
            for role in ("state", "ownership"):
                raw=(root/f"v2-{name}-{role}.json").read_bytes()
                value=json.loads(raw)
                self.assertEqual(probe.decode_dns_document(value,raw,role),old)
                with self.assertRaises(probe.ProbeObservationError):
                    probe.decode_dns_document(value,raw,"ownership" if role=="state" else "state")
                for bad in (raw+b" ",raw.replace(b'"acquisition_sha256":"',b'"acquisition_sha256":"0',1),
                            raw.replace(b'"acquisition":',b'"extra":0,"acquisition":',1),
                            raw.replace(b'"schema":',b'"schema":"ignored","schema":',1)):
                    with self.assertRaises(probe.ProbeObservationError):
                        probe.decode_dns_document(json.loads(bad),bad,role)
