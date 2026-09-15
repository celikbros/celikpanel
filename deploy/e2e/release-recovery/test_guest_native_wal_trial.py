"""Private evidence read checks; temporary files only, no VM or service access."""
import importlib.util
import copy
import hashlib
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('native_wal_guest_test', HERE / 'guest_native_wal_trial.py')
g = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = g
spec.loader.exec_module(g)


@unittest.skipUnless(sys.platform == 'linux' and os.geteuid() == 0,
                     'root Linux evidence metadata tests')
class PrivateEvidenceTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='cp-native-wal-read-')
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.path = self.root / 'events.jsonl'
        self.path.write_bytes(b'{"event":"armed"}\n{"event":"gate_released"}\n')
        self.path.chmod(0o600)

    def test_jsonl_raw_read_preserves_exact_evidence(self):
        self.assertEqual(g.private_raw(self.path), self.path.read_bytes())

    def test_symlink_and_fifo_are_refused_without_waiting(self):
        link = self.root / 'link'
        link.symlink_to(self.path)
        with self.assertRaises(OSError):
            g.private_raw(link)
        fifo = self.root / 'fifo'
        os.mkfifo(fifo, 0o600)
        with self.assertRaises(ValueError):
            g.private_raw(fifo)

    def test_oversized_file_is_refused_before_read(self):
        with self.path.open('r+b') as stream:
            stream.truncate(16 * 1024 * 1024 + 1)
        with mock.patch.object(g.os, 'read', side_effect=AssertionError('oversized read')):
            with self.assertRaises(ValueError):
                g.private_raw(self.path)

    def test_wrong_owner_permissions_or_hardlink_are_refused(self):
        for uid, mode in ((65534, 0o600), (0, 0o640)):
            with self.subTest(uid=uid, mode=mode):
                os.chown(self.path, uid, 0)
                self.path.chmod(mode)
                with self.assertRaises(ValueError):
                    g.private_raw(self.path)
        os.chown(self.path, 0, 0)
        self.path.chmod(0o600)
        os.link(self.path, self.root / 'second-name')
        with self.assertRaises(ValueError):
            g.private_raw(self.path)

    def test_change_during_read_cannot_publish_old_observation(self):
        read = os.read
        def changed(fd, count):
            result = read(fd, count)
            self.path.write_bytes(b'{"different":"evidence"}\n')
            return result
        with mock.patch.object(g.os, 'read', side_effect=changed):
            with self.assertRaises(ValueError):
                g.private_raw(self.path)


@unittest.skipUnless(sys.platform == 'linux' and os.geteuid() == 0,
                     'root Linux exact exchange pair copy tests')
