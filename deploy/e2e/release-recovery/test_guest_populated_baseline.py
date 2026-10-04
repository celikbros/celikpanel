#!/usr/bin/env python3
"""Root filesystem component tests; no VM, native service or installed DB access."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import signal
import sqlite3
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    value = importlib.util.module_from_spec(spec)
    sys.modules[name] = value
    spec.loader.exec_module(value)
    return value


g = module('guest_populated_under_test', 'guest_populated_baseline.py')
old = module('guest_populated_old_sql', 'test_populated_database.py')
REAL_SERVICES = g._services


@unittest.skipUnless(sys.platform == 'linux' and os.geteuid() == 0, 'root Linux ownership/exchange component tests')
class GuestPopulatedBaselineTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        old.PopulatedDatabaseTests.setUpClass()
        cls.baseline = old.PopulatedDatabaseTests.baseline_bytes
        cls.sql = old.PopulatedDatabaseTests.sql

    @classmethod
    def tearDownClass(cls):
        old.PopulatedDatabaseTests.tearDownClass()

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='cp-guest-populated-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.private = self.root / 'private'
        self.private.mkdir(mode=0o700)
        self.parent = self.root / 'data'
        self.parent.mkdir(mode=0o750)
        # Exact ownership fixture must be independent of the runner umask.
        self.parent.chmod(0o750)
        os.chown(self.parent, 65534, 65534)
        self.database = self.parent / 'celikpanel.db'
        self.database.write_bytes(self.baseline)
        self.database.chmod(0o600)
        os.chown(self.database, 65534, 65534)
        self.transactions = self.root / 'transactions'
        self.transactions.mkdir(mode=0o700)
        (self.transactions / 'transaction.lock').write_bytes(b'')
        (self.transactions / 'transaction.lock').chmod(0o600)
        self.proc = self.root / 'proc'
        self.proc.mkdir(mode=0o700)
        self.cgroup = self.root / 'cgroup'
        self.cgroup.mkdir(mode=0o700)
        self.args = argparse.Namespace(lab_nonce='a' * 64, vm_uuid='12345678-1234-4234-8234-123456789abc',
                                       cell_id='fixture-component', node='arch', seed_id='b' * 32,
                                       capture_id='c' * 32, expected_version=38)
        self.identity = {'nonce': self.args.lab_nonce, 'vm_uuid': self.args.vm_uuid,
                         'cell_id': self.args.cell_id, 'node': self.args.node}
        for name, value in (('PRIVATE', self.private), ('CANONICAL', self.database),
                            ('TRANSACTIONS', self.transactions), ('PROC', self.proc), ('CGROUP', self.cgroup)):
            patch = mock.patch.object(g, name, value)
            patch.start()
            self.addCleanup(patch.stop)
        patch = mock.patch.object(g.probe, 'guard_guest', side_effect=lambda args: dict(self.identity))
        self.guard = patch.start()
        self.addCleanup(patch.stop)
        patch = mock.patch.object(g.pwd, 'getpwnam', return_value=argparse.Namespace(pw_uid=65534, pw_gid=65534))
        patch.start()
        self.addCleanup(patch.stop)
        patch = mock.patch.object(g, '_baseline', return_value={'agent': 'a' * 64, 'panel': 'b' * 64})
        patch.start()
        self.addCleanup(patch.stop)
        patch = mock.patch.object(g, '_services', side_effect=self.services)
        patch.start()
        self.addCleanup(patch.stop)

    def services(self, mode, deadline):
        return {unit: {'properties': {'mode': mode}, 'processes': {}} for unit in g.UNITS}

    def receipt_dir(self):
        return self.private / ('populated-baseline-' + self.args.seed_id)

    def seeded(self):
        return g.seed(self.args)

    def test_real_exchange_preserves_original_inode_and_source_rows(self):
        before = g._identity(self.database.stat())
        result = self.seeded()
        original = self.parent / result['stage_name'] / 'celikpanel.db'
        self.assertEqual(original.read_bytes(), self.baseline)
        self.assertEqual(g._stable(g._identity(original.stat())), g._stable(before))
        self.assertNotEqual(self.database.stat().st_ino, before['st_ino'])
        self.assertEqual(self.database.stat().st_uid, 65534)
        self.assertEqual(self.database.stat().st_gid, 65534)
        self.assertEqual(self.database.stat().st_mode & 0o777, 0o600)
        self.assertEqual(original.parent.stat().st_uid, 0)
        self.assertEqual(original.parent.stat().st_mode & 0o777, 0o700)
        self.assertTrue((self.receipt_dir() / 'installed.json').is_file())
        self.assertGreaterEqual(self.guard.call_count, 3)
        g.capture(self.args)
        proof = g.verify(self.args)
        self.assertEqual(proof['proof']['old_table_count'], 55)
        self.assertEqual(proof['proof']['fixture_domain_count'], 2)
        self.assertEqual(proof['proof']['old_rows_missing_or_changed'], 0)

    def test_final_retained_directory_name_change_refuses_without_undo(self):
        before = self.database.stat().st_ino
        calls = 0
        renamed = self.parent / 'owner-retained-original'
        def final_change(mode, deadline):
            nonlocal calls
            calls += 1
            if calls == 3:
                stage = self.parent / ('.lab-populated-' + self.args.seed_id)
                stage.rename(renamed)
                stage.mkdir(mode=0o700)
            return self.services(mode, deadline)
        with mock.patch.object(g, '_services', side_effect=final_change), self.assertRaises(g.Refused):
            self.seeded()
        self.assertEqual((renamed / 'celikpanel.db').stat().st_ino, before)
        self.assertEqual((renamed / 'celikpanel.db').read_bytes(), self.baseline)
        self.assertFalse((self.receipt_dir() / 'installed.json').exists())
        self.assertNotEqual(self.database.stat().st_ino, before)

    def test_final_installed_bytes_change_refuses_without_undo(self):
        calls = 0
        def final_change(mode, deadline):
            nonlocal calls
            calls += 1
            if calls == 3:
                with self.database.open('ab') as stream:
                    stream.write(b'owner-change')
            return self.services(mode, deadline)
        with mock.patch.object(g, '_services', side_effect=final_change), self.assertRaises(g.Refused):
            self.seeded()
        self.assertTrue(self.database.read_bytes().endswith(b'owner-change'))
        self.assertEqual((self.parent / ('.lab-populated-' + self.args.seed_id) / 'celikpanel.db').read_bytes(), self.baseline)
        self.assertFalse((self.receipt_dir() / 'installed.json').exists())

    def test_capture_opens_no_sqlite_and_preserves_raw_files(self):
        self.seeded()
        before = self.database.read_bytes()
        with mock.patch.object(g.sqlite3, 'connect', side_effect=AssertionError('capture must not open SQLite')):
            captured = g.capture(self.args)
        self.assertFalse(captured['sqlite_opened_original'])
        self.assertEqual(self.database.read_bytes(), before)
        raw = self.receipt_dir() / ('capture-' + self.args.capture_id) / 'raw' / 'celikpanel.db'
        self.assertEqual(raw.read_bytes(), before)

    def test_frozen_wal_copy_then_private_backup_includes_committed_wal_rows(self):
        self.seeded()
        connection = sqlite3.connect(self.database)
        self.addCleanup(connection.close)
        connection.execute('PRAGMA journal_mode=WAL')
        connection.execute('PRAGMA wal_autocheckpoint=0')
        connection.execute("INSERT INTO metrics_samples VALUES('2001-01-01T00:00:00Z',3.5,123,999,456,888,0.75)")
        connection.commit()
        for suffix in ('-wal', '-shm'):
            sidecar = Path(str(self.database) + suffix)
            sidecar.chmod(0o600)
            os.chown(sidecar, 65534, 65534)
        captured = g.capture(self.args)
        self.assertEqual(set(captured['source']), {'celikpanel.db', 'celikpanel.db-wal', 'celikpanel.db-shm'})
        directory = self.receipt_dir() / ('capture-' + self.args.capture_id) / 'raw'
        before = {p.name: (g._identity(p.stat()), p.read_bytes()) for p in directory.iterdir()}
        result = g.verify(self.args)
        self.assertEqual(result['proof']['tables']['metrics_samples'], {'before': 1, 'after': 2, 'added': 1})
        self.assertEqual(before, {p.name: (g._identity(p.stat()), p.read_bytes()) for p in directory.iterdir()})

    def test_real_sql38_to42_all_old_rows_and_domain_defaults_verified(self):
        self.seeded()
        connection = sqlite3.connect(self.database)
        old.apply(connection, self.sql[38:42])
        connection.close()
        self.args.expected_version = 42
        g.capture(self.args)
        result = g.verify(self.args)
        self.assertEqual(result['proof']['table_count'], 65)
        self.assertTrue(result['proof']['domain_defaults_40_42_verified'])
        self.assertEqual(result['proof']['old_rows_missing_or_changed'], 0)

    def test_wrong_guest_guard_refuses_before_any_seed_mutation(self):
        self.guard.side_effect = g.probe.ProbeError('DMI differs')
        with self.assertRaises(g.probe.ProbeError):
            self.seeded()
        self.assertEqual(self.database.read_bytes(), self.baseline)
        self.assertEqual(list(self.private.iterdir()), [])

    def test_real_marker_validation_rejects_wrong_nonce_and_dmi(self):
        raw = json.dumps({'schema': g.probe.MARKER_SCHEMA, **self.identity}).encode()
        for nonce, dmi in (('f' * 64, self.args.vm_uuid), (self.args.lab_nonce, '87654321-1234-4234-8234-123456789abc')):
            with self.subTest(nonce=nonce, dmi=dmi), self.assertRaises(g.probe.ProbeError):
                g.probe.validate_identity(raw, nonce, self.args.vm_uuid, self.args.cell_id, 'arch', dmi, 'QEMU', 'Standard PC')

    def test_active_unknown_transaction_or_busy_lock_refuses(self):
        for name in ('active', 'completion.pending', 'scheduler-restore.pending', 'owner-extra'):
            path = self.transactions / name
            path.write_bytes(b'unknown')
            try:
                with self.assertRaises(g.Refused):
                    self.seeded()
            finally:
                path.unlink()
        with g._idle_lock():
            with self.assertRaises(BlockingIOError):
                self.seeded()
        self.assertEqual(self.database.read_bytes(), self.baseline)

    def test_live_or_unverified_services_refuse_before_copy(self):
        with mock.patch.object(g, '_services', side_effect=g.Refused('not stopped')):
            with self.assertRaises(g.Refused):
                self.seeded()
        self.assertEqual(list(self.private.iterdir()), [])

    def test_actual_service_parser_requires_both_userspace_and_kernel_frozen_proof(self):
        for unit in g.UNITS:
            directory = self.cgroup / unit
            directory.mkdir()
            (directory / 'cgroup.procs').write_text('123\n')
            (directory / 'cgroup.events').write_text('populated 1\nfrozen 1\n')
            (directory / 'cgroup.freeze').write_text('1\n')
        process = self.proc / '123'
        process.mkdir()
        (process / 'stat').write_text('123 (fixture) ' + ' '.join(['S'] + ['0'] * 18 + ['321']))
        freezer = 'frozen'
        def observe(argv, **kwargs):
            self.assertEqual(argv[:2], ['/usr/bin/systemctl', 'show'])
            unit = argv[2]
            values = {'Id': unit, 'LoadState': 'loaded', 'ActiveState': 'active', 'SubState': 'running',
                      'MainPID': '123', 'ControlGroup': '/system.slice/' + unit, 'FreezerState': freezer}
            return argparse.Namespace(returncode=0, stdout=''.join(k + '=' + v + '\n' for k, v in values.items()))
        with mock.patch.object(g.subprocess, 'run', observe):
            proof = REAL_SERVICES('frozen', time.monotonic() + 5)
            self.assertEqual(proof[g.UNITS[0]]['processes'], {'123': '321'})
            freezer = 'running'
            with self.assertRaises(g.Refused):
                REAL_SERVICES('frozen', time.monotonic() + 5)
            freezer = 'frozen'
            (self.cgroup / g.UNITS[0] / 'cgroup.freeze').write_text('0\n')
            with self.assertRaises(g.Refused):
                REAL_SERVICES('frozen', time.monotonic() + 5)

    def test_partial_stopped_cgroup_is_unknown_not_empty(self):
        (self.cgroup / g.UNITS[0]).mkdir()
        def observe(argv, **kwargs):
            values = {'Id': argv[2], 'LoadState': 'loaded', 'ActiveState': 'inactive', 'SubState': 'dead',
                      'MainPID': '0', 'ControlGroup': '', 'FreezerState': 'running'}
            return argparse.Namespace(returncode=0, stdout=''.join(k + '=' + v + '\n' for k, v in values.items()))
        with mock.patch.object(g.subprocess, 'run', observe):
            with self.assertRaises(g.probe.ProbeError):
                REAL_SERVICES('stopped', time.monotonic() + 5)

    def test_capture_deadline_expiry_does_not_publish_capture_receipt(self):
        self.seeded()
        copied = False
        original = g.populated._write_new
        def write(path, content):
            nonlocal copied
            original(path, content)
            if path.parent.name == 'raw':
                copied = True
        with mock.patch.object(g.populated, '_write_new', write), mock.patch.object(g.time, 'monotonic', side_effect=lambda: 106 if copied else 100):
            with self.assertRaisesRegex(g.Refused, 'deadline'):
                g.capture(self.args)
        directory = self.receipt_dir() / ('capture-' + self.args.capture_id)
        self.assertTrue((directory / 'raw/celikpanel.db').exists())
        self.assertFalse((directory / 'capture.json').exists())

    def test_new_transaction_before_exchange_refuses_without_database_mutation(self):
        original = g._write_json
        def write(path, content):
            original(path, content)
            if path.name == 'exchange.json':
                (self.transactions / 'active').write_bytes(b'new owner operation')
        with mock.patch.object(g, '_write_json', write):
            with self.assertRaises(g.Refused):
                self.seeded()
        self.assertEqual(self.database.read_bytes(), self.baseline)
        self.assertFalse((self.receipt_dir() / 'installed.json').exists())

    def test_foreign_database_handle_is_refused(self):
        descriptor = self.proc / '12345' / 'fd'
        descriptor.mkdir(parents=True)
        (descriptor / '3').symlink_to(self.database)
        with self.assertRaisesRegex(g.Refused, 'foreign process'):
            self.seeded()
        self.assertEqual(self.database.read_bytes(), self.baseline)

    def test_source_sidecar_symlink_and_owner_layout_refused(self):
        for suffix in ('-wal', '-shm', '-journal'):
            path = Path(str(self.database) + suffix)
            path.symlink_to(self.database)
            try:
                with self.assertRaises((g.Refused, OSError)):
                    self.seeded()
            finally:
                path.unlink()
        self.parent.chmod(0o700)
        with self.assertRaises(g.Refused):
            self.seeded()
        self.assertEqual(self.database.read_bytes(), self.baseline)

    def test_reseed_does_not_adopt_or_overwrite_completed_or_partial_attempt(self):
        self.seeded()
        before = (g._identity(self.database.stat()), self.database.read_bytes())
        with self.assertRaises(FileExistsError):
            self.seeded()
        self.assertEqual(before, (g._identity(self.database.stat()), self.database.read_bytes()))

    def test_owner_change_immediately_before_exchange_is_not_overwritten(self):
        original = g._write_json
        changed = b'owner-change-preserve'
        def change(path, value):
            original(path, value)
            if path.name == 'exchange.json':
                self.database.write_bytes(changed)
        with mock.patch.object(g, '_write_json', change):
            with self.assertRaises(g.Refused):
                self.seeded()
        self.assertEqual(self.database.read_bytes(), changed)
        self.assertFalse((self.receipt_dir() / 'installed.json').exists())

    def test_actual_sigkill_after_exchange_retains_both_images_no_success_receipt(self):
        before_inode = self.database.stat().st_ino
        child = os.fork()
        if child == 0:
            original = g._exchange
            def killed(parent, stage):
                original(parent, stage)
                os.kill(os.getpid(), signal.SIGKILL)
            g._exchange = killed
            try:
                self.seeded()
            except BaseException:
                os._exit(91)
            os._exit(92)
        _, status = os.waitpid(child, 0)
        self.assertTrue(os.WIFSIGNALED(status))
        self.assertEqual(os.WTERMSIG(status), signal.SIGKILL)
        retained = self.parent / ('.lab-populated-' + self.args.seed_id) / 'celikpanel.db'
        self.assertEqual(retained.stat().st_ino, before_inode)
        self.assertEqual(retained.read_bytes(), self.baseline)
        self.assertNotEqual(self.database.stat().st_ino, before_inode)
        self.assertTrue((self.receipt_dir() / 'exchange.json').exists())
        self.assertFalse((self.receipt_dir() / 'installed.json').exists())
        with self.assertRaises(FileExistsError):
            self.seeded()

    def test_capture_change_or_deadline_preserves_unknown_raw_evidence(self):
        self.seeded()
        original = g._files
        calls = 0
        def changed(*args, **kwargs):
            nonlocal calls
            calls += 1
            if calls == 2:
                self.database.write_bytes(self.database.read_bytes() + b'x')
            return original(*args, **kwargs)
        with mock.patch.object(g, '_files', changed):
            with self.assertRaises(g.Refused):
                g.capture(self.args)
        directory = self.receipt_dir() / ('capture-' + self.args.capture_id)
        self.assertTrue((directory / 'raw/celikpanel.db').exists())
        self.assertFalse((directory / 'capture.json').exists())

    def test_modified_raw_capture_refuses_without_original_sqlite_open(self):
        self.seeded()
        g.capture(self.args)
        raw = self.receipt_dir() / ('capture-' + self.args.capture_id) / 'raw/celikpanel.db'
        raw.write_bytes(raw.read_bytes() + b'changed')
        with mock.patch.object(g.sqlite3, 'connect', side_effect=AssertionError('must refuse before SQLite')):
            with self.assertRaises(g.Refused):
                g.verify(self.args)

    def test_wrong_expected_schema_and_changed_old_rows_refuse(self):
        self.seeded()
        connection = sqlite3.connect(self.database)
        connection.execute("UPDATE users SET password_hash='changed' WHERE id=81")
        connection.commit()
        connection.close()
        g.capture(self.args)
        with self.assertRaises(g.populated.Refused):
            g.verify(self.args)


if __name__ == '__main__':
    unittest.main()
