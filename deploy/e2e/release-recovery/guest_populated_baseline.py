#!/usr/bin/env python3
"""SQL fixture seeding/capture in one registered disposable Alpha64 QEMU guest.

No service lifecycle commands, update, repair, arbitrary paths or production
admission. The host controller must independently guard the registered VM, stop
the two coordinators before seed, or freeze them around capture and always thaw.
Original DB inodes and raw DB/WAL/SHM evidence are retained. SQL fixture rows do
not establish real hosting, DNS provisioning or user/API admission acceptance.
"""
from __future__ import annotations

import argparse
import contextlib
import ctypes
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import pwd
import re
import sqlite3
import stat
import subprocess
import sys
import tempfile
import time

HERE = Path(__file__).resolve().parent


def _module(name, filename):
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


probe = _module('populated_guest_probe', 'guest_probe.py')
populated = _module('populated_guest_copy', 'populated_database.py')
profiles = _module('populated_guest_profiles', 'baseline_profiles.py')
PRIVATE = Path('/root/celikpanel-release-recovery-lab')
CANONICAL = Path('/var/lib/celikpanel/celikpanel.db')
TRANSACTIONS = Path('/var/lib/celikpanel-release-transaction')
CGROUP = Path('/sys/fs/cgroup/system.slice')
BIN = Path('/opt/celikpanel/bin')
PROC = Path('/proc')
HEX32 = re.compile(r'[0-9a-f]{32}\Z')
UNITS = ('celikpanel-panel.service', 'celikpanel-agent.service')
SIDECARS = ('', '-wal', '-shm', '-journal')
SCOPE = 'disposable-lab-sql-data-not-native-hosting'
FIELDS = ('st_dev', 'st_ino', 'st_mode', 'st_uid', 'st_gid', 'st_nlink', 'st_size', 'st_mtime_ns', 'st_ctime_ns')


class Refused(ValueError):
    pass


def _identity(info):
    return {field: getattr(info, field) for field in FIELDS}


def _stable(record):
    # Our own exchange changes ctime. Inode/bytes/mode/owner/mtime remain exact.
    return {key: value for key, value in record.items() if key != 'st_ctime_ns'}


def _sha(raw):
    return hashlib.sha256(raw).hexdigest()


def _json(raw):
    return probe.strict_object(raw)


def _write_json(path, value):
    populated._write_new(path, json.dumps(value, sort_keys=True, separators=(',', ':')).encode())


def _read_json(path):
    raw, _ = populated._read_private(path)
    return _json(raw), _sha(raw)


def _tick(deadline):
    if time.monotonic() >= deadline:
        raise Refused('bounded observation deadline exceeded; preserve evidence')


def _args(args):
    if not isinstance(args.seed_id, str) or not HEX32.fullmatch(args.seed_id):
        raise Refused('exact seed identity required')
    identity = probe.guard_guest(args)
    populated._private_directory(PRIVATE)
    return identity, PRIVATE / ('populated-baseline-' + args.seed_id)


def _parent():
    user = pwd.getpwnam('celikpanel')
    if user.pw_uid <= 0 or user.pw_gid <= 0:
        raise Refused('unprivileged panel account required')
    anchors = probe.protected_parents(CANONICAL, {0, user.pw_uid})
    info = CANONICAL.parent.lstat()
    if (info.st_uid != user.pw_uid or info.st_gid != user.pw_gid
            or stat.S_IMODE(info.st_mode) != 0o750 or os.listxattr(CANONICAL.parent, follow_symlinks=False)):
        raise Refused('exact panel-owned0750 canonical parent required')
    fd = os.open(CANONICAL.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    opened = os.fstat(fd)
    if (opened.st_dev, opened.st_ino) != (info.st_dev, info.st_ino):
        os.close(fd)
        raise Refused('canonical parent changed before pin')
    return fd, user.pw_uid, user.pw_gid, anchors


def _parent_check(fd, uid, anchors):
    current = probe.protected_parents(CANONICAL, {0, uid})
    info = CANONICAL.parent.lstat()
    if (current != anchors or (info.st_dev, info.st_ino) != (os.fstat(fd).st_dev, os.fstat(fd).st_ino)
            or os.listxattr(fd)):
        raise Refused('canonical ancestry changed')


def _read_at(parent, name, uid, gid, deadline):
    _tick(deadline)
    fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=parent)
    try:
        info = os.fstat(fd)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != uid or info.st_gid != gid
                or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1
                or info.st_size > populated.MAX_BYTES or os.listxattr(fd)):
            raise Refused('unsafe canonical file metadata')
        chunks, size = [], 0
        while raw := os.read(fd, 1024 * 1024):
            _tick(deadline)
            size += len(raw)
            if size > populated.MAX_BYTES:
                raise Refused('canonical copy bound exceeded')
            chunks.append(raw)
        if (_identity(info) != _identity(os.fstat(fd))
                or _identity(info) != _identity(os.stat(name, dir_fd=parent, follow_symlinks=False))):
            raise Refused('canonical file changed while copying')
        raw = b''.join(chunks)
        return raw, {**_identity(info), 'sha256': _sha(raw)}
    finally:
        os.close(fd)


