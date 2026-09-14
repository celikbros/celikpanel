#!/usr/bin/env python3
"""Offline tests for the disposable promotion cut; no VM or host service actions."""
import argparse
import copy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('tested_runtime_promotion', HERE / 'runtime_promotion_trial.py')
c = importlib.util.module_from_spec(spec); sys.modules[spec.name] = c; spec.loader.exec_module(c)
g = c.guest
IDENTITY = {'nonce': 'a' * 64, 'vm_uuid': '80b9153c-fac7-503e-90e8-7c984b901a3d', 'cell_id': 'release-recovery__abc', 'node': 'arch'}
OPERATION = '1' * 32
PREVIOUS = '2' * 64
TARGET = '3' * 64
WORKER = {'unit': 'celikpanel-lab-local-update-' + OPERATION + '.service', 'pid': 100, 'start_ticks': '200',
          'invocation_id': '4' * 32, 'cgroup': '/system.slice/celikpanel-lab-local-update-' + OPERATION + '.service', 'boot_id': 'boot'}


def file_identity(ino, raw, mode):
    return {'dev': 1, 'ino': ino, 'mode': stat.S_IFREG | mode, 'uid': 0, 'gid': 0, 'size': len(raw),
            'mtime_sec': 1, 'mtime_nsec': 2, 'ctime_sec': 3, 'ctime_nsec': 4, 'sha256': hashlib.sha256(raw).hexdigest()}


def record():
    return {'schema': 'celikpanel/recovery-promotion/v1', 'nonce': '5' * 32, 'previous': PREVIOUS, 'target': TARGET, 'mode': '--normal',
            'old_launcher': file_identity(1, b'old', 0o755), 'new_launcher': file_identity(2, b'new', 0o755),
            'old_selection': file_identity(3, g.selection(PREVIOUS), 0o600), 'new_selection': file_identity(4, g.selection(TARGET), 0o600)}


def encode(value): return (json.dumps(value, separators=(',', ':')) + '\n').encode()


def old_intent():
    old = g.old; operation = '8' * 32; paths = old.names(operation)
    candidate = {'archive_sha256': old.ARCHIVE_SHA, 'commit': old.COMMIT, 'tree': old.TREE, 'root_name': 'celikpanel-v0.1.0-alpha.81',
                 'provenance': 'unpublished-local-build-not-signed-agent-admission',
                 'files': {'recovery-runtime/' + name: hashlib.sha256(name.encode()).hexdigest() for name in (*old.FILES, 'runtime.manifest')}}
    return {'schema': old.SCHEMA, 'identity': IDENTITY, 'operation_id': operation, 'provenance': old.PROVENANCE,
            'baseline_artifacts': old.BASELINE, 'candidate': candidate, 'archive_path': str(paths['archive']),
            'source_root': str(paths['stage'] / candidate['root_name'])}


def old_result(intent):
    old = g.old; files = {name: intent['candidate']['files']['recovery-runtime/' + name] for name in (*old.FILES, 'runtime.manifest')}
    before = {name: {kind: {'sha256': digest} for kind in ('installed', 'running')} for name, digest in old.BASELINE.items()}
    return {'schema': 'celikpanel/runtime-predecessor-result/v1', 'identity': IDENTITY, 'operation_id': intent['operation_id'],
            'provenance': old.PROVENANCE, 'status': 'verified', 'before': before, 'after': copy.deepcopy(before),
            'commands': [{'argv': argv, 'exit_code': 0} for argv in old.native_commands(intent)],
            'runtime': {'manifest_sha256': files['runtime.manifest'], 'inventory': {'files': files},
                        'launcher': {'sha256': files['bin/recovery']}, 'selector': {'sha256': hashlib.sha256(g.selection(files['runtime.manifest'])).hexdigest()}}}


