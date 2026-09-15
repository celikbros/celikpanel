"""Native adapter contracts with real validators and a fake kernel boundary.

No test in this file attaches to a pre-existing process or starts a VM/update.
The earlier controlled-child suite exercises actual kernel syscall stops.
"""
import copy
import ctypes
import hashlib
import os
from pathlib import Path
import signal
import stat
import struct
import sys
import unittest
from unittest import mock

import native_trace as native

OP, TOKEN, ADMISSION, PANEL = 'a' * 32, 'b' * 64, 'c' * 64, 'd' * 64
BOOT = '11111111-2222-3333-4444-555555555555'
REG = {'operation_id': OP, 'unit': 'celikpanel-self-update-' + OP + '.service',
       'worker_pid': 100, 'worker_start_ticks': 200, 'boot_id': BOOT,
       'gate_executable_sha256': 'e' * 64, 'gate_executable_device': 5, 'gate_executable_inode': 7}
EXPECTED = {'operation_id': OP, 'snapshot': '20260915T120000Z-from-unknown-to-' + 'f' * 40 + '-' + '1' * 32,
            'token_sha256': TOKEN, 'admission_sha256': ADMISSION, 'candidate_panel_sha256': PANEL,
            'uid': 1001, 'gid': 1001, 'worker_start_ticks': 200,
            'work': {'dev': 5, 'ino': 10, 'mode': stat.S_IFDIR | 0o700, 'uid': 1001, 'gid': 1001}}
WRITE = {'number': 18, 'fd': 8, 'offset': 56, 'requested_bytes': 512, 'returned_bytes': 512,
         'is_error': False, 'arch': native.AMD64, 'entry_exit_matched': True}


def stop(event=0, sig=signal.SIGTRAP):
    return (event << 16) | (sig << 8) | 0x7f


