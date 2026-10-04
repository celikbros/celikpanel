#!/usr/bin/env python3
"""Opt-in actual-binary component test; never a native installed-panel update.

Set both CELIKPANEL_POPULATED_OLD_PANEL and
CELIKPANEL_POPULATED_CANDIDATE_PANEL to explicit local files, then run as a
nonroot Linux user. Only the two fixed hashes below can execute, from fresh
private copies, with --migrate-only and a new private CELIKPANEL_DATA_DIR.
Every attempt (including failure) remains under a new /var/tmp directory.
Missing inputs produce an explicit skip, never a native acceptance result.
"""
from __future__ import annotations

import hashlib
import importlib.util
import json
import os
from pathlib import Path
import secrets
import signal
import stat
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
INPUTS = (
    ('old-panel', 'CELIKPANEL_POPULATED_OLD_PANEL',
     'c6e91e9e478d261c104397518fdae0145733832be7eeb3e53a123ef30c5d7460'),
    ('candidate-panel', 'CELIKPANEL_POPULATED_CANDIDATE_PANEL',
     '74e5b00e674d720bc40caea0e2fe46c04d8d8a9d9d5085496983a7c26cc121ad'),
)
SCOPE = 'actual-binaries-private-copy-not-native-update-or-hosting'


def _sha(raw):
    return hashlib.sha256(raw).hexdigest()


def _stamp(path):
    path = Path(path)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        if not stat.S_ISREG(before.st_mode) or before.st_nlink != 1 or before.st_size > 128 * 1024 * 1024:
            raise ValueError('unsupported evidence file')
        digest, total = hashlib.sha256(), 0
        while chunk := os.read(fd, 1024 * 1024):
            total += len(chunk)
            if total > 128 * 1024 * 1024:
                raise ValueError('evidence file size bound')
            digest.update(chunk)
        fields = ('st_dev', 'st_ino', 'st_mode', 'st_uid', 'st_gid', 'st_nlink', 'st_size', 'st_mtime_ns', 'st_ctime_ns')
        identity = {field: getattr(before, field) for field in fields}
        if (identity != {field: getattr(os.fstat(fd), field) for field in fields}
                or identity != {field: getattr(path.lstat(), field) for field in fields}):
            raise ValueError('evidence file changed')
        return {**identity, 'sha256': digest.hexdigest()}
    finally:
        os.close(fd)


def _write(path, raw):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    with os.fdopen(fd, 'wb') as output:
        output.write(raw)
        output.flush()
        os.fsync(output.fileno())


def _json(path, value):
    _write(path, json.dumps(value, ensure_ascii=True, sort_keys=True, separators=(',', ':')).encode())


