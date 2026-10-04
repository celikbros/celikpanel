#!/usr/bin/env python3
"""Offline predecessor fixture tests; no VM, network or installed service action."""
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
spec = importlib.util.spec_from_file_location('tested_runtime_predecessor', HERE / 'runtime_predecessor.py')
c = importlib.util.module_from_spec(spec); sys.modules[spec.name] = c; spec.loader.exec_module(c)
g = c.guest
IDENTITY = {'nonce': 'a' * 64, 'vm_uuid': '80b9153c-fac7-503e-90e8-7c984b901a3d',
            'cell_id': 'release-recovery__abc', 'node': 'arch'}
OPERATION = '1' * 32


def candidate():
    return {'archive_sha256': g.ARCHIVE_SHA, 'commit': g.COMMIT, 'tree': g.TREE,
            'root_name': 'celikpanel-v0.1.0-alpha.81', 'provenance': 'unpublished-local-build-not-signed-agent-admission',
            'files': {'recovery-runtime/' + name: hashlib.sha256(name.encode()).hexdigest() for name in (*g.FILES, 'runtime.manifest')}}


def intent():
    paths = g.names(OPERATION)
    return {'schema': g.SCHEMA, 'identity': IDENTITY, 'operation_id': OPERATION, 'provenance': g.PROVENANCE,
            'baseline_artifacts': g.BASELINE, 'candidate': candidate(), 'archive_path': str(paths['archive']),
            'source_root': str(paths['stage'] / 'celikpanel-v0.1.0-alpha.81')}


class ContractTests(unittest.TestCase):
    def test_exact_old_artifact_and_commit_are_pinned(self):
        value = candidate(); self.assertIs(g.validate_candidate(value), value)
        for field in ('archive_sha256', 'commit', 'tree', 'root_name', 'provenance'):
            wrong = copy.deepcopy(value); wrong[field] = 'different'
            with self.subTest(field=field), self.assertRaises(ValueError): g.validate_candidate(wrong)
        del value['files']['recovery-runtime/bin/recovery']
        with self.assertRaises(ValueError): g.validate_candidate(value)

    def test_exact_identity_and_private_staging_paths(self):
        value = intent(); g.validate_plan(value, IDENTITY, OPERATION)
        for field in ('identity', 'operation_id', 'provenance', 'baseline_artifacts', 'archive_path', 'source_root'):
            wrong = copy.deepcopy(value); wrong[field] = 'wrong'
            with self.subTest(field=field), self.assertRaises(ValueError): g.validate_plan(wrong, IDENTITY, OPERATION)

    def test_no_unknown_operation_or_traversal(self):
        for value in ('', '../other', 'A' * 32, 'a' * 33, None):
            with self.subTest(value=value), self.assertRaises(ValueError): g.names(value)

    def test_only_real_enrollment_and_selected_read_only_checks(self):
        value = intent(); commands = g.native_commands(value)
        self.assertEqual(commands[0], [value['source_root'] + '/recovery-runtime/bin/recovery', 'enroll-runtime',
                                      '--source', value['source_root'] + '/recovery-runtime', '--transaction-fd', '9'])
        self.assertEqual(commands[1], [str(g.LAUNCHER), 'verify-material-support', '--layout', 'snapshot-name-sha256-v1'])
        self.assertEqual(commands[2], [str(g.LAUNCHER), 'verify-compatibility', '--mode', '--normal'])
        self.assertFalse(any('update.sh' in arg or arg == 'recover' for command in commands for arg in command))

    def test_native_launch_has_no_recovery_or_update_onfailure(self):
        argv = c.launch_argv(intent())
        self.assertIn('--unit=celikpanel-lab-runtime-predecessor-' + OPERATION + '.service', argv)
        self.assertIn('--property=RuntimeMaxSec=300', argv)
        self.assertFalse(any('OnFailure' in part for part in argv))
        self.assertEqual(argv[-2:], ['--operation-id', OPERATION])
        self.assertIn('enroll', argv)

    def test_mutation_requires_execute_before_any_vm_access(self):
        with mock.patch.object(c.lab, 'load') as load, mock.patch('sys.stderr', new=io.StringIO()):
            with self.assertRaises(SystemExit):
                c.main(['--work-root', '/var/tmp/cp-release-drill-not-run', '--node', 'arch', '--mode', 'enroll'])
            load.assert_not_called()

    def test_unknown_baseline_hash_not_signed75(self):
        value = {'installed_artifacts': g.BASELINE, 'running_artifacts': dict(g.BASELINE)}
        with mock.patch.object(c.trial, 'validate_baseline'):
            c.validate_baseline(value)
            value['running_artifacts']['agent'] = 'f' * 64
            with self.assertRaises(ValueError): c.validate_baseline(value)

    def test_guest_identity_guard_runs_before_plan_or_action(self):
        argv = ['guest_runtime_predecessor.py', '--mode', 'enroll', '--lab-nonce', IDENTITY['nonce'],
                '--vm-uuid', IDENTITY['vm_uuid'], '--cell-id', IDENTITY['cell_id'], '--node', 'arch', '--operation-id', OPERATION]
        with mock.patch.object(sys, 'argv', argv), mock.patch.object(g.probe, 'guard_guest', side_effect=g.probe.ProbeError('wrong DMI')), \
             mock.patch.object(g, 'private_json') as read, mock.patch.object(g, 'enroll') as action:
            with self.assertRaises(g.probe.ProbeError): g.main()
            read.assert_not_called(); action.assert_not_called()


