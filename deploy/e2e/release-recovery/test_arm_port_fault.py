import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("arm_port_fault_tested", Path(__file__).with_name("arm_port_fault.py"))
subject = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = subject
SPEC.loader.exec_module(subject)

class ArmTests(unittest.TestCase):
    def setUp(self):
        self.identity = {"nonce": "a" * 64, "vm_uuid": "fixture", "cell_id": "fixture", "node": "arch"}
        self.operation = "b" * 32
    def event(self, kind="armed"):
        return {"schema": subject.EVENT_SCHEMA, "event": kind, "identity": self.identity, "operation_id": self.operation, "at": "2026-09-14T08:00:00Z"}
    def raw(self, events):
        return b"".join((json.dumps(e) + "\n").encode() for e in events)
    def test_event_identity_and_order(self):
        raw = self.raw([self.event(), self.event("port_held"), self.event("candidate_installed_with_port_conflict"), self.event("released")])
        self.assertEqual(len(subject.validate_events(raw, self.identity, self.operation)), 4)
        with self.assertRaises(ValueError):
            subject.validate_events(raw, self.identity, "c" * 32)
    def test_unknown_or_duplicate_or_missing_armed_refused(self):
        for events in ([self.event("port_held")], [self.event(), self.event()], [self.event("made-up")]):
            with self.subTest(events=events), self.assertRaises(ValueError):
                subject.validate_events(self.raw(events), self.identity, self.operation)
    def test_partial_stream_refused(self):
        with self.assertRaises(ValueError):
            subject.validate_events(self.raw([self.event()])[:-1], self.identity, self.operation)
    def test_collector_generated_python_parses_and_is_fixed(self):
        script = subject.guest_collection_script(self.operation)
        program = script.split("\n", 1)[1].rsplit("CELIKPANEL_FAULT_COLLECT", 1)[0]
        compile(program, "collector", "exec")
        self.assertNotIn("self-update-worker", script)
        with self.assertRaises(ValueError):
            subject.guest_collection_script("../evil")
    def test_start_attempt_refused(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            target = root / "evidence/arch"
            target.mkdir(parents=True)
            subject.assert_not_started(root, "arch")
            (target / "update-start-attempt.json").write_text("{}")
            with self.assertRaises(ValueError):
                subject.assert_not_started(root, "arch")
    def test_execute_required_before_host_access(self):
        with mock.patch.object(subject.lab, "checked_root") as checked:
            with self.assertRaises(SystemExit):
                subject.main(["--work-root", "/var/tmp/cp-release-drill-test", "--mode", "arm"])
            checked.assert_not_called()
    def test_duplicate_manifest_fields_refused(self):
        with self.assertRaises(ValueError):
            subject.manifest_fields(b"sequence=80\nsequence=79\n")
    def test_local_manifest_mismatch_refused(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "manifest"
            path.write_bytes(b"sequence=80\n")
            with self.assertRaises(ValueError):
                subject.candidate_artifacts({"assets": {"manifest": {"sha256": "0" * 64}}}, path)

if __name__ == "__main__":
    unittest.main()