class ContractTests(unittest.TestCase):
    def test_exact_canonical_record(self):
        value = record(); self.assertEqual(g.parse_record(encode(value), PREVIOUS, TARGET), value)
        with self.assertRaises(g.Unavailable): g.parse_record(json.dumps(value).encode(), PREVIOUS, TARGET)
        with self.assertRaises(g.Unavailable): g.parse_record(encode(value), TARGET, PREVIOUS)
        with self.assertRaises(g.Unavailable): g.parse_record(encode(value), PREVIOUS, PREVIOUS)

    def test_unknown_duplicate_and_noncanonical_fields_refused(self):
        value = record(); value['unknown'] = 'ignored?'
        with self.assertRaises(g.Unavailable): g.parse_record(encode(value), PREVIOUS, TARGET)
        raw = encode(record()).replace(b'"nonce":', b'"nonce":"' + b'5' * 32 + b'","nonce":', 1)
        with self.assertRaises(g.probe.ProbeError): g.parse_record(raw, PREVIOUS, TARGET)
        value = record(); value['old_launcher']['unknown'] = 1
        with self.assertRaises(g.Unavailable): g.parse_record(encode(value), PREVIOUS, TARGET)

    def test_metadata_and_selection_binding_rejections(self):
        edits = (('uid', 1000), ('gid', 1000), ('mode', stat.S_IFREG | 0o777), ('size', 0), ('ino', 0), ('mtime_nsec', 10**9), ('ctime_nsec', -1), ('sha256', 'A' * 64), ('dev', True))
        for field, wrong in edits:
            value = record(); value['new_launcher'][field] = wrong
            with self.subTest(field=field), self.assertRaises(g.Unavailable): g.parse_record(encode(value), PREVIOUS, TARGET)
        value = record(); value['new_selection']['sha256'] = '9' * 64
        with self.assertRaises(g.Unavailable): g.parse_record(encode(value), PREVIOUS, TARGET)
        value = record(); value['new_launcher']['ino'] = value['old_launcher']['ino']
        with self.assertRaises(g.Unavailable): g.parse_record(encode(value), PREVIOUS, TARGET)

    def test_predecessor_requires_actual_enrollment_result_and_full_inventory(self):
        intent = old_intent(); result = old_result(intent); g.validate_predecessor(intent, result, IDENTITY)
        for field, wrong in (('status', 'unavailable'), ('identity', {}), ('operation_id', OPERATION), ('commands', []), ('after', {})):
            value = copy.deepcopy(result); value[field] = wrong
            with self.subTest(field=field), self.assertRaises(g.Unavailable): g.validate_predecessor(intent, value, IDENTITY)
        value = copy.deepcopy(result); value['runtime']['inventory']['files'].pop('bin/recovery')
        with self.assertRaises(g.Unavailable): g.validate_predecessor(intent, value, IDENTITY)

    def test_start_durably_consumes_shared_marker_before_single_launch(self):
        intent = {'identity': IDENTITY, 'operation_id': OPERATION, 'source_root': '/fixed/source'}
        spec = {'fixture': 'sealed'}
        arm = {'identity': IDENTITY, 'operation_id': OPERATION, 'intent_sha256': hashlib.sha256(c.encoded(spec)).hexdigest()}
        state = {'fault': {'ActiveState': 'active'}, 'worker': {'LoadState': 'not-found'}}
        trace = []
        with mock.patch.object(c.local, 'assert_absent'), mock.patch.object(c.trial, 'read_private', return_value=c.encoded(arm)), \
             mock.patch.object(c, 'read_guest', return_value=(state, b'', [{'event': 'armed'}])), \
             mock.patch.object(c.trial, 'save', side_effect=lambda *args: trace.append(('saved', args[2]))), \
             mock.patch.object(c.local, 'launch_once', side_effect=lambda *args: trace.append(('launch', args[-1]))), \
             mock.patch('sys.stdout', new=io.StringIO()):
            c.start(Path('/fixture'), {}, {}, 'arch', intent, spec)
        self.assertEqual(trace, [('saved', c.local.START), ('launch', 'start')])
        with mock.patch.object(c.local, 'assert_absent'), mock.patch.object(c.trial, 'read_private', return_value=c.encoded(arm)), \
             mock.patch.object(c, 'read_guest', return_value=(state, b'', [{'event': 'armed'}])), \
             mock.patch.object(c.trial, 'save', side_effect=FileExistsError), mock.patch.object(c.local, 'launch_once') as launch:
            with self.assertRaises(FileExistsError): c.start(Path('/fixture'), {}, {}, 'arch', intent, spec)
            launch.assert_not_called()

    def test_guarded_cleanup_and_finite_watch_unit(self):
        argv = c.launch_argv({'identity': IDENTITY, 'operation_id': OPERATION})
        self.assertIn('--property=RuntimeMaxSec=650', argv)
        cleanup = next(arg for arg in argv if arg.startswith('--property=ExecStopPost='))
        self.assertIn('guest_runtime_promotion_fault.py', cleanup); self.assertIn('--mode cleanup', cleanup)
        self.assertNotIn('systemctl thaw', cleanup)
        self.assertIn(OPERATION, cleanup); self.assertIn(IDENTITY['nonce'], cleanup)
        self.assertFalse(any('OnFailure=' in arg for arg in argv))

    def test_mutations_require_execute_before_any_lab_access(self):
        for mode in ('prepare', 'arm', 'start', 'owner-resume'):
            with self.subTest(mode=mode), mock.patch.object(c.lab, 'load') as load, mock.patch('sys.stderr', new=io.StringIO()):
                with self.assertRaises(SystemExit): c.main(['--work-root', '/var/tmp/not-run', '--node', 'arch', '--mode', mode])
                load.assert_not_called()

    def test_guest_identity_guard_precedes_plan_and_signal(self):
        argv = ['--mode', 'fault', '--lab-nonce', IDENTITY['nonce'], '--vm-uuid', IDENTITY['vm_uuid'], '--cell-id', IDENTITY['cell_id'], '--node', 'arch', '--operation-id', OPERATION]
        with mock.patch.object(g.probe, 'guard_guest', side_effect=g.probe.ProbeError('wrong DMI')), mock.patch.object(g, 'native_from') as load:
            with self.assertRaises(g.probe.ProbeError): g.main(argv)
            load.assert_not_called()

    def test_only_armed_exact_watcher_allows_start(self):
        state = {'fault': {'ActiveState': 'active'}, 'worker': {'LoadState': 'not-found'}}
        c.require_armed(state, [{'event': 'armed'}], {})
        for values in ([], [{'event': 'inconclusive'}], [{'event': 'armed'}, {'event': 'freeze_requested'}]):
            with self.subTest(values=values), self.assertRaises(ValueError): c.require_armed(state, values, {})
        state['worker']['LoadState'] = 'loaded'
        with self.assertRaises(ValueError): c.require_armed(state, [{'event': 'armed'}], {})