def _copy_binary(source, destination, expected):
    source = Path(source)
    if not source.is_absolute() or str(source) != os.path.normpath(str(source)):
        raise ValueError('binary input requires an absolute canonical path')
    for prefix in ('/var/lib/celikpanel', '/opt/celikpanel', '/run/celikpanel', '/usr/libexec/celikpanel'):
        if source == Path(prefix) or Path(prefix) in source.parents:
            raise ValueError('installed product paths cannot be binary inputs')
    parents = {}
    for path in reversed(source.parents):
        info = path.lstat()
        sticky_root = info.st_uid == 0 and bool(info.st_mode & stat.S_ISVTX)
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid not in (0, os.geteuid())
                or (stat.S_IMODE(info.st_mode) & 0o022 and not sticky_root)):
            raise ValueError('unsafe binary input ancestry')
        parents[str(path)] = (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid)
    before = _stamp(source)
    if (before['sha256'] != expected or before['st_uid'] not in (0, os.geteuid())
            or stat.S_IMODE(before['st_mode']) & 0o022 or before['st_mode'] & (stat.S_ISUID | stat.S_ISGID)
            or not before['st_mode'] & stat.S_IXUSR or os.listxattr(source, follow_symlinks=False)):
        raise ValueError('binary does not match exact allowed hash and metadata')
    # A descriptor copy followed by independent target hashing prevents a
    # concurrently replaced source from becoming an unverified executable.
    fd = os.open(source, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    try:
        opened = os.fstat(fd)
        fields = ('st_dev', 'st_ino', 'st_mode', 'st_uid', 'st_gid', 'st_nlink', 'st_size', 'st_mtime_ns', 'st_ctime_ns')
        if (not stat.S_ISREG(opened.st_mode)
                or any(getattr(opened, field) != before[field] for field in fields)):
            raise ValueError('binary descriptor identity changed before reading')
        raw = bytearray()
        while chunk := os.read(fd, 1024 * 1024):
            raw.extend(chunk)
            if len(raw) > 64 * 1024 * 1024:
                raise ValueError('binary copy bound')
    finally:
        os.close(fd)
    if _sha(raw) != expected or _stamp(source) != before:
        raise ValueError('binary input changed during copy')
    for path, identity in parents.items():
        info = Path(path).lstat()
        if identity != (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid):
            raise ValueError('binary input ancestor changed')
    _write(destination, raw)
    destination.chmod(0o500)
    copied = _stamp(destination)
    if copied['sha256'] != expected:
        raise ValueError('copied executable hash differs')
    return {'input': before, 'copy': copied}


def _migrate(binary, directory, trial, label, expected):
    before = _stamp(binary)
    if before['sha256'] != expected or before['st_uid'] != os.geteuid() or stat.S_IMODE(before['st_mode']) != 0o500:
        raise ValueError('private executable identity differs')
    env = {'PATH': '/usr/bin:/bin', 'HOME': str(trial), 'LC_ALL': 'C', 'CELIKPANEL_DATA_DIR': str(directory)}
    process = subprocess.Popen([str(binary), '--migrate-only'], cwd=trial, env=env,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE, close_fds=True,
                               start_new_session=True)
    timed_out = False
    try:
        stdout, stderr = process.communicate(timeout=45)
    except subprocess.TimeoutExpired:
        timed_out = True
        # This is only the new child session that this test created; no service,
        # cgroup, installed process or externally supplied PID is accepted.
        os.killpg(process.pid, signal.SIGKILL)
        stdout, stderr = process.communicate(timeout=5)
    except BaseException:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGKILL)
        stdout, stderr = process.communicate(timeout=5)
        _write(trial / (label + '.stdout'), stdout)
        _write(trial / (label + '.stderr'), stderr)
        raise
    _write(trial / (label + '.stdout'), stdout)
    _write(trial / (label + '.stderr'), stderr)
    _json(trial / (label + '.execution.json'), {'argv': ['private-fixed-binary', '--migrate-only'],
          'binary_sha256': expected, 'returncode': process.returncode, 'timeout': timed_out,
          'uid': os.geteuid(), 'stdout_sha256': _sha(stdout), 'stderr_sha256': _sha(stderr)})
    if timed_out or process.returncode != 0 or _stamp(binary) != before:
        raise ValueError(label + ' migration failed or executable changed')


@unittest.skipUnless(sys.platform == 'linux', 'Linux descriptor-copy safety test')
class PopulatedBinaryInputSafety(unittest.TestCase):
    def test_fifo_replacement_between_stamp_and_open_is_bounded_refusal(self):
        # Run the race in our own child with a hard deadline: the old blocking
        # second open must fail this regression instead of hanging the suite.
        with tempfile.TemporaryDirectory(prefix='cp-populated-input-') as temporary:
            root = Path(temporary)
            source = root / 'input'
            raw = b'fixture-copy-safety-only-never-executed'
            source.write_bytes(raw)
            source.chmod(0o700)
            child = os.fork()
            if child == 0:
                original = _stamp
                def replace_with_fifo(path):
                    evidence = original(path)
                    source.unlink()
                    os.mkfifo(source, 0o600)
                    return evidence
                try:
                    with mock.patch(__name__ + '._stamp', replace_with_fifo):
                        _copy_binary(source, root / 'copy', _sha(raw))
                except ValueError:
                    os._exit(0)
                except BaseException:
                    os._exit(91)
                os._exit(92)
            status, expired = None, False
            deadline = time.monotonic() + 3
            try:
                while time.monotonic() < deadline:
                    waited, value = os.waitpid(child, os.WNOHANG)
                    if waited:
                        status = value
                        break
                    time.sleep(0.01)
                if status is None:
                    expired = True
                    os.kill(child, signal.SIGKILL)
                    _, status = os.waitpid(child, 0)
            finally:
                if status is None:
                    os.kill(child, signal.SIGKILL)
                    os.waitpid(child, 0)
            self.assertFalse(expired, 'replacement FIFO blocked the binary copy')
            self.assertTrue(os.WIFEXITED(status))
            self.assertEqual(os.WEXITSTATUS(status), 0)
            self.assertFalse((root / 'copy').exists())


