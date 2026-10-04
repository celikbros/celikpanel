#!/usr/bin/env python3
"""Unit contracts and opt-in real tests against the freshly built private child."""
import ctypes
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

import trace_fixture as trace


class ObservationContractTests(unittest.TestCase):
    def info(self, result=4096, failed=0):
        value = trace.SyscallInfo()
        value.arch, value.op = trace.AUDIT_ARCH_X86_64, 2
        value.payload.exit.result = result
        value.payload.exit.is_error = failed
        return value

    def test_native_syscall_layout_matches_uapi(self):
        self.assertEqual(ctypes.sizeof(trace.SyscallInfo), 80)
        self.assertEqual(trace.SyscallInfo.payload.offset, 24)

    def test_positive_positioned_write_is_metadata_only(self):
        result = trace.write_exit((18, (8, 0xdeadbeef, 4096, 12416, 0, 0)), self.info())
        self.assertEqual(result, dict(syscall='pwrite64', number=18, fd=8,
                                     returned_bytes=4096, requested_bytes=4096, offset=12416))
        self.assertNotIn('deadbeef', json.dumps(result))

    def test_failure_zero_missing_entry_and_other_syscall_do_not_qualify(self):
        for entry, info in [((18, (8, 0, 4096, 0, 0, 0)), self.info(-4, 1)),
                            ((18, (8, 0, 4096, 0, 0, 0)), self.info(0)),
                            (None, self.info()), ((9, (0,) * 6), self.info())]:
            with self.subTest(entry=entry):
                self.assertIsNone(trace.write_exit(entry, info))

    def test_abi_and_impossible_return_are_refused(self):
        info = self.info()
        info.arch = 0x40000003
        with self.assertRaises(trace.TraceRefused):
            trace.write_exit((18, (8, 0, 4096, 0, 0, 0)), info)
        with self.assertRaises(trace.TraceRefused):
            trace.write_exit((18, (8, 0, 4000, 0, 0, 0)), self.info())

    def test_only_exact_new_suffix_positioned_write_can_select(self):
        write = dict(number=18, returned_bytes=4096, offset=12416)
        self.assertTrue(trace.appended_range(write, 12392, 16512))
        for change in ({'number': 1}, {'offset': 0}, {'returned_bytes': 0}, {'offset': 12417}):
            self.assertFalse(trace.appended_range({**write, **change}, 12392, 16512))

    def test_memory_and_register_operations_cannot_be_requested(self):
        for request in (4, 5, 6, 13, 14, 0x4205):
            with self.subTest(request=request), self.assertRaises(trace.TraceRefused):
                trace.ptrace(request, os.getpid())

    def test_symlink_and_multilink_evidence_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            original = root / 'original'
            original.write_bytes(b'private')
            (root / 'symbolic').symlink_to(original)
            with self.assertRaises(OSError):
                trace.read_file(root / 'symbolic')
            os.link(original, root / 'hardlink')
            with self.assertRaises(trace.TraceRefused):
                trace.read_file(original)

    def test_fifo_evidence_is_refused_without_waiting_for_writer(self):
        with tempfile.TemporaryDirectory() as directory:
            fifo = Path(directory) / 'fifo'
            os.mkfifo(fifo)
            started = time.monotonic()
            with self.assertRaises(trace.TraceRefused):
                trace.read_file(fifo)
            self.assertLess(time.monotonic() - started, 0.5)

    def test_no_live_pid_argument(self):
        result = subprocess.run([sys.executable, str(Path(trace.__file__)), '--pid', str(os.getpid())],
                                capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('--fixture-binary', result.stderr)

    def test_dead_stop_remains_admitted_until_real_wait_exit(self):
        controller = object.__new__(trace.ControlledTrace)
        controller.known = {123: {'start_time': 99}}
        controller.stopped, controller.entries = {123}, {123: (1, (0,) * 6)}
        controller.event = mock.Mock()
        with mock.patch.object(trace, 'process_status', side_effect=FileNotFoundError):
            self.assertTrue(controller.awaiting_exit(123))
        self.assertIn(123, controller.known)
        self.assertNotIn(123, controller.stopped)
        with mock.patch.object(trace, 'process_status', return_value={'State': 'S (sleeping)'}), mock.patch.object(trace, 'start_time', return_value=99):
            self.assertTrue(controller.awaiting_exit(123))
            self.assertIn(123, controller.known)
        with mock.patch.object(trace, 'process_status', return_value={'State': 'Z (zombie)'}), mock.patch.object(trace, 'start_time', return_value=100):
            with self.assertRaises(trace.TraceRefused):
                controller.awaiting_exit(123)

    def test_foreign_wait_identity_is_refused_before_ptrace(self):
        controller = object.__new__(trace.ControlledTrace)
        controller.known = {}
        with mock.patch.object(trace, 'ptrace') as operation:
            with self.assertRaises(trace.TraceRefused):
                controller.status_event(os.getpid(), 0)
            operation.assert_not_called()


@unittest.skipUnless(os.environ.get('CELIKPANEL_WALTRACE_CHILD'), 'requires freshly built controlled child')
class RealControlledChildTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.binary = Path(os.environ['CELIKPANEL_WALTRACE_CHILD']).resolve()
        cls.evidence = Path(os.environ['CELIKPANEL_WALTRACE_EVIDENCE']).resolve()
        cls.evidence.mkdir(mode=0o700)

    def run_mode(self, mode, timeout=20):
        target = self.evidence / self._testMethodName
        result = subprocess.run([sys.executable, str(Path(trace.__file__)), '--fixture-binary', str(self.binary),
                                 '--output-directory', str(target), '--mode', mode, '--timeout', str(timeout)],
                                capture_output=True, text=True, timeout=timeout + 10)
        (self.evidence / (self._testMethodName + '.stdout')).write_text(result.stdout)
        (self.evidence / (self._testMethodName + '.stderr')).write_text(result.stderr)
        self.assertTrue((target / 'result.json').exists(), result.stderr)
        proof = json.loads((target / 'result.json').read_text())
        self.assertTrue(proof['cleanup']['all_admitted_tasks_reaped'])
        self.assertEqual(proof['cleanup']['remaining_tasks'], [])
        return result, proof, target

    def test_actual_uncommitted_spill_all_threads_and_original_images(self):
        outcome, proof, target = self.run_mode('spill')
        self.assertEqual(outcome.returncode, 0, proof.get('reason'))
        self.assertEqual(proof['status'], 'verified-feasibility')
        self.assertEqual(proof['successful_write_exit']['syscall'], 'pwrite64')
        self.assertGreaterEqual(proof['maximum_admitted_tasks'], 5)
        self.assertEqual(len(proof['controlled_execs']), 2)
        self.assertEqual(len({entry['sha256'] for entry in proof['controlled_execs']}), 1)
        self.assertEqual(len({entry['tgid'] for entry in proof['stopped_tasks']}), 2)
        self.assertTrue(all(item['state'].startswith('t') for item in proof['stopped_tasks']))
        self.assertTrue(proof['fixture_transaction_begun'])
        self.assertFalse(proof['fixture_commit_entered'])
        self.assertTrue(proof['wal_evidence']['growth_after_prior_commit'])
        self.assertEqual(proof['wal_evidence']['prior_prefix']['appended_commit_frames'], 0)
        self.assertEqual(proof['private_copy_visibility']['visible_rows'], 1)
        self.assertTrue(proof['original_images_preserved'])
        for filename, observed in proof['stopped_images'].items():
            self.assertEqual(hashlib.sha256((target / 'work' / filename).read_bytes()).hexdigest(), observed['sha256'])

    def test_no_spill_does_not_misreport_commit_time_write(self):
        outcome, proof, target = self.run_mode('small')
        self.assertEqual(outcome.returncode, 2)
        self.assertEqual(proof['status'], 'inconclusive')
        self.assertIn('no qualifying', proof['reason'])
        self.assertIn('WALTRACE_COMMIT_RETURNED', (target / 'child.stdout').read_text())
        self.assertNotIn('stopped_images', proof)

    def test_timeout_kills_and_reaps_all_created_processes(self):
        outcome, proof, _ = self.run_mode('hang', timeout=0.8)
        self.assertEqual(outcome.returncode, 2)
        self.assertEqual(proof['status'], 'inconclusive')
        self.assertIn('timed out', proof['reason'])
        self.assertGreaterEqual(len(proof['controlled_execs']), 2)

    def test_tracer_death_exitkill_leaves_no_running_children(self):
        target = self.evidence / self._testMethodName
        stdout = (self.evidence / (self._testMethodName + '.stdout')).open('w')
        stderr = (self.evidence / (self._testMethodName + '.stderr')).open('w')
        tracer = subprocess.Popen([sys.executable, str(Path(trace.__file__)), '--fixture-binary', str(self.binary),
                                   '--output-directory', str(target), '--mode', 'hang', '--timeout', '10'],
                                  stdout=stdout, stderr=stderr)
        identities = {}
        try:
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                events = target / 'events.jsonl'
                if events.exists():
                    lines = events.read_text().splitlines()
                    values = [json.loads(line) for line in lines if line.endswith('}')]
                    identities = {v['tid']: v['start_time'] for v in values if v['event'] == 'owned-task-admitted'}
                    if sum(v['event'] == 'controlled-image-exec' for v in values) >= 2:
                        break
                time.sleep(0.01)
            self.assertGreaterEqual(len(identities), 5)
            tracer.kill()
            tracer.wait(timeout=5)
            deadline = time.monotonic() + 3
            while time.monotonic() < deadline:
                running = []
                for tid, started in identities.items():
                    try:
                        if trace.start_time(tid) == started and not trace.process_status(tid)['State'].startswith('Z'):
                            running.append(tid)
                    except FileNotFoundError:
                        pass
                if not running:
                    break
                time.sleep(0.01)
            (target / 'external-exitkill-check.json').write_text(json.dumps({'admitted': identities, 'remaining_running': running}))
            self.assertEqual(running, [])
        finally:
            if tracer.poll() is None:
                tracer.kill()
                tracer.wait(timeout=5)
            stdout.close()
            stderr.close()


if __name__ == '__main__':
    unittest.main()