class FakeNative:
    previous = PREVIOUS; target = TARGET
    def __init__(self):
        self.trace = []; self.props = {'ActiveState': 'active'}; self.point = {'record': record(), 'file': {'sha256': 'a' * 64}}
        self.lock = True; self.failure = None; self.checks = 0
    def properties(self): return self.props
    def checkpoint(self):
        self.checks += 1
        if self.failure == 'late-checkpoint' and self.checks > 1: return None
        return self.point
    def worker_identity(self): return WORKER
    def owns_transaction_lock(self, props): return self.lock
    def freeze(self):
        self.trace.append('freeze')
        if self.failure == 'freeze': raise g.Unavailable('freeze-outcome-unknown')
    def revalidate(self, worker):
        self.trace.append('revalidate')
        if self.failure == 'identity': raise g.Unavailable('replaced-worker')
    def proof(self, checkpoint, tick):
        self.trace.append('proof'); tick()
        if self.failure == 'proof': raise g.Unavailable('full-proof-failed')
        if self.failure == 'lost-lock': self.lock = False
        return {'checkpoint': checkpoint, 'both_kits': 'fully verified'}
    def kill(self):
        self.trace.append('kill')
        if self.failure == 'kill': raise g.Unavailable('kill-outcome-unknown')
    def safe_thaw(self, worker): self.trace.append('thaw'); return 'thawed'


