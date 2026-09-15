"""Exchange boundary tests; actual kernel cases own their newly forked child.

Mock cases replace only the OS/callback boundaries. Actual child cases establish
renameat2 stop semantics, not native product registration or recovery acceptance.
"""
import copy
import ctypes
import errno
import json
import os
from pathlib import Path
import platform
import signal
import stat
import shutil
import subprocess
import sys
import tempfile
import time
import unittest

import native_trace as native
import exchange_trace as exchange
from test_native_trace import Kernel as WalKernel, Callbacks as WalCallbacks, REG, OP, TOKEN, EXPECTED as WAL_EXPECTED, task, stop

EXPECTED = {'operation_id': OP, 'token_sha256': TOKEN, 'worker_start_ticks': 200,
            'snapshot': WAL_EXPECTED['snapshot'],
            'checker_path': '/usr/libexec/celikpanel/recovery-runtimes/v1/' + '3' * 64 + '/bin/panel-checker',
            'checker_sha256': '4' * 64}
ENTRY = {'op': 'entry', 'number': 316, 'args': (8, 4096, 9, 8192, 2, 0)}
EXIT = {'op': 'exit', 'result': 0, 'is_error': False}


class Kernel(WalKernel):
    def __init__(self):
        super().__init__()
        self.executable_value['sha256'] = EXPECTED['checker_sha256']
        self.command_value = exchange.checker_command(EXPECTED)
        self.names = {4096: exchange.NAME, 8192: exchange.NAME}
        self.directories = {'old_dirfd': {'path': '/var/lib/celikpanel', 'identity': {'dev': 1, 'ino': 3}},
                            'new_dirfd': {'path': '/var/lib/celikpanel/.release-db-migrations/build',
                                          'identity': {'dev': 1, 'ino': 4}}}
        self.exit_value, self.exit_status = dict(EXIT), stop(sig=native.SYSCALL_STOP)
        self.step_hook = None
    def command(self, tid): return self.command_value
    def exchange_names(self, tid, arguments):
        for key in ('old_pointer', 'new_pointer'):
            native.require(self.names.get(arguments[key], b'')[:len(exchange.NAME)] == exchange.NAME, 'exchange-name-not-fixed-database')
        return {'old_name': 'celikpanel.db', 'new_name': 'celikpanel.db'}
    def exchange_directories(self, tid, arguments): return copy.deepcopy(self.directories)
    def resume(self, tid, sig=0, *, syscalls=False):
        super().resume(tid, sig, syscalls=syscalls)
        if syscalls and self.syscalls.get(tid) == ENTRY:
            if self.step_hook: self.step_hook(tid)
            self.syscalls[tid] = dict(self.exit_value)
            self.queue.append((tid, self.exit_status))


class Callbacks(WalCallbacks):
    def __init__(self):
        super().__init__()
        self.expected = dict(EXPECTED)
        self.entry_result = None
        self.entry_hook = None
    def exchange_entry(self, proof):
        self.calls.append(('entry',))
        if self.entry_hook: self.entry_hook(proof)
        if self.entry_result is not None: return self.entry_result
        return {'status': 'verified', 'operation_id': OP, 'trace_sha256': native.proof_digest(proof),
                'bound_record_sha256': '5' * 64}
    def authorize_cut(self, proof, entry):
        self.calls.append(('authorize',))
        if self.authorize_hook: self.authorize_hook(proof, entry)
        if self.authorize_result is not None: return self.authorize_result
        return {'status': 'verified', 'operation_id': OP, 'trace_sha256': native.proof_digest(proof),
                'entry_proof_sha256': native.proof_digest(entry)}


