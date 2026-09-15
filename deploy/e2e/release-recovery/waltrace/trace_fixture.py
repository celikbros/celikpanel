#!/usr/bin/env python3
"""Owned-child-only Linux amd64 syscall-stop feasibility, never an attach tool.

The only process initially seized is returned by our own gated os.fork(). The
kernel reports every later clone/fork/vfork. No PID input, memory reads, register
writes, syscall substitution, SQL injection or network operation is supported.
Successful evidence ends by killing/reaping this disposable process family.
"""
from __future__ import annotations

import argparse
import ctypes
import errno
import hashlib
import json
import os
from pathlib import Path
import platform
import signal
import sqlite3
import stat
import sys
import time

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))
from wal_frames import inspect_wal

SEIZE, INTERRUPT, GETEVENTMSG, GET_SYSCALL_INFO = 0x4206, 0x4207, 0x4201, 0x420e
SYSCALL = 24
TRACE_OPTIONS = 1 | 2 | 4 | 8 | 16 | 0x100000  # syscall, fork/vfork/clone/exec, EXITKILL
EVENT_FORK, EVENT_VFORK, EVENT_CLONE, EVENT_EXEC, EVENT_STOP = 1, 2, 3, 4, 128
SYSCALL_STOP = signal.SIGTRAP | 0x80
WAIT_ALL = 0x40000000
AUDIT_ARCH_X86_64 = 0xc000003e
WRITE_SYSCALLS = {1: 'write', 18: 'pwrite64', 20: 'writev', 296: 'pwritev', 328: 'pwritev2'}
MAX_BYTES = 64 * 1024 * 1024
ALLOWED_PTRACE = {SEIZE, INTERRUPT, GETEVENTMSG, GET_SYSCALL_INFO, SYSCALL}
LIMITS = [
    'controlled-single-writer-child-feasibility-only',
    'not-a-native-product-migration-or-update-acceptance-test',
    'large-fixed-SQL-statement-and-small-cache-force-spill; short-product-DDL-not-proven',
    'kernel-syscall-stop-and-private-copy-visibility; not-power-loss-durability',
    'Linux-x86_64-only; PTRACE_SEIZE-and-GET_SYSCALL_INFO-required',
    'no-attach-to-existing-PID; no-caller-supplied-SQL; no-memory-or-register-edits',
]


class TraceRefused(RuntimeError):
    pass


class Entry(ctypes.Structure):
    _fields_ = [('number', ctypes.c_uint64), ('args', ctypes.c_uint64 * 6)]


class Exit(ctypes.Structure):
    _fields_ = [('result', ctypes.c_int64), ('is_error', ctypes.c_uint8)]


class Payload(ctypes.Union):
    _fields_ = [('entry', Entry), ('exit', Exit)]


class SyscallInfo(ctypes.Structure):
    _fields_ = [('op', ctypes.c_uint8), ('padding', ctypes.c_uint8 * 3),
                ('arch', ctypes.c_uint32), ('ip', ctypes.c_uint64),
                ('sp', ctypes.c_uint64), ('payload', Payload)]


_libc = ctypes.CDLL(None, use_errno=True)
_libc.ptrace.argtypes = [ctypes.c_uint, ctypes.c_uint, ctypes.c_void_p, ctypes.c_void_p]
_libc.ptrace.restype = ctypes.c_long


def ptrace(request, tid, address=0, data=0):
    if request not in ALLOWED_PTRACE:
        raise TraceRefused('unsupported ptrace operation')
    ctypes.set_errno(0)
    value = _libc.ptrace(request, tid, ctypes.c_void_p(address), data)
    if value == -1:
        err = ctypes.get_errno()
        raise OSError(err, os.strerror(err))
    return value


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def start_time(tid):
    raw = Path(f'/proc/{tid}/stat').read_text()
    return int(raw[raw.rfind(')') + 2:].split()[19])


def process_status(tid):
    answer = {}
    for line in Path(f'/proc/{tid}/status').read_text().splitlines():
        key, _, value = line.partition(':')
        answer[key] = value.strip()
    return answer


