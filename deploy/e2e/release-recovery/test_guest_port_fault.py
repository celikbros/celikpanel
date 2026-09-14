#!/usr/bin/env python3
"""Offline fault-state tests; no product service or port 2083 is touched."""
import argparse
import hashlib
import importlib.util
import os
from pathlib import Path
import tempfile
import types
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("guest_port_fault", Path(__file__).with_name("guest_port_fault.py"))
fault = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(fault)
SNAP = "20260914T080000Z-from-unknown-to-" + "a" * 40 + "-" + "b" * 32
ARGS = argparse.Namespace(operation_id="c" * 32, candidate_agent="d" * 64, candidate_panel="e" * 64)


def state(update="active", pid="0", recovery="inactive", operation="update"):
    return {"update": {"ActiveState": update}, "panel": {"MainPID": pid},
            "recovery": {"ActiveState": recovery},
            "transaction": {"operation": operation, "snapshot": SNAP, "phase": "active"}}


class Listener:
    def __init__(self):
        self.closed = False
    def close(self):
        self.closed = True


class FaultLoopTests(unittest.TestCase):
    def run_states(self, states, prover=None, interrupted=lambda: False):
        events = []
        listener = Listener()
        values = iter(states)
        clock = [0.0]
        def pause(seconds):
            clock[0] += seconds
        result = fault.run_fault(ARGS, lambda event, **fields: events.append(dict(event=event, **fields)),
                    observer=lambda args: next(values), binder=lambda: listener,
                    prover=prover or (lambda *a: {"snapshot": SNAP, "manifest_sha256": "f"*64, "verified_files": 2}),
                    clock=lambda: clock[0], pause=pause, interrupted=interrupted)
        return result, events, listener

    def test_exact_update_and_stopped_panel_precede_port_fault(self):
        result, events, listener = self.run_states([state(update="inactive", pid="42"),
            state(pid="42"), state(), state(update="failed")])
        self.assertEqual(result, 0)
        self.assertEqual([x["event"] for x in events],
                         ["armed", "port_held", "candidate_installed_with_port_conflict", "released"])
        self.assertTrue(listener.closed)
        self.assertEqual(events[-1]["reason"], "update-unit-exited")

    def test_recovery_service_and_rollback_transition_release_immediately(self):
        for last in (state(recovery="activating"), state(operation="rollback")):
            result, events, listener = self.run_states([state(), last])
            self.assertEqual(result, 0)
            self.assertTrue(listener.closed)
            self.assertIn(events[-1]["reason"], ("recovery-unit-started", "transaction-entered-rollback"))

    def test_early_failure_is_unknown_not_fault_proof(self):
        result, events, listener = self.run_states([state(pid="42"), state(update="failed")])
        self.assertEqual(result, 2)
        self.assertFalse(events[-1]["checkpoint_verified"])
        self.assertFalse(events[-1]["port_was_held"])

    def test_exception_during_proof_always_releases(self):
        def broken(*args):
            raise OSError("simulated unreadable snapshot")
        result, events, listener = self.run_states([state()], prover=broken)
        self.assertEqual(result, 2)
        self.assertTrue(listener.closed)
        self.assertEqual(events[-1]["reason"], "observation-error:OSError")

    def test_signal_releases_even_inside_proof(self):
        stop = [False]
        def signal_during_hash(args, snapshot, tick):
            stop[0] = True
            tick()
        result, events, listener = self.run_states([state()], prover=signal_during_hash,
                                                   interrupted=lambda: stop[0])
        self.assertEqual(result, 2)
        self.assertTrue(listener.closed)
        self.assertEqual(events[-1]["reason"], "signal")

    def test_recovery_is_polled_during_long_proof(self):
        events, listener, clock = [], Listener(), [0.0]
        values = iter([state(), state(recovery="activating")])
        def proving(args, snapshot, tick):
            clock[0] = 0.3
            tick()
            self.fail("recovery transition must interrupt proof")
        code = fault.run_fault(ARGS, lambda e, **kw: events.append(dict(event=e, **kw)),
                 observer=lambda args: next(values), binder=lambda: listener, prover=proving,
                 clock=lambda: clock[0], pause=lambda _: None)
        self.assertEqual(code, 2)
        self.assertTrue(listener.closed)
        self.assertEqual(events[-1]["reason"], "recovery-unit-started")

    def test_snapshot_identity_change_releases_without_rebinding(self):
        changed = state()
        changed["transaction"]["snapshot"] = SNAP + "-other"
        code, events, listener = self.run_states([state(), changed])
        self.assertEqual(code, 0)
        self.assertTrue(listener.closed)
        self.assertEqual(events[-1]["reason"], "transaction-snapshot-changed")

    def test_ten_minute_timeout_is_bounded(self):
        events, listener, clock = [], Listener(), [0.0]
        def pause(_):
            clock[0] = 601.0
        code = fault.run_fault(ARGS, lambda e, **kw: events.append(dict(event=e, **kw)),
                 observer=lambda _: state(pid="42"), binder=lambda: listener,
                 clock=lambda: clock[0], pause=pause)
        self.assertEqual(code, 2)
        self.assertEqual(events[-1]["reason"], "timeout")

    def test_guard_refusal_happens_before_output_or_socket(self):
        argv = ["--lab-nonce", "a"*64, "--vm-uuid", "12345678-1234-5678-1234-567812345678",
                "--cell-id", "cell", "--node", "arch", "--operation-id", "c"*32,
                "--candidate-agent", "d"*64, "--candidate-panel", "e"*64]
        with patch.object(fault.probe, "guard_guest", side_effect=fault.probe.ProbeError("not lab")), \
             patch.object(fault.os, "open", side_effect=AssertionError("write before guard")):
            with self.assertRaises(fault.probe.ProbeError):
                fault.main(argv)



    def test_absent_transient_unit_waits_but_missing_panel_does_not(self):
        output = "LoadState=not-found\nActiveState=inactive\nMainPID=0\n"
        for code in (0, 1):
            with patch.object(fault.subprocess, "run", return_value=types.SimpleNamespace(stdout=output, returncode=code)):
                self.assertEqual(fault.unit_state("expected-update.service", allow_not_found=True)["MainPID"], "0")
                with self.assertRaises(fault.probe.ProbeError):
                    fault.unit_state("celikpanel-panel.service")

    def test_only_address_in_use_is_retried_until_bounded_release(self):
        events, listener, clock, calls = [], Listener(), [0.0], [0]
        values = iter([state(), state(), state(update="failed")])
        def bind():
            calls[0] += 1
            if calls[0] == 1:
                raise OSError(fault.errno.EADDRINUSE, "old socket closing")
            return listener
        def pause(seconds):
            clock[0] += seconds
        code = fault.run_fault(ARGS, lambda e, **kw: events.append(dict(event=e, **kw)),
                 observer=lambda _: next(values), binder=bind,
                 prover=lambda *a: {"snapshot": SNAP}, clock=lambda: clock[0], pause=pause)
        self.assertEqual(code, 0)
        self.assertEqual(calls[0], 2)
        self.assertTrue(listener.closed)


