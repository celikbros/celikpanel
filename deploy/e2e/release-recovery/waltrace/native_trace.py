"""Fixture-only native WAL tracer, imported by the registered lab controller.

There is deliberately no PID/command-line entry point. The controller validates
VM registration, immutable source, updater admission and signal authority. This
module derives a single fixed self-update unit, follows its admitted kernel
process family and observes actual syscall exits. It never edits code, SQL,
process memory, registers, syscall arguments or return values.

EXITKILL is intentionally absent: attaching a tracer must not grant tracer death
permission to kill an external updater. Kernel detach/resume on tracer death is
an inconclusive observation; the controller owns the startup-gate watchdog.
Normal failure/timeout detaches only identities still traced by this process.
The final cut is an independently authorized controller callback, not os.kill.
"""
from __future__ import annotations

import ctypes
import errno
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import signal
import stat
import sys
import time

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE if (HERE / 'wal_frames.py').exists() else HERE.parent))
from wal_frames import inspect_wal
from wal_migration_identity import COMMAND, match_writer

SEIZE, INTERRUPT, EVENT_MESSAGE, SYSCALL_INFO = 0x4206, 0x4207, 0x4201, 0x420e
CONT, SYSCALL, DETACH = 7, 24, 17
OPTIONS = 1 | 2 | 4 | 8 | 16 | 64  # TRACESYSGOOD + FORK/VFORK/CLONE/EXEC/EXIT
EVENT_FORK, EVENT_VFORK, EVENT_CLONE, EVENT_EXEC, EVENT_EXIT, EVENT_STOP = 1, 2, 3, 4, 6, 128
SYSCALL_STOP, WAIT_ALL, AMD64 = signal.SIGTRAP | 0x80, 0x40000000, 0xc000003e
MAX_TASKS, MAX_EVENTS, MAX_WAL = 512, 50000, 64 * 1024 * 1024
ALLOWED = frozenset((SEIZE, INTERRUPT, EVENT_MESSAGE, SYSCALL_INFO, CONT, SYSCALL, DETACH))
REGISTRATION = frozenset(('operation_id', 'unit', 'worker_pid', 'worker_start_ticks', 'boot_id',
                          'gate_executable_sha256', 'gate_executable_device', 'gate_executable_inode'))
HEX32, HEX64 = re.compile(r'[0-9a-f]{32}\Z'), re.compile(r'[0-9a-f]{64}\Z')
BOOT = re.compile(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}\Z')


class Inconclusive(RuntimeError):
    """A fixed diagnostic code, never raw argv, environment or SQL."""


def require(condition, code):
    if not condition:
        raise Inconclusive(code)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def proof_digest(value):
    return digest(json.dumps(value, sort_keys=True, separators=(',', ':')).encode('ascii'))


def integer(value, low=0, high=(1 << 63) - 1):
    return type(value) is int and low <= value <= high


def validate_registration(value):
    require(type(value) is dict and value.keys() == REGISTRATION, 'registration-shape')
    require(type(value['operation_id']) is str and HEX32.fullmatch(value['operation_id']), 'operation-format')
    require(value['unit'] == 'celikpanel-self-update-' + value['operation_id'] + '.service', 'unit-outside-operation')
    require(type(value['boot_id']) is str and BOOT.fullmatch(value['boot_id']), 'boot-format')
    require(type(value['gate_executable_sha256']) is str and HEX64.fullmatch(value['gate_executable_sha256']), 'gate-digest')
    for key in ('worker_pid', 'worker_start_ticks', 'gate_executable_device', 'gate_executable_inode'):
        require(integer(value[key], 2 if key == 'worker_pid' else 1), 'registration-integer')
    return dict(value)


class Entry(ctypes.Structure):
    _fields_ = [('number', ctypes.c_uint64), ('args', ctypes.c_uint64 * 6)]


class Exit(ctypes.Structure):
    _fields_ = [('result', ctypes.c_int64), ('is_error', ctypes.c_uint8)]


class Payload(ctypes.Union):
    _fields_ = [('entry', Entry), ('exit', Exit)]


class Info(ctypes.Structure):
    _fields_ = [('op', ctypes.c_uint8), ('pad', ctypes.c_uint8 * 3), ('arch', ctypes.c_uint32),
                ('ip', ctypes.c_uint64), ('sp', ctypes.c_uint64), ('payload', Payload)]