class ExchangeCaptureTests(unittest.TestCase):
    def setUp(self):
        # Trusted ancestors are required by the real descriptor walk, even in tests.
        temporary = tempfile.TemporaryDirectory(prefix='cp-exchange-copy-', dir='/root')
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.private = self.root / 'private'
        self.private.mkdir(mode=0o700)
        self.parent = self.root / 'data'
        self.parent.mkdir(mode=0o750)
        self.parent.chmod(0o750)
        self.operation = 'a' * 32
        self.token = 'b' * 64
        self.build = self.parent / '.release-db-migrations' / self.token / 'authority' / ('.build-' + 'c' * 32)
        self.build.mkdir(parents=True, mode=0o700)
        for directory in (self.parent / '.release-db-migrations', self.build.parent.parent, self.build.parent):
            directory.chmod(0o700)
        self.canonical = self.parent / 'celikpanel.db'
        self.retired = self.build / 'celikpanel.db'
        self.canonical.write_bytes(b'complete-42-after-image')
        self.retired.write_bytes(b'exact-38-before-image')
        for path in (self.canonical, self.retired):
            path.chmod(0o600)
            os.chown(path, 65534, 65534)
        def file(path):
            return {'path': str(path), 'identity': g.file_identity(path.stat()), 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}
        def directory(path):
            return {k: v for k, v in g.file_identity(path.stat()).items() if k in ('dev', 'ino', 'mode', 'uid', 'gid')}
        self.proof = {'transaction': {'transaction_token_sha256': self.token},
                      'database': {'canonical': file(self.canonical), 'build': file(self.retired),
                                   'directories': {'parent': directory(self.parent), 'build': directory(self.build)}}}
        for key, value in (('PRIVATE', self.private), ('DATABASE_PARENT', self.parent)):
            self.addCleanup(mock.patch.stopall)
            mock.patch.object(g, key, value).start()
        self.target = self.private / ('native-database-exchange-' + self.operation + '-capture')

    def test_pair_copy_preserves_both_exact_original_inodes_and_bytes(self):
        before = copy.deepcopy(self.proof)
        answer = g.capture_exchange_checkpoint(self.operation, self.proof)
        self.assertEqual(self.proof, before)
        self.assertEqual(set(answer['files']), {'canonical.db', 'retained-before.db'})
        for label, path in (('canonical.db', self.canonical), ('retained-before.db', self.retired)):
            self.assertEqual((self.target / label).read_bytes(), path.read_bytes())
            self.assertEqual(answer['files'][label]['source']['identity'], g.file_identity(path.stat()))
        self.assertEqual(set(p.name for p in self.target.iterdir()), {'canonical.db', 'retained-before.db', 'capture.json'})
        with self.assertRaises(FileExistsError):
            g.capture_exchange_checkpoint(self.operation, self.proof)

    def test_foreign_build_or_canonical_path_is_refused_before_output(self):
        for field in ('canonical', 'build'):
            changed = copy.deepcopy(self.proof)
            changed['database'][field]['path'] = str(self.root / 'foreign.db')
            with self.subTest(field=field), self.assertRaises(ValueError):
                g.capture_exchange_checkpoint(self.operation, changed)
            self.assertFalse(self.target.exists())

    def test_source_symlink_and_fifo_refused_without_read(self):
        self.canonical.rename(self.parent / 'saved.db')
        self.canonical.symlink_to(self.parent / 'saved.db')
        with self.assertRaises(OSError):
            g.capture_exchange_checkpoint(self.operation, self.proof)
        self.assertFalse((self.target / 'capture.json').exists())
        self.canonical.unlink()
        os.mkfifo(self.canonical, 0o600)
        # Retained failed attempt prevents accidental reuse; use another fixed operation.
        with self.assertRaises(ValueError):
            g.capture_exchange_checkpoint('d' * 32, self.proof)
        self.assertFalse((self.private / ('native-database-exchange-' + 'd' * 32 + '-capture') / 'capture.json').exists())

    def test_wrong_named_parent_identity_is_refused_before_output(self):
        self.proof['database']['directories']['build']['ino'] += 1
        with self.assertRaises(ValueError):
            g.capture_exchange_checkpoint(self.operation, self.proof)
        self.assertFalse(self.target.exists())

    def test_change_of_first_input_while_copying_second_refuses_receipt(self):
        original_read = os.read
        retained_inode = self.retired.stat().st_ino
        changed = False
        def read(fd, count):
            nonlocal changed
            if os.fstat(fd).st_ino == retained_inode and not changed:
                self.canonical.write_bytes(b'owner-edited-after-first-copy')
                changed = True
            return original_read(fd, count)
        with mock.patch.object(g.os, 'read', side_effect=read):
            with self.assertRaises(ValueError):
                g.capture_exchange_checkpoint(self.operation, self.proof)
        self.assertTrue(changed)
        self.assertFalse((self.target / 'capture.json').exists())
        self.assertEqual((self.target / 'canonical.db').read_bytes(), b'complete-42-after-image')
        self.assertEqual(self.canonical.read_bytes(), b'owner-edited-after-first-copy')


if __name__ == '__main__':
    unittest.main()