def read_file(path):
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
    try:
        before = os.fstat(descriptor)
        if not stat.S_ISREG(before.st_mode) or before.st_nlink != 1 or before.st_size > MAX_BYTES:
            raise TraceRefused('unsupported evidence file')
        raw = os.pread(descriptor, before.st_size + 1, 0)
        after = os.fstat(descriptor)
        stable = lambda st: (st.st_dev, st.st_ino, st.st_mode, st.st_uid, st.st_gid, st.st_nlink, st.st_size, st.st_mtime_ns, st.st_ctime_ns)
        if stable(before) != stable(after) or len(raw) != before.st_size:
            raise TraceRefused('evidence file changed during read')
        return raw, {'device': before.st_dev, 'inode': before.st_ino,
                     'mode': stat.S_IMODE(before.st_mode), 'uid': before.st_uid,
                     'gid': before.st_gid, 'size': before.st_size, 'mtime_ns': before.st_mtime_ns,
                     'ctime_ns': before.st_ctime_ns, 'sha256': sha(raw)}
    finally:
        os.close(descriptor)


def write_exit(entry, info):
    """Only a matching positive native syscall exit can select WAL bytes."""
    if info.arch != AUDIT_ARCH_X86_64:
        raise TraceRefused('non-amd64 syscall ABI')
    if info.op != 2 or entry is None or info.payload.exit.is_error:
        return None
    result = int(info.payload.exit.result)
    number, args = entry
    if number not in WRITE_SYSCALLS or result <= 0:
        return None
    value = {'syscall': WRITE_SYSCALLS[number], 'number': number,
             'fd': int(args[0]), 'returned_bytes': result}
    if number in (1, 18):
        if result > args[2]:
            raise TraceRefused('write result exceeds requested count')
        value['requested_bytes'] = int(args[2])
    if number == 18:
        value['offset'] = int(args[3])
    return value


def appended_range(write, prior_size, observed_size):
    """A selected pwrite must contribute bytes to the newly appended suffix."""
    return (write['number'] == 18 and write.get('offset', -1) >= prior_size
            and write['returned_bytes'] > 0
            and write['offset'] + write['returned_bytes'] == observed_size)