_libc = ctypes.CDLL(None, use_errno=True)
_libc.ptrace.argtypes = [ctypes.c_uint, ctypes.c_uint, ctypes.c_void_p, ctypes.c_void_p]
_libc.ptrace.restype = ctypes.c_long


def ptrace(request, tid, address=0, data=0):
    require(request in ALLOWED, 'ptrace-operation-not-allowed')
    ctypes.set_errno(0)
    result = _libc.ptrace(request, tid, ctypes.c_void_p(address), data)
    if result == -1:
        number = ctypes.get_errno()
        raise OSError(number, os.strerror(number))
    return result


def metadata(st):
    return {'dev': st.st_dev, 'ino': st.st_ino, 'mode': st.st_mode, 'uid': st.st_uid, 'gid': st.st_gid,
            'links': st.st_nlink, 'size': st.st_size, 'mtime_ns': st.st_mtime_ns, 'ctime_ns': st.st_ctime_ns}


def directory_metadata(st):
    return {key: value for key, value in metadata(st).items() if key in ('dev', 'ino', 'mode', 'uid', 'gid')}


def read_bounded(path, limit=16384):
    fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NONBLOCK)
    try:
        raw = os.read(fd, limit + 1)
        require(len(raw) <= limit, 'proc-read-bound')
        return raw
    finally:
        os.close(fd)


def directory_fd(path):
    """Open each fixed absolute path component without following symlinks."""
    require(path.is_absolute(), 'directory-not-absolute')
    fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    try:
        for part in path.parts[1:]:
            require(part not in ('.', '..'), 'directory-component')
            nextfd = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                             dir_fd=fd)
            os.close(fd)
            fd = nextfd
        return fd
    except BaseException:
        os.close(fd)
        raise


