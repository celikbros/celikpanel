"""Private evidence read checks; temporary files only, no VM or service access."""
import importlib.util
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


if __name__ == '__main__':
    unittest.main()