class ControlledTrace:
    def __init__(self, executable, directory, *, mode='spill', timeout=20):
        if platform.system() != 'Linux' or platform.machine() != 'x86_64':
            raise TraceRefused('only Linux x86_64 is supported')
        if mode not in ('spill', 'small', 'hang') or not 0.1 <= timeout <= 60:
            raise TraceRefused('unsupported bounded fixture request')
        self.binary = Path(executable).resolve(strict=True)
        self.binary_bytes, self.binary_identity = read_file(self.binary)
        if self.binary_bytes[:4] != b'\x7fELF' or not os.access(self.binary, os.X_OK):
            raise TraceRefused('controlled fixture must be an executable ELF')
        self.root = Path(directory).absolute()
        self.root.mkdir(mode=0o700, parents=False, exist_ok=False)
        self.work = self.root / 'work'
        self.work.mkdir(mode=0o700)
        self.mode, self.timeout = mode, timeout
        self.deadline = time.monotonic() + timeout
        self.known = {}  # only our fork result and kernel-reported descendants
        self.stopped, self.entries, self.pending_signal = set(), {}, {}
        self.pidfds, self.exits, self.events, self.execs = {}, [], [], []
        self.maximum_tasks = 0
        self.root_pid = None
        self.baseline = None
        self.wal_identity = None
        self.begun = False
        self.commit_entered = False
        self.stdout = self.root / 'child.stdout'
        self.stderr = self.root / 'child.stderr'
        self.event_file = self.root / 'events.jsonl'
        self.result = {'schema': 'celikpanel/lab-waltrace-feasibility/v1',
                       'status': 'inconclusive', 'mode': mode, 'limits': list(LIMITS),
                       'fixture_binary': self.binary_identity, 'kernel': platform.release()}

    def event(self, event, **fields):
        value = {'event': event, 'monotonic_ns': time.monotonic_ns(), **fields}
        self.events.append(value)
        with self.event_file.open('a', encoding='utf-8') as output:
            output.write(json.dumps(value, sort_keys=True) + '\n')

    def admit(self, tid, parent):
        if tid in self.known:
            raise TraceRefused('duplicate child identity')
        status = process_status(tid)
        tgid = int(status['Tgid'])
        self.known[tid] = {'tid': tid, 'tgid': tgid, 'start_time': start_time(tid), 'parent': parent}
        if tgid not in self.pidfds:
            self.pidfds[tgid] = os.pidfd_open(tgid)
        self.maximum_tasks = max(self.maximum_tasks, len(self.known))
        self.event('owned-task-admitted', **self.known[tid])

    def launch(self):
        gate_read, gate_write = os.pipe2(os.O_CLOEXEC)
        ready_read, ready_write = os.pipe2(os.O_CLOEXEC)
        original_parent = os.getpid()
        child = os.fork()
        if child == 0:
            try:
                os.close(gate_write)
                os.close(ready_read)
                os.setsid()
                os.umask(0o077)
                if _libc.prctl(1, signal.SIGKILL, 0, 0, 0) != 0 or os.getppid() != original_parent:
                    os._exit(120)
                os.write(ready_write, b'R')
                os.close(ready_write)
                if os.read(gate_read, 1) != b'G':
                    os._exit(121)
                os.close(gate_read)
                for target, path in ((1, self.stdout), (2, self.stderr)):
                    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
                    os.dup2(fd, target)
                    os.close(fd)
                os.chdir(self.work)
                os.execve(self.binary, [str(self.binary), self.mode, str(self.work)],
                          {'PATH': '/usr/bin:/bin', 'HOME': str(self.work), 'LC_ALL': 'C'})
            except BaseException:
                os._exit(122)
        self.root_pid = child
        os.close(gate_read)
        os.close(ready_write)
        try:
            import select
            if not select.select([ready_read], [], [], min(2, self.timeout))[0] or os.read(ready_read, 1) != b'R':
                raise TraceRefused('controlled child gate did not become ready')
            self.admit(child, os.getpid())
            if os.getpgid(child) != child:
                raise TraceRefused('controlled child process group mismatch')
            ptrace(SEIZE, child, data=TRACE_OPTIONS)
            ptrace(INTERRUPT, child)
            tid, status = self.wait_one()
            if tid != child or not os.WIFSTOPPED(status) or status >> 16 != EVENT_STOP:
                raise TraceRefused('initial seized stop not confirmed')
            self.stopped.add(child)
            self.event('controlled-child-seized', tid=child, start_time=self.known[child]['start_time'])
            os.write(gate_write, b'G')
            self.resume(child)
        finally:
            os.close(ready_read)
            os.close(gate_write)

    def wait_one(self, deadline=None):
        deadline = self.deadline if deadline is None else deadline
        while time.monotonic() < deadline:
            for tid in list(self.known):
                try:
                    got, status = os.waitpid(tid, os.WNOHANG | WAIT_ALL)
                except ChildProcessError:
                    continue
                if got:
                    return got, status
            if not self.known:
                raise TraceRefused('controlled workload exited without qualifying WAL write')
            time.sleep(0.0002)
        raise TimeoutError('bounded controlled-child trace timed out')

    def awaiting_exit(self, tid):
        # exit_group may retire a ptrace-stopped sibling before its stop is
        # processed. Keep its admission until the kernel wait exit arrives.
        try:
            current = process_status(tid)
            if start_time(tid) != self.known[tid]['start_time']:
                raise TraceRefused('task identity changed while awaiting exit')
            # ESRCH can precede the visible zombie state during exit_group.
            # This is only a wait request, never proof of exit: the task stays
            # admitted and the ordinary deadline still applies.
        except FileNotFoundError:
            pass
        self.stopped.discard(tid)
        self.entries.pop(tid, None)
        self.event('owned-task-awaiting-kernel-exit', tid=tid)
        return True

    def resume(self, tid):
        if tid not in self.stopped:
            raise TraceRefused('resume requires a known stopped task')
        try:
            ptrace(SYSCALL, tid, data=self.pending_signal.pop(tid, 0))
        except ProcessLookupError:
            if not self.awaiting_exit(tid):
                raise
        self.stopped.discard(tid)

    def resumed_all(self):
        for tid in sorted(self.stopped):
            self.resume(tid)

    def status_event(self, tid, status):
        if tid not in self.known:
            raise TraceRefused('unadmitted wait result')
        if os.WIFEXITED(status) or os.WIFSIGNALED(status):
            self.exits.append({'tid': tid, 'exit_code': os.WEXITSTATUS(status) if os.WIFEXITED(status) else None,
                               'signal': os.WTERMSIG(status) if os.WIFSIGNALED(status) else None})
            self.event('owned-task-exited', **self.exits[-1])
            self.known.pop(tid)
            self.stopped.discard(tid)
            self.entries.pop(tid, None)
            return None
        if not os.WIFSTOPPED(status):
            raise TraceRefused('unexpected wait state')
        if start_time(tid) != self.known[tid]['start_time']:
            raise TraceRefused('task identity changed')
        self.stopped.add(tid)
        event, sig = status >> 16, os.WSTOPSIG(status)
        if event in (EVENT_FORK, EVENT_VFORK, EVENT_CLONE):
            new_tid = ctypes.c_ulong()
            ptrace(GETEVENTMSG, tid, data=ctypes.byref(new_tid))
            self.admit(int(new_tid.value), tid)
            self.event('kernel-child-event', tid=tid, child_tid=int(new_tid.value), kind=event)
        elif event == EVENT_EXEC:
            old_tid = ctypes.c_ulong()
            ptrace(GETEVENTMSG, tid, data=ctypes.byref(old_tid))
            # The controlled Go helper execs from its process leader. A future
            # nonleader-exec adapter must explicitly reconcile all retired IDs.
            if old_tid.value != tid:
                raise TraceRefused('nonleader exec is outside this feasibility contract')
            descriptor = os.open(f'/proc/{tid}/exe', os.O_RDONLY | os.O_CLOEXEC | os.O_NONBLOCK)
            try:
                image_stat = os.fstat(descriptor)
                if (not stat.S_ISREG(image_stat.st_mode) or image_stat.st_size > MAX_BYTES
                        or (image_stat.st_dev, image_stat.st_ino) != (self.binary_identity['device'], self.binary_identity['inode'])):
                    raise TraceRefused('controlled executable inode changed')
                raw = os.pread(descriptor, image_stat.st_size + 1, 0)
                if len(raw) != image_stat.st_size or os.fstat(descriptor) != image_stat:
                    raise TraceRefused('controlled executable changed during read')
            finally:
                os.close(descriptor)
            if sha(raw) != self.binary_identity['sha256']:
                raise TraceRefused('controlled child executed a different image')
            self.execs.append({'tid': tid, 'sha256': sha(raw)})
            self.entries.pop(tid, None)
            self.event('controlled-image-exec', **self.execs[-1])
        elif event == EVENT_STOP:
            pass
        elif sig == SYSCALL_STOP:
            info = SyscallInfo()
            size = ptrace(GET_SYSCALL_INFO, tid, ctypes.sizeof(info), ctypes.byref(info))
            if size < 24 or info.arch != AUDIT_ARCH_X86_64:
                raise TraceRefused('unsupported syscall information')
            if info.op == 1:
                if size < 80:
                    raise TraceRefused('truncated syscall entry')
                self.entries[tid] = (int(info.payload.entry.number), tuple(info.payload.entry.args))
            elif info.op == 2:
                if size < 33:
                    raise TraceRefused('truncated syscall exit')
                return write_exit(self.entries.pop(tid, None), info)
            else:
                raise TraceRefused('not a syscall entry or exit stop')
        elif event:
            raise TraceRefused('unexpected ptrace event')
        else:
            # Preserve real signal delivery, including Go's SIGURG preemption.
            self.pending_signal[tid] = sig
        return None

    def quiesce(self):
        for tid in sorted(set(self.known) - self.stopped):
            try:
                ptrace(INTERRUPT, tid)
            except OSError as error:
                if error.errno != errno.ESRCH:
                    raise
        bound = min(self.deadline, time.monotonic() + 3)
        while set(self.known) != self.stopped:
            tid, status = self.wait_one(bound)
            # New clone descendants may appear while their parent enters stop;
            # inherited tracing already holds each newborn until resumed.
            self.status_event(tid, status)
        inventory = []
        expected = set(self.known)
        actual = set()
        for tgid in sorted({entry['tgid'] for entry in self.known.values()}):
            actual.update(int(p.name) for p in Path(f'/proc/{tgid}/task').iterdir())
        if actual != expected:
            raise TraceRefused('stopped task inventory is incomplete')
        for tid in sorted(expected):
            current = process_status(tid)
            if (int(current['TracerPid']) != os.getpid() or not current['State'].startswith('t')
                    or start_time(tid) != self.known[tid]['start_time']):
                raise TraceRefused('task is not stopped under this tracer')
            inventory.append({**self.known[tid], 'tracer_pid': os.getpid(), 'state': current['State']})
        self.event('all-owned-tasks-stopped', tasks=inventory)
        return inventory

    def markers(self):
        if not self.stdout.exists():
            return ''
        raw, _ = read_file(self.stdout)
        if len(raw) > 65536:
            raise TraceRefused('fixture marker output exceeded bound')
        text = raw.decode('ascii', errors='strict')
        self.begun = 'WALTRACE_TRANSACTION_BEGUN\n' in text
        self.commit_entered = 'WALTRACE_COMMIT_ENTERING\n' in text
        return text

    def snapshot(self, name):
        dest = self.root / name
        dest.mkdir(mode=0o700)
        evidence = {}
        for filename in ('fixture.db', 'fixture.db-wal', 'fixture.db-shm'):
            source = self.work / filename
            raw, identity = read_file(source)
            with (dest / filename).open('xb') as output:
                output.write(raw)
            evidence[filename] = identity
        return evidence

    def wal_at_exit(self, tid, write):
        # The bounded first adapter proves an exact positioned payload write.
        # write/writev/pwritev may be observed but never qualify this proof.
        if write['number'] != 18:
            return None
        if write['fd'] < 0 or write['fd'] > 65535:
            return None
        descriptor = Path(f'/proc/{tid}/fd/{write["fd"]}')
        try:
            if os.readlink(descriptor) != str(self.work / 'fixture.db-wal'):
                return None
            actual = descriptor.stat()
            raw, identity = read_file(self.work / 'fixture.db-wal')
        except FileNotFoundError:
            return None
        if (actual.st_dev, actual.st_ino) != (identity['device'], identity['inode']):
            raise TraceRefused('write descriptor does not match observed WAL inode')
        if self.wal_identity != (identity['device'], identity['inode']):
            raise TraceRefused('WAL generation file changed since committed baseline')
        if not appended_range(write, len(self.baseline), len(raw)):
            return None
        return raw, identity

    def private_visibility(self):
        copy = self.root / 'sqlite-read-copy'
        copy.mkdir(mode=0o700)
        for filename in ('fixture.db', 'fixture.db-wal', 'fixture.db-shm'):
            raw, _ = read_file(self.root / 'stopped-images' / filename)
            (copy / filename).write_bytes(raw)
        with sqlite3.connect(f'file:{copy / "fixture.db"}?mode=ro', uri=True) as db:
            rows = db.execute('SELECT count(*) FROM samples').fetchone()[0]
            check = db.execute('PRAGMA quick_check').fetchall()
        if rows != 1 or check != [('ok',)]:
            raise TraceRefused('private copy did not preserve only the committed fixture row')
        return {'reader': 'Python SQLite on separate copies only', 'visible_rows': rows,
                'quick_check': 'ok', 'original_sidecars_opened_by_sqlite': False}

    def cleanup(self):
        # pidfds pin only our fork and kernel-reported process descendants.
        for descriptor in self.pidfds.values():
            try:
                signal.pidfd_send_signal(descriptor, signal.SIGKILL)
            except ProcessLookupError:
                pass
        if self.root_pid is not None and not self.pidfds:
            # Even a seize/admission failure cannot leave our direct gated fork.
            try:
                os.kill(self.root_pid, signal.SIGKILL)
                os.waitpid(self.root_pid, 0)
            except (ProcessLookupError, ChildProcessError):
                pass
        end = time.monotonic() + 3
        while self.known and time.monotonic() < end:
            try:
                tid, status = self.wait_one(end)
                if os.WIFEXITED(status) or os.WIFSIGNALED(status):
                    self.status_event(tid, status)
                elif os.WIFSTOPPED(status):
                    # A fork event already pending when failure occurred still
                    # owns its automatically traced child; admit and kill it.
                    if status >> 16 in (EVENT_FORK, EVENT_VFORK, EVENT_CLONE):
                        new_tid = ctypes.c_ulong()
                        ptrace(GETEVENTMSG, tid, data=ctypes.byref(new_tid))
                        if int(new_tid.value) not in self.known:
                            self.admit(int(new_tid.value), tid)
                    for descriptor in self.pidfds.values():
                        try:
                            signal.pidfd_send_signal(descriptor, signal.SIGKILL)
                        except ProcessLookupError:
                            pass
            except TimeoutError:
                break
        for descriptor in self.pidfds.values():
            os.close(descriptor)
        self.result['cleanup'] = {'all_admitted_tasks_reaped': not self.known,
                                  'remaining_tasks': sorted(self.known), 'exits': self.exits}
        if self.known:
            self.result['status'] = 'inconclusive'
            self.result['reason'] = 'owned child cleanup not confirmed'

    def run(self):
        selected = False
        try:
            self.launch()
            while self.known:
                tid, status = self.wait_one()
                try:
                    write = self.status_event(tid, status)
                except (ProcessLookupError, FileNotFoundError):
                    if not self.awaiting_exit(tid):
                        raise
                    continue
                if tid not in self.stopped:
                    continue
                if write:
                    text = self.markers()
                    if self.baseline is None and 'WALTRACE_BASELINE_READY\n' in text:
                        self.quiesce()
                        self.baseline, identity = read_file(self.work / 'fixture.db-wal')
                        parsed = inspect_wal(self.baseline)
                        if parsed['status'] != 'verified' or parsed['classification'] != 'committed-prefix-only':
                            raise TraceRefused('baseline is not an exact complete committed WAL prefix')
                        self.wal_identity = identity['device'], identity['inode']
                        self.result['baseline_wal'] = parsed
                        self.result['baseline_images'] = self.snapshot('baseline-images')
                        self.event('committed-baseline-captured', wal_sha256=sha(self.baseline))
                        self.resumed_all()
                        continue
                    if self.baseline is not None and self.begun and not self.commit_entered:
                        observation = self.wal_at_exit(tid, write)
                        if observation:
                            raw, identity = observation
                            parsed = inspect_wal(raw, prior_prefix=self.baseline)
                            if (parsed['status'] == 'verified' and parsed['classification'] == 'valid-noncommit-suffix'
                                    and parsed['growth_after_prior_commit']):
                                inventory = self.quiesce()
                                again = self.wal_at_exit(tid, write)
                                if again is None or again[0] != raw:
                                    raise TraceRefused('WAL changed while stopping all owned tasks')
                                self.markers()
                                if not self.begun or self.commit_entered:
                                    raise TraceRefused('fixture entered commit before final stop proof')
                                self.result.update({'status': 'verified-feasibility', 'successful_write_exit':
                                    {'tid': tid, **write, 'wal': identity}, 'stopped_tasks': inventory,
                                    'wal_evidence': parsed, 'stopped_images': self.snapshot('stopped-images'),
                                    'fixture_transaction_begun': True, 'fixture_commit_entered': False})
                                self.event('uncommitted-write-held', tid=tid, write=write, wal_sha256=sha(raw))
                                selected = True
                                break
                self.resume(tid)
            if not selected:
                raise TraceRefused('no qualifying pre-commit WAL write was observed')
        except (TraceRefused, OSError, TimeoutError, ValueError) as error:
            self.result.update(status='inconclusive', reason=f'{type(error).__name__}: {error}')
            self.event('inconclusive', reason=self.result['reason'])
        finally:
            self.cleanup()
        if selected and self.result['status'] == 'verified-feasibility':
            try:
                for filename, before in self.result['stopped_images'].items():
                    _, after = read_file(self.work / filename)
                    if after != before:
                        raise TraceRefused('original stopped database image changed during cleanup')
                self.result['private_copy_visibility'] = self.private_visibility()
                for filename, before in self.result['stopped_images'].items():
                    _, after = read_file(self.work / filename)
                    if after != before:
                        raise TraceRefused('original image changed during private-copy inspection')
                self.result['original_images_preserved'] = True
            except (TraceRefused, OSError, sqlite3.Error) as error:
                self.result.update(status='inconclusive', reason=str(error))
        self.result.update(maximum_admitted_tasks=self.maximum_tasks, controlled_execs=self.execs,
                           event_count=len(self.events))
        (self.root / 'result.json').write_text(json.dumps(self.result, indent=2, sort_keys=True) + '\n')
        return self.result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--fixture-binary', required=True, type=Path)
    parser.add_argument('--output-directory', required=True, type=Path,
                        help='must not exist; private evidence is retained on every outcome')
    parser.add_argument('--mode', choices=('spill', 'small', 'hang'), default='spill')
    parser.add_argument('--timeout', type=float, default=20)
    args = parser.parse_args()
    result = ControlledTrace(args.fixture_binary, args.output_directory, mode=args.mode, timeout=args.timeout).run()
    print(json.dumps({'status': result['status'], 'reason': result.get('reason'),
                      'evidence': str(args.output_directory)}, sort_keys=True))
    return 0 if result['status'] == 'verified-feasibility' else 2


if __name__ == '__main__':
    sys.exit(main())