class LinuxKernel:
    """All OS effects are here; unit tests replace this boundary, not validators."""
    def __init__(self, registration):
        require(platform.system() == 'Linux' and platform.machine() == 'x86_64', 'linux-amd64-required')
        self.registration = registration
        self._wait_after = 0
        self.cgroup = Path('/sys/fs/cgroup/system.slice') / registration['unit']
        self.cgroup_fd = directory_fd(self.cgroup)
        self.cgroup_identity = directory_metadata(os.fstat(self.cgroup_fd))

    def close(self):
        os.close(self.cgroup_fd)

    def now(self):
        return time.monotonic()

    def boot(self):
        return read_bounded('/proc/sys/kernel/random/boot_id').decode('ascii').strip()

    def task(self, tid):
        require(integer(tid, 2, (1 << 31) - 1), 'task-id')
        base = Path('/proc') / str(tid)
        fields = {}
        for line in read_bounded(base / 'status').decode('ascii').splitlines():
            key, _, value = line.partition(':')
            fields[key] = value.strip()
        raw = read_bounded(base / 'stat').decode('ascii')
        started = int(raw[raw.rfind(')') + 2:].split()[19])
        return {'tid': tid, 'tgid': int(fields['Tgid']), 'start_ticks': started,
                'tracer_pid': int(fields['TracerPid']), 'state': fields['State'].split()[0],
                'uids': tuple(map(int, fields['Uid'].split())), 'gids': tuple(map(int, fields['Gid'].split())),
                'cgroup_raw': read_bounded(base / 'cgroup')}

    def tasks(self):
        current = directory_fd(self.cgroup)
        try:
            require(directory_metadata(os.fstat(current)) == self.cgroup_identity, 'cgroup-directory-changed')
        finally:
            os.close(current)
        fd = os.open('cgroup.threads', os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW, dir_fd=self.cgroup_fd)
        try:
            raw = os.read(fd, 32769)
        finally:
            os.close(fd)
        require(len(raw) <= 32768, 'cgroup-task-bound')
        values = raw.splitlines()
        require(all(value.isdigit() for value in values), 'cgroup-task-format')
        tids = [int(value) for value in values]
        require(len(tids) == len(set(tids)) and len(tids) <= MAX_TASKS, 'cgroup-task-inventory')
        return set(tids)

    def executable(self, tid):
        fd = os.open(f'/proc/{tid}/exe', os.O_RDONLY | os.O_NONBLOCK | os.O_CLOEXEC)
        try:
            before = metadata(os.fstat(fd))
            require(stat.S_ISREG(before['mode']) and 0 < before['size'] <= 256 * 1024 * 1024, 'executable-bound')
            h = hashlib.sha256()
            offset = 0
            while offset < before['size']:
                chunk = os.pread(fd, min(1048576, before['size'] - offset), offset)
                require(bool(chunk), 'executable-truncated')
                h.update(chunk)
                offset += len(chunk)
            require(metadata(os.fstat(fd)) == before, 'executable-changed')
            return {'dev': before['dev'], 'ino': before['ino'], 'sha256': h.hexdigest()}
        finally:
            os.close(fd)

    def command(self, tid):
        return read_bounded(f'/proc/{tid}/cmdline')

    def seize(self, tid):
        ptrace(SEIZE, tid, data=OPTIONS)

    def interrupt(self, tid):
        ptrace(INTERRUPT, tid)

    def resume(self, tid, sig=0, *, syscalls=False):
        ptrace(SYSCALL if syscalls else CONT, tid, data=sig)

    def detach(self, tid, sig=0):
        ptrace(DETACH, tid, data=sig)

    def event_message(self, tid):
        value = ctypes.c_ulong()
        ptrace(EVENT_MESSAGE, tid, data=ctypes.byref(value))
        return int(value.value)

    def syscall(self, tid):
        value = Info()
        size = ptrace(SYSCALL_INFO, tid, ctypes.sizeof(value), ctypes.byref(value))
        require(value.arch == AMD64, 'syscall-abi')
        if value.op == 1:
            require(size >= 80, 'syscall-entry-truncated')
            return {'op': 'entry', 'number': int(value.payload.entry.number), 'args': tuple(value.payload.entry.args)}
        require(value.op == 2 and size >= 33, 'syscall-exit-truncated')
        return {'op': 'exit', 'result': int(value.payload.exit.result), 'is_error': bool(value.payload.exit.is_error)}

    def wait(self, tids, deadline):
        # Poll only admitted tasks, rotating after each consumed event. A Go
        # thread with another immediately ready syscall must not starve its
        # siblings while the real publisher's unchanged timeout is running.
        ordered = sorted(tids)
        ordered = [tid for tid in ordered if tid > self._wait_after] + [
            tid for tid in ordered if tid <= self._wait_after]
        while self.now() < deadline:
            for tid in ordered:
                try:
                    got, status = os.waitpid(tid, WAIT_ALL | os.WNOHANG)
                except ChildProcessError:
                    continue
                if got:
                    self._wait_after = got
                    return got, status
            time.sleep(0.0002)
        raise Inconclusive('trace-deadline')

    def is_wal_descriptor(self, expected, tid, fd):
        path = '/var/lib/celikpanel/.release-db-migrations/' + expected['token_sha256'] + '/work/celikpanel.db-wal'
        try:
            return os.readlink(f'/proc/{tid}/fd/{fd}') == path
        except FileNotFoundError:
            return False

    def writer_observation(self, expected, tid, fd):
        status = self.task(tid)
        leader = self.task(status['tgid'])
        work = Path('/var/lib/celikpanel/.release-db-migrations') / expected['token_sha256'] / 'work'
        workfd = directory_fd(work)
        wal = None
        try:
            work_stat = directory_metadata(os.fstat(workfd))
            wal = os.open('celikpanel.db-wal', os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK,
                          dir_fd=workfd)
            before = metadata(os.fstat(wal))
            require(stat.S_ISREG(before['mode']) and before['links'] == 1 and before['size'] <= MAX_WAL,
                    'wal-file-bound')
            entry = metadata(os.stat('celikpanel.db-wal', dir_fd=workfd, follow_symlinks=False))
            procfd = Path(f'/proc/{tid}/fd/{fd}')
            require(os.readlink(procfd) == str(work / 'celikpanel.db-wal'), 'wal-fd-path')
            descriptor = metadata(os.stat(procfd))
            raw = os.pread(wal, before['size'] + 1, 0)
            require(len(raw) == before['size'] and metadata(os.fstat(wal)) == before
                    and before == entry == descriptor, 'wal-image-changed')
            value = {'pid': status['tgid'], 'tid': tid, 'start_ticks': leader['start_ticks'],
                     'thread_start_ticks': status['start_ticks'], 'cmdline': self.command(tid),
                     'environ': read_bounded(f'/proc/{tid}/environ'), 'cgroup_raw': status['cgroup_raw'],
                     'uids': status['uids'], 'gids': status['gids'],
                     'executable_sha256': self.executable(tid)['sha256'], 'work': work_stat,
                     'wal_fd': fd, 'wal_path': str(work / 'celikpanel.db-wal'),
                     'wal_descriptor': descriptor, 'wal_entry': entry}
            stable = lambda value: {key: item for key, item in value.items() if key != 'state'}
            require(self.task(tid) == status and stable(self.task(status['tgid'])) == stable(leader),
                    'writer-task-changed')
            return value, raw
        finally:
            if wal is not None:
                os.close(wal)
            os.close(workfd)


