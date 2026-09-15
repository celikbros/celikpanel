#!/usr/bin/env python3
"""Read-only native WAL checkpoint evidence; never attach, signal or authorize SQL.

The tracer supplies an actual matched syscall exit and its stopped inventory.
This adapter independently rereads the fixed guest, process and filesystem facts.
A positive result proves physical noncommit WAL bytes at that stop, not entry or
non-entry into SQLite's Commit function. No original SQLite file is opened by SQL.
"""
from __future__ import annotations
import hashlib
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import pwd
import re
import stat
import sys
import time

HERE = Path(__file__).resolve().parent

def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    value = importlib.util.module_from_spec(spec)
    sys.modules[name] = value
    spec.loader.exec_module(value)
    return value

checkpoint = module('wal_checkpoint_database', 'guest_database_checkpoint.py')
matcher = module('wal_checkpoint_matcher', 'wal_migration_identity.py')
wal = module('wal_checkpoint_frames', 'wal_frames.py')
probe = checkpoint.probe
SCHEMA = 'celikpanel/lab-native-wal-checkpoint/v1'
PARENT = Path('/var/lib/celikpanel')
MIGRATIONS = PARENT / '.release-db-migrations'
TRANSACTIONS = Path('/var/lib/celikpanel-release-transaction')
SNAPSHOTS = Path('/var/backups/celikpanel/update-snapshots')
PROC = Path('/proc')
CGROUP = Path('/sys/fs/cgroup')
INSTALLED = Path('/opt/celikpanel/bin')
FLAGS = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC
HEX32 = re.compile(r'[0-9a-f]{32}\Z')
HEX64 = re.compile(r'[0-9a-f]{64}\Z')
SNAPSHOT = re.compile(r'[0-9]{8}T[0-9]{6}Z-from-(?:unknown|[0-9a-f]{40})-to-[0-9a-f]{40}-[0-9a-f]{32}\Z')
ADMISSION_KEYS = {'schema', 'snapshot', 'transaction_token_sha256', 'material_sha256',
                  'snapshot_manifest_sha256', 'candidate_panel_sha256', 'parent',
                  'before', 'root', 'transaction', 'authority', 'work', 'initial'}
LIMITS = ['read-only-observation-not-signal-or-product-authority',
          'successful-syscall-stop-supplied-by-exact-native-tracer',
          'physical-noncommit-WAL-not-SQLite-BEGIN-or-Commit-call-observation',
          'no-original-SQLite-open-no-row-data-output-no-durability-claim']

class Inconclusive(ValueError):
    pass

def require(value, reason):
    if not value:
        raise Inconclusive(reason)

def sha(raw):
    return hashlib.sha256(raw).hexdigest()

def directory_id(info):
    return {k: v for k, v in zip(('dev', 'ino', 'mode', 'uid', 'gid'),
            (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid))}

def identity(info):
    return {**directory_id(info), 'links': info.st_nlink, 'size': info.st_size,
            'mtime_ns': info.st_mtime_ns, 'ctime_ns': info.st_ctime_ns}

