#!/usr/bin/env python3
"""Portable acceptance tests; synthetic records here are never native VM proof."""
import copy
import importlib.util
import json
from pathlib import Path
import unittest

SPEC = importlib.util.spec_from_file_location("recovery_evidence", Path(__file__).with_name("evidence.py"))
evidence = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(evidence)


def example_record():
    """Complete input schema example, made only for classifier unit tests."""
    old = {"agent": "1" * 64, "panel": "2" * 64, "web": "3" * 64}
    new = {"agent": "4" * 64, "panel": "5" * 64, "web": "6" * 64}
    operation, snapshot = "a" * 32, "20260914T120000Z-from-old-to-candidate"
    return {
        "schema": evidence.SCHEMA, "run_id": "unit-fixture-not-native-proof",
        "baseline": {
            "origin": "released-installation", "release": "v0.1.0-alpha.75",
            "commit": "a" * 40, "manifest_sha256": "a" * 64, "artifacts": old,
            "protected_sentinels": {"fixture-domain": "7" * 64},
            "refs": ["baseline-install.json", "baseline-observation.json"],
        },
        "candidate": {
            "release": "v0.1.0-alpha.80", "commit": "b" * 40,
            "manifest_sha256": "b" * 64, "artifacts": new,
            "refs": ["candidate-signature-verification.json"],
        },
        "update": {
            "operation_id": operation, "snapshot_id": snapshot,
            "entrypoint": "installed-agent-self-update-worker",
            "started_operation_ids": [operation], "snapshot_complete": True,
            "refs": ["update-journal.json", "snapshot-before-fault.json"],
        },
        "fault": {
            "operation_id": operation, "kind": "sigkill", "boundary": "candidate-installed",
            "observed": True, "installed_artifacts": copy.deepcopy(new),
            "refs": ["fault-observation.json"],
        },
        "recovery": {
            "operation_id": operation, "snapshot_id": snapshot, "automatic": True,
            "entrypoint": "retained-release-rollback", "rollback_script_sha256": "8" * 64,
            "restore_started": True, "restore_completed": True, "exit_code": 0,
            "terminal_outcome": "rolled-back", "failure_code": None,
            "refs": ["native-recovery-journal.json", "restoration-transition.json"],
        },
        "after": {
            "artifacts": copy.deepcopy(old),
            "running_artifacts": {k: old[k] for k in ("agent", "panel")},
            "protected_sentinels": {"fixture-domain": "7" * 64},
            "database": {"integrity": "ok", "semantic_checks": [
                {"name": "fixture-domain-preserved", "result": "PASS", "refs": ["database-read.json"]},
            ]},
            "services": {"agent": "active", "panel": "active"},
            "transaction_markers": "clear", "https": "PASS", "refs": ["after-observation.json"],
        },
        "workloads": {
            "required": ["dns-a", "sample-web"],
            "observations": [
                {"name": "dns-a", "before": "PASS", "after": "PASS", "refs": ["dns.json"]},
                {"name": "sample-web", "before": "PASS", "after": "PASS", "refs": ["web.json"]},
            ],
            "refs": ["native-workload-probes.json"],
        },
    }