class WatchTests(unittest.TestCase):
    def run_fixture(self, native, **kwargs):
        values = []; result = g.run_watch(native, lambda event, **fields: values.append({'event': event, **fields}), **kwargs)
        return result, values

    def test_full_proof_precedes_exact_one_kill_and_cleanup(self):
        native = FakeNative(); result, values = self.run_fixture(native)
        self.assertEqual(result, 'fault-applied-native-outcome-unconfirmed')
        self.assertEqual([item['event'] for item in values], ['armed', 'freeze_requested', 'promotion_checkpoint_verified', 'kill_attempt', 'kill_sent', 'cleanup'])
        self.assertLess(native.trace.index('proof'), native.trace.index('kill')); self.assertEqual(native.trace.count('kill'), 1)
        self.assertEqual(native.trace[-1], 'thaw')

    def test_lock_required_before_freeze(self):
        native = FakeNative(); native.lock = False; result, values = self.run_fixture(native)
        self.assertEqual(result, 'inconclusive'); self.assertEqual(native.trace, [])
        self.assertNotIn('kill_sent', [item['event'] for item in values])

    def test_changed_or_unproven_checkpoint_never_killed(self):
        for failure in ('freeze', 'identity', 'proof', 'lost-lock', 'late-checkpoint'):
            native = FakeNative(); native.failure = failure
            with self.subTest(failure=failure):
                result, values = self.run_fixture(native)
                self.assertEqual(result, 'inconclusive'); self.assertNotIn('kill', native.trace)
                self.assertEqual(native.trace[-1], 'thaw'); self.assertNotIn('kill_sent', [v['event'] for v in values])

    def test_unknown_signal_outcome_is_not_retried_or_declared_applied(self):
        native = FakeNative(); native.failure = 'kill'; result, values = self.run_fixture(native)
        self.assertEqual(result, 'inconclusive'); self.assertEqual(native.trace.count('kill'), 1)
        self.assertEqual([v['event'] for v in values][-3:], ['kill_attempt', 'inconclusive', 'cleanup'])

    def test_missed_window_and_interruption_never_fabricated(self):
        for interrupted in (False, True):
            native = FakeNative(); native.props = {'ActiveState': 'inactive'}
            clock = iter((0, 601))
            result, values = self.run_fixture(native, clock=lambda: next(clock), pause=lambda _: None, interrupted=lambda: interrupted)
            self.assertEqual(result, 'inconclusive'); self.assertEqual(native.trace, [])

    def test_worker_exit_after_seen_is_inconclusive(self):
        native = FakeNative(); native.point = None
        states = iter(({'ActiveState': 'active'}, {'ActiveState': 'failed'})); native.properties = lambda: next(states)
        result, values = self.run_fixture(native, pause=lambda _: None)
        self.assertEqual(result, 'inconclusive'); self.assertEqual(native.trace, [])

    def test_cleanup_refuses_replacement_invocation_before_thaw(self):
        native = g.Native.__new__(g.Native)
        native.properties = lambda: {'Id': WORKER['unit'], 'InvocationID': 'different', 'MainPID': '100', 'ControlGroup': WORKER['cgroup']}
        native.thaw = mock.Mock()
        with mock.patch.object(Path, 'read_text', return_value='boot\n'):
            self.assertEqual(native.safe_thaw(WORKER), 'refused-replacement-identity')
        native.thaw.assert_not_called()

    def test_cleanup_can_thaw_exact_dead_invocation(self):
        native = g.Native.__new__(g.Native)
        native.properties = lambda: {'Id': WORKER['unit'], 'InvocationID': WORKER['invocation_id'], 'MainPID': '0', 'ControlGroup': WORKER['cgroup']}
        native.thaw = mock.Mock(return_value=0)
        with mock.patch.object(Path, 'read_text', return_value='boot\n'):
            self.assertEqual(native.safe_thaw(WORKER), 'thawed')
        native.thaw.assert_called_once()