def _files(parent, uid, gid, deadline, *, standalone):
    result, raw = {}, {}
    for suffix in SIDECARS:
        name = 'celikpanel.db' + suffix
        try:
            content, record = _read_at(parent, name, uid, gid, deadline)
        except FileNotFoundError:
            if not suffix:
                raise
            continue
        if standalone and suffix:
            raise Refused('stopped source retains sidecars; do not remove or normalize them')
        result[name], raw[name] = record, content
    if raw['celikpanel.db'][:16] != b'SQLite format 3\x00':
        raise Refused('canonical SQLite header unavailable')
    return result, raw


def _services(mode, deadline):
    result = {}
    wanted = {'Id', 'LoadState', 'ActiveState', 'SubState', 'MainPID', 'ControlGroup', 'FreezerState'}
    for unit in UNITS:
        _tick(deadline)
        command = ['/usr/bin/systemctl', 'show', unit]
        for name in sorted(wanted):
            command.extend(('-p', name))
        observation = subprocess.run(command, stdin=subprocess.DEVNULL, capture_output=True, text=True,
                                     timeout=min(1, max(0.01, deadline - time.monotonic())),
                                     env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LC_ALL': 'C'})
        fields = {}
        if len(observation.stdout) > 8192 or observation.returncode:
            raise Refused('native service observation unavailable')
        for line in observation.stdout.splitlines():
            key, separator, value = line.partition('=')
            if not separator or key in fields:
                raise Refused('native service observation malformed')
            fields[key] = value
        expected_group = '/system.slice/' + unit
        if (set(fields) != wanted or fields['Id'] != unit or fields['LoadState'] != 'loaded'
                or not fields['MainPID'].isdigit() or fields['ControlGroup'] not in ('', expected_group)):
            raise Refused('exact native service identity unavailable')
        group = CGROUP / unit
        try:
            processes = probe.bounded_file(group / 'cgroup.procs', 32768, virtual=True).decode().split()
        except probe.ProbeError:
            if mode != 'stopped' or group.exists() or group.is_symlink():
                raise
            processes = []
        if any(not p.isdigit() or int(p) <= 1 for p in processes):
            raise Refused('unsafe cgroup process observation')
        if mode == 'stopped':
            if fields['ActiveState'] not in ('inactive', 'failed') or fields['MainPID'] != '0' or processes:
                raise Refused('both coordinators must already be stopped and empty')
        elif mode == 'frozen':
            events = probe.bounded_file(group / 'cgroup.events', 4096, virtual=True).decode().splitlines()
            freeze = probe.bounded_file(group / 'cgroup.freeze', 32, virtual=True).strip()
            if (fields['ActiveState'] != 'active' or fields['FreezerState'] != 'frozen'
                    or fields['ControlGroup'] != expected_group or fields['MainPID'] not in processes
                    or freeze != b'1' or 'frozen 1' not in events):
                raise Refused('both exact coordinator cgroups must already be frozen')
        else:
            raise Refused('unknown service proof mode')
        starts = {}
        for pid in processes:
            data = probe.bounded_file(PROC / pid / 'stat', 16384, virtual=True).decode()
            tail = data.rsplit(')', 1)[1].split()
            if len(tail) < 20 or not tail[19].isdigit():
                raise Refused('process identity unavailable')
            starts[pid] = tail[19]
        result[unit] = {'properties': fields, 'processes': starts}
    return result


def _no_foreign_handles(records, services, deadline):
    targets = {(r['st_dev'], r['st_ino']) for r in records.values()}
    allowed = {os.getpid()} | {int(pid) for service in services.values() for pid in service['processes']}
    processes = [entry for entry in PROC.iterdir() if entry.name.isdigit()]
    if len(processes) > 4096:
        raise Refused('process scan bound exceeded')
    count = 0
    for process in processes:
        _tick(deadline)
        if int(process.name) in allowed:
            continue
        try:
            descriptors = list((process / 'fd').iterdir())
        except FileNotFoundError:
            continue
        for descriptor in descriptors:
            count += 1
            if count > 100000:
                raise Refused('descriptor scan bound exceeded')
            try:
                info = descriptor.stat()
            except FileNotFoundError:
                continue
            if (info.st_dev, info.st_ino) in targets:
                raise Refused('foreign process retains a database handle')


def _idle_names():
    populated._private_directory(TRANSACTIONS)
    if set(p.name for p in TRANSACTIONS.iterdir()) != {'transaction.lock'}:
        raise Refused('release state is not strictly idle')


@contextlib.contextmanager
def _idle_lock():
    _idle_names()
    lock = TRANSACTIONS / 'transaction.lock'
    fd = os.open(lock, os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    try:
        info = os.fstat(fd)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_gid != 0
                or stat.S_IMODE(info.st_mode) != 0o600 or info.st_size != 0 or info.st_nlink != 1
                or os.listxattr(fd)):
            raise Refused('unsafe existing release lock')
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        def revalidate():
            _idle_names()
            if _identity(lock.lstat()) != _identity(info) or _identity(os.fstat(fd)) != _identity(info):
                raise Refused('release lock identity changed')
        revalidate()
        yield revalidate
        revalidate()
    finally:
        os.close(fd)


def _baseline():
    profile = profiles.get_profile(profiles.ALPHA64)
    expected = {'agent': profile.agent_sha256, 'panel': profile.panel_sha256}
    for name, digest in expected.items():
        path = BIN / name
        probe.protected_parents(path, {0})
        info = path.lstat()
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_gid != 0
                or info.st_nlink != 1 or stat.S_IMODE(info.st_mode) & 0o022):
            raise Refused('unsafe baseline executable')
        if _sha(probe.bounded_file(path, 64 * 1024 * 1024)) != digest:
            raise Refused('genuine Alpha64 executable identity differs')
    return expected