class EvidenceTests(unittest.TestCase):
    def test_complete_facts_pass(self):
        self.assertEqual(evidence.classify(example_record())["status"], "PASS")

    def test_each_missing_section_is_inconclusive(self):
        for section in evidence.SECTIONS:
            with self.subTest(section=section):
                value = example_record()
                del value[section]
                self.assertEqual(evidence.classify(value)["status"], "INCONCLUSIVE")

    def test_actual_released_baseline_and_transition_are_required(self):
        value = example_record()
        value["baseline"]["origin"] = "manually-created-receipts"
        self.assertEqual(evidence.classify(value)["status"], "INCONCLUSIVE")
        value = example_record()
        value["candidate"]["artifacts"] = copy.deepcopy(value["baseline"]["artifacts"])
        value["fault"]["installed_artifacts"] = copy.deepcopy(value["baseline"]["artifacts"])
        self.assertEqual(evidence.classify(value)["status"], "INCONCLUSIVE")

    def test_known_failure_survives_missing_evidence(self):
        value = example_record()
        value["recovery"]["exit_code"] = 1
        del value["after"]
        result = evidence.classify(value)
        self.assertEqual(result["status"], "FAIL")
        self.assertTrue(result["missing_evidence"])

    def test_unknown_is_not_verified_failure(self):
        value = example_record()
        value["after"]["services"]["panel"] = "unknown"
        value["after"]["database"]["integrity"] = None
        self.assertEqual(evidence.classify(value)["status"], "INCONCLUSIVE")

    def test_old_hashes_do_not_replace_actual_restore_proof(self):
        value = example_record()
        del value["recovery"]["restore_completed"]
        self.assertEqual(evidence.classify(value)["status"], "INCONCLUSIVE")
        value["recovery"]["restore_completed"] = False
        self.assertEqual(evidence.classify(value)["status"], "FAIL")

    def test_fault_boundary_and_candidate_identity_are_required(self):
        value = example_record()
        value["fault"]["observed"] = False
        self.assertEqual(evidence.classify(value)["status"], "INCONCLUSIVE")
        value["fault"]["observed"] = True
        value["fault"]["installed_artifacts"]["agent"] = "9" * 64
        self.assertEqual(evidence.classify(value)["status"], "FAIL")

    def test_wrong_restored_running_and_protected_hashes_fail(self):
        for group, key in (("artifacts", "web"), ("running_artifacts", "agent"),
                           ("protected_sentinels", "fixture-domain")):
            value = example_record()
            value["after"][group][key] = "9" * 64
            self.assertEqual(evidence.classify(value)["status"], "FAIL")

    def test_duplicate_unrelated_wrong_snapshot_and_manual_recovery_fail(self):
        changes = [
            lambda x: x["update"]["started_operation_ids"].append("a" * 32),
            lambda x: x["update"].update(started_operation_ids=["b" * 32]),
            lambda x: x["recovery"].update(operation_id="b" * 32),
            lambda x: x["recovery"].update(snapshot_id="another-snapshot"),
            lambda x: x["recovery"].update(automatic=False),
        ]
        for change in changes:
            value = example_record()
            change(value)
            self.assertEqual(evidence.classify(value)["status"], "FAIL")

    def test_workload_and_semantic_database_checks_are_required(self):
        value = example_record()
        value["workloads"]["observations"].pop()
        self.assertEqual(evidence.classify(value)["status"], "INCONCLUSIVE")
        value = example_record()
        value["after"]["database"]["semantic_checks"][0]["result"] = "FAIL"
        self.assertEqual(evidence.classify(value)["status"], "FAIL")

    def test_strict_schema_types_and_refs(self):
        changes = [
            lambda x: x.update(schema="future/v2"),
            lambda x: x.update(claimed_success=True),
            lambda x: x["fault"].update(kind=[]),
            lambda x: x["update"].update(entrypoint={}),
            lambda x: x["after"].update(refs=[]),
            lambda x: x["recovery"].update(exit_code=False),
        ]
        for change in changes:
            value = example_record()
            change(value)
            self.assertEqual(evidence.classify(value)["status"], "INCONCLUSIVE")

    def test_regression_reproduction_is_separate_and_needs_fault_proof(self):
        value = example_record()
        value["recovery"].update(exit_code=1, terminal_outcome="recovery-required",
                                 restore_started=False, restore_completed=False,
                                 failure_code="rollback-inherited-lock-refused")
        result = evidence.classify(value, expected_regression="rollback-inherited-lock-refused")
        self.assertEqual(result["status"], "FAIL")
        self.assertEqual(result["regression"]["status"], "REPRODUCED")
        value["fault"]["observed"] = None
        result = evidence.classify(value, expected_regression="rollback-inherited-lock-refused")
        self.assertEqual(result["regression"]["status"], "INCONCLUSIVE")

    def test_success_does_not_reproduce_expected_failure(self):
        result = evidence.classify(example_record(), expected_regression="old-bug")
        self.assertEqual(result["status"], "PASS")
        self.assertEqual(result["regression"]["status"], "NOT_REPRODUCED")

    def test_ambiguous_oversized_or_non_object_json_is_rejected(self):
        for raw in (b'{"schema":"a","schema":"b"}', b'{"number":NaN}', b'[]',
                    b" " * (evidence.MAX_BYTES + 1)):
            with self.assertRaises(evidence.EvidenceError):
                evidence.decode(raw)
        self.assertEqual(evidence.decode(json.dumps(example_record()).encode())["schema"], evidence.SCHEMA)


if __name__ == "__main__":
    unittest.main()