def successful_pwrite(entry, syscall):
    if syscall['op'] != 'exit' or entry is None or syscall['is_error'] or syscall['result'] <= 0:
        return None
    if entry['number'] != 18:
        return None
    fd, _, count, offset, _, _ = entry['args']
    require(integer(fd, 0, (1 << 20) - 1) and integer(count, 1) and integer(offset)
            and syscall['result'] <= count, 'pwrite-result-invalid')
    return {'number': 18, 'fd': fd, 'offset': offset, 'requested_bytes': count,
            'returned_bytes': syscall['result'], 'is_error': False, 'arch': AMD64,
            'entry_exit_matched': True}


class NativeTrace:
    def __init__(self, registration, callbacks, *, timeout=900, kernel=None):
        self.registration = validate_registration(registration)
        require(type(timeout) in (int, float) and 0.1 <= timeout <= 1800, 'trace-timeout-bound')
        self.callbacks = callbacks
        self.kernel = kernel if kernel is not None else LinuxKernel(self.registration)
        self.deadline = self.kernel.now() + timeout
        self.known, self.stopped, self.entries, self.signals = {}, set(), {}, {}
        self.exiting, self.exit_threads = set(), set()
        self.syscall_tgids = set()
        self.events = []
        self.baseline = None
        self.expected = None
        self.expected_checked = 0.0
        self.gate_released = False
        self.cut_called = False
        self.initialized = False

    def event(self, kind, **fields):
        require(len(self.events) < MAX_EVENTS, 'trace-event-bound')
        value = {'event': kind, 'monotonic_ns': int(self.kernel.now() * 1000000000), **fields}
        self.events.append(value)
        reporter = getattr(self.callbacks, 'record', None)
        if reporter is not None:
            reporter(value)

    def same_scope(self, task):
        require(task['cgroup_raw'] == ('0::/system.slice/' + self.registration['unit'] + '\n').encode('ascii'),
                'task-left-registered-cgroup')
        require(task['start_ticks'] >= self.registration['worker_start_ticks'], 'task-predates-worker')

    def remember(self, tid, *, parent=None, seized=False):
        require(tid not in self.known and len(self.known) < MAX_TASKS, 'task-admission-bound')
        value = self.kernel.task(tid)
        if seized:
            require(value['tracer_pid'] in (0, os.getpid()), 'task-tracer-differs')
        else:
            require(value['tracer_pid'] == os.getpid(), 'inherited-tracer-differs')
        self.known[tid] = {'tid': tid, 'tgid': value['tgid'], 'start_ticks': value['start_ticks']}
        self.same_scope(value)
        self.event('task-admitted', **self.known[tid], parent=parent)

    def validate_known(self, tid, *, scope=True):
        require(tid in self.known, 'unadmitted-task')
        now = self.kernel.task(tid)
        require(now['start_ticks'] == self.known[tid]['start_ticks'] and now['tgid'] == self.known[tid]['tgid'],
                'task-identity-changed')
        require(now['tracer_pid'] == os.getpid(), 'tracer-ownership-lost')
        if scope:
            self.same_scope(now)
        return now

    def reconcile_exiting_threads(self):
        # exit_group can retire a cloning parent before its CLONE event is
        # consumed, leaving an automatically traced sibling stopped. Admit no
        # new process: kernel TGID must name an already admitted zombie leader
        # whose EXIT event we consumed. These threads may only drain to exit;
        # they cannot supply a WAL/exchange observation or a cut proof.
        leaders = {}
        for tid in self.exiting & self.known.keys():
            if self.known[tid]['tgid'] != tid:
                continue
            try:
                value = self.validate_known(tid)
            except FileNotFoundError:
                continue
            if value['state'] == 'Z':
                leaders[tid] = value
        if not leaders:
            return
        for tid in sorted(self.kernel.tasks() - self.known.keys()):
            try:
                value = self.kernel.task(tid)
                leader = leaders.get(value['tgid'])
                if leader is None:
                    continue
                require(value['tracer_pid'] == os.getpid() and value['state'] == 't',
                        'exit-thread-trace-unproved')
                require(value['start_ticks'] >= leader['start_ticks'], 'exit-thread-predates-leader')
                self.same_scope(value)
                require(self.validate_known(leader['tid']) == leader
                        and self.kernel.task(tid) == value, 'exit-thread-identity-changed')
            except FileNotFoundError:
                continue
            require(len(self.known) < MAX_TASKS, 'task-admission-bound')
            self.known[tid] = {key: value[key] for key in ('tid', 'tgid', 'start_ticks')}
            self.exit_threads.add(tid)
            self.event('exit-thread-admitted', **self.known[tid],
                       leader_start_ticks=leader['start_ticks'], authority='kernel-zombie-thread-group')

    def wait_known(self, deadline):
        # Bounded slices discover siblings even if no admitted TID has a ready
        # wait event. Total observation/cleanup deadlines remain unchanged.
        # Never waitpid(-1) or reap an unrelated process.
        while self.kernel.now() < deadline:
            try:
                return self.kernel.wait(set(self.known), min(deadline, self.kernel.now() + 0.1))
            except Inconclusive as error:
                if str(error) != 'trace-deadline':
                    raise
                self.reconcile_exiting_threads()
        raise Inconclusive('trace-deadline')

    def initial_attach(self):
        r = self.registration
        require(self.kernel.boot() == r['boot_id'], 'boot-changed')
        self.callbacks.revalidate('before-seize', {'registration': dict(r)})
        task = self.kernel.task(r['worker_pid'])
        require(task['tgid'] == r['worker_pid'] and task['start_ticks'] == r['worker_start_ticks']
                and task['tracer_pid'] == 0 and task['state'] in ('R', 'S', 'T'), 'worker-gate-state')
        self.same_scope(task)
        executable = self.kernel.executable(r['worker_pid'])
        require(executable == {'dev': r['gate_executable_device'], 'ino': r['gate_executable_inode'],
                               'sha256': r['gate_executable_sha256']}, 'gate-executable-differs')
        # The controller's pre-exec release-file gate is a single-threaded launcher.
        # Refuse a busy unit instead of trying to race an arbitrary live family.
        require(self.kernel.tasks() == {r['worker_pid']}, 'initial-gate-not-single-task')
        self.kernel.seize(r['worker_pid'])
        # Record the pinned pre-seize identity before any fallible validation;
        # a failed recheck must still release our newly attached relationship.
        self.known[r['worker_pid']] = {key: task[key] for key in ('tid', 'tgid', 'start_ticks')}
        self.initialized = True
        self.validate_known(r['worker_pid'])
        self.event('task-admitted', **self.known[r['worker_pid']], parent=None)
        self.freeze(initial=True)
        proof = self.snapshot()
        self.callbacks.revalidate('gate-seized', proof)
        self.callbacks.release_start_gate(proof)
        self.gate_released = True
        self.event('startup-gate-released', worker_pid=r['worker_pid'])
        self.resume_all()

    def refresh_expected(self, force=False):
        if force or self.kernel.now() - self.expected_checked > 0.2:
            value = self.callbacks.writer_expected()
            if value is not None:
                require(type(value) is dict and value.get('operation_id') == self.registration['operation_id']
                        and value.get('worker_start_ticks') == self.registration['worker_start_ticks'],
                        'writer-expectation-outside-operation')
                require(type(value.get('token_sha256')) is str and HEX64.fullmatch(value['token_sha256']),
                        'writer-token-format')
                if self.expected is not None:
                    require(value == self.expected, 'writer-admission-changed')
                self.expected = dict(value)
            elif self.expected is not None:
                raise Inconclusive('writer-admission-disappeared')
            self.expected_checked = self.kernel.now()
        return self.expected

    def await_exit(self, tid):
        # ESRCH is not exit proof, even while exit_group has started. Keep the
        # kernel admission until wait reports exit, or the normal deadline wins.
        try:
            value = self.kernel.task(tid)
            require(value['start_ticks'] == self.known[tid]['start_ticks'], 'pending-exit-identity-changed')
        except FileNotFoundError:
            pass
        self.stopped.discard(tid)
        self.entries.pop(tid, None)
        self.event('awaiting-real-wait-exit', tid=tid)

    def resume(self, tid):
        require(tid in self.stopped, 'resume-without-stop')
        self.validate_known(tid)
        try:
            self.kernel.resume(tid, self.signals.pop(tid, 0), syscalls=tid not in self.exit_threads and self.known[tid]['tgid'] in self.syscall_tgids)
        except ProcessLookupError:
            self.await_exit(tid)
        self.stopped.discard(tid)

    def resume_all(self):
        for tid in sorted(self.stopped):
            self.resume(tid)

    def exec_transition(self, tid, *, observing=True):
        old = self.kernel.event_message(tid)
        require(old in self.known and tid in self.known and self.known[old]['tgid'] == tid,
                'exec-transition-unadmitted')
        group = [value for value in self.known.values() if value['tgid'] == tid]
        now = self.kernel.task(tid)
        require(now['tgid'] == tid and now['tracer_pid'] == os.getpid(), 'exec-leader-unproved')
        if observing:
            self.same_scope(now)
        require(now['start_ticks'] in {self.known[tid]['start_ticks'], self.known[old]['start_ticks']},
                'exec-start-transition-unproved')
        if old != tid:
            # Kernel EXEC names the former execing thread and retires its group.
            # It is not a guessed PID replacement or a fabricated wait exit.
            for value in group:
                if value['tid'] != tid:
                    try:
                        current = self.kernel.task(value['tid'])
                        require(current['tgid'] != tid, 'exec-sibling-still-present')
                    except FileNotFoundError:
                        pass
                    self.known.pop(value['tid'])
                    self.exiting.discard(value['tid'])
                    self.exit_threads.discard(value['tid'])
                    self.stopped.discard(value['tid'])
                    self.entries.pop(value['tid'], None)
                    self.signals.pop(value['tid'], None)
            self.known[tid] = {'tid': tid, 'tgid': tid, 'start_ticks': now['start_ticks']}
        self.entries.pop(tid, None)
        self.syscall_tgids.discard(tid)
        expected = self.refresh_expected(force=True) if observing else None
        self.select_syscall_target(tid, expected)
        self.event('kernel-exec-transition', tid=tid, former_tid=old,
                   retired_tids=[v['tid'] for v in group if v['tid'] != tid] if old != tid else [])

    def select_syscall_target(self, tid, expected):
        """Closed observer hook; lifecycle and admitted exec proof stay shared."""
        if expected is not None and self.kernel.command(tid) == COMMAND:
            require(self.kernel.executable(tid)['sha256'] == expected['candidate_panel_sha256'],
                    'migrator-executable-differs')
            self.syscall_tgids.add(tid)
            self.event('admitted-migrator-syscalls-enabled', tgid=tid, candidate_panel_sha256=expected['candidate_panel_sha256'])

    def syscall_event(self, tid, syscall):
        if syscall['op'] == 'entry':
            self.entries[tid] = syscall
            return None
        return successful_pwrite(self.entries.pop(tid, None), syscall)

    def observe_boundary(self, tid, boundary):
        return self.observe_write(tid, boundary)

    def missing_boundary_reason(self):
        return 'no-exact-native-wal-boundary'

    def consume(self, tid, status, *, initial=False):
        require(tid in self.known, 'wait-outside-admitted-family')
        if os.WIFEXITED(status) or os.WIFSIGNALED(status):
            self.known.pop(tid)
            self.exiting.discard(tid)
            self.exit_threads.discard(tid)
            self.stopped.discard(tid)
            self.entries.pop(tid, None)
            self.signals.pop(tid, None)
            self.event('kernel-wait-exit', tid=tid, signal=os.WTERMSIG(status) if os.WIFSIGNALED(status) else None)
            return None
        require(os.WIFSTOPPED(status), 'unexpected-wait-state')
        event, sig = status >> 16, os.WSTOPSIG(status)
        self.stopped.add(tid)
        if tid in self.exit_threads:
            require(event == EVENT_EXIT or (event == EVENT_STOP and sig == signal.SIGTRAP),
                    'exit-thread-not-exiting')
        if event == EVENT_EXEC:
            self.exec_transition(tid)
            return None
        self.validate_known(tid)
        if event in (EVENT_FORK, EVENT_VFORK, EVENT_CLONE):
            child = self.kernel.event_message(tid)
            self.remember(child, parent=tid)
        elif event == EVENT_EXIT:
            self.exiting.add(tid)
            self.event('kernel-exit-started', tid=tid)
        elif event == EVENT_STOP:
            require(initial or sig == signal.SIGTRAP, 'external-group-stop')
        elif event:
            raise Inconclusive('unsupported-ptrace-event')
        elif sig == SYSCALL_STOP:
            syscall = self.kernel.syscall(tid)
            return self.syscall_event(tid, syscall)
        else:
            self.signals[tid] = sig
        return None

    def freeze(self, *, initial=False, deadline=None):
        bound = min(self.deadline, self.kernel.now() + 5) if deadline is None else deadline
        for tid in sorted(set(self.known) - self.stopped):
            self.validate_known(tid)
            try:
                self.kernel.interrupt(tid)
            except ProcessLookupError:
                self.await_exit(tid)
        while set(self.known) != self.stopped:
            require(bool(self.known), 'updater-exited-before-stop')
            tid, status = self.wait_known(bound)
            self.consume(tid, status, initial=initial)
        require(self.kernel.boot() == self.registration['boot_id'], 'boot-changed')
        require(self.kernel.tasks() == set(self.known), 'cgroup-has-untraced-tasks')
        require(not self.exit_threads, 'exit-thread-drain-incomplete')
        for tid in self.known:
            require(self.validate_known(tid)['state'] == 't', 'task-not-in-ptrace-stop')

    def snapshot(self, tid=None, write=None):
        value = {'tracer_pid': os.getpid(), 'tasks': [dict(self.known[k]) for k in sorted(self.known)]}
        if tid is not None:
            value.update(writer={'pid': self.known[tid]['tgid'], 'tid': tid}, write=dict(write))
        return value

    def observe_write(self, tid, write):
        expected = self.refresh_expected()
        if (self.known[tid]['tgid'] not in self.syscall_tgids or expected is None or self.kernel.command(tid) != COMMAND
                or not self.kernel.is_wal_descriptor(expected, tid, write['fd'])):
            return None
        first, raw = self.kernel.writer_observation(expected, tid, write['fd'])
        second, second_raw = self.kernel.writer_observation(expected, tid, write['fd'])
        matched = match_writer(expected, first, second)
        require(raw == second_raw, 'wal-observation-changed')
        wal_key = (matched['wal']['dev'], matched['wal']['ino'])
        parsed = inspect_wal(raw)
        if parsed['status'] != 'verified':
            return None
        if parsed['classification'] in ('header-only', 'committed-prefix-only'):
            self.baseline = {'raw': raw, 'key': wal_key, 'expected': dict(expected)}
            self.event('committed-wal-prefix-observed', sha256=digest(raw), byte_count=len(raw), wal_dev=wal_key[0], wal_ino=wal_key[1])
            return None
        if self.baseline is None:
            return None
        require(self.baseline['key'] == wal_key and self.baseline['expected'] == expected, 'wal-generation-changed')
        prior = self.baseline['raw']
        parsed = inspect_wal(raw, prior_prefix=prior)
        if not (parsed['status'] == 'verified' and parsed['classification'] == 'valid-noncommit-suffix'
                and parsed['growth_after_prior_commit'] is True):
            return None
        require(write['offset'] >= len(prior) and write['offset'] + write['returned_bytes'] == len(raw),
                'write-does-not-end-appended-wal')
        self.freeze()
        self.refresh_expected(force=True)
        again, final_raw = self.kernel.writer_observation(expected, tid, write['fd'])
        match_writer(expected, first, again)
        require(final_raw == raw, 'wal-changed-during-family-stop')
        trace_snapshot = self.snapshot(tid, write)
        self.callbacks.revalidate('before-cut-proof', trace_snapshot)
        signature = proof_digest(trace_snapshot)
        verified = self.callbacks.authorize_cut(trace_snapshot, prior)
        require(proof_digest(trace_snapshot) == signature, 'trace-proof-was-mutated')
        require(type(verified) is dict and verified.get('status') == 'verified'
                and verified.get('operation_id') == self.registration['operation_id']
                and verified.get('trace_sha256') == signature
                and verified.get('prior_wal_sha256') == digest(prior), 'cut-not-independently-authorized')
        # Authorization may take time; the writer must still be stopped at the
        # same causal exit and no task/byte/admission transition can be accepted.
        self.freeze()
        self.refresh_expected(force=True)
        last, last_raw = self.kernel.writer_observation(expected, tid, write['fd'])
        match_writer(expected, again, last)
        require(last_raw == raw and self.snapshot(tid, write) == trace_snapshot, 'cut-final-boundary-changed')
        self.callbacks.revalidate('immediately-before-cut', trace_snapshot)
        self.cut_called = True
        receipt = self.callbacks.perform_cut(verified)
        require(type(receipt) is dict and receipt.get('status') == 'cut-sent'
                and receipt.get('operation_id') == self.registration['operation_id']
                and receipt.get('trace_sha256') == signature, 'cut-receipt-unconfirmed')
        self.event('authorized-controller-cut-sent', trace_sha256=signature)
        return {'status': 'cut-sent', 'trace': trace_snapshot, 'trace_sha256': signature,
                'prior_wal_sha256': digest(prior), 'wal': parsed, 'independent_proof': verified,
                'cut_receipt': receipt, 'limits': ['physical-uncommitted-WAL-boundary',
                    'does-not-prove-SQLite-Commit-was-not-entered', 'automatic-recovery-terminal-proof-is-separate']}

    def cleanup(self):
        detached, exited, uncertain = [], [], []
        bound = self.kernel.now() + 5
        # Do not use freeze(): losing operation authority must still permit
        # releasing only our own trace relationship, without a kill or SIGCONT.
        for tid in list(self.known):
            try:
                self.validate_known(tid, scope=False)
                if tid not in self.stopped:
                    self.kernel.interrupt(tid)
            except (FileNotFoundError, ProcessLookupError):
                pass
            except Exception:
                uncertain.append(tid)
        while self.known and self.kernel.now() < bound:
            for tid in list(self.stopped):
                try:
                    self.validate_known(tid, scope=False)
                    self.kernel.detach(tid, self.signals.pop(tid, 0))
                    self.known.pop(tid)
                    self.stopped.remove(tid)
                    detached.append(tid)
                except (FileNotFoundError, ProcessLookupError):
                    self.stopped.discard(tid)
                except Exception:
                    uncertain.append(tid)
                    self.stopped.discard(tid)
            if not self.known:
                break
            try:
                tid, status = self.wait_known(bound)
                if os.WIFEXITED(status) or os.WIFSIGNALED(status):
                    self.known.pop(tid)
                    self.stopped.discard(tid)
                    exited.append(tid)
                elif os.WIFSTOPPED(status):
                    self.stopped.add(tid)
                    event = status >> 16
                    if event in (EVENT_FORK, EVENT_VFORK, EVENT_CLONE):
                        child = self.kernel.event_message(tid)
                        if child not in self.known:
                            # Automatically attached descendants still need to
                            # be released if the parent fails before consume().
                            self.remember(child, parent=tid)
                    elif event == EVENT_EXEC:
                        self.exec_transition(tid, observing=False)
                    elif not event and os.WSTOPSIG(status) != SYSCALL_STOP:
                        self.signals[tid] = os.WSTOPSIG(status)
            except Exception:
                break
        return {'detached': sorted(detached), 'observed_exits': sorted(exited),
                'remaining_traced': sorted(self.known), 'uncertain': sorted(set(uncertain)),
                'complete': not self.known and not uncertain,
                'startup_gate_released': self.gate_released, 'no_kill_by_tracer': True}

    def run(self):
        result = {'status': 'inconclusive', 'operation_id': self.registration['operation_id']}
        try:
            self.initial_attach()
            while self.known and self.kernel.now() < self.deadline:
                tid, status = self.wait_known(self.deadline)
                try:
                    write = self.consume(tid, status)
                except (FileNotFoundError, ProcessLookupError):
                    self.await_exit(tid)
                    continue
                if tid not in self.stopped:
                    continue
                if write:
                    candidate = self.observe_boundary(tid, write)
                    if candidate is not None:
                        result.update(candidate)
                        break
                self.resume(tid)
            else:
                raise Inconclusive(self.missing_boundary_reason())
        except Exception as error:
            result['reason'] = str(error) if isinstance(error, Inconclusive) else type(error).__name__
        finally:
            result['cleanup'] = self.cleanup()
            self.kernel.close()
        if not result['cleanup']['complete']:
            result['status'] = 'inconclusive'
            result['reason'] = 'trace-detach-incomplete-controller-watchdog-required'
        result['controller_cut_called'] = self.cut_called
        result['events'] = self.events
        return result


def trace_native(registration, callbacks, *, timeout=900):
    """Imported only by the separately registered disposable-lab controller."""
    return NativeTrace(registration, callbacks, timeout=timeout).run()