def product_file(value):
    i = value['identity']
    return {'identity': {**{k: i[k] for k in ('dev', 'ino', 'mode', 'uid', 'gid', 'links', 'size')},
            'mtime': {'Sec': i['mtime_ns'] // 1000000000, 'Nsec': i['mtime_ns'] % 1000000000},
            'ctime': {'Sec': i['ctime_ns'] // 1000000000, 'Nsec': i['ctime_ns'] % 1000000000}},
            'sha256': value['sha256']}

class Budget:
    def __init__(self, tick):
        self.end, self.bytes, self.tick = time.monotonic() + 30, 0, tick
    def __call__(self, size=0):
        self.bytes += size
        require(time.monotonic() < self.end and self.bytes <= 2 * 1024 ** 3, 'observation-budget')
        self.tick()

def account():
    owner = pwd.getpwnam('celikpanel')
    require(owner.pw_uid > 0 and owner.pw_gid > 0, 'panel-account-unavailable')
    return owner.pw_uid, owner.pw_gid

def _open_dir(path, owners):
    require(path.is_absolute() and '..' not in path.parts, 'invalid-fixed-path')
    fd = os.open('/', FLAGS | os.O_DIRECTORY)
    chain = []
    try:
        current = Path('/')
        for part in ('', *path.parts[1:]):
            if part:
                nxt = os.open(part, FLAGS | os.O_DIRECTORY, dir_fd=fd)
                os.close(fd)
                fd, current = nxt, current / part
            info = os.fstat(fd)
            panel_parent = current == PARENT or (current.name == 'work' and current.parent.parent == MIGRATIONS)
            virtual = current == PROC or PROC in current.parents
            allowed = {0, account()[0]} if panel_parent else owners | {0} if virtual else {0}
            sticky = info.st_uid == 0 and bool(stat.S_IMODE(info.st_mode) & stat.S_ISVTX)
            require(stat.S_ISDIR(info.st_mode) and info.st_uid in allowed
                    and (virtual or sticky or not stat.S_IMODE(info.st_mode) & 0o022), 'unsafe-directory-chain')
            chain.append(directory_id(info))
        return fd, chain
    except BaseException:
        os.close(fd)
        raise

def _read(path, budget, owners, limit=256 * 1024 ** 2, *, raw=False, private=False, virtual=False):
    budget()
    parent, chain = _open_dir(path.parent, owners)
    fd = None
    try:
        fd = os.open(path.name, FLAGS, dir_fd=parent)
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1
                and before.st_uid in owners and (virtual or not stat.S_IMODE(before.st_mode) & 0o022)
                and (virtual or 0 <= before.st_size <= limit), 'unsafe-file')
        if private:
            require(before.st_uid == before.st_gid == 0 and stat.S_IMODE(before.st_mode) == 0o600, 'unsafe-private-file')
        if not virtual:
            require(not os.listxattr(fd), 'unsupported-file-attributes')
        h, chunks, total = hashlib.sha256(), [], 0
        while True:
            chunk = os.read(fd, min(1048576, limit + 1 - total))
            if not chunk:
                break
            total += len(chunk)
            budget(len(chunk))
            require(total <= limit, 'file-byte-bound')
            h.update(chunk)
            if raw:
                chunks.append(chunk)
        require(identity(os.fstat(fd)) == identity(before)
                and identity(os.stat(path.name, dir_fd=parent, follow_symlinks=False)) == identity(before), 'file-changed')
        fresh, again = _open_dir(path.parent, owners)
        os.close(fresh)
        require(again == chain, 'parent-chain-changed')
        proof = {'identity': identity(before), 'sha256': h.hexdigest()}
        return (proof, b''.join(chunks)) if raw else proof
    finally:
        if fd is not None:
            os.close(fd)
        os.close(parent)

def _directory(path, owners, expected_uid=None, expected_gid=None, mode=None):
    fd, _ = _open_dir(path, owners)
    try:
        info = os.fstat(fd)
        require(not os.listxattr(fd), 'unsupported-directory-attributes')
        if expected_uid is not None:
            require(info.st_uid == expected_uid and info.st_gid == expected_gid
                    and stat.S_IMODE(info.st_mode) == mode, 'directory-layout-differs')
        return directory_id(info)
    finally:
        os.close(fd)

def _names(path, owners, maximum=20000):
    fd, _ = _open_dir(path, owners)
    try:
        before = directory_id(os.fstat(fd))
        names = []
        with os.scandir(fd) as entries:
            for entry in entries:
                names.append(entry.name)
                require(len(names) <= maximum, 'directory-entry-bound')
        require(before == directory_id(os.fstat(fd)), 'directory-changed')
        return sorted(names)
    finally:
        os.close(fd)

def transaction(budget, *, allow_absent=False, hint=False):
    records = []
    for phase in ('active', 'completion.pending', 'scheduler-restore.pending', 'quiesce.pending'):
        try:
            proof, raw = _read(TRANSACTIONS / phase, budget, {0}, 4096, raw=True, private=True)
        except FileNotFoundError:
            continue
        if phase != 'active' and hint:
            return None
        require(phase == 'active', 'not-active-update')
        match = re.fullmatch(rb'version=1\ntoken=([0-9a-f]{64})\noperation=update\nsnapshot=([^\n]+)\n', raw)
        require(match is not None, 'transaction-malformed')
        snapshot = match[2].decode('ascii')
        require(SNAPSHOT.fullmatch(snapshot), 'snapshot-name-invalid')
        records.append({'snapshot': snapshot, 'transaction_token_sha256': sha(match[1]),
                        'transaction_operation': 'update', 'transaction_phase': 'active',
                        'marker': proof})
    if not records and allow_absent:
        return None
    require(len(records) == 1, 'active-transaction-unavailable')
    return records[0]

def valid_product_file(value, uid, gid):
    require(type(value) is dict and set(value) == {'identity', 'sha256'}
            and type(value['sha256']) is str and HEX64.fullmatch(value['sha256']), 'admission-file-shape')
    item = value['identity']
    fields = {'dev', 'ino', 'mode', 'uid', 'gid', 'links', 'size', 'mtime', 'ctime'}
    require(type(item) is dict and set(item) == fields
            and all(type(item[k]) is int and item[k] >= 0 for k in fields - {'mtime', 'ctime'})
            and item['dev'] > 0 and item['ino'] > 0 and item['mode'] == stat.S_IFREG | 0o600
            and item['uid'] == uid and item['gid'] == gid and item['links'] == 1
            and 0 < item['size'] <= 256 * 1024 ** 2, 'admission-file-metadata')
    for name in ('mtime', 'ctime'):
        stamp = item[name]
        require(type(stamp) is dict and set(stamp) == {'Sec', 'Nsec'}
                and type(stamp['Sec']) is int and type(stamp['Nsec']) is int
                and 0 <= stamp['Nsec'] < 1000000000, 'admission-file-timestamp')

def admission(plan, tx, budget, uid, gid):
    token, snapshot = tx['transaction_token_sha256'], tx['snapshot']
    base = MIGRATIONS / token
    authority, work = base / 'authority', base / 'work'
    proof, raw = _read(authority / 'admission.json', budget, {0}, 1048576, raw=True, private=True)
    value = probe.strict_object(raw)
    require(set(value) == ADMISSION_KEYS and value['schema'] == 'celikpanel/database-migration-admission/v1', 'admission-shape')
    expected = {'snapshot': snapshot, 'transaction_token_sha256': token,
                'candidate_panel_sha256': plan['candidate']['files']['bin/panel']}
    require(all(value[k] == v for k, v in expected.items()), 'admission-tuple-differs')
    require(all(type(value[k]) is str and HEX64.fullmatch(value[k]) for k in
                ('material_sha256', 'snapshot_manifest_sha256', 'candidate_panel_sha256')), 'admission-digest-invalid')
    dirs = {'parent': _directory(PARENT, {uid}, uid, gid, 0o750),
            'root': _directory(MIGRATIONS, {uid}, 0, gid, 0o710),
            'transaction': _directory(base, {uid}, 0, gid, 0o710),
            'authority': _directory(authority, {uid}, 0, 0, 0o700),
            'work': _directory(work, {uid}, uid, gid, 0o700)}
    require(all(type(value[k]) is dict and set(value[k]) == set(v)
                and all(type(n) is int for n in value[k].values()) and value[k] == v
                for k, v in dirs.items()), 'admission-directory-differs')
    valid_product_file(value['before'], uid, gid)
    valid_product_file(value['initial'], uid, gid)
    # Only an active, unsealed migration can be selected. Never reinterpret a
    # publication or a partial authority record as an unfinished WAL operation.
    require(_names(authority, {uid}, 64) == ['admission.json'], 'migration-already-sealed-or-incomplete-authority')
    return value, proof, work

def validate_plan(args, plan):
    guest = probe.guard_guest(args)
    require(guest == plan.get('identity') and plan.get('operation_id') == args.operation_id
            and type(args.operation_id) is str and HEX32.fullmatch(args.operation_id), 'guest-or-operation-differs')
    candidate = plan['candidate']
    require(type(candidate.get('commit')) is str and re.fullmatch(r'[0-9a-f]{40}', candidate['commit'])
            and type(candidate.get('manifest_sha256')) is str and HEX64.fullmatch(candidate['manifest_sha256']), 'candidate-identity-invalid')
    require(all(type(candidate['files'].get('bin/' + n)) is str and HEX64.fullmatch(candidate['files']['bin/' + n])
                for n in ('agent', 'panel')), 'candidate-binary-identity-invalid')
    return guest

def writer_expected(args, plan, worker_start_ticks, tick=lambda: None):
    """Lightweight non-authorizing identity hint; absent admission returns None."""
    validate_plan(args, plan)
    require(type(worker_start_ticks) is int and worker_start_ticks > 0, 'worker-start-hint-invalid')
    budget = Budget(tick)
    uid, gid = account()
    try:
        tx = transaction(budget, allow_absent=True, hint=True)
        if tx is None:
            return None
        value, proof, _ = admission(plan, tx, budget, uid, gid)
    except FileNotFoundError:
        return None
    return {'operation_id': args.operation_id, 'snapshot': tx['snapshot'],
            'token_sha256': tx['transaction_token_sha256'], 'admission_sha256': proof['sha256'],
            'candidate_panel_sha256': plan['candidate']['files']['bin/panel'], 'uid': uid, 'gid': gid,
            'worker_start_ticks': int(worker_start_ticks), 'work': value['work']}

def _virtual(path, budget, owners, limit=16384):
    return _read(path, budget, owners, limit, raw=True, virtual=True)[1]

def proc_status(tid, budget, owners):
    raw = _virtual(PROC / str(tid) / 'status', budget, owners)
    values = {}
    for line in raw.decode('ascii').splitlines():
        key, sep, value = line.partition(':')
        require(sep and key not in values, 'process-status-malformed')
        values[key] = value.strip()
    return values

def proc_start(tid, budget, owners):
    raw = _virtual(PROC / str(tid) / 'stat', budget, owners).decode('ascii')
    tail = raw.rsplit(')', 1)[1].split()
    require(len(tail) >= 20 and tail[19].isdigit(), 'process-start-unavailable')
    return int(tail[19])

def stopped_inventory(worker, trace, budget, uid):
    require(type(trace) is dict and set(trace) == {'tracer_pid', 'tasks', 'writer', 'write'}
            and trace['tracer_pid'] == os.getpid(), 'tracer-identity-differs')
    tasks = trace['tasks']
    require(type(tasks) is list and 1 <= len(tasks) <= 256, 'task-inventory-bound')
    expected = {}
    for task in tasks:
        require(type(task) is dict and set(task) == {'tid', 'tgid', 'start_ticks'}
                and all(type(x) is int and x > 1 for x in task.values()), 'task-identity-malformed')
        require(task['tid'] not in expected, 'duplicate-task')
        expected[task['tid']] = task
    group = worker['cgroup']
    def ids(name):
        raw = _virtual(CGROUP / group.lstrip('/') / name, budget, {0})
        require(re.fullmatch(rb'(?:[1-9][0-9]*\n)+', raw), 'cgroup-inventory-malformed')
        values = [int(v) for v in raw.split()]
        require(len(values) <= 256 and len(set(values)) == len(values), 'cgroup-inventory-bound')
        return set(values)
    require(ids('cgroup.threads') == set(expected)
            and ids('cgroup.procs') == {t['tgid'] for t in tasks}, 'cgroup-inventory-incomplete')
    observations = []
    for tid, task in sorted(expected.items()):
        status = proc_status(tid, budget, {0, uid})
        require(status.get('State', '').startswith('t ') and status.get('TracerPid') == str(os.getpid())
                and status.get('Tgid') == str(task['tgid']) and status.get('Pid') == str(tid)
                and proc_start(tid, budget, {0, uid}) == task['start_ticks'], 'task-not-exact-trace-stop')
        require(_virtual(PROC / str(tid) / 'cgroup', budget, {0, uid}, 4096) == ('0::' + group + '\n').encode(), 'task-cgroup-differs')
        observations.append(dict(task, tracer_pid=os.getpid(), state='tracing-stop'))
    require(worker['pid'] in expected and expected[worker['pid']]['tgid'] == worker['pid']
            and expected[worker['pid']]['start_ticks'] == int(worker['start_ticks']), 'worker-not-in-stopped-inventory')
    return observations

def lock_mount_identity(raw, mountinfo, expected_inode):
    """Bind one granted FD lock to that FD's mount, not getattr's st_dev.

    Linux locks.c and proc_namespace.c expose superblock s_dev. Btrfs getattr
    exposes its subvolume anon_dev instead. The canonical path/held-FD stat
    equality is proved separately and is never replaced by this mount lookup.
    """
    def one_decimal(label):
        fields = [line.partition(':')[2].strip() for line in raw.splitlines() if line.startswith(label + ':')]
        require(len(fields) == 1 and re.fullmatch(r'[1-9][0-9]{0,19}', fields[0]), 'fdinfo-' + label + '-unavailable')
        return int(fields[0])
    mount_id, fd_inode = one_decimal('mnt_id'), one_decimal('ino')
    require(fd_inode == expected_inode, 'fdinfo-inode-differs')
    records = [line.split() for line in raw.splitlines() if line.startswith('lock:')]
    require(len(records) == 1, 'kernel-lock-count')
    row = records[0]
    require(len(row) == 9 and re.fullmatch(r'[1-9][0-9]*:', row[1])
            and row[2:5] == ['FLOCK', 'ADVISORY', 'WRITE']
            and re.fullmatch(r'-?[0-9]{1,20}', row[5]) and row[7:] == ['0', 'EOF'], 'kernel-lock-not-granted-exclusive')
    address = re.fullmatch(r'([0-9a-fA-F]{1,8}):([0-9a-fA-F]{1,8}):([1-9][0-9]{0,19})', row[6])
    require(address is not None, 'kernel-lock-address-malformed')
    require(int(address[3]) == fd_inode, 'kernel-lock-inode-differs')
    lines = mountinfo.splitlines()
    require(len(lines) <= 8192, 'mountinfo-record-bound')
    selected = [line for line in lines if line.split(' ', 1)[0] == str(mount_id)]
    require(len(selected) == 1, 'fd-mount-unavailable')
    before, sep, after = selected[0].partition(' - ')
    fields, tail = before.split(), after.split()
    require(sep and len(fields) >= 6 and len(tail) == 3 and re.fullmatch(r'[0-9]+', fields[1]), 'fd-mount-malformed')
    device = re.fullmatch(r'([0-9]{1,10}):([0-9]{1,10})', fields[2])
    require(device is not None and re.fullmatch(r'[a-zA-Z0-9_.-]{1,64}', tail[0]), 'fd-mount-device-unavailable')
    major, minor = int(device[1]), int(device[2])
    require(major < 2**32 and minor < 2**32
            and (int(address[1], 16), int(address[2], 16)) == (major, minor), 'kernel-lock-superblock-differs')
    return {'mount_id': mount_id, 'inode': fd_inode, 'filesystem': tail[0],
            'superblock_device': {'major': major, 'minor': minor},
            'selected_mount_record_sha256': sha(selected[0].encode('ascii'))}

def mount_namespace(root):
    # Kernel namespace link is read for identity only, never followed as a path.
    value = os.readlink(root / 'ns' / 'mnt')
    require(re.fullmatch(r'mnt:\[[1-9][0-9]{0,19}\]', value), 'mount-namespace-unavailable')
    return value

def exclusive_lock(worker, tasks, budget, uid):
    lock = _read(TRANSACTIONS / 'transaction.lock', budget, {0}, 1, private=True)
    require(lock['identity']['size'] == 0, 'transaction-lock-not-empty')
    inode, dev = lock['identity']['ino'], lock['identity']['dev']
    matches = []
    for pid in sorted({t['tgid'] for t in tasks}):
        root = PROC / str(pid)
        for name in _names(root / 'fdinfo', {0, uid}, 512):
            require(name.isdigit(), 'fdinfo-name-invalid')
            try:
                fd_path = root / 'fd' / name
                # The sole deliberate link traversal is a kernel-owned process
                # FD, followed only after matching its fixed canonical target.
                if os.readlink(fd_path) != str(TRANSACTIONS / 'transaction.lock'):
                    continue
                held = fd_path.stat()
                if (held.st_dev, held.st_ino) != (dev, inode):
                    continue
                namespace = mount_namespace(root)
                raw = _virtual(root / 'fdinfo' / name, budget, {0, uid}).decode('ascii')
            except FileNotFoundError:
                continue
            records = [line.split() for line in raw.splitlines() if line.startswith('lock:')]
            if len(records) != 1:
                continue
            row = records[0]
            if len(row) != 9 or row[2:5] != ['FLOCK', 'ADVISORY', 'WRITE'] or row[7:] != ['0', 'EOF']:
                continue
            mountinfo = _virtual(root / 'mountinfo', budget, {0, uid}, 1024 ** 2).decode('ascii')
            mount = lock_mount_identity(raw, mountinfo, inode)
            require(_virtual(root / 'fdinfo' / name, budget, {0, uid}).decode('ascii') == raw
                    and lock_mount_identity(raw, _virtual(root / 'mountinfo', budget, {0, uid}, 1024 ** 2).decode('ascii'), inode) == mount
                    and mount_namespace(root) == namespace
                    and os.readlink(fd_path) == str(TRANSACTIONS / 'transaction.lock')
                    and identity(fd_path.stat()) == identity(held) == lock['identity'], 'kernel-lock-observation-changed')
            matches.append({'pid': pid, 'fd': int(name), 'kernel_lock_type': 'FLOCK-ADVISORY-WRITE',
                            'lock_record_sha256': sha(raw.encode()), 'mount': mount,
                            'mount_namespace': namespace, 'file': lock})
    require(matches, 'updater-exclusive-lock-unproved')
    require(_read(TRANSACTIONS / 'transaction.lock', budget, {0}, 1, private=True) == lock, 'transaction-lock-changed')
    return matches

def executable(pid, budget, owners, expected_sha):
    fd = os.open(PROC / str(pid) / 'exe', os.O_RDONLY | os.O_NONBLOCK | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_size <= 128 * 1024 ** 2, 'executable-bound')
        h, total = hashlib.sha256(), 0
        while chunk := os.read(fd, 1048576):
            total += len(chunk); budget(len(chunk))
            require(total <= 128 * 1024 ** 2, 'executable-bound')
            h.update(chunk)
        require(identity(before) == identity(os.fstat(fd)) and h.hexdigest() == expected_sha, 'executable-changed-or-differs')
        return {'identity': identity(before), 'sha256': h.hexdigest()}
    finally:
        os.close(fd)

def writer_observation(worker, trace, work, budget, uid, gid, panel_sha):
    writer, write = trace['writer'], trace['write']
    require(type(writer) is dict and set(writer) == {'pid', 'tid'}
            and all(type(n) is int and n > 1 for n in writer.values()), 'writer-shape')
    require(type(write) is dict and set(write) == {'number', 'fd', 'offset', 'requested_bytes', 'returned_bytes', 'is_error', 'arch', 'entry_exit_matched'}
            and write['number'] == 18 and write['arch'] == 0xc000003e
            and write['is_error'] is False and write['entry_exit_matched'] is True
            and all(type(write[k]) is int for k in ('number', 'arch', 'fd', 'offset', 'requested_bytes', 'returned_bytes'))
            and 0 <= write['fd'] < 1048576 and write['offset'] >= 0
            and 0 < write['returned_bytes'] <= write['requested_bytes'] <= wal.MAX_BYTES, 'write-exit-not-proved')
    pid, tid = writer['pid'], writer['tid']
    require(any(t['tid'] == tid and t['tgid'] == pid for t in trace['tasks']), 'writer-not-admitted-task')
    status = proc_status(tid, budget, {0, uid})
    root = PROC / str(pid)
    wal_path = work / 'celikpanel.db-wal'
    fd_path = PROC / str(tid) / 'fd' / str(write['fd'])
    require(os.readlink(fd_path) == str(wal_path), 'writer-wal-target-differs')
    held = fd_path.stat()
    observed, raw = _read(wal_path, budget, {uid}, wal.MAX_BYTES, raw=True)
    require(identity(held) == observed['identity'], 'writer-wal-fd-entry-differs')
    exe = executable(pid, budget, {uid}, panel_sha)
    installed = _read(INSTALLED / 'panel', budget, {0}, 128 * 1024 ** 2)
    require(exe == installed, 'writer-not-installed-candidate-inode')
    value = {'pid': pid, 'tid': tid, 'start_ticks': proc_start(pid, budget, {0, uid}),
             'thread_start_ticks': proc_start(tid, budget, {0, uid}),
             'cmdline': _virtual(root / 'cmdline', budget, {0, uid}),
             'environ': _virtual(root / 'environ', budget, {0, uid}),
             'cgroup_raw': _virtual(root / 'cgroup', budget, {0, uid}, 4096),
             'uids': tuple(int(x) for x in status['Uid'].split()),
             'gids': tuple(int(x) for x in status['Gid'].split()),
             'executable_sha256': exe['sha256'], 'work': _directory(work, {uid}, uid, gid, 0o700),
             'wal_fd': write['fd'], 'wal_path': str(wal_path), 'wal_descriptor': identity(held),
             'wal_entry': observed['identity']}
    return value, raw, observed

def tree(root, budget, owners, maximum=20000):
    result, dirs = {}, {}
    def visit(path):
        budget()
        dirs[str(path)] = _directory(path, owners)
        for name in _names(path, owners, maximum):
            child = path / name
            require(len(result) + len(dirs) < maximum, 'tree-entry-bound')
            fd, _ = _open_dir(path, owners)
            try:
                info = os.stat(name, dir_fd=fd, follow_symlinks=False)
            finally:
                os.close(fd)
            if stat.S_ISDIR(info.st_mode):
                visit(child)
            else:
                result[child.relative_to(root).as_posix()] = _read(child, budget, owners)
    visit(root)
    return result, dirs

def snapshot_proof(tx, candidate, budget):
    require('-to-' + candidate['commit'] + '-' in tx['snapshot'], 'snapshot-candidate-differs')
    root = SNAPSHOTS / tx['snapshot']
    files, dirs = tree(root, budget, {0})
    require('SHA256SUMS' in files and 'snapshot.version' in files and 'celikpanel.db' in files, 'snapshot-incomplete')
    manifest, raw = _read(root / 'SHA256SUMS', budget, {0}, 8 * 1024 ** 2, raw=True)
    rows = {}
    for line in raw.decode('ascii').splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  \./([^\r\n\x00]+)', line)
        require(match is not None, 'snapshot-manifest-malformed')
        name = match[2]
        require(str(PurePosixPath(name)) == name and not name.startswith('/') and '..' not in PurePosixPath(name).parts
                and name not in rows and name != 'SHA256SUMS', 'snapshot-manifest-path')
        rows[name] = match[1]
    require(rows == {n: p['sha256'] for n, p in files.items() if n != 'SHA256SUMS'}, 'snapshot-inventory-differs')
    require(_read(root / 'snapshot.version', budget, {0}, 32, raw=True)[1] == b'6\n', 'snapshot-version-differs')
    return {'snapshot': tx['snapshot'], 'manifest_sha256': manifest['sha256'], 'verified_files': len(rows),
            'database': files['celikpanel.db'], 'files': files, 'directories': dirs}

def database_state(plan, tx, budget, uid, gid):
    value, proof, work = admission(plan, tx, budget, uid, gid)
    snap = snapshot_proof(tx, plan['candidate'], budget)
    material = checkpoint.forward.material_proof(tx['snapshot'], tx['transaction_token_sha256'], plan['candidate']['manifest_sha256'], budget)
    require(material['schema'] == 'celikpanel/recovery-material/v3'
            and material['snapshot_manifest_sha256'] == snap['manifest_sha256'] == value['snapshot_manifest_sha256']
            and material['record_sha256'] == value['material_sha256'], 'material-admission-snapshot-differs')
    before = _read(PARENT / 'celikpanel.db', budget, {uid})
    require(product_file(before) == value['before'], 'canonical-before-changed')
    before_flat = {k: before['identity'][k] for k in ('dev', 'ino', 'mode', 'uid', 'gid', 'links', 'size')}
    for name in ('mtime', 'ctime'):
        before_flat[name + '_sec'] = before['identity'][name + '_ns'] // 1000000000
        before_flat[name + '_nsec'] = before['identity'][name + '_ns'] % 1000000000
    expected_parent = {**value['parent'], 'links': 0, 'size': 0, 'mtime_sec': 0, 'mtime_nsec': 0, 'ctime_sec': 0, 'ctime_nsec': 0}
    require(material['database_before']['file'] == before_flat
            and material['database_before']['parent'] == expected_parent
            and material['database_before']['sha256'] == before['sha256'], 'material-before-differs')
    require(value['initial'].get('sha256') == snap['database']['sha256'], 'initial-copy-not-snapshot')
    initial = value['initial']['identity']
    current = _read(work / 'celikpanel.db', budget, {uid})
    require(all(initial.get(k) == current['identity'][k] for k in ('dev', 'ino', 'mode', 'uid', 'gid', 'links')),
            'work-database-replaced')
    require(stat.S_IMODE(current['identity']['mode']) == 0o600 and current['identity']['gid'] == gid, 'work-database-metadata')
    for suffix in ('-wal', '-shm', '-journal'):
        try:
            _read(PARENT / ('celikpanel.db' + suffix), budget, {uid})
        except FileNotFoundError:
            continue
        raise Inconclusive('canonical-sidecar-present')
    names = _names(work, {uid}, 4)
    require('celikpanel.db-wal' in names and set(names) <= {'celikpanel.db', 'celikpanel.db-wal', 'celikpanel.db-shm'}, 'work-sidecar-layout')
    work_files = {n: _read(work / n, budget, {uid}) for n in names}
    return {'admission': value, 'admission_record': proof, 'snapshot': snap, 'material': material,
            'canonical_before': before, 'work_directory': value['work'], 'work_files': work_files}, work

def worker_proof(args, plan, worker, budget, uid):
    unit = 'celikpanel-self-update-' + args.operation_id + '.service'
    require(worker['unit'] == unit and worker['cgroup'] == '/system.slice/' + unit
            and type(worker['pid']) is int and worker['pid'] > 1
            and HEX32.fullmatch(worker['invocation_id']), 'worker-tuple-differs')
    root = PROC / str(worker['pid'])
    require(proc_start(worker['pid'], budget, {0, uid}) == int(worker['start_ticks']), 'worker-start-changed')
    command = b'/bin/bash\0' + (plan['source_root'] + '/bootstrap-prebuilt-update.sh').encode('ascii') + b'\0--normal\0'
    require(_virtual(root / 'cmdline', budget, {0}) == command, 'worker-command-differs')
    env = _virtual(root / 'environ', budget, {0})
    matching = [v.partition(b'=')[2] for v in env.rstrip(b'\0').split(b'\0') if v.startswith(b'INVOCATION_ID=')]
    require(matching == [worker['invocation_id'].encode()], 'worker-invocation-differs')
    boot = _virtual(PROC / 'sys/kernel/random/boot_id', budget, {0}, 128).decode('ascii').strip()
    require(boot == worker['boot_id'], 'worker-boot-differs')
    exe = executable(worker['pid'], budget, {0}, worker['running_executable_sha256'])
    return {'unit': unit, 'pid': worker['pid'], 'start_ticks': int(worker['start_ticks']),
            'invocation_id': worker['invocation_id'], 'boot_id': boot, 'cgroup': worker['cgroup'], 'executable': exe}

def inspect(args, plan, worker_identity, trace_snapshot, *, prior_wal, tick=lambda: None):
    """Return verified physical WAL-at-stop evidence, or a closed inconclusive code."""
    try:
        guest = validate_plan(args, plan)
        require(type(prior_wal) is bytes, 'prior-wal-not-captured-bytes')
        budget = Budget(tick)
        uid, gid = account()
        worker = worker_proof(args, plan, worker_identity, budget, uid)
        tasks = stopped_inventory(worker, trace_snapshot, budget, uid)
        locks = exclusive_lock(worker, tasks, budget, uid)
        tx = transaction(budget)
        state, work = database_state(plan, tx, budget, uid, gid)
        first, raw, wal_file = writer_observation(worker, trace_snapshot, work, budget, uid, gid, plan['candidate']['files']['bin/panel'])
        parsed = wal.inspect_wal(raw, prior_prefix=prior_wal)
        write = trace_snapshot['write']
        require(parsed['status'] == 'verified' and parsed['classification'] == 'valid-noncommit-suffix'
                and parsed['growth_after_prior_commit'] is True
                and write['offset'] >= len(prior_wal)
                and write['offset'] + write['returned_bytes'] == len(raw), 'noncommit-write-prefix-not-proved')
        expected = {'operation_id': args.operation_id, 'snapshot': tx['snapshot'], 'token_sha256': tx['transaction_token_sha256'],
                    'admission_sha256': state['admission_record']['sha256'], 'candidate_panel_sha256': plan['candidate']['files']['bin/panel'],
                    'uid': uid, 'gid': gid, 'worker_start_ticks': worker['start_ticks'], 'work': state['work_directory']}
        second, again, again_file = writer_observation(worker, trace_snapshot, work, budget, uid, gid, expected['candidate_panel_sha256'])
        matched = matcher.match_writer(expected, first, second)
        require(raw == again and wal_file == again_file, 'wal-changed')
        require(database_state(plan, tx, budget, uid, gid) == (state, work), 'database-evidence-changed')
        require(transaction(budget) == tx and exclusive_lock(worker, tasks, budget, uid) == locks
                and stopped_inventory(worker, trace_snapshot, budget, uid) == tasks
                and worker_proof(args, plan, worker_identity, budget, uid) == worker, 'final-native-identity-changed')
        return {'schema': SCHEMA, 'status': 'verified', 'classification': 'physical-noncommit-WAL-write-held',
                'identity': guest, 'operation_id': args.operation_id, 'worker': worker, 'transaction': tx,
                'tasks': tasks, 'exclusive_locks': locks, 'database': state, 'writer': matched,
                'successful_write_exit': dict(write), 'wal': parsed, 'wal_file': wal_file, 'limits': list(LIMITS)}
    except (OSError, ValueError, KeyError, TypeError, IndexError, AttributeError, probe.ProbeError,
            checkpoint.forward.kill.MissedCheckpoint) as error:
        reason = str(error) if isinstance(error, Inconclusive) and re.fullmatch(r'[a-z0-9-]{1,96}', str(error)) else type(error).__name__
        return {'schema': SCHEMA, 'status': 'inconclusive', 'reason': reason, 'limits': list(LIMITS)}
