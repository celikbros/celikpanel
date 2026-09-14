"""Real Linux filesystem evidence for the disposable retained-data fault."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location('tested_candidate_data_fault', Path(__file__).with_name('guest_candidate_data_fault.py'))
s = importlib.util.module_from_spec(SPEC); sys.modules[SPEC.name] = s; SPEC.loader.exec_module(s)


class Interrupted(BaseException): pass


@unittest.skipUnless(sys.platform == 'linux' and getattr(os, 'geteuid', lambda: -1)() == 0,
                     'root-owned real Linux filesystem required')
class FilesystemTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='candidate-data-fixture-', dir='/root')
        self.addCleanup(self.temp.cleanup); self.base = Path(self.temp.name)
        self.releases = self.base / 'releases'; self.releases.mkdir(mode=0o700)
        self.root = self.releases / ('a' * 12 + '-' + 'b' * 24); self.root.mkdir(mode=0o700)
        self.q = self.base / 'quarantine'; self.op = 'd' * 32
        self.ident = {'nonce': 'c' * 64, 'vm_uuid': 'fixture', 'cell_id': 'fixture', 'node': 'arch'}
        self.args = argparse.Namespace(operation_id=self.op)
        self.bytes = {name: ('original:' + name).encode() for name in (*s.TARGETS, 'update.sh', 'bin/panel')}
        self.bytes['SHA256SUMS'] = ''.join(hashlib.sha256(raw).hexdigest() + '  ./' + name + '\n'
                                        for name, raw in sorted(self.bytes.items())).encode()
        for name, raw in self.bytes.items():
            path = self.root / name; path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
            path.write_bytes(raw); path.chmod(0o600)
        self.files = {name: hashlib.sha256(raw).hexdigest() for name, raw in self.bytes.items()}
        self.protected = self.base / 'installed'; self.protected.write_bytes(b'owner bytes'); self.protected.chmod(0o600)
        self.plan = {'identity': self.ident, 'operation_id': self.op, 'candidate_data_fault': s.OPTION,
                     'candidate': {'commit': 'a' * 40, 'files': self.files, 'manifest_sha256': self.files['SHA256SUMS']}}
        self.snapshot = {'snapshot': 'snapshot', 'manifest_sha256': 'e' * 64, 'verified_files': 129}
        self.proof = {**self.snapshot, 'retained_candidate': {'root': str(self.root),
                      'manifest_sha256': self.files['SHA256SUMS'], 'verified_files': len(self.files)}}
        self.worker = {'pid': 33, 'invocation_id': 'f' * 32}
        self.txn = {'transaction_operation': 'update', 'transaction_phase': 'active', 'snapshot': 'snapshot',
                    'transaction_token_sha256': '9' * 64}
        for target, name, value in ((s, 'RELEASES', self.releases), (s, 'QUARANTINE', self.q),
                                   (s, 'PROTECTED', (self.protected,))):
            patch = mock.patch.object(target, name, value); patch.start(); self.addCleanup(patch.stop)
        self.guard = mock.patch.object(s.fault.probe, 'guard_guest', return_value=self.ident)
        self.guard.start(); self.addCleanup(self.guard.stop)
        for target, name, value in ((s.fault, 'read_transaction', self.txn), (s.hand, 'selection', ('1' * 64, b'selection')),
                                   (s.fault, 'verify_runtime', {'runtime_manifest_sha256': '1' * 64, 'verified_files': 12}),
                                   (s.fault.files, 'verify_snapshot', self.snapshot)):
            patch = mock.patch.object(target, name, return_value=value); patch.start(); self.addCleanup(patch.stop)

    def apply(self, **kwargs):
        return s.apply(self.args, self.plan, self.proof, self.worker, lambda: None, lambda _: None, **kwargs)

    def test_exact_three_renamed_originals_and_manifest_and_owner_preserved(self):
        before = self.protected.stat(); result = self.apply()
        self.assertEqual(result['removed'], list(s.TARGETS)); self.assertTrue(result['protected_state_unchanged'])
        for i, name in enumerate(s.TARGETS):
            self.assertFalse((self.root / name).exists())
            self.assertEqual((self.q / self.op / (str(i) + '.original')).read_bytes(), self.bytes[name])
        self.assertEqual((self.root / 'SHA256SUMS').read_bytes(), self.bytes['SHA256SUMS'])
        self.assertEqual(self.protected.read_bytes(), b'owner bytes')
        self.assertEqual(s.identity(self.protected.stat()), s.identity(before))
        record = s.collect(self.op)
        self.assertEqual(record['status'], 'recorded'); self.assertEqual(len(record['originals']), 3)
        self.assertTrue(all(record['retained']['files'][name]['status'] == 'absent' for name in s.TARGETS))
        self.assertEqual(record['retained']['files']['SHA256SUMS']['proof']['sha256'], self.files['SHA256SUMS'])
        self.assertEqual(record['records']['applied.json']['sha256'], result['applied_sha256'])
        self.assertEqual((self.q / self.op / 'intent.json').stat().st_mode & 0o777, 0o600)

    def test_duplicate_complete_attempt_never_reapplies(self):
        self.apply(); previous = {p.name: p.read_bytes() for p in (self.q / self.op).iterdir()}
        with self.assertRaises((ValueError, OSError)): self.apply()
        self.assertEqual(previous, {p.name: p.read_bytes() for p in (self.q / self.op).iterdir()})

    def test_interrupted_attempt_remains_consumed_without_restore_or_resume(self):
        def stop(point):
            if point == 'renamed-0': raise Interrupted()
        with self.assertRaises(Interrupted): self.apply(after_step=stop)
        self.assertFalse((self.root / s.TARGETS[0]).exists())
        self.assertTrue((self.root / s.TARGETS[1]).exists())
        self.assertTrue((self.q / self.op / 'pending-0.json').exists())
        self.assertFalse((self.q / self.op / 'applied.json').exists())
        with self.assertRaises((ValueError, OSError)): self.apply()
        self.assertTrue((self.root / s.TARGETS[1]).exists())
        self.assertEqual(len(s.collect(self.op)['originals']), 1)

    def test_interrupted_after_intent_is_consumed_before_first_rename(self):
        def stop(point):
            if point == 'intent_durable': raise Interrupted()
        with self.assertRaises(Interrupted): self.apply(after_step=stop)
        with self.assertRaises(FileExistsError): self.apply()
        self.assertTrue(all((self.root / name).exists() for name in s.TARGETS))

    def test_real_sigkill_after_rename_preserves_original_and_consumes_attempt(self):
        import signal
        pid = os.fork()
        if pid == 0:
            try:
                self.apply(after_step=lambda point: os.kill(os.getpid(), signal.SIGKILL) if point == 'renamed-1' else None)
            finally: os._exit(93)
        waited, status = os.waitpid(pid, 0)
        self.assertEqual(waited, pid); self.assertTrue(os.WIFSIGNALED(status)); self.assertEqual(os.WTERMSIG(status), signal.SIGKILL)
        self.assertEqual(len(s.collect(self.op)['originals']), 2)
        self.assertFalse((self.q / self.op / 'applied.json').exists())
        with self.assertRaises((ValueError, OSError)): self.apply()
        self.assertTrue((self.root / s.TARGETS[2]).exists())

    def test_guard_mismatch_refuses_before_creating_quarantine(self):
        with mock.patch.object(s.fault.probe, 'guard_guest', return_value={**self.ident, 'nonce': '0' * 64}):
            with self.assertRaises(ValueError): self.apply()
        self.assertFalse(self.q.exists())

    def test_wrong_root_or_manifest_or_option_is_not_admitted(self):
        for key, bad in (('root', str(self.base)), ('manifest_sha256', '0' * 64), ('verified_files', 999)):
            original = self.proof['retained_candidate'][key]; self.proof['retained_candidate'][key] = bad
            with self.subTest(key=key), self.assertRaises(ValueError): self.apply()
            self.proof['retained_candidate'][key] = original
        self.plan['candidate_data_fault'] = True
        with self.assertRaises(ValueError): self.apply()
        self.assertFalse(self.q.exists())

    def test_wrong_transaction_or_snapshot_refuses_before_mutation(self):
        for field, value in (('snapshot', 'other'), ('transaction_phase', 'completion.pending'), ('transaction_operation', 'rollback')):
            with mock.patch.object(s.fault, 'read_transaction', return_value={**self.txn, field: value}), self.assertRaises(ValueError): self.apply()
        with mock.patch.object(s.fault.files, 'verify_snapshot', return_value=None), self.assertRaises(ValueError): self.apply()
        self.assertFalse(self.q.exists())

    def test_symlink_hardlink_fifo_and_writable_target_refused(self):
        path = self.root / s.TARGETS[0]
        for kind in ('symlink', 'hardlink', 'fifo', 'mode', 'owner'):
            path.unlink()
            if kind == 'symlink': path.symlink_to(self.protected)
            elif kind == 'hardlink': os.link(self.protected, path)
            elif kind == 'fifo': os.mkfifo(path, 0o600)
            else:
                path.write_bytes(self.bytes[s.TARGETS[0]]); path.chmod(0o666 if kind == 'mode' else 0o600)
                if kind == 'owner': os.chown(path, 65534, 65534)
            with self.subTest(kind=kind), self.assertRaises((ValueError, OSError)): self.apply()
            self.assertFalse(self.q.exists())
        self.assertEqual(self.protected.read_bytes(), b'owner bytes')

    def test_unsafe_quarantine_symlink_never_changes_originals(self):
        self.q.symlink_to(self.base)
        with self.assertRaises((ValueError, OSError)): self.apply()
        self.assertTrue(all((self.root / name).exists() for name in s.TARGETS))

    def test_intervening_quarantine_file_is_never_overwritten(self):
        def change(point):
            if point == 'intent_durable': (self.q / self.op / '0.original').write_bytes(b'owner evidence')
        with self.assertRaises(FileExistsError): self.apply(after_step=change)
        self.assertEqual((self.q / self.op / '0.original').read_bytes(), b'owner evidence')
        self.assertEqual((self.root / s.TARGETS[0]).read_bytes(), self.bytes[s.TARGETS[0]])

    def test_changed_target_after_intent_is_not_moved(self):
        def change(point):
            if point == 'intent_durable': (self.root / s.TARGETS[0]).write_bytes(b'late owner change')
        with self.assertRaises(ValueError): self.apply(after_step=change)
        self.assertEqual((self.root / s.TARGETS[0]).read_bytes(), b'late owner change')
        self.assertFalse((self.q / self.op / '0.original').exists())

    def test_replaced_parent_after_intent_never_moves_replacement(self):
        def change(point):
            if point == 'intent_durable':
                self.root.rename(self.root.with_name('retained-original'))
                self.root.mkdir(mode=0o700); (self.root / 'rollback.sh').write_bytes(b'owner replacement')
        with self.assertRaises(ValueError): self.apply(after_step=change)
        self.assertEqual((self.root / 'rollback.sh').read_bytes(), b'owner replacement')
        self.assertFalse((self.q / self.op / '0.original').exists())

    def test_unrelated_candidate_change_prevents_any_fault(self):
        (self.root / 'owner.txt').write_bytes(b'extra')
        with self.assertRaises(ValueError): self.apply()
        self.assertFalse(self.q.exists())

    def test_late_installed_owner_change_never_overwritten_or_passed(self):
        def change(point):
            if point == 'renamed-0': self.protected.write_bytes(b'owner change')
        with self.assertRaises(ValueError): self.apply(after_step=change)
        self.assertEqual(self.protected.read_bytes(), b'owner change')
        self.assertFalse((self.q / self.op / 'applied.json').exists())


if __name__ == '__main__': unittest.main()
