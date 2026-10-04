#!/usr/bin/env python3
"""Exercise real filesystem/flock/marker handling with mocked service starts.
Gerçek dosya sistemi/flock/işaretçi akışını servis başlangıçlarını taklit ederek sınar.
"""
import contextlib
import hashlib
import importlib.util
import os
from pathlib import Path
import shutil
import sqlite3
import subprocess
import tempfile
import types
import unittest
from unittest.mock import patch

REPO = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location('recovery', REPO / 'deploy/recover-frankfurt-alpha75.py')
recovery = importlib.util.module_from_spec(spec)
spec.loader.exec_module(recovery)

HISTORICAL_RELEASE_COMMIT = '9c55f235d569a91264304a74cf09b26c83123e68'


def historical_guard_bytes():
    # This incident pins published bytes, not the evolving worktree helper.
    # Bu olay, değişen çalışma dosyasını değil yayımlanmış baytları sabitler.
    try:
        content = subprocess.check_output(
            ['git', 'show', HISTORICAL_RELEASE_COMMIT + ':deploy/release-transaction-guard.sh'],
            cwd=REPO, stderr=subprocess.PIPE, timeout=30)
    except (OSError, subprocess.SubprocessError) as error:
        raise RuntimeError('Historical guard object unavailable; fetch full repository history') from error
    if hashlib.sha256(content).hexdigest() != recovery.GUARD_SHA:
        raise RuntimeError('Historical guard object does not match the incident pin')
    return content


class RecoveryTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.historical_guard = historical_guard_bytes()

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='celikpanel-incident-test.', dir='/var/lib')
        self.addCleanup(self.temp.cleanup)
        base = Path(self.temp.name)
        replacements = {
            'TX': base / 'transaction', 'SNAPS': base / 'snapshots',
            'RELEASES': base / 'releases', 'EVIDENCE': base / 'evidence',
            'BIN': base / 'bin', 'WEB': base / 'web', 'DB': base / 'data/celikpanel.db',
            'AGENT_STATE': base / 'agent', 'RUNTIME': base / 'run',
        }
        replacements.update(STAGE=replacements['SNAPS'] / recovery.STAGE.name,
                            LOCK=replacements['RUNTIME'] / 'service-mutation.lock',
                            SOCKET=replacements['RUNTIME'] / 'agent.sock')
        replacements['CHILD'] = replacements['STAGE'] / recovery.NAME
        self.stack = contextlib.ExitStack()
        self.addCleanup(self.stack.close)
        for name, value in replacements.items():
            self.stack.enter_context(patch.object(recovery, name, value))
        self.stack.enter_context(patch.object(recovery.socket, 'gethostname', return_value='frankfurt'))
        self.stack.enter_context(patch('grp.getgrnam', return_value=types.SimpleNamespace(gr_gid=0)))
        for directory in [recovery.TX, recovery.CHILD, recovery.BIN, recovery.WEB,
                          recovery.DB.parent, recovery.AGENT_STATE, recovery.RUNTIME, recovery.RELEASES]:
            directory.mkdir(mode=0o700, parents=True, exist_ok=True)
        recovery.STAGE.chmod(0o700)
        self.marker = b'version=1\ntoken=' + b'a' * 64 + b'\noperation=update\nsnapshot=' + recovery.NAME.encode() + b'\n'
        self.write(recovery.TX / 'active', self.marker)
        self.write(recovery.TX / 'transaction.lock', b'')
        self.write(recovery.LOCK, b'')
        programs = {}
        for name in recovery.PROGRAMS:
            self.write(recovery.BIN / name, ('fixture-' + name).encode())
            programs[name] = recovery.sha(recovery.BIN / name)
        self.stack.enter_context(patch.object(recovery, 'PROGRAMS', programs))
        self.write(recovery.WEB / 'index.html', b'fixture')
        web_row = 'F ' + recovery.sha(recovery.WEB / 'index.html') + ' index.html\n'
        self.stack.enter_context(patch.object(recovery, 'WEB_SHA', hashlib.sha256(web_row.encode()).hexdigest()))
        self.write(recovery.CHILD / 'service-states.tsv', recovery.SERVICES)
        self.write(recovery.CHILD / 'quiesce-coordinators.tsv', recovery.COORDINATORS)
        self.write(recovery.CHILD / 'snapshot-transition.state', b'normal\n')
        (recovery.CHILD / 'agent-state').mkdir(mode=0o700)
        self.write(recovery.CHILD / 'agent-state-root', str(recovery.AGENT_STATE).encode() + b'\n')
        self.write(recovery.CHILD / 'agent-ledger.state', b'present\n')
        self.write(recovery.AGENT_STATE / 'service-mutations.json', b'{"fixture":true}\n')
        shutil.copyfile(recovery.AGENT_STATE / 'service-mutations.json', recovery.CHILD / 'agent-state/service-mutations.json')
        (recovery.CHILD / 'agent-state/service-mutations.json').chmod(0o600)
        with contextlib.closing(sqlite3.connect(recovery.DB)) as db:
            db.execute('CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)')
            db.execute("INSERT INTO test VALUES (1, 'preserved')")
            db.commit()
        recovery.DB.chmod(0o600)
        shutil.copyfile(recovery.DB, recovery.CHILD / 'celikpanel.db')
        (recovery.CHILD / 'celikpanel.db').chmod(0o600)
        root = recovery.RELEASES / (HISTORICAL_RELEASE_COMMIT[:12] + '-' + '0' * 24)
        (root / 'deploy').mkdir(mode=0o700, parents=True)
        root.chmod(0o700)
        self.write(root / 'deploy/release-transaction-guard.sh', self.historical_guard)
        self.write(root / 'release.commit', HISTORICAL_RELEASE_COMMIT.encode() + b'\n')
        manifest = subprocess.check_output(['/bin/bash', '-c',
            "cd \"$1\"; find . -type f -print0 | sort -z | xargs -0 sha256sum", 'fixture', str(root)])
        self.write(root / 'SHA256SUMS', manifest)
        self.started = []
        self.stack.enter_context(patch.object(recovery, 'unit_info', return_value={
            'LoadState': 'loaded', 'MainPID': '0', 'ActiveState': 'inactive',
            'Job': '', 'RuntimeDirectoryPreserve': 'yes', 'ControlGroup': '',
        }))
        self.stack.enter_context(patch.object(recovery, 'start_existing', side_effect=self.started.append))
        self.stack.enter_context(patch.object(recovery, 'verify_idle'))

    @staticmethod
    def write(path, content):
        path.write_bytes(content)
        path.chmod(0o600)

    def rejected(self):
        with self.assertRaises((RuntimeError, OSError)):
            recovery.run(True)
        self.assertEqual((recovery.TX / 'active').read_bytes(), self.marker)
        self.assertEqual(self.started, [])
        self.assertTrue(recovery.CHILD.exists())

    def test_retained_guard_matches_the_historical_incident_pin(self):
        root = recovery.find_guard()
        self.assertEqual(recovery.sha(root / 'deploy/release-transaction-guard.sh'), recovery.GUARD_SHA)
        self.assertEqual((root / 'release.commit').read_bytes(), HISTORICAL_RELEASE_COMMIT.encode() + b'\n')

    def test_check_is_read_only(self):
        recovery.run(False)
        self.assertTrue((recovery.TX / 'active').exists())
        self.assertFalse(recovery.EVIDENCE.exists())
        self.assertEqual(self.started, [])

    def test_restores_only_previous_services_and_archives_evidence(self):
        old_db = recovery.sha(recovery.DB)
        recovery.run(True)
        self.assertEqual(self.started, ['agent', 'panel'])
        self.assertFalse((recovery.TX / 'active').exists())
        self.assertTrue((recovery.EVIDENCE / 'snapshot-stage' / recovery.NAME / 'celikpanel.db').exists())
        self.assertEqual((recovery.EVIDENCE / 'active.before').read_bytes(), self.marker)
        self.assertEqual(recovery.sha(recovery.DB), old_db)
        self.assertFalse(recovery.STAGE.exists())

    def test_changed_program(self):
        self.write(recovery.BIN / 'agent', b'different')
        self.rejected()

    def test_changed_web(self):
        self.write(recovery.WEB / 'extra.js', b'unexpected')
        self.rejected()

    def test_changed_database(self):
        with contextlib.closing(sqlite3.connect(recovery.DB)) as db:
            db.execute("UPDATE test SET value='changed'")
            db.commit()
        self.rejected()

    def test_changed_ledger(self):
        self.write(recovery.AGENT_STATE / 'service-mutations.json', b'changed')
        self.rejected()

    def test_final_snapshot_exists(self):
        (recovery.SNAPS / recovery.NAME).mkdir()
        self.rejected()

    def test_unexpected_stage(self):
        self.write(recovery.CHILD / 'snapshot.version', b'6\n')
        self.rejected()

    def test_wrong_incident(self):
        self.write(recovery.TX / 'active', self.marker.replace(b'20260913T190800Z', b'20260913T190801Z'))
        with self.assertRaises(RuntimeError):
            recovery.run(True)
        self.assertEqual(self.started, [])

    def test_transaction_lock_held_by_another_opener(self):
        fd = os.open(recovery.TX / 'transaction.lock', os.O_RDWR)
        try:
            import fcntl
            fcntl.flock(fd, fcntl.LOCK_EX)
            self.rejected()
        finally:
            os.close(fd)

    def test_mutation_lock_held_by_another_opener(self):
        fd = os.open(recovery.LOCK, os.O_RDWR)
        try:
            import fcntl
            fcntl.flock(fd, fcntl.LOCK_EX)
            self.rejected()
        finally:
            os.close(fd)

    def test_untrusted_guard(self):
        guard = next(recovery.RELEASES.glob('*/deploy/release-transaction-guard.sh'))
        self.write(guard, b'changed')
        self.rejected()

    def test_live_coordinator(self):
        with patch.object(recovery, 'unit_info', return_value={
                'LoadState': 'loaded', 'MainPID': '999', 'ActiveState': 'active', 'Job': ''}):
            self.rejected()

    def test_package_or_panel_work_not_idle(self):
        with patch.object(recovery, 'verify_idle', side_effect=RuntimeError('busy')):
            self.rejected()

    def test_start_failure_keeps_evidence_and_does_not_retry_update(self):
        with patch.object(recovery, 'start_existing', side_effect=RuntimeError('start failed')):
            with self.assertRaisesRegex(RuntimeError, 'start failed'):
                recovery.run(True)
        self.assertFalse((recovery.TX / 'active').exists())
        self.assertEqual((recovery.EVIDENCE / 'active.before').read_bytes(), self.marker)
        self.assertTrue((recovery.EVIDENCE / 'snapshot-stage' / recovery.NAME).exists())

    def test_escaping_stage_link(self):
        path = recovery.CHILD / 'agent-state-root'
        path.unlink()
        path.symlink_to(recovery.DB)
        self.rejected()