def _exchange(parent, stage):
    libc = ctypes.CDLL(None, use_errno=True)
    function = libc.renameat2
    function.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
    function.restype = ctypes.c_int
    if function(parent, b'celikpanel.db', stage, b'celikpanel.db', 2):
        error = ctypes.get_errno()
        raise OSError(error, os.strerror(error))


def seed(args):
    identity, directory = _args(args)
    deadline = time.monotonic() + 30
    with _idle_lock() as lock_check:
        services = _services('stopped', deadline)
        baseline = _baseline()
        parent, uid, gid, anchors = _parent()
        stage_fd = None
        try:
            before, raw = _files(parent, uid, gid, deadline, standalone=True)
            _no_foreign_handles(before, services, deadline)
            directory.mkdir(mode=0o700)  # Existing/partial attempts are never adopted.
            _write_json(directory / 'admission.json', {'schema': 'celikpanel/lab-populated-seed/v1',
                        'identity': identity, 'seed_id': args.seed_id, 'scope': SCOPE,
                        'baseline_artifacts': baseline, 'source': before})
            populated._write_new(directory / 'source.db', raw['celikpanel.db'])
            request = {'schema': populated.ADMISSION_SCHEMA, 'purpose': populated.PURPOSE,
                       'baseline_commit': populated.BASELINE_COMMIT,
                       'baseline_migrations_sha256': populated.MIGRATIONS[38],
                       'source_sha256': before['celikpanel.db']['sha256'], 'nonce': args.seed_id}
            built = populated.build_private_copy(directory / 'source.db', directory, request)
            manifest_raw = json.dumps(built['manifest'], sort_keys=True, separators=(',', ':')).encode()
            populated._write_new(directory / 'manifest.json', manifest_raw)
            payload, _ = populated._read_private(Path(built['database']))
            stage_name = '.lab-populated-' + args.seed_id
            os.mkdir(stage_name, 0o700, dir_fd=parent)
            stage_fd = os.open(stage_name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent)
            fd = os.open('celikpanel.db', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC,
                         0o600, dir_fd=stage_fd)
            with os.fdopen(fd, 'wb') as stream:
                stream.write(payload)
                stream.flush()
                os.fchown(stream.fileno(), uid, gid)
                os.fsync(stream.fileno())
            _, after = _read_at(stage_fd, 'celikpanel.db', uid, gid, deadline)
            stage_identity = _identity(os.fstat(stage_fd))
            os.fsync(stage_fd)
            os.fsync(parent)
            intent = {'schema': 'celikpanel/lab-populated-exchange/v1', 'identity': identity,
                      'seed_id': args.seed_id, 'before': before['celikpanel.db'], 'after': after,
                      'stage_name': stage_name, 'stage_identity': stage_identity,
                      'manifest_sha256': built['manifest_sha256'], 'scope': SCOPE}
            _write_json(directory / 'exchange.json', intent)
            # Reprove every live authority/input immediately before the one exchange.
            if (_args(args)[0] != identity or _services('stopped', deadline) != services
                    or _baseline() != baseline or _files(parent, uid, gid, deadline, standalone=True)[0] != before):
                raise Refused('seed precondition changed')
            _no_foreign_handles(before, services, deadline)
            _parent_check(parent, uid, anchors)
            lock_check()
            if (_identity(os.stat(stage_name, dir_fd=parent, follow_symlinks=False)) != _identity(os.fstat(stage_fd))
                    or _read_at(stage_fd, 'celikpanel.db', uid, gid, deadline)[1] != after):
                raise Refused('seed staging identity changed')
            _exchange(parent, stage_fd)
            os.fsync(stage_fd)
            os.fsync(parent)
            installed = _read_at(parent, 'celikpanel.db', uid, gid, deadline)[1]
            original = _read_at(stage_fd, 'celikpanel.db', uid, gid, deadline)[1]
            if _stable(installed) != _stable(after) or _stable(original) != _stable(before['celikpanel.db']):
                raise Refused('seed exchange result unconfirmed; preserve same attempt')
            _no_foreign_handles({'celikpanel.db': installed}, services, deadline)
            _parent_check(parent, uid, anchors)
            lock_check()
            if _args(args)[0] != identity or _services('stopped', deadline) != services:
                raise Refused('seed final authority changed')
            # Bind the retained pathname again after final authority observation;
            # an owner rename must not produce a receipt for a stale name.
            if (_identity(os.stat(stage_name, dir_fd=parent, follow_symlinks=False)) != _identity(os.fstat(stage_fd))
                    or _read_at(parent, 'celikpanel.db', uid, gid, deadline)[1] != installed
                    or _read_at(stage_fd, 'celikpanel.db', uid, gid, deadline)[1] != original):
                raise Refused('seed final retained name or bytes changed; preserve same attempt')
            result = {'schema': 'celikpanel/lab-populated-installed/v1', 'identity': identity,
                      'seed_id': args.seed_id, 'scope': SCOPE, 'installed': installed,
                      'original_retained': original, 'stage_name': stage_name,
                      'manifest_sha256': built['manifest_sha256']}
            _write_json(directory / 'installed.json', result)
            return result
        finally:
            if stage_fd is not None:
                os.close(stage_fd)
            os.close(parent)