def wal(frame=False, commit=False):
    def checksum(raw, seed=(0, 0)):
        words = struct.unpack('<' + str(len(raw) // 4) + 'I', raw)
        sums = list(seed)
        for index, word in enumerate(words):
            j = index % 2
            sums[j] = (sums[j] + word + sums[1 - j]) & 0xffffffff
        return tuple(sums)
    raw = struct.pack('>6I', 0x377f0682, 3007000, 512, 0, 17, 23)
    seed = checksum(raw)
    raw += struct.pack('>2I', *seed)
    if frame:
        page = b'x' * 512
        prefix = struct.pack('>2I', 1, int(commit))
        checks = checksum(prefix + page, seed)
        raw += prefix + struct.pack('>4I', 17, 23, *checks) + page
    return raw


def task(tid=100, tgid=None, started=200, traced=False, state='S'):
    return {'tid': tid, 'tgid': tid if tgid is None else tgid, 'start_ticks': started,
            'tracer_pid': os.getpid() if traced else 0, 'state': state,
            'uids': (1001,) * 4, 'gids': (1001,) * 4,
            'cgroup_raw': ('0::/system.slice/' + REG['unit'] + '\n').encode()}


class Kernel:
    def __init__(self):
        self.processes = {100: task()}
        self.calls, self.queue, self.messages, self.syscalls = [], [], {}, {}
        self.clock = 1.0
        self.raw = wal()
        self.raw_reads = 0
        self.read_hook = None
        self.wal_fd = True
        self.executable_value = {'dev': 5, 'ino': 7, 'sha256': 'e' * 64}
        self.after_seize = None
        self.after_detach = None

    def now(self): return self.clock
    def boot(self): return BOOT
    def close(self): self.calls.append(('close',))
    def task(self, tid):
        if tid not in self.processes: raise FileNotFoundError()
        return dict(self.processes[tid])
    def tasks(self): return set(self.processes)
    def executable(self, tid): return dict(self.executable_value)
    def command(self, tid): return native.COMMAND
    def is_wal_descriptor(self, expected, tid, fd): return self.wal_fd
    def seize(self, tid):
        self.calls.append(('seize', tid))
        self.processes[tid]['tracer_pid'] = os.getpid()
        if self.after_seize: self.after_seize()
    def interrupt(self, tid):
        self.calls.append(('interrupt', tid))
        self.queue.append((tid, stop(native.EVENT_STOP)))
    def resume(self, tid, sig=0, *, syscalls=False):
        self.calls.append(('resume', tid, sig, syscalls)); self.processes[tid]['state'] = 'R'
    def detach(self, tid, sig=0):
        self.calls.append(('detach', tid, sig)); self.processes[tid]['tracer_pid'] = 0
        self.processes[tid]['state'] = 'R'
        if self.after_detach: self.after_detach(tid)
    def event_message(self, tid): return self.messages[tid]
    def syscall(self, tid): return self.syscalls[tid]
    def wait(self, tids, deadline):
        if not self.queue:
            self.clock = deadline
            raise native.Inconclusive('trace-deadline')
        tid, value = self.queue.pop(0)
        if os.WIFSTOPPED(value): self.processes[tid]['state'] = 't'
        elif os.WIFEXITED(value) or os.WIFSIGNALED(value): self.processes.pop(tid, None)
        return tid, value
    def writer_observation(self, expected, tid, fd):
        self.raw_reads += 1
        if self.read_hook: self.read_hook(self.raw_reads)
        status = self.task(tid)
        leader = self.task(status['tgid'])
        meta = {'dev': 5, 'ino': 11, 'mode': stat.S_IFREG | 0o600, 'uid': 1001, 'gid': 1001,
                'links': 1, 'size': len(self.raw), 'mtime_ns': len(self.raw), 'ctime_ns': len(self.raw)}
        workpath = '/var/lib/celikpanel/.release-db-migrations/' + TOKEN + '/work'
        observed = {'pid': status['tgid'], 'tid': tid, 'start_ticks': leader['start_ticks'],
                    'thread_start_ticks': status['start_ticks'], 'cmdline': native.COMMAND,
                    'environ': ('PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\0HOME=/var/lib/celikpanel\0LC_ALL=C\0CELIKPANEL_DATA_DIR=' + workpath + '\0').encode(),
                    'cgroup_raw': status['cgroup_raw'], 'uids': status['uids'], 'gids': status['gids'],
                    'executable_sha256': PANEL, 'work': dict(EXPECTED['work']), 'wal_fd': fd,
                    'wal_path': workpath + '/celikpanel.db-wal', 'wal_descriptor': dict(meta), 'wal_entry': dict(meta)}
        return observed, self.raw


class Callbacks:
    def __init__(self):
        self.calls = []
        self.expected = copy.deepcopy(EXPECTED)
        self.reject_stage = None
        self.authorize_hook = None
        self.authorize_result = None
        self.cut_result = None
    def revalidate(self, stage, proof):
        self.calls.append(('revalidate', stage))
        if stage == self.reject_stage: raise native.Inconclusive('controller-authority-changed')
    def writer_expected(self): return copy.deepcopy(self.expected)
    def release_start_gate(self, proof): self.calls.append(('release',))
    def authorize_cut(self, proof, prior):
        self.calls.append(('authorize',))
        if self.authorize_hook: self.authorize_hook(proof, prior)
        if self.authorize_result is not None: return self.authorize_result
        return {'status': 'verified', 'operation_id': OP, 'trace_sha256': native.proof_digest(proof),
                'prior_wal_sha256': native.digest(prior)}
    def perform_cut(self, proof):
        self.calls.append(('cut',))
        if self.cut_result is not None: return self.cut_result
        return {'status': 'cut-sent', 'operation_id': OP, 'trace_sha256': proof['trace_sha256']}


class NativeTraceTests(unittest.TestCase):
    def make(self, attached=False):
        kernel, callbacks = Kernel(), Callbacks()
        tracer = native.NativeTrace(REG, callbacks, timeout=30, kernel=kernel)
        if attached:
            kernel.processes[100].update(tracer_pid=os.getpid(), state='t')
            tracer.remember(100)
            tracer.stopped.add(100)
            tracer.gate_released = True
            tracer.syscall_tgids.add(100)
        return tracer, kernel, callbacks

    def baseline(self):
        tracer, kernel, callbacks = self.make(attached=True)
        header_write = {**WRITE, 'offset': 0, 'requested_bytes': 32, 'returned_bytes': 32}
        self.assertIsNone(tracer.observe_write(100, header_write))
        kernel.raw = wal(frame=True)
        return tracer, kernel, callbacks

    def test_registration_accepts_only_fixed_exact_operation_unit(self):
        self.assertEqual(native.validate_registration(REG), REG)
        for change in ({'unit': 'celikpanel-agent.service'}, {'worker_pid': True}, {'operation_id': '../bad'},
                       {'boot_id': ''}, {'gate_executable_sha256': 'x' * 64}):
            with self.subTest(change=change), self.assertRaises(native.Inconclusive):
                native.validate_registration({**REG, **change})
        with self.assertRaises(native.Inconclusive): native.validate_registration({**REG, 'pid': 1})

    def test_no_exitkill_or_memory_register_mutation(self):
        self.assertEqual(native.OPTIONS & 0x100000, 0)
        self.assertEqual(ctypes.sizeof(native.Info), 80)
        for request in (4, 5, 6, 13, 14, 0x4205):
            with self.assertRaises(native.Inconclusive): native.ptrace(request, os.getpid())

    def test_positive_pwrite_requires_matching_successful_entry_exit(self):
        entry = {'op': 'entry', 'number': 18, 'args': (8, 0xdeadbeef, 512, 56, 0, 0)}
        result = {'op': 'exit', 'result': 512, 'is_error': False}
        self.assertEqual(native.successful_pwrite(entry, result), WRITE)
        for e, r in ((None, result), ({**entry, 'number': 1}, result), (entry, {**result, 'result': 0}),
                     (entry, {**result, 'is_error': True, 'result': -4})):
            self.assertIsNone(native.successful_pwrite(e, r))
        with self.assertRaises(native.Inconclusive): native.successful_pwrite(entry, {**result, 'result': 513})

    def test_controller_refusal_prevents_attach(self):
        tracer, kernel, callback = self.make()
        callback.reject_stage = 'before-seize'
        result = tracer.run()
        self.assertEqual(result['status'], 'inconclusive')
        self.assertFalse(any(c[0] == 'seize' for c in kernel.calls))
        self.assertFalse(any(c[0] == 'cut' for c in callback.calls))

    def test_gate_executable_and_single_task_are_required_before_attach(self):
        for change in ('executable', 'extra', 'foreign-tracer'):
            tracer, kernel, _ = self.make()
            if change == 'executable': kernel.executable_value['sha256'] = '0' * 64
            elif change == 'extra': kernel.processes[101] = task(101, 100, 201)
            else: kernel.processes[100]['tracer_pid'] = 9876
            with self.subTest(change=change), self.assertRaises(native.Inconclusive): tracer.initial_attach()
            self.assertFalse(any(c[0] == 'seize' for c in kernel.calls))

    def test_poll_gate_seize_stop_revalidate_release_order(self):
        tracer, kernel, callback = self.make()
        tracer.initial_attach()
        self.assertEqual(kernel.calls, [('seize', 100), ('interrupt', 100), ('resume', 100, 0, False)])
        self.assertEqual(callback.calls, [('revalidate', 'before-seize'), ('revalidate', 'gate-seized'), ('release',)])
        self.assertTrue(tracer.gate_released)

    def test_failed_postseize_scope_recheck_still_detaches_exact_task(self):
        tracer, kernel, callback = self.make()
        kernel.after_seize = lambda: kernel.processes[100].update(cgroup_raw=b'0::/changed\n')
        result = tracer.run()
        self.assertEqual(result['status'], 'inconclusive')
        self.assertIn(('detach', 100, 0), kernel.calls)
        self.assertTrue(result['cleanup']['complete'])
        self.assertFalse(any(c[0] in ('release', 'cut') for c in callback.calls))

    def test_timeout_detaches_without_killing_or_releasing_other_tasks(self):
        tracer, kernel, callback = self.make()
        result = tracer.run()
        self.assertEqual(result['status'], 'inconclusive')
        self.assertEqual(result['cleanup']['detached'], [100])
        self.assertTrue(result['cleanup']['no_kill_by_tracer'])
        self.assertFalse(any(c[0] == 'cut' for c in callback.calls))
        self.assertEqual(kernel.processes[100]['tracer_pid'], 0)

    def test_real_signals_are_forwarded_without_substitution(self):
        tracer, kernel, _ = self.make(attached=True)
        tracer.consume(100, stop(sig=signal.SIGURG))
        tracer.resume(100)
        self.assertEqual(kernel.calls[-1], ('resume', 100, signal.SIGURG, True))

    def test_clone_kernel_event_admits_child_then_stop_inventory_is_exact(self):
        tracer, kernel, _ = self.make(attached=True)
        kernel.processes[101] = task(101, 100, 201, traced=True)
        kernel.messages[100] = 101
        tracer.consume(100, stop(native.EVENT_CLONE))
        tracer.freeze()
        self.assertEqual(tracer.stopped, {100, 101})
        kernel.processes[102] = task(102, 100, 202, traced=False)
        with self.assertRaises(native.Inconclusive): tracer.freeze()

    def test_changed_pid_identity_never_detaches_foreign_task(self):
        tracer, kernel, _ = self.make(attached=True)
        kernel.processes[100]['start_ticks'] += 1
        proof = tracer.cleanup()
        self.assertFalse(proof['complete'])
        self.assertFalse(any(c[0] == 'detach' for c in kernel.calls))

    def test_nonleader_exec_requires_kernel_old_tid_and_retired_siblings(self):
        tracer, kernel, _ = self.make(attached=True)
        kernel.processes[101] = task(101, 100, 201, traced=True, state='t')
        tracer.remember(101); tracer.stopped.add(101)
        kernel.messages[100] = 101
        kernel.processes.pop(101)
        kernel.processes[100]['start_ticks'] = 201
        kernel.executable_value['sha256'] = PANEL
        tracer.consume(100, stop(native.EVENT_EXEC))
        self.assertEqual(set(tracer.known), {100})
        self.assertEqual(tracer.known[100]['start_ticks'], 201)
        self.assertEqual(tracer.events[-1]['retired_tids'], [101])

    def test_only_exact_admitted_migrator_exec_switches_to_syscalls(self):
        for candidate in (False, True):
            tracer, kernel, callbacks = self.make(attached=True)
            tracer.syscall_tgids.clear()
            kernel.messages[100] = 100
            kernel.executable_value['sha256'] = PANEL
            if not candidate: callbacks.expected = None
            tracer.consume(100, stop(native.EVENT_EXEC))
            tracer.resume(100)
            self.assertEqual(kernel.calls[-1], ('resume', 100, 0, candidate))

    def test_unreported_exec_or_live_sibling_is_inconclusive(self):
        for old in (999, 101):
            tracer, kernel, _ = self.make(attached=True)
            kernel.processes[101] = task(101, 100, 201, traced=True)
            tracer.remember(101)
            kernel.messages[100] = old
            with self.subTest(old=old), self.assertRaises(native.Inconclusive): tracer.consume(100, stop(native.EVENT_EXEC))

    def test_no_admission_or_nonwal_fd_cannot_be_observed_as_cut(self):
        tracer, kernel, callback = self.make(attached=True)
        callback.expected = None
        self.assertIsNone(tracer.observe_write(100, WRITE))
        callback.expected = copy.deepcopy(EXPECTED); kernel.clock += 1
        kernel.wal_fd = False
        self.assertIsNone(tracer.observe_write(100, WRITE))
        self.assertEqual(kernel.raw_reads, 0)
        self.assertFalse(any(c[0] == 'cut' for c in callback.calls))

    def test_noncommit_bytes_without_previous_observation_do_not_synthesize_prefix(self):
        tracer, kernel, callback = self.make(attached=True)
        kernel.raw = wal(frame=True)
        self.assertIsNone(tracer.observe_write(100, WRITE))
        self.assertIsNone(tracer.baseline)
        self.assertFalse(any(c[0] == 'authorize' for c in callback.calls))

    def test_valid_physical_boundary_authorization_and_cut_are_ordered(self):
        tracer, kernel, callback = self.baseline()
        result = tracer.observe_write(100, WRITE)
        self.assertEqual(result['status'], 'cut-sent')
        self.assertEqual(result['wal']['noncommit_suffix_frames'], 1)
        self.assertEqual(callback.calls, [('revalidate', 'before-cut-proof'), ('authorize',),
                                         ('revalidate', 'immediately-before-cut'), ('cut',)])
        self.assertEqual(result['trace']['write'], WRITE)
        self.assertNotIn('BEGIN', str(result))

    def test_invalid_checksum_or_commit_frame_does_not_authorize_cut(self):
        for raw in (wal(frame=True)[:-1], wal(frame=True, commit=True)):
            tracer, kernel, callback = self.baseline()
            kernel.raw = raw
            self.assertIsNone(tracer.observe_write(100, WRITE))
            self.assertFalse(any(c[0] == 'authorize' for c in callback.calls))

    def test_changed_or_removed_admission_refuses(self):
        for expected in (None, {**EXPECTED, 'admission_sha256': '0' * 64}):
            tracer, kernel, callback = self.baseline()
            callback.expected = expected
            kernel.clock += 1
            with self.assertRaises(native.Inconclusive): tracer.observe_write(100, WRITE)
            self.assertFalse(any(c[0] == 'cut' for c in callback.calls))

    def test_admission_change_during_authorization_refuses_cut(self):
        for replacement in (None, {**EXPECTED, 'admission_sha256': '0' * 64}):
            tracer, kernel, callback = self.baseline()
            def alter(proof, prior):
                callback.expected = replacement
            callback.authorize_hook = alter
            with self.subTest(replacement=replacement), self.assertRaises(native.Inconclusive):
                tracer.observe_write(100, WRITE)
            self.assertFalse(any(c[0] == 'cut' for c in callback.calls))

    def test_authorization_requires_exact_trace_prior_and_operation_binding(self):
        for value in (True, {}, {'status': 'verified'}, {'status': 'verified', 'operation_id': '0' * 32}):
            tracer, kernel, callback = self.baseline()
            callback.authorize_result = value
            with self.subTest(value=value), self.assertRaises(native.Inconclusive): tracer.observe_write(100, WRITE)
            self.assertFalse(any(c[0] == 'cut' for c in callback.calls))

    def test_callback_cannot_mutate_trace_or_wal_before_kill(self):
        for change in ('trace', 'wal', 'scope'):
            tracer, kernel, callback = self.baseline()
            def alter(proof, prior):
                if change == 'trace': proof['write']['offset'] += 1
                elif change == 'wal': kernel.raw = wal(frame=True, commit=True)
                else: kernel.processes[100]['cgroup_raw'] = b'0::/other\n'
            callback.authorize_hook = alter
            with self.subTest(change=change), self.assertRaises(native.Inconclusive): tracer.observe_write(100, WRITE)
            self.assertFalse(any(c[0] == 'cut' for c in callback.calls))

    def test_bad_cut_receipt_stays_unconfirmed_after_callback(self):
        tracer, kernel, callback = self.baseline()
        callback.cut_result = {'scope': 'unit'}
        with self.assertRaises(native.Inconclusive): tracer.observe_write(100, WRITE)
        self.assertTrue(tracer.cut_called)
        self.assertEqual(callback.calls[-1], ('cut',))

    def test_pending_esrch_keeps_admission_until_real_wait_exit(self):
        tracer, kernel, _ = self.make(attached=True)
        tracer.await_exit(100)
        self.assertIn(100, tracer.known)
        self.assertNotIn(100, tracer.stopped)
        tracer.consume(100, 0)
        self.assertNotIn(100, tracer.known)

    def test_cleanup_exec_does_not_need_still_valid_product_admission(self):
        tracer, kernel, callback = self.make(attached=True)
        tracer.stopped.clear()
        kernel.messages[100] = 100
        kernel.queue = [(100, stop(native.EVENT_EXEC))]
        callback.writer_expected = mock.Mock(side_effect=RuntimeError('authority unavailable'))
        result = tracer.cleanup()
        self.assertTrue(result['complete'])
        self.assertEqual(result['detached'], [100])
        callback.writer_expected.assert_not_called()

    def test_complete_driver_path_observes_write_and_detaches_after_authorized_cut(self):
        tracer, kernel, callback = self.make()
        phases = [stop(native.EVENT_EXEC), stop(sig=native.SYSCALL_STOP), stop(sig=native.SYSCALL_STOP),
                  stop(sig=native.SYSCALL_STOP), stop(sig=native.SYSCALL_STOP)]
        steps = iter(phases)
        count = [0]
        original_resume = kernel.resume
        def resume(tid, sig=0, *, syscalls=False):
            original_resume(tid, sig, syscalls=syscalls)
            count[0] += 1
            kernel.executable_value['sha256'] = PANEL
            kernel.messages[100] = 100
            if count[0] == 2:
                kernel.syscalls[100] = {'op': 'entry', 'number': 18, 'args': (8, 0, 32, 0, 0, 0)}
            elif count[0] == 3:
                kernel.raw = wal()
                kernel.syscalls[100] = {'op': 'exit', 'result': 32, 'is_error': False}
            elif count[0] == 4:
                kernel.syscalls[100] = {'op': 'entry', 'number': 18, 'args': (8, 0, 512, 56, 0, 0)}
            elif count[0] == 5:
                kernel.raw = wal(frame=True)
                kernel.syscalls[100] = {'op': 'exit', 'result': 512, 'is_error': False}
            kernel.queue.append((tid, next(steps)))
        kernel.resume = resume
        result = tracer.run()
        self.assertEqual(result['status'], 'cut-sent', result)
        self.assertTrue(result['controller_cut_called'])
        self.assertTrue(result['cleanup']['complete'])
        self.assertEqual(sum(value[0] == 'cut' for value in callback.calls), 1)
        modes = [value[3] for value in kernel.calls if value[0] == 'resume']
        self.assertEqual(modes, [False, True, True, True, True])

    def test_cleanup_pending_fork_event_detaches_child_without_any_kill(self):
        tracer, kernel, _ = self.make(attached=True)
        tracer.stopped.clear()
        kernel.processes[101] = task(101, 100, 201, traced=True)
        kernel.messages[100] = 101
        kernel.queue = [(100, stop(native.EVENT_CLONE)), (101, stop(native.EVENT_STOP))]
        result = tracer.cleanup()
        self.assertTrue(result['complete'])
        self.assertEqual(result['detached'], [100, 101])


class KernelWaitFairnessTests(unittest.TestCase):
    def kernel(self):
        kernel = native.LinuxKernel.__new__(native.LinuxKernel)
        kernel._wait_after = 0
        kernel.now = lambda: 0
        return kernel

    def test_continuously_ready_low_tid_does_not_starve_other_threads(self):
        kernel = self.kernel()
        with mock.patch.object(native.os, 'waitpid', side_effect=lambda tid, flags: (tid, 123)) as wait:
            results = [kernel.wait({101, 102, 103}, 1)[0] for _ in range(6)]
        self.assertEqual(results, [101, 102, 103, 101, 102, 103])
        self.assertTrue(all(call.args[1] == native.WAIT_ALL | native.os.WNOHANG for call in wait.call_args_list))

    def test_removed_cursor_and_new_threads_only_wait_on_current_admitted_set(self):
        kernel = self.kernel()
        with mock.patch.object(native.os, 'waitpid', side_effect=lambda tid, flags: (tid, 123)) as wait:
            self.assertEqual(kernel.wait({101, 103}, 1)[0], 101)
            self.assertEqual(kernel.wait({100, 102, 103}, 1)[0], 102)
            self.assertEqual(kernel.wait({100, 103}, 1)[0], 103)
            self.assertEqual(kernel.wait({100}, 1)[0], 100)
        self.assertEqual([call.args[0] for call in wait.call_args_list], [101, 102, 103, 100])

    def test_not_ready_and_exited_tasks_do_not_hide_ready_thread(self):
        kernel = self.kernel()
        def waitpid(tid, flags):
            if tid == 101:
                return 0, 0
            if tid == 102:
                raise ChildProcessError()
            return tid, 123
        with mock.patch.object(native.os, 'waitpid', side_effect=waitpid):
            self.assertEqual(kernel.wait({101, 102, 103}, 1), (103, 123))

    def test_deadline_still_bounds_wait_without_consuming_another_task(self):
        kernel = self.kernel()
        kernel.now = mock.Mock(side_effect=[0, 1])
        with mock.patch.object(native.os, 'waitpid', return_value=(0, 0)) as wait, mock.patch.object(native.time, 'sleep'):
            with self.assertRaisesRegex(native.Inconclusive, 'trace-deadline'):
                kernel.wait({101}, 1)
        wait.assert_called_once_with(101, native.WAIT_ALL | native.os.WNOHANG)


if __name__ == '__main__':
    unittest.main()