class HistoricalGuardSourceTests(unittest.TestCase):
    def test_changed_guard_cannot_replace_historical_pin(self):
        with patch.object(subprocess, 'check_output', return_value=b'changed worktree guard\n'):
            with self.assertRaisesRegex(RuntimeError, 'does not match the incident pin'):
                historical_guard_bytes()

    def test_missing_git_object_has_no_worktree_fallback(self):
        with patch.object(subprocess, 'check_output', side_effect=subprocess.CalledProcessError(128, ['git', 'show'])):
            with self.assertRaisesRegex(RuntimeError, 'fetch full repository history'):
                historical_guard_bytes()


class RealStartupTests(unittest.TestCase):
    def test_waits_for_real_fork_exec_transition(self):
        # The child initially runs this Python test, then execs the expected
        # binary. Its PID stays constant, as with an early systemd simple start.
        # Alt süreç önce Python testidir, sonra aynı PID ile beklenen programı
        # exec eder; erken systemd simple başlangıcındaki geçişi sınar.
        import signal
        import socket
        import time
        with tempfile.TemporaryDirectory(dir='/var/lib', prefix='celikpanel-start-test.') as directory:
            root = Path(directory)
            shutil.copyfile('/usr/bin/sleep', root / 'agent')
            (root / 'agent').chmod(0o700)
            sock = socket.socket(socket.AF_UNIX)
            sock.bind(str(root / 'agent.sock'))
            pid = os.fork()
            if pid == 0:
                sock.close()
                time.sleep(0.35)
                os.execv(str(root / 'agent'), [str(root / 'agent'), '30'])
            try:
                with patch.object(recovery, 'BIN', root), patch.object(recovery, 'SOCKET', root / 'agent.sock'), \
                     patch.object(recovery, 'PROGRAMS', {'agent': recovery.sha(root / 'agent')}), \
                     patch.object(recovery, 'command'), \
                     patch.object(recovery, 'unit_info', return_value={'ActiveState': 'active', 'MainPID': str(pid)}):
                    recovery.start_existing('agent')
                    self.assertEqual(Path('/proc', str(pid), 'exe').resolve(), root / 'agent')
            finally:
                os.kill(pid, signal.SIGKILL)
                os.waitpid(pid, 0)
                sock.close()

    def test_persistent_wrong_executable_never_becomes_ready(self):
        with patch.object(recovery, 'command'), \
             patch.object(recovery, 'unit_info', return_value={'ActiveState': 'active', 'MainPID': str(os.getpid())}), \
             patch.object(recovery.time, 'sleep'):
            with self.assertRaisesRegex(RuntimeError, 'did not become ready'):
                recovery.start_existing('agent')


if __name__ == '__main__':
    unittest.main()
