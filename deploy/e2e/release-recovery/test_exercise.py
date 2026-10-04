#!/usr/bin/env python3
"""Offline evidence and single-attempt tests; every guest operation is mocked.

Çevrimdışı kanıt ve tek deneme testleri; bütün konuk işlemleri taklittir.
"""
import contextlib
import copy
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("release_recovery_exercise", Path(__file__).with_name("exercise.py"))
exercise = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(exercise)
NONCE = "a" * 64
UUID = "f9c6016c-e418-4fdb-9b7c-7ce7c7a69ed8"
NODE = "debian13"
RECORD = {"nonce": NONCE, "cell_id": "release-recovery__" + NONCE[:16]}
PLAN = {"nodes": {NODE: {"qemu_command": ["qemu-system-x86_64", "-uuid", UUID]}}}
IDENTITY = dict(RECORD, vm_uuid=UUID, node=NODE)


def terminal(**changes):
    value = {"schema": "celikpanel-release-recovery-seed/v1", "event": "seed_complete",
             "cell_id": RECORD["cell_id"], "node": NODE, "zone": exercise.ZONE,
             "generation": "b" * 64, "catalog_serial": 2,
             "ownership_sha256": "c" * 64, "ownership_unchanged": True}
    value.update(changes)
    return value


def completed(text, stderr=""):
    return subprocess.CompletedProcess([], 0, text, stderr)


class ExerciseTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.evidence = self.root / "evidence" / NODE
        self.stdout = io.StringIO()
        output = contextlib.redirect_stdout(self.stdout)
        output.__enter__()
        self.addCleanup(output.__exit__, None, None, None)
        self.put = patch.object(exercise.lab, "put_file", return_value=("/root/celikpanel-release-recovery-lab/seed", "d" * 64)).start()
        self.dispatch = patch.object(exercise.lab, "guarded_script").start()
        self.addCleanup(patch.stopall)

    def seed(self):
        return exercise.seed(self.root, RECORD, PLAN, NODE, self.root / "seed-binary")

    def capture(self, label="observation", operation_id=None):
        return exercise.capture(self.root, RECORD, PLAN, NODE, label, "2026-09-14T00:00:00Z", operation_id)

    def assert_retry_refused(self):
        self.put.reset_mock()
        self.dispatch.reset_mock()
        with self.assertRaisesRegex(ValueError, "already attempted"):
            self.seed()
        self.put.assert_not_called()
        self.dispatch.assert_not_called()

    def test_seed_intent_exists_before_the_only_mutating_dispatch(self):
        def run(*args, **kwargs):
            intent = json.loads((self.evidence / "seed-intent.json").read_text())
            self.assertEqual(intent["binary_sha256"], "d" * 64)
            self.assertEqual(intent["zone"], exercise.ZONE)
            self.assertEqual(kwargs["timeout"], 930)
            return completed(json.dumps(terminal()) + "\n")
        self.dispatch.side_effect = run
        self.seed()
        self.dispatch.assert_called_once()
        self.assertEqual(json.loads(self.stdout.getvalue())["action"], "seeded")
        self.assert_retry_refused()

    def test_existing_intent_prevents_upload_and_second_mutation(self):
        exercise.save(self.root, NODE, "seed-intent.json", b'{"status":"unknown"}')
        self.assert_retry_refused()

    def test_timeout_preserves_byte_output_and_unknown_outcome_without_retry(self):
        self.dispatch.side_effect = subprocess.TimeoutExpired("mocked seed", 930,
            output=b'{"event":"lease_started"}\n', stderr=b"partial diagnostic\n")
        with self.assertRaisesRegex(ValueError, "result unknown"):
            self.seed()
        self.assertEqual((self.evidence / "seed.stdout.jsonl").read_bytes(), b'{"event":"lease_started"}\n')
        self.assertEqual((self.evidence / "seed.stderr.txt").read_bytes(), b"partial diagnostic\n")
        self.assertEqual(json.loads((self.evidence / "seed-outcome.json").read_text()),
            {"status": "unknown", "error_type": "TimeoutExpired"})
        self.assertNotIn('"action": "seeded"', self.stdout.getvalue())
        self.assert_retry_refused()

    def test_failed_process_preserves_partial_evidence_without_retry(self):
        self.dispatch.side_effect = subprocess.CalledProcessError(1, "mocked seed",
            output='{"event":"lease_started"}\n', stderr="reconciliation required\n")
        with self.assertRaises(ValueError):
            self.seed()
        self.assertEqual((self.evidence / "seed.stdout.jsonl").read_text(), '{"event":"lease_started"}\n')
        self.assertEqual((self.evidence / "seed.stderr.txt").read_text(), "reconciliation required\n")
        self.assert_retry_refused()

    def test_incomplete_or_malformed_seed_output_is_retained_and_never_seeded(self):
        for index, raw in enumerate(("", "not json\n", '{"event":"publication_complete"}\n', "[]\n")):
            with self.subTest(raw=raw):
                self.root = Path(self.directory.name) / str(index)
                self.evidence = self.root / "evidence" / NODE
                self.dispatch.side_effect = None
                self.dispatch.return_value = completed(raw)
                with self.assertRaises(ValueError):
                    self.seed()
                self.assertEqual((self.evidence / "seed.stdout.jsonl").read_bytes(), raw.encode())
                self.assert_retry_refused()
        self.assertNotIn('"action": "seeded"', self.stdout.getvalue())

    def test_foreign_terminal_or_invalid_publication_proof_cannot_report_seeded(self):
        invalid = ({"schema": "different/v1"}, {"cell_id": "another-cell"}, {"node": "arch"},
                   {"zone": "unrelated.test"}, {"generation": "b" * 63}, {"ownership_sha256": None},
                   {"catalog_serial": True}, {"catalog_serial": -1}, {"ownership_unchanged": 1},
                   {"ownership_unchanged": False})
        for index, changes in enumerate(invalid):
            with self.subTest(changes=changes):
                self.root = Path(self.directory.name) / str(index)
                self.evidence = self.root / "evidence" / NODE
                raw = json.dumps(terminal(**changes)) + "\n"
                self.dispatch.return_value = completed(raw)
                with self.assertRaises(ValueError):
                    self.seed()
                self.assertEqual((self.evidence / "seed.stdout.jsonl").read_text(), raw)
        self.assertNotIn('"action": "seeded"', self.stdout.getvalue())

    def test_later_nonterminal_event_invalidates_an_earlier_complete_event(self):
        self.dispatch.return_value = completed(json.dumps(terminal()) + '\n{"event":"unexpected_continuation"}\n')
        with self.assertRaises(ValueError):
            self.seed()
        self.assert_retry_refused()

    def test_exact_observation_identity_is_required_but_unknown_facts_are_preserved(self):
        observation = {"schema": "celikpanel/release-recovery-observation/v1", "status": "observed",
                       "identity": IDENTITY, "database": {"status": "unknown", "reason": "active WAL"}}
        raw = json.dumps(observation)
        self.dispatch.return_value = completed(raw)
        result = self.capture(operation_id="e" * 32)
        self.assertEqual(result, observation)
        self.assertEqual((self.evidence / "observation.json").read_text(), raw)
        self.assertIn("--operation-id " + "e" * 32, self.dispatch.call_args.args[4])
        event = json.loads(self.stdout.getvalue())
        self.assertEqual(event["action"], "observed")
        self.assertEqual(event["evidence"]["sha256"], hashlib.sha256(raw.encode()).hexdigest())
        self.assertNotIn("PASS", self.stdout.getvalue())

    def test_invalid_observation_identity_or_status_retains_raw_evidence(self):
        observation = {"schema": "celikpanel/release-recovery-observation/v1", "status": "observed", "identity": IDENTITY}
        cases = []
        for key, value in (("nonce", "b" * 64), ("cell_id", "foreign"), ("node", "arch"), ("vm_uuid", "other")):
            changed = copy.deepcopy(observation)
            changed["identity"][key] = value
            cases.append(changed)
        cases.extend((dict(observation, status="refused"), dict(observation, schema="other/v1"), []))
        for index, value in enumerate(cases):
            with self.subTest(value=value):
                raw = json.dumps(value)
                self.dispatch.return_value = completed(raw)
                label = "invalid-" + str(index)
                with self.assertRaises(ValueError):
                    self.capture(label)
                self.assertEqual((self.evidence / (label + ".json")).read_text(), raw)
        self.assertNotIn('"action": "observed"', self.stdout.getvalue())

    def test_malformed_observation_is_saved_before_json_parsing(self):
        self.dispatch.return_value = completed("partial-json{")
        with self.assertRaises(ValueError):
            self.capture()
        self.assertEqual((self.evidence / "observation.json").read_bytes(), b"partial-json{")

    def test_evidence_is_exclusive_and_never_overwritten(self):
        exercise.save(self.root, NODE, "retained.json", b"original")
        with self.assertRaises(FileExistsError):
            exercise.save(self.root, NODE, "retained.json", b"replacement")
        self.assertEqual((self.evidence / "retained.json").read_bytes(), b"original")

    def test_seed_dry_run_never_uploads_or_dispatches(self):
        argv = ["exercise.py", "seed", "--work-root", "/fixture", "--node", NODE, "--binary", "/fixture/seed"]
        with patch.object(sys, "argv", argv), patch.object(exercise.lab, "checked_root", return_value=self.root), patch.object(exercise.lab, "load", return_value=(RECORD, PLAN)):
            exercise.main()
        self.put.assert_not_called()
        self.dispatch.assert_not_called()
        self.assertFalse(self.evidence.exists())
        self.assertEqual(json.loads(self.stdout.getvalue())["execute"], False)


if __name__ == "__main__":
    unittest.main()
