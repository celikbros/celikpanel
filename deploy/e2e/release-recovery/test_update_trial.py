"""Pure fixture admission checks; no VM or installed Agent is contacted."""
import contextlib
import copy
import io
import stat
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("trial_under_test", Path(__file__).with_name("update_trial.py"))
trial = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = trial
SPEC.loader.exec_module(trial)


class TrialAdmissionTests(unittest.TestCase):
    def test_real_recorded_producer_shape_requires_generation_advancement(self):
        record = {"cell_id": "fixture"}
        events = []
        for number, operation in enumerate(("switch", "publication", "publication"), 1):
            for phase in ("start", "complete"):
                events.append({"schema": "celikpanel-release-recovery-seed/v1",
                               "cell_id": "fixture", "node": "debian13", "zone": trial.exercise.ZONE,
                               "event": operation + "_" + phase, "request_id": str(number) * 32,
                               "owner_id": "a" * 32, "generation": str(number) * 64,
                               "ownership_sha256": "b" * 64, "ownership_unchanged": True,
                               "catalog_serial": 0})
        events.append({**events[-1], "event": "seed_complete"})
        encode = lambda value: b"\n".join(json.dumps(item).encode() for item in value)
        self.assertEqual(trial.validate_seed(encode(events), record, "debian13")["generation"], "3" * 64)
        for changed in ("node", "ownership_sha256", "generation"):
            bad = copy.deepcopy(events)
            bad[3][changed] = {"node": "arch", "ownership_sha256": "c" * 64, "generation": "1" * 64}[changed]
            with self.subTest(changed=changed), self.assertRaises(ValueError):
                trial.validate_seed(encode(bad), record, "debian13")
        with self.assertRaises(ValueError):
            trial.validate_seed(encode(events[:-1]), record, "debian13")

    def test_prepared_start_is_single_attempt_even_after_transport_ambiguity(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            evidence = root / "evidence" / "debian13"
            evidence.mkdir(parents=True)
            (evidence / "update-start-attempt.json").write_text("{}")
            argv = ["trial", "--work-root", str(root), "--node", "debian13", "--sequence", "79", "--mode", "start", "--execute"]
            with mock.patch.object(sys, "argv", argv), mock.patch.object(trial.lab, "checked_root", return_value=root), \
                    mock.patch.object(trial.lab, "load", return_value=({}, {"nodes": {"debian13": {}}})), \
                    mock.patch.object(trial.lab, "process_guard"), mock.patch.object(trial, "driver") as driver:
                with self.assertRaisesRegex(ValueError, "already attempted"):
                    trial.main()
                driver.assert_not_called()

    def test_prepare_returns_before_any_start_attempt(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            argv = ["trial", "--work-root", str(root), "--node", "arch", "--sequence", "80", "--mode", "prepare", "--execute"]
            with mock.patch.object(sys, "argv", argv), mock.patch.object(trial.lab, "checked_root", return_value=root), \
                    mock.patch.object(trial.lab, "load", return_value=({}, {"nodes": {"arch": {}}})), \
                    mock.patch.object(trial.lab, "process_guard"), \
                    mock.patch.object(trial, "prepare", return_value={"request_id": "a" * 32}), \
                    mock.patch.object(trial, "save") as save, mock.patch.object(trial, "driver") as driver:
                trial.main()
                save.assert_not_called()
                driver.assert_not_called()

    @unittest.skipUnless(sys.platform.startswith("linux"), "native directory fsync requires Linux")
    def test_native_journal_is_private_while_stdout_has_only_metadata(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            intent = {"request_id": "a" * 32, "started_at": "2026-09-14T00:00:00Z"}
            unit = "celikpanel-self-update-" + intent["request_id"] + ".service"
            native = {unit: {"Id": unit, "ActiveState": "failed"},
                      "active_transaction": {"operation": "rollback", "snapshot": "retained-snapshot"},
                      "journal": {"exit_code": 0, "truncated": False,
                                  "text": "fixture-secret-sentinel=do-not-publish\n"}}
            raw = json.dumps(native)
            stdout = io.StringIO()
            with mock.patch.object(trial.lab, "guarded_script", return_value=mock.Mock(stdout=raw)), contextlib.redirect_stdout(stdout):
                result = trial.native(root, {}, {}, "arch", intent, "native-test")
            self.assertEqual(result, native)
            self.assertNotIn("fixture-secret-sentinel", stdout.getvalue())
            published = json.loads(stdout.getvalue())
            self.assertEqual(published["observation"]["journal"],
                             {"exit_code": 0, "truncated": False, "bytes": len(native["journal"]["text"].encode())})
            self.assertEqual(published["observation"][unit], native[unit])
            private = Path(published["evidence"]["path"])
            self.assertEqual(private.read_text(), raw)
            self.assertEqual(stat.S_IMODE(private.stat().st_mode), 0o600)

    @unittest.skipUnless(sys.platform.startswith("linux"), "native directory fsync requires Linux")
    def test_driver_details_stay_private_on_success_and_unknown_timeout(self):
        for timed_out in (False, True):
            with self.subTest(timed_out=timed_out), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                intent = {"request_id": "b" * 32, "assets": {
                    name: {"guest_path": "/private/" + name} for name in ("driver", "manifest", "signature")}}
                raw = '{"event":"status","detail":"fixture-secret-sentinel"}\n'
                error = "private-stderr-sentinel"
                result = mock.Mock(stdout=raw, stderr=error, returncode=0)
                side_effect = trial.subprocess.TimeoutExpired("mocked", 180, output=raw.encode(), stderr=error.encode()) if timed_out else None
                stdout = io.StringIO()
                with mock.patch.object(trial.lab, "guarded_script", return_value=result, side_effect=side_effect), contextlib.redirect_stdout(stdout):
                    confirmed = trial.driver(root, {"nonce": "c" * 64}, {}, "arch", intent, "status", "driver-test")
                self.assertEqual(confirmed, not timed_out)
                self.assertNotIn("fixture-secret-sentinel", stdout.getvalue())
                self.assertNotIn("private-stderr-sentinel", stdout.getvalue())
                published = json.loads(stdout.getvalue())
                self.assertEqual(published["request_id"], intent["request_id"])
                self.assertEqual(published["error"], "transport-timeout-outcome-unknown" if timed_out else None)
                for key, expected in (("stdout", raw), ("stderr", error)):
                    private = Path(published["evidence"][key]["path"])
                    self.assertEqual(private.read_text(), expected)
                    self.assertEqual(stat.S_IMODE(private.stat().st_mode), 0o600)

    def test_baseline_requires_running_same_bytes_and_live_panel(self):
        baseline = {"schema": "celikpanel/release-baseline-install-result/v1", "version": "v0.1.0-alpha.75",
                    "exit_code": 0, "error_type": None, "https_curl_exit": 0, "https_http_code": "200",
                    "installed_artifacts": {"agent": "a" * 64, "panel": "b" * 64},
                    "running_artifacts": {"agent": "a" * 64, "panel": "b" * 64},
                    "services": {name: {"ActiveState": "active"} for name in ("agent", "panel")}}
        trial.validate_baseline(baseline)
        baseline["running_artifacts"]["agent"] = "c" * 64
        with self.assertRaises(ValueError):
            trial.validate_baseline(baseline)


if __name__ == "__main__":
    unittest.main()