def _seed_record(args):
    identity, directory = _args(args)
    installed, _ = _read_json(directory / 'installed.json')
    manifest, digest = _read_json(directory / 'manifest.json')
    if (installed.get('schema') != 'celikpanel/lab-populated-installed/v1'
            or installed.get('identity') != identity or installed.get('seed_id') != args.seed_id
            or installed.get('manifest_sha256') != digest or manifest['admission']['nonce'] != args.seed_id):
        raise Refused('populated seed receipt binding differs')
    return identity, directory, installed, manifest


def capture(args):
    identity, directory, installed, _ = _seed_record(args)
    if not isinstance(args.capture_id, str) or not HEX32.fullmatch(args.capture_id):
        raise Refused('exact capture identity required')
    deadline = time.monotonic() + 5
    with _idle_lock() as lock_check:
        services = _services('frozen', deadline)
        parent, uid, gid, anchors = _parent()
        try:
            before, raw = _files(parent, uid, gid, deadline, standalone=False)
            _no_foreign_handles(before, services, deadline)
            observation = directory / ('capture-' + args.capture_id)
            observation.mkdir(mode=0o700)
            raw_directory = observation / 'raw'
            raw_directory.mkdir(mode=0o700)
            for name, content in raw.items():
                _tick(deadline)
                populated._write_new(raw_directory / name, content)
            if (_files(parent, uid, gid, deadline, standalone=False)[0] != before
                    or _services('frozen', deadline) != services or _args(args)[0] != identity):
                raise Refused('frozen canonical bytes or authority changed')
            _parent_check(parent, uid, anchors)
            lock_check()
            _tick(deadline)
            result = {'schema': 'celikpanel/lab-populated-capture/v1', 'identity': identity,
                      'seed_id': args.seed_id, 'capture_id': args.capture_id, 'scope': SCOPE,
                      'manifest_sha256': installed['manifest_sha256'], 'source': before,
                      'services': services, 'sqlite_opened_original': False}
            _write_json(observation / 'capture.json', result)
            _tick(deadline)
            return result
        finally:
            os.close(parent)