@unittest.skipUnless(sys.platform == 'linux' and getattr(os, 'geteuid', lambda: -1)() == 0, 'real root no-follow file fixtures')
class NativeFileTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(dir='/run'); self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name); self.root.chmod(0o700)
        self.launcher = self.root / 'recovery'; self.selector = self.root / 'selection'
        self.promotions = self.root / 'promotions'; self.promotions.mkdir(mode=0o700)
        self.current = self.promotions / 'current'; self.current.mkdir(mode=0o700)
        for obj, name, value in ((g.old, 'LAUNCHER', self.launcher), (g.old, 'SELECTION', self.selector), (g, 'PROMOTIONS', self.promotions)):
            patch = mock.patch.object(obj, name, value); patch.start(); self.addCleanup(patch.stop)
        self.value = record(); paths = ((self.launcher, 'old_launcher', b'old', 0o755),
            (Path(str(self.launcher) + '.promotion-' + self.value['nonce']), 'new_launcher', b'new', 0o755),
            (self.selector, 'old_selection', g.selection(PREVIOUS), 0o600),
            (Path(str(self.selector) + '.promotion-' + self.value['nonce']), 'new_selection', g.selection(TARGET), 0o600))
        for path, key, raw, mode in paths:
            path.write_bytes(raw); path.chmod(mode); info = path.stat()
            self.value[key] = {'dev': info.st_dev, 'ino': info.st_ino, 'mode': info.st_mode, 'uid': info.st_uid, 'gid': info.st_gid, 'size': info.st_size,
                               'mtime_sec': info.st_mtime_ns // 10**9, 'mtime_nsec': info.st_mtime_ns % 10**9,
                               'ctime_sec': info.st_ctime_ns // 10**9, 'ctime_nsec': info.st_ctime_ns % 10**9, 'sha256': hashlib.sha256(raw).hexdigest()}
        self.intent = self.current / 'intent.json'; self.intent.write_bytes(encode(self.value)); self.intent.chmod(0o600)

    def swap_launcher(self):
        stage = Path(str(self.launcher) + '.promotion-' + self.value['nonce']); temporary = self.root / 'exchange-test'
        self.launcher.rename(temporary); stage.rename(self.launcher); temporary.rename(stage)

    def test_real_pair_accepts_only_new_launcher_old_selector(self):
        proof = g.read_record(PREVIOUS, TARGET)
        self.assertIsNone(g.mixed_pair(proof))
        self.swap_launcher(); self.assertTrue(g.mixed_pair(proof))
        verified = g.mixed_pair(proof, full=True); self.assertEqual(len(verified), 4)
        self.assertEqual(verified[str(self.launcher)]['sha256'], self.value['new_launcher']['sha256'])
        self.assertEqual(self.selector.read_bytes(), g.selection(PREVIOUS))

    def test_moved_ctime_only_allowed_for_launcher_pair(self):
        self.swap_launcher(); proof = g.read_record(PREVIOUS, TARGET)
        self.assertTrue(g.mixed_pair(proof))
        self.selector.chmod(0o640); self.selector.chmod(0o600)
        self.assertIsNone(g.mixed_pair(proof))

    def test_content_change_even_same_size_and_mtime_refused(self):
        self.swap_launcher(); proof = g.read_record(PREVIOUS, TARGET); before = self.launcher.stat()
        self.launcher.write_bytes(b'bad'); os.utime(self.launcher, ns=(before.st_atime_ns, before.st_mtime_ns))
        with self.assertRaises(g.Unavailable): g.mixed_pair(proof, full=True)

    def test_links_fifo_or_writable_record_never_mean_absent(self):
        self.intent.unlink(); os.mkfifo(self.intent, 0o600)
        with self.assertRaises(ValueError): g.read_record(PREVIOUS, TARGET)
        self.intent.unlink(); self.intent.symlink_to(self.selector)
        with self.assertRaises(OSError): g.read_record(PREVIOUS, TARGET)
        self.intent.unlink(); self.intent.write_bytes(encode(self.value)); self.intent.chmod(0o666)
        with self.assertRaises(ValueError): g.read_record(PREVIOUS, TARGET)

    def test_missing_and_unsafe_ancestor_are_distinct(self):
        self.intent.unlink(); self.current.rmdir(); self.assertIsNone(g.read_record(PREVIOUS, TARGET))
        self.current.symlink_to(self.root)
        with self.assertRaises(g.Unavailable): g.read_record(PREVIOUS, TARGET)

    def test_unknown_journal_object_and_commit_refuse_window(self):
        unknown = self.current / 'foreign'; unknown.write_bytes(b''); unknown.chmod(0o600)
        with self.assertRaises(g.Unavailable): g.read_record(PREVIOUS, TARGET)
        unknown.unlink(); committed = self.current / 'committed.json'; committed.write_bytes(b''); committed.chmod(0o600)
        with self.assertRaises(g.Unavailable): g.read_record(PREVIOUS, TARGET)

    def test_partial_first_attempt_is_never_overwritten(self):
        path = self.root / 'attempt.json'; path.write_bytes(b'{partial'); path.chmod(0o600)
        with self.assertRaises(FileExistsError): g.old.save_once(path, {'second': 'attempt'})
        self.assertEqual(path.read_bytes(), b'{partial')

if __name__ == '__main__': unittest.main()