@unittest.skipUnless(sys.platform == 'linux' and getattr(os, 'geteuid', lambda: -1)() == 0,
                     'real root no-follow files and native flock')
class NativeFileTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(dir='/run'); self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name); self.root.chmod(0o700)

    def test_bounded_read_refuses_fifo_symlink_hardlink_and_writable_file(self):
        real = self.root / 'real'; real.write_bytes(b'original'); real.chmod(0o600)
        self.assertEqual(g.read_file(real, 8, 0o600)[0], b'original')
        fifo = self.root / 'fifo'; os.mkfifo(fifo, 0o600)
        link = self.root / 'link'; link.symlink_to(real)
        for path in (fifo, link):
            with self.subTest(path=path.name), self.assertRaises((OSError, ValueError)): g.read_file(path, 8)
        os.link(real, self.root / 'second')
        with self.assertRaises(ValueError): g.read_file(real, 8)
        (self.root / 'second').unlink(); real.chmod(0o666)
        with self.assertRaises(ValueError): g.read_file(real, 8)

    def test_save_once_preserves_first_partial_or_complete_attempt(self):
        path = self.root / 'attempt.json'; first = g.save_once(path, {'operation': 'first'})
        before = path.stat()
        with self.assertRaises(FileExistsError): g.save_once(path, {'operation': 'second'})
        self.assertEqual(hashlib.sha256(path.read_bytes()).hexdigest(), first)
        self.assertEqual(g.identity(before), g.identity(path.stat()))
        partial = self.root / 'partial.json'; partial.write_bytes(b'{'); partial.chmod(0o600)
        with self.assertRaises(FileExistsError): g.save_once(partial, {'new': True})
        self.assertEqual(partial.read_bytes(), b'{')

    def test_unknown_transaction_evidence_blocks_enrollment_without_change(self):
        transaction = self.root / 'transaction'; transaction.mkdir(mode=0o700)
        lock = transaction / 'transaction.lock'; lock.touch(mode=0o600)
        with mock.patch.object(g, 'TRANSACTION', transaction):
            g.empty_boundary()
            for name in ('active', 'quiesce.pending', 'completion.pending', 'scheduler-restore.pending', 'unknown'):
                extra = transaction / name; extra.write_text('evidence'); extra.chmod(0o600)
                before = extra.stat()
                with self.subTest(name=name), self.assertRaises(ValueError): g.empty_boundary()
                self.assertEqual(g.identity(before), g.identity(extra.stat()))
                extra.unlink()

    def test_full_runtime_inventory_and_launcher_pair_are_verified(self):
        runtimes = self.root / 'runtimes'; runtimes.mkdir(mode=0o700)
        selection = self.root / 'selection'; launcher = self.root / 'launcher'
        value = candidate(); raw_files = {name: ('real fixture ' + name + '\n').encode() for name in g.FILES}
        manifest = b'format=celikpanel-recovery-runtime-v1\nprotocol=1\nsnapshot=6\n' + ''.join(hashlib.sha256(raw_files[name]).hexdigest() + '  ' + name + '\n' for name in sorted(g.FILES)).encode()
        digest = hashlib.sha256(manifest).hexdigest(); selected = runtimes / digest; selected.mkdir(mode=0o700)
        raw_files['runtime.manifest'] = manifest
        for name, raw in raw_files.items():
            target = selected / name; target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            target.write_bytes(raw); target.chmod(0o755 if name in g.EXECUTABLE else 0o600 if name == 'runtime.manifest' else 0o644)
            value['files']['recovery-runtime/' + name] = hashlib.sha256(raw).hexdigest()
        selection.write_text('format=celikpanel-recovery-selection-v1\nruntime=' + digest + '\n'); selection.chmod(0o600)
        launcher.write_bytes(raw_files['bin/recovery']); launcher.chmod(0o755)
        with mock.patch.object(g, 'RUNTIMES', runtimes), mock.patch.object(g, 'SELECTION', selection), mock.patch.object(g, 'LAUNCHER', launcher):
            result = g.runtime_proof(value)
            self.assertEqual(result['manifest_sha256'], digest); self.assertEqual(result['inventory']['verified_files'], 13)
            launcher.write_bytes(b'wrong launcher')
            with self.assertRaises(ValueError): g.runtime_proof(value)
            launcher.write_bytes(raw_files['bin/recovery'])
            (selected / 'extra').write_bytes(b'owner state')
            with self.assertRaises(ValueError): g.runtime_proof(value)

    def test_host_unknown_admission_preserves_once_record_and_prevents_second_launch(self):
        node = 'arch'; directory = self.root / 'evidence' / node; directory.mkdir(parents=True)
        value = intent(); stage = {'identity': IDENTITY, 'operation_id': OPERATION, 'files': value['candidate']['files']}
        path = directory / c.STAGE; path.write_bytes(c.encoded(stage)); path.chmod(0o600)
        with mock.patch.object(c.lab, 'guarded_script', side_effect=subprocess.TimeoutExpired('fixture', 30)) as launch:
            with self.assertRaises(ValueError): c.enroll(self.root, {}, {}, node, value)
            self.assertTrue((directory / c.START).is_file())
            first = (directory / c.START).read_bytes()
            with self.assertRaises(ValueError): c.enroll(self.root, {}, {}, node, value)
            self.assertEqual((directory / c.START).read_bytes(), first)
            self.assertEqual(launch.call_count, 1)

    def test_enrollment_child_receives_real_exclusive_fd9_and_duplicate_is_refused(self):
        transaction = self.root / 'transaction'; transaction.mkdir(mode=0o700)
        lock = transaction / 'transaction.lock'; lock.touch(mode=0o600)
        value = intent(); paths = {name: self.root / name for name in ('proof', 'attempt', 'result', 'log')}
        proof = {'identity': IDENTITY, 'operation_id': OPERATION, 'files': value['candidate']['files'], 'root': value['source_root']}
        g.save_once(paths['proof'], proof)
        observed = []
        def native(argv, **kwargs):
            self.assertEqual(kwargs['pass_fds'], (9,)); self.assertEqual(os.fstat(9).st_ino, lock.stat().st_ino)
            other = os.open(lock, os.O_RDWR)
            try:
                with self.assertRaises(BlockingIOError): g.fcntl.flock(other, g.fcntl.LOCK_EX | g.fcntl.LOCK_NB)
            finally:
                os.close(other)
            observed.append(argv)
            return subprocess.CompletedProcess(argv, 0)
        with mock.patch.object(g, 'TRANSACTION', transaction), mock.patch.object(g, 'no_selected_runtime'), \
             mock.patch.object(g, 'baseline', return_value={'unchanged': True}), mock.patch.object(g, 'tree_proof'), \
             mock.patch.object(g, 'runtime_proof', return_value={'manifest_sha256': 'a' * 64}), mock.patch.object(g.subprocess, 'run', side_effect=native):
            result = g.enroll(value, paths)
            self.assertEqual(result['status'], 'verified'); self.assertEqual(observed, list(g.native_commands(value)))
            with self.assertRaises(FileExistsError): g.enroll(value, paths)
            self.assertEqual(len(observed), 3)
        self.assertEqual(set(p.name for p in transaction.iterdir()), {'transaction.lock'})


if __name__ == '__main__':
    unittest.main()