def verify(args):
    identity, directory, installed, manifest = _seed_record(args)
    if not isinstance(args.capture_id, str) or not HEX32.fullmatch(args.capture_id):
        raise Refused('exact capture identity required')
    if type(args.expected_version) is not int or args.expected_version not in (38, 42):
        raise Refused('exact38 or42 verification required')
    observation = directory / ('capture-' + args.capture_id)
    record, record_hash = _read_json(observation / 'capture.json')
    if (record.get('schema') != 'celikpanel/lab-populated-capture/v1' or record.get('identity') != identity
            or record.get('seed_id') != args.seed_id or record.get('capture_id') != args.capture_id
            or record.get('manifest_sha256') != installed['manifest_sha256']):
        raise Refused('capture receipt binding differs')
    raw_directory = observation / 'raw'
    populated._private_directory(raw_directory)
    names = set(record['source'])
    if (not names.issubset({'celikpanel.db', 'celikpanel.db-wal', 'celikpanel.db-shm'})
            or 'celikpanel.db' not in names or set(p.name for p in raw_directory.iterdir()) != names
            or ('celikpanel.db-wal' in names) != ('celikpanel.db-shm' in names)):
        raise Refused('raw SQLite inventory needs unsupported recovery')
    # No mutation/read-only SQLite opening of the retained raw originals. Copy
    # each through a descriptor into another new private directory first.
    work = Path(tempfile.mkdtemp(prefix='verify-', dir=observation))
    raw_records = {}
    for name in sorted(names):
        # _read_private rejects sidecars of a basename. Each raw file here has
        # already passed exact inventory validation; use pinned _read_at instead.
        fd = os.open(raw_directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            content, raw_records[name] = _read_at(fd, name, 0, 0, time.monotonic() + 5)
        finally:
            os.close(fd)
        if raw_records[name]['sha256'] != record['source'][name]['sha256']:
            raise Refused('retained raw evidence hash differs')
        populated._write_new(work / name, content)
    standalone = work / 'standalone.db'
    populated._write_new(standalone, b'')
    source_connection = target_connection = None
    deadline = time.monotonic() + 10
    try:
        source_connection = sqlite3.connect((work / 'celikpanel.db').as_uri() + '?mode=ro', uri=True, timeout=0)
        source_connection.execute('PRAGMA query_only=ON')
        source_connection.execute('PRAGMA trusted_schema=OFF')
        target_connection = sqlite3.connect(standalone.as_uri() + '?mode=rw', uri=True, timeout=0)
        source_connection.backup(target_connection, pages=128, sleep=0, progress=lambda *_: _tick(deadline))
    finally:
        if target_connection is not None:
            target_connection.close()
        if source_connection is not None:
            source_connection.close()
    proof = populated.verify_copy(standalone, manifest, manifest_sha256=installed['manifest_sha256'],
                                  expected_version=args.expected_version)
    fd = os.open(raw_directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        for name, original in raw_records.items():
            if _read_at(fd, name, 0, 0, time.monotonic() + 5)[1] != original:
                raise Refused('raw evidence changed during private verification')
    finally:
        os.close(fd)
    if (_read_json(observation / 'capture.json')[1] != record_hash
            or _seed_record(args)[2]['manifest_sha256'] != installed['manifest_sha256']):
        raise Refused('verification authority changed')
    result = {'schema': 'celikpanel/lab-populated-verification/v1', 'scope': SCOPE,
              'identity': identity, 'seed_id': args.seed_id, 'capture_id': args.capture_id,
              'capture_sha256': record_hash, 'raw_evidence_unchanged': True, 'proof': proof}
    _write_json(work / 'verification.json', result)
    return result


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('seed', 'capture', 'verify'))
    for name in ('lab-nonce', 'vm-uuid', 'cell-id', 'node', 'seed-id'):
        parser.add_argument('--' + name, required=True)
    parser.add_argument('--capture-id')
    parser.add_argument('--expected-version', type=int, choices=(38, 42))
    args = parser.parse_args(argv)
    if ((args.action == 'seed' and (args.capture_id is not None or args.expected_version is not None))
            or (args.action == 'capture' and (args.capture_id is None or args.expected_version is not None))
            or (args.action == 'verify' and (args.capture_id is None or args.expected_version is None))):
        parser.error('arguments must match the exact seed, capture or verify action')
    try:
        result = {'seed': seed, 'capture': capture, 'verify': verify}[args.action](args)
        print(json.dumps(result, sort_keys=True))
        return 0
    except Exception as error:
        print(json.dumps({'status': 'unconfirmed', 'reason': type(error).__name__, 'message': str(error),
                          'action': 'Preserve this attempt; do not infer success or retry seed in place.'}), file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