@unittest.skipUnless(sys.platform == 'linux', 'Linux-only private actual-binary component test')
class PopulatedDatabaseActualBinaries(unittest.TestCase):
    def test_exact_binary_schema38_to42_private_copy(self):
        values = [os.environ.get(variable) for _, variable, _ in INPUTS]
        if not any(values):
            self.skipTest('explicit actual-binary inputs absent; no native acceptance was run')
        if not all(values):
            self.fail('both explicit binary inputs are required')
        self.assertNotEqual(os.geteuid(), 0, 'explicit nonroot execution is required')
        previous_mask = os.umask(0o077)
        trial = Path(tempfile.mkdtemp(prefix='cp-populated-binaries-', dir='/var/tmp'))
        source = None
        try:
            _json(trial / 'attempt.json', {'schema': 'celikpanel/lab-populated-binaries/v1',
                  'scope': SCOPE, 'uid': os.geteuid(), 'native_acceptance': False})
            helper_bytes = (HERE / 'populated_database.py').read_bytes()
            _write(trial / 'populated_database.py', helper_bytes)
            old_bytecode = sys.dont_write_bytecode
            sys.dont_write_bytecode = True
            try:
                spec = importlib.util.spec_from_file_location('private_populated_fixture', trial / 'populated_database.py')
                helper = importlib.util.module_from_spec(spec)
                spec.loader.exec_module(helper)
            finally:
                sys.dont_write_bytecode = old_bytecode
            inputs = {}
            for (label, _, expected), value in zip(INPUTS, values):
                inputs[label] = _copy_binary(value, trial / label, expected)
            _json(trial / 'input-identities.json', {'inputs': inputs, 'fixture_sha256': _sha(helper_bytes)})
            old = trial / 'old'
            old.mkdir(mode=0o700)
            _migrate(trial / 'old-panel', old, trial, 'baseline', INPUTS[0][2])
            source = old / 'celikpanel.db'
            before = _stamp(source)
            _json(trial / 'source-before.json', before)
            admission = {'schema': helper.ADMISSION_SCHEMA, 'purpose': helper.PURPOSE,
                         'baseline_commit': helper.BASELINE_COMMIT,
                         'baseline_migrations_sha256': helper.MIGRATIONS[38],
                         'source_sha256': before['sha256'], 'nonce': secrets.token_hex(16)}
            seeded = helper.build_private_copy(source, trial, admission)
            database = Path(seeded['database'])
            proof38 = helper.verify_copy(database, seeded['manifest'],
                                         manifest_sha256=seeded['manifest_sha256'], expected_version=38)
            _json(trial / 'proof38.json', proof38)
            _migrate(trial / 'candidate-panel', database.parent, trial, 'candidate', INPUTS[1][2])
            proof42 = helper.verify_copy(database, seeded['manifest'],
                                         manifest_sha256=seeded['manifest_sha256'], expected_version=42)
            _json(trial / 'proof42.json', proof42)
            after = _stamp(source)
            _json(trial / 'source-after.json', after)
            self.assertEqual(before, after, 'original source bytes and exact identity must be retained')
            self.assertEqual(proof42['old_table_count'], 55)
            self.assertEqual(proof42['table_count'], 65)
            self.assertEqual(proof42['old_rows_missing_or_changed'], 0)
            self.assertTrue(proof42['domain_defaults_40_42_verified'])
            _json(trial / 'result.json', {'status': 'passed', 'scope': SCOPE, 'native_acceptance': False,
                  'uid': os.geteuid(), 'fixture_sha256': _sha(helper_bytes),
                  'manifest_sha256': seeded['manifest_sha256'], 'source_exactly_unchanged': True,
                  'old_rows_verified': proof42['old_row_count'], 'old_table_count': 55, 'candidate_table_count': 65})
        except BaseException as error:
            if source is not None and not (trial / 'source-after.json').exists():
                try:
                    _json(trial / 'source-after.json', _stamp(source))
                except BaseException as stamp_error:
                    _json(trial / 'source-after-unavailable.json', {'error': type(stamp_error).__name__})
            _json(trial / 'failure.json', {'status': 'failed', 'scope': SCOPE,
                  'native_acceptance': False, 'error': type(error).__name__, 'message': str(error)})
            raise
        finally:
            os.umask(previous_mask)
            # Every attempt is retained, never automatically removed or retried.
            print('Private component evidence retained: ' + str(trial), flush=True)


if __name__ == '__main__':
    unittest.main()