class ExchangeTraceTests(unittest.TestCase):
    def make(self, sibling=False):
        kernel, callbacks = Kernel(), Callbacks()
        tracer = exchange.ExchangeTrace(REG, callbacks, timeout=30, kernel=kernel)
        kernel.processes[100].update(tracer_pid=os.getpid(), state='t', uids=(0,) * 4, gids=(0,) * 4)
        tracer.remember(100); tracer.stopped.add(100); tracer.syscall_tgids.add(100)
        tracer.gate_released = True
        if sibling:
            kernel.processes[101] = task(101, 100, 201, traced=True)
            kernel.processes[101].update(uids=(0,) * 4, gids=(0,) * 4)
            tracer.remember(101)
        kernel.syscalls[100] = copy.deepcopy(ENTRY)
        boundary = tracer.consume(100, stop(sig=native.SYSCALL_STOP))
        return tracer, kernel, callbacks, boundary

    def assert_no_cut(self, callbacks):
        self.assertNotIn(('cut',), callbacks.calls)

    def test_closed_expectation_shape_and_selected_namespace(self):
        for change in ({'extra': 1}, {'checker_path': '/opt/celikpanel/bin/panel'},
                       {'checker_sha256': 'x' * 64}, {'snapshot': '../bad'},
                       {'token_sha256': 'x' * 64}, {'worker_start_ticks': 201}):
            tracer, kernel, callbacks, _ = self.make()
            callbacks.expected.update(change)
            with self.subTest(change=change), self.assertRaises(native.Inconclusive):
                tracer.refresh_expected(force=True)
            self.assert_no_cut(callbacks)

    def test_only_exact_checker_exec_is_traced(self):
        for bad in ('arguments', 'digest', 'uid', 'gid', 'unavailable', None):
            tracer, kernel, callbacks, _ = self.make()
            tracer.syscall_tgids.clear()
            kernel.messages[100] = 100
            if bad == 'arguments': kernel.command_value = native.COMMAND
            if bad == 'digest': kernel.executable_value['sha256'] = '0' * 64
            if bad == 'uid': kernel.processes[100]['uids'] = (1,) * 4
            if bad == 'gid': kernel.processes[100]['gids'] = (1,) * 4
            if bad == 'unavailable': callbacks.expected = None
            if bad in ('digest', 'uid', 'gid'):
                with self.assertRaises(native.Inconclusive): tracer.consume(100, stop(native.EVENT_EXEC))
            else:
                tracer.consume(100, stop(native.EVENT_EXEC))
                self.assertEqual(bool(tracer.syscall_tgids), bad is None)

    def test_exact_syscall_flags_descriptors_and_bounded_names(self):
        self.assertIsNotNone(exchange.exchange_arguments(ENTRY))
        for syscall in (18, 82, 264):
            self.assertIsNone(exchange.exchange_arguments({**ENTRY, 'number': syscall}))
        for flags in (0, 1, 3, 4, True):
            self.assertIsNone(exchange.exchange_arguments({**ENTRY, 'args': (8, 4096, 9, 8192, flags, 0)}))
        for args in ((-100, 4096, 9, 8192, 2, 0), (8, 4096, 8, 8192, 2, 0),
                     (8, 0, 9, 8192, 2, 0), (8, 1 << 64, 9, 8192, 2, 0),
                     (True, 4096, 9, 8192, 2, 0)):
            with self.assertRaises(native.Inconclusive): exchange.exchange_arguments({**ENTRY, 'args': args})

    def test_exit_requires_matched_entry_exact_integer_zero(self):
        self.assertEqual(exchange.successful_exchange(ENTRY, EXIT), exchange.exchange_arguments(ENTRY))
        self.assertIsNone(exchange.successful_exchange(None, EXIT))
        for value in ({'result': -2, 'is_error': True}, {'result': False}, {'result': 1},
                      {'result': 0.0}, {'is_error': 0}, {'op': 'entry'}):
            with self.assertRaises(native.Inconclusive): exchange.successful_exchange(ENTRY, {**EXIT, **value})

    def test_all_family_held_and_only_publisher_steps_to_exit(self):
        tracer, kernel, callbacks, boundary = self.make(sibling=True)
        def entry_check(proof):
            self.assertEqual(tracer.stopped, {100, 101})
            self.assertEqual(proof['exchange']['stage'], 'entry')
            self.assertIsNone(proof['exchange']['result'])
        callbacks.entry_hook = entry_check
        def step_check(tid):
            self.assertEqual(tid, 100)
            self.assertIn(101, tracer.stopped)
            self.assertEqual(kernel.processes[101]['state'], 't')
        kernel.step_hook = step_check
        result = tracer.observe_boundary(100, boundary)
        self.assertEqual(result['status'], 'cut-sent')
        self.assertEqual([c for c in kernel.calls if c[0] == 'resume'], [('resume', 100, 0, True)])
        self.assertEqual(result['trace']['tasks'], result['entry_trace']['tasks'])
        self.assertEqual(result['trace']['exchange']['result'], 0)
        self.assertEqual(callbacks.calls, [('revalidate', 'before-exchange-entry-proof'), ('entry',),
                         ('revalidate', 'before-cut-proof'), ('authorize',),
                         ('revalidate', 'immediately-before-cut'), ('cut',)])
        self.assertNotIn('pointer', json.dumps(result))
        self.assertTrue(tracer.cleanup()['complete'])

    def test_name_truncation_or_another_database_prevents_step(self):
        for name in (exchange.NAME[:-1], b'other.db\0', b'celikpanel.dbX\0'):
            tracer, kernel, callbacks, boundary = self.make()
            kernel.names[4096] = name
            with self.assertRaises(native.Inconclusive): tracer.observe_boundary(100, boundary)
            self.assertFalse(any(c[0] == 'resume' for c in kernel.calls))
            self.assert_no_cut(callbacks)

    def test_entry_proof_requires_verified_exact_operation_and_trace(self):
        for result in ({}, {'status': 'inconclusive'}, {'status': 'verified', 'operation_id': '0' * 32},
                       {'status': 'verified', 'operation_id': OP, 'trace_sha256': '0' * 64}):
            tracer, kernel, callbacks, boundary = self.make()
            callbacks.entry_result = result
            with self.assertRaises(native.Inconclusive): tracer.observe_boundary(100, boundary)
            self.assertFalse(any(c[0] == 'resume' for c in kernel.calls))
            self.assert_no_cut(callbacks)

    def test_entry_callback_changes_refuse_before_any_syscall_progress(self):
        for change in ('trace', 'names', 'directory', 'admission', 'entry', 'task'):
            tracer, kernel, callbacks, boundary = self.make()
            def mutate(proof):
                if change == 'trace': proof['exchange']['flags'] = 1
                elif change == 'names': kernel.names[8192] = b'bad'
                elif change == 'directory': kernel.directories['old_dirfd']['identity']['ino'] += 1
                elif change == 'admission': callbacks.expected = None
                elif change == 'entry': kernel.syscalls[100] = {**ENTRY, 'number': 1}
                else: kernel.processes[100]['start_ticks'] += 1
            callbacks.entry_hook = mutate
            with self.subTest(change=change), self.assertRaises(native.Inconclusive): tracer.observe_boundary(100, boundary)
            self.assertFalse(any(c[0] == 'resume' for c in kernel.calls))
            self.assert_no_cut(callbacks)

    def test_failed_or_interrupted_syscall_does_not_cut_and_cleanup_forwards_signal(self):
        for kind in ('failed', 'signal', 'exit', 'deadline'):
            tracer, kernel, callbacks, boundary = self.make(sibling=True)
            if kind == 'failed': kernel.exit_value = {'op': 'exit', 'result': -4, 'is_error': True}
            elif kind == 'signal': kernel.exit_status = stop(sig=signal.SIGURG)
            elif kind == 'exit': kernel.exit_status = 0
            else:
                original = kernel.resume
                def resume(*args, **kwargs):
                    original(*args, **kwargs)
                    kernel.queue.clear()
                kernel.resume = resume
            with self.subTest(kind=kind), self.assertRaises(native.Inconclusive): tracer.observe_boundary(100, boundary)
            self.assert_no_cut(callbacks)
            self.assertTrue(tracer.cleanup()['complete'])
            if kind == 'signal': self.assertIn(('detach', 100, signal.SIGURG), kernel.calls)

    def test_postexchange_pair_authority_refusal_never_cuts(self):
        tracer, kernel, callbacks, boundary = self.make()
        callbacks.authorize_result = {'status': 'inconclusive', 'reason': 'publication-receipt-already-exists'}
        with self.assertRaises(native.Inconclusive): tracer.observe_boundary(100, boundary)
        self.assert_no_cut(callbacks)
        self.assertTrue(tracer.cleanup()['complete'])

    def test_postexchange_callback_cannot_change_bound_proofs_or_stopped_identity(self):
        for change in ('trace', 'entry-proof', 'directory', 'names', 'admission', 'syscall', 'task'):
            tracer, kernel, callbacks, boundary = self.make()
            def mutate(proof, entry):
                if change == 'trace': proof['exchange']['result'] = 1
                elif change == 'entry-proof': entry['bound_record_sha256'] = '0' * 64
                elif change == 'directory': kernel.directories['new_dirfd']['identity']['ino'] += 1
                elif change == 'names': kernel.names[4096] = b'bad'
                elif change == 'admission': callbacks.expected['checker_sha256'] = '0' * 64
                elif change == 'syscall': kernel.syscalls[100] = {**EXIT, 'result': -1, 'is_error': True}
                else: kernel.processes[100]['start_ticks'] += 1
            callbacks.authorize_hook = mutate
            with self.subTest(change=change), self.assertRaises(native.Inconclusive): tracer.observe_boundary(100, boundary)
            self.assert_no_cut(callbacks)

    def test_exit_proof_must_bind_raw_entry_proof_digest(self):
        tracer, kernel, callbacks, boundary = self.make()
        def authorize(proof, entry):
            return {'status': 'verified', 'operation_id': OP, 'trace_sha256': native.proof_digest(proof),
                    'entry_proof_sha256': '0' * 64}
        callbacks.authorize_cut = authorize
        with self.assertRaises(native.Inconclusive): tracer.observe_boundary(100, boundary)
        self.assert_no_cut(callbacks)

    def test_final_revalidate_mutation_refuses_cut(self):
        tracer, kernel, callbacks, boundary = self.make()
        original = callbacks.revalidate
        def revalidate(stage, proof):
            original(stage, proof)
            if stage == 'immediately-before-cut': kernel.names[4096] = b'bad'
        callbacks.revalidate = revalidate
        with self.assertRaises(native.Inconclusive): tracer.observe_boundary(100, boundary)
        self.assert_no_cut(callbacks)

    def test_complete_driver_reuses_gate_exec_stop_and_detach_lifecycle(self):
        kernel, callbacks = Kernel(), Callbacks()
        kernel.processes[100].update(uids=(0,) * 4, gids=(0,) * 4)
        kernel.executable_value['sha256'] = REG['gate_executable_sha256']
        tracer = exchange.ExchangeTrace(REG, callbacks, timeout=30, kernel=kernel)
        steps = [0]
        def resume(tid, sig=0, *, syscalls=False):
            WalKernel.resume(kernel, tid, sig, syscalls=syscalls)
            steps[0] += 1
            if steps[0] == 1:
                kernel.executable_value['sha256'] = EXPECTED['checker_sha256']
                kernel.messages[100] = 100
                event = stop(native.EVENT_EXEC)
            elif steps[0] == 2:
                kernel.syscalls[100] = copy.deepcopy(ENTRY)
                event = stop(sig=native.SYSCALL_STOP)
            elif steps[0] == 3:
                kernel.syscalls[100] = dict(EXIT)
                event = stop(sig=native.SYSCALL_STOP)
            else:
                self.fail('unexpected resume past the held exit')
            kernel.queue.append((tid, event))
        kernel.resume = resume
        result = tracer.run()
        self.assertEqual(result['status'], 'cut-sent', result)
        self.assertTrue(result['cleanup']['complete'])
        self.assertEqual(result['cleanup']['detached'], [100])
        self.assertEqual([c[3] for c in kernel.calls if c[0] == 'resume'], [False, True, True])
        self.assertEqual(callbacks.calls.count(('cut',)), 1)

    def test_isolated_import_works_for_repository_and_flat_pinned_upload(self):
        root = Path(exchange.__file__).resolve().parent
        flat = Path(tempfile.mkdtemp(prefix='celikpanel-exchange-import-'))
        for name in ('exchange_trace.py', 'native_trace.py'):
            shutil.copy2(root / name, flat / name)
        for name in ('wal_frames.py', 'wal_migration_identity.py'):
            source = root / name if (root / name).exists() else root.parent / name
            shutil.copy2(source, flat / name)
        code = ("import importlib.util,sys; "
                "s=importlib.util.spec_from_file_location('native_boundary_tracer',sys.argv[1]); "
                "m=importlib.util.module_from_spec(s); s.loader.exec_module(m); "
                "assert callable(m.trace_database_publication)")
        for path in (root / 'exchange_trace.py', flat / 'exchange_trace.py'):
            run = subprocess.run([sys.executable, '-I', '-B', '-c', code, str(path)],
                                 capture_output=True, text=True, timeout=10)
            self.assertEqual(run.returncode, 0, run.stderr)

    def test_no_syscall_mutation_or_exitkill_capability_added(self):
        self.assertEqual(native.OPTIONS & 0x100000, 0)
        self.assertEqual(native.ALLOWED, frozenset((native.SEIZE, native.INTERRUPT, native.EVENT_MESSAGE,
                         native.SYSCALL_INFO, native.CONT, native.SYSCALL, native.DETACH)))


