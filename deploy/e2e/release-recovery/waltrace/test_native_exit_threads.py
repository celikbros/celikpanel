"""Opt-in kernel test: only forked pipe-gated children, never external PIDs.

Build exit-fixture/main.go and set CELIKPANEL_EXIT_TRACE_CHILD plus a fresh
CELIKPANEL_EXIT_TRACE_EVIDENCE directory. Uses the real NativeTrace event loop
and ptrace adapter; the fixture's existing cgroup replaces registered VM scope.
This is tracer evidence, not an installed update or native recovery acceptance.
"""
import hashlib
import json
import os
from pathlib import Path
import platform
import signal
import time
import unittest

import native_trace as native


class OwnedKernel(native.LinuxKernel):
    def __init__(self, child):
        self.child = child
        self._wait_after = 0

    def close(self):
        pass

    def seize(self, tid):
        if tid != self.child:
            raise AssertionError('only this test fork child can be seized')
        super().seize(tid)

    def tasks(self):
        return {int(p.name) for p in Path('/proc', str(self.child), 'task').iterdir()}


class OwnedTrace(native.NativeTrace):
    def same_scope(self, task):
        native.require(task['cgroup_raw'] == self.fixture_cgroup, 'fixture-cgroup-changed')
        native.require(task['start_ticks'] >= self.registration['worker_start_ticks'],
                       'task-predates-worker')


class Callbacks:
    def __init__(self, gate):
        self.gate = gate

    def revalidate(self, stage, proof):
        pass  # no external fault authority exists in this private-child fixture

    def release_start_gate(self, proof):
        os.write(self.gate, b'x')

    def writer_expected(self):
        return None

    def perform_cut(self, proof):
        raise AssertionError('fixture cannot authorize any cut')


@unittest.skipUnless(os.environ.get('CELIKPANEL_EXIT_TRACE_CHILD') and
                     platform.system() == 'Linux' and platform.machine() == 'x86_64',
                     'requires freshly built private Go exit fixture on Linux/amd64')
class RealExitThreadTests(unittest.TestCase):
    def test_exit_race_drains_without_lost_thread_or_synthetic_exit(self):
        binary = Path(os.environ['CELIKPANEL_EXIT_TRACE_CHILD']).resolve(strict=True)
        evidence = Path(os.environ['CELIKPANEL_EXIT_TRACE_EVIDENCE'])
        evidence.mkdir(mode=0o700)  # refuse overwriting an earlier campaign
        reconciled = 0
        outcomes = {}
        for trial in range(50):
            rd, wr = os.pipe()
            child = os.fork()
            if child == 0:
                os.close(wr)
                os.read(rd, 1)
                os.close(rd)
                os.execv(str(binary), [str(binary)])
            os.close(rd)
            pidfd = os.pidfd_open(child)
            kernel = OwnedKernel(child)
            value = kernel.task(child)
            executable = kernel.executable(child)
            operation = 'a' * 32
            registration = dict(operation_id=operation,
                                unit='celikpanel-self-update-' + operation + '.service',
                                worker_pid=child, worker_start_ticks=value['start_ticks'],
                                boot_id=kernel.boot(), gate_executable_sha256=executable['sha256'],
                                gate_executable_device=executable['dev'], gate_executable_inode=executable['ino'])
            tracer = OwnedTrace(registration, Callbacks(wr), kernel=kernel, timeout=3)
            tracer.fixture_cgroup = value['cgroup_raw']
            result = None
            reaped = False
            try:
                result = tracer.run()
            finally:
                # Exact owned pidfd only. NativeTrace itself must never kill.
                try:
                    signal.pidfd_send_signal(pidfd, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                os.close(wr)
                deadline = time.monotonic() + 3
                while time.monotonic() < deadline:
                    try:
                        got, status = os.waitpid(-1, native.WAIT_ALL | os.WNOHANG)
                    except ChildProcessError:
                        reaped = True
                        break
                    if got and os.WIFSTOPPED(status):
                        try:
                            native.ptrace(native.CONT, got)
                        except ProcessLookupError:
                            pass
                    if not got:
                        time.sleep(0.001)
                os.close(pidfd)
                (evidence / f'{trial:03d}.json').write_text(json.dumps(
                    {'result': result, 'owned_child_reaped': reaped}, sort_keys=True) + '\n')
            self.assertTrue(reaped)
            outcomes[result['reason']] = outcomes.get(result['reason'], 0) + 1
            self.assertFalse(result['controller_cut_called'])
            self.assertTrue(result['cleanup']['complete'], result)
            if result['reason'] == 'task-id':
                # An invalid kernel event-message is a separate inconclusive
                # trace, never counted as a verified exit-race completion.
                continue
            self.assertEqual(result['reason'], 'no-exact-native-wal-boundary', result)
            self.assertEqual(result['cleanup']['detached'], [], result)
            admitted = {e['tid'] for e in result['events'] if e['event'] == 'exit-thread-admitted'}
            exited = {e['tid'] for e in result['events'] if e['event'] == 'kernel-wait-exit'}
            self.assertTrue(admitted <= exited)
            reconciled += len(admitted)
        summary = dict(trials=50, outcomes=outcomes, reconciled_threads=reconciled, kernel=platform.release(),
                       binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),
                       scope='controlled children only; not native recovery acceptance')
        (evidence / 'summary.json').write_text(json.dumps(summary, sort_keys=True) + '\n')
        self.assertGreater(reconciled, 0, 'race was not exercised; no positive race proof')


if __name__ == '__main__':
    unittest.main()