@unittest.skipUnless(hasattr(os, "O_NOFOLLOW"), "native Linux no-follow file contract")
class SnapshotProofTests(unittest.TestCase):
    def make(self, root):
        target = root / SNAP
        target.mkdir()
        (target / "snapshot.version").write_bytes(b"6\n")
        (target / "payload").write_bytes(b"old released state")
        rows = [hashlib.sha256((target/name).read_bytes()).hexdigest()+"  ./"+name
                for name in ("snapshot.version", "payload")]
        (target / "SHA256SUMS").write_text("\n".join(rows)+"\n")
        return target

    def test_real_complete_manifest_and_all_hashes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self.make(root)
            self.assertEqual(fault.verify_snapshot(SNAP, root=root)["verified_files"], 2)

    def test_tampered_extra_and_traversing_files_refused(self):
        for change in ("tamper", "extra", "traversal"):
            with tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                target = self.make(root)
                if change == "tamper":
                    (target / "payload").write_bytes(b"changed")
                elif change == "extra":
                    (target / "extra").write_bytes(b"not captured")
                else:
                    (target / "SHA256SUMS").write_text("a"*64+"  ./../outside\n")
                with self.assertRaises(fault.probe.ProbeError):
                    fault.verify_snapshot(SNAP, root=root)

    def test_nonfinal_missing_snapshot_is_unknown(self):
        with tempfile.TemporaryDirectory() as tmp:
            self.assertIsNone(fault.verify_snapshot(SNAP, root=Path(tmp)))
        with self.assertRaises(fault.probe.ProbeError):
            fault.verify_snapshot("../outside")



    def test_unlisted_symlink_and_special_objects_refused(self):
        for kind in ("symlink", "directory-symlink", "fifo"):
            with tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                target = self.make(root)
                if kind == "symlink":
                    (target / "link").symlink_to(target / "payload")
                elif kind == "directory-symlink":
                    other = root / "outside"
                    other.mkdir()
                    (target / "link").symlink_to(other, target_is_directory=True)
                else:
                    os.mkfifo(target / "pipe")
                with self.assertRaises(fault.probe.ProbeError):
                    fault.verify_snapshot(SNAP, root=root)


if __name__ == "__main__":
    unittest.main()