@unittest.skipUnless(platform.system() == 'Linux' and platform.machine() == 'x86_64', 'Linux amd64 syscall fixture')
class OwnedChildExchangeTests(unittest.TestCase):
    def real_exchange(self, succeeds):
        if not hasattr(os, 'pidfd_open') or not hasattr(signal, 'pidfd_send_signal'):
            self.skipTest('owned-child pidfd cleanup unavailable')
        stage = Path(tempfile.mkdtemp(prefix='celikpanel-owned-exchange-'))
        canonical, build = stage / 'canonical', stage / 'build'
        canonical.mkdir(); build.mkdir()
        (canonical / 'celikpanel.db').write_bytes(b'original-before')
        if succeeds: (build / 'celikpanel.db').write_bytes(b'candidate-after')
        before = os.stat(canonical / 'celikpanel.db')
        other = os.stat(build / 'celikpanel.db') if succeeds else None
        oldfd = os.open(canonical, os.O_RDONLY | os.O_DIRECTORY)
        newfd = os.open(build, os.O_RDONLY | os.O_DIRECTORY)
        ready_r, ready_w = os.pipe()
        name = ctypes.create_string_buffer(exchange.NAME)
        child = os.fork()
        if child == 0:
            try:
                os.close(ready_w)
                if os.read(ready_r, 1) != b'1': os._exit(99)
                libc = ctypes.CDLL(None, use_errno=True)
                libc.syscall.restype = ctypes.c_long
                result = libc.syscall(ctypes.c_long(316), ctypes.c_int(oldfd), ctypes.byref(name),
                                      ctypes.c_int(newfd), ctypes.byref(name), ctypes.c_uint(2))
                (stage / 'returned.json').write_text(json.dumps({'result': result, 'errno': ctypes.get_errno()}))
                os._exit(0)
            except BaseException:
                os._exit(98)
        os.close(ready_r)
        pidfd = os.pidfd_open(child)
        kernel = exchange.ExchangeKernel.__new__(exchange.ExchangeKernel)
        attached, reaped = False, False
        deadline = time.monotonic() + 10
        evidence = {'scope': 'new-owned-child-kernel-only', 'pid': child, 'success_requested': succeeds}
        try:
            try:
                kernel.seize(child)
            except OSError as error:
                if error.errno in (errno.EPERM, errno.ENOSYS):
                    evidence['skip'] = 'ptrace-kernel-unavailable'
                    self.skipTest(evidence['skip'])
                raise
            attached = True
            kernel.interrupt(child)
            tid, status = kernel.wait({child}, deadline)
            self.assertEqual(tid, child); self.assertTrue(os.WIFSTOPPED(status))
            os.write(ready_w, b'1')
            kernel.resume(child, syscalls=True)
            while True:
                tid, status = kernel.wait({child}, deadline)
                self.assertTrue(os.WIFSTOPPED(status), status)
                if status >> 16 == 0 and os.WSTOPSIG(status) == native.SYSCALL_STOP:
                    info = kernel.syscall(child)
                    if info.get('op') == 'entry' and info.get('number') == 316:
                        entry = info
                        break
                kernel.resume(child, syscalls=True)
            args = exchange.exchange_arguments(entry)
            self.assertEqual(kernel.exchange_names(child, args), {'old_name': 'celikpanel.db', 'new_name': 'celikpanel.db'})
            directories = kernel.exchange_directories(child, args)
            self.assertEqual(os.stat(canonical / 'celikpanel.db').st_ino, before.st_ino)
            self.assertFalse((stage / 'returned.json').exists())
            kernel.resume(child, syscalls=True)
            tid, status = kernel.wait({child}, deadline)
            self.assertEqual(tid, child)
            self.assertEqual(status >> 16, 0)
            self.assertEqual(os.WSTOPSIG(status), native.SYSCALL_STOP)
            exit_info = kernel.syscall(child)
            evidence.update(entry_number=entry['number'], exit=exit_info,
                            names=kernel.exchange_names(child, args),
                            directory_fds_unchanged=kernel.exchange_directories(child, args) == directories,
                            post_return_marker_absent=not (stage / 'returned.json').exists())
            self.assertTrue(evidence['directory_fds_unchanged'])
            self.assertTrue(evidence['post_return_marker_absent'])
            if succeeds:
                self.assertEqual(exchange.successful_exchange(entry, exit_info), args)
                self.assertEqual(os.stat(canonical / 'celikpanel.db').st_ino, other.st_ino)
                self.assertEqual(os.stat(build / 'celikpanel.db').st_ino, before.st_ino)
                self.assertEqual((canonical / 'celikpanel.db').read_bytes(), b'candidate-after')
                self.assertEqual((build / 'celikpanel.db').read_bytes(), b'original-before')
            else:
                self.assertEqual(exit_info['result'], -errno.ENOENT)
                with self.assertRaises(native.Inconclusive): exchange.successful_exchange(entry, exit_info)
                self.assertEqual(os.stat(canonical / 'celikpanel.db').st_ino, before.st_ino)
            kernel.detach(child); attached = False
            while time.monotonic() < deadline:
                got, status = os.waitpid(child, os.WNOHANG)
                if got:
                    reaped = True
                    self.assertEqual(status, 0)
                    break
                time.sleep(.002)
            self.assertTrue(reaped, 'owned child did not exit')
            evidence['status'] = 'verified'
        finally:
            if not reaped:
                # pidfd is opened from our fork result before any wait/reap.
                # This cleanup cannot select a reused or external PID.
                try: signal.pidfd_send_signal(pidfd, signal.SIGKILL)
                except ProcessLookupError: pass
                end = time.monotonic() + 5
                while time.monotonic() < end:
                    try:
                        got, status = os.waitpid(child, native.WAIT_ALL | os.WNOHANG)
                    except ChildProcessError:
                        reaped = True; break
                    if got and (os.WIFEXITED(status) or os.WIFSIGNALED(status)):
                        reaped = True; break
                    if got and os.WIFSTOPPED(status) and attached:
                        try: kernel.resume(child)
                        except ProcessLookupError: pass
                    time.sleep(.002)
            evidence['child_reaped'] = reaped
            (stage / 'proof.json').write_text(json.dumps(evidence, sort_keys=True, indent=2) + '\n')
            for fd in (pidfd, oldfd, newfd, ready_w): os.close(fd)
        self.assertTrue(reaped)

    def test_actual_successful_exchange_exit_precedes_child_return_and_swaps_inodes(self):
        self.real_exchange(True)

    def test_actual_failed_exchange_keeps_before_and_cannot_form_positive_proof(self):
        self.real_exchange(False)


if __name__ == '__main__':
    unittest.main()
