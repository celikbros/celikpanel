"""Closed native DB-publication observer sharing NativeTrace process lifecycle.

Observes the unchanged selected recovery checker. No SQL, marker, syscall,
register or process-memory writes. The only memory read is exactly the fixed
NUL-terminated database filename at a held renameat2 entry. Independent callbacks
prove product authority and perform any cut. There is no command-line PID API.
A successful exchange exit precedes directory fsyncs: this is not a power-loss
or durable-publication claim. Native automatic recovery is separately observed.
"""
from __future__ import annotations

import os
from pathlib import Path
import re
import stat
import sys

# The guest controller loads this pinned file with importlib under python -I.
# Support both the repository subdirectory and the explicitly pinned flat upload.
HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

from native_trace import (AMD64, HEX64, Inconclusive, LinuxKernel, NativeTrace,
                          SYSCALL_STOP, directory_metadata, integer, proof_digest, require)
from wal_migration_identity import SNAPSHOT

RENAMEAT2, RENAME_EXCHANGE = 316, 2
NAME = b'celikpanel.db\0'
EXPECTATION = frozenset(('operation_id', 'token_sha256', 'worker_start_ticks',
                         'snapshot', 'checker_path', 'checker_sha256'))
CHECKER = re.compile(r'/usr/libexec/celikpanel/recovery-runtimes/v1/[0-9a-f]{64}/bin/panel-checker\Z')


def checker_command(expected):
    return (expected['checker_path'] + '\0--publish-update-database=' + expected['snapshot'] + '\0').encode('ascii')


def exchange_arguments(entry):
    """Decode only the fixed amd64 exchange, never accept AT_FDCWD/flag subsets."""
    if entry.get('op') != 'entry' or entry.get('number') != RENAMEAT2:
        return None
    args = entry.get('args')
    require(type(args) in (tuple, list) and len(args) == 6, 'exchange-arguments-shape')
    oldfd, oldname, newfd, newname, flags, _ = args
    if type(flags) is not int or flags != RENAME_EXCHANGE:
        return None
    require(integer(oldfd, 0, (1 << 20) - 1) and integer(newfd, 0, (1 << 20) - 1)
            and oldfd != newfd, 'exchange-directory-descriptors')
    require(integer(oldname, 1, (1 << 63) - len(NAME))
            and integer(newname, 1, (1 << 63) - len(NAME)), 'exchange-name-pointers')
    return {'old_dirfd': oldfd, 'new_dirfd': newfd, 'old_pointer': oldname, 'new_pointer': newname}


def successful_exchange(entry, syscall):
    arguments = exchange_arguments(entry) if entry is not None else None
    if arguments is None:
        return None
    require(syscall.get('op') == 'exit' and type(syscall.get('result')) is int
            and syscall['result'] == 0 and syscall.get('is_error') is False,
            'exchange-did-not-exit-successfully')
    return arguments


class ExchangeKernel(LinuxKernel):
    def exchange_names(self, tid, arguments):
        """Exactly two 14-byte reads from our already stopped admitted task."""
        before = self.task(tid)
        require(before['tracer_pid'] == os.getpid() and before['state'] == 't', 'exchange-memory-task-not-stopped')
        fd = os.open(f'/proc/{tid}/mem', os.O_RDONLY | os.O_CLOEXEC | os.O_NONBLOCK | os.O_NOFOLLOW)
        try:
            for key in ('old_pointer', 'new_pointer'):
                pointer = arguments[key]
                require(integer(pointer, 1, (1 << 63) - len(NAME)), 'exchange-name-pointers')
                require(os.pread(fd, len(NAME), pointer) == NAME, 'exchange-name-not-fixed-database')
            require(self.task(tid) == before, 'exchange-memory-task-changed')
        finally:
            os.close(fd)
        return {'old_name': NAME[:-1].decode('ascii'), 'new_name': NAME[:-1].decode('ascii')}

    def exchange_directories(self, tid, arguments):
        result = {}
        for key in ('old_dirfd', 'new_dirfd'):
            path = Path(f'/proc/{tid}/fd/{arguments[key]}')
            named = os.readlink(path)
            fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC | os.O_NONBLOCK)
            try:
                first = directory_metadata(os.fstat(fd))
                require(stat.S_ISDIR(first['mode']) and first == directory_metadata(os.stat(path))
                        and os.readlink(path) == named, 'exchange-directory-fd-changed')
                result[key] = {'path': named, 'identity': first}
            finally:
                os.close(fd)
        require(result['old_dirfd']['identity'] != result['new_dirfd']['identity'], 'exchange-directories-alias')
        return result


class ExchangeTrace(NativeTrace):
    def __init__(self, registration, callbacks, *, timeout=900, kernel=None):
        # Validate registration before an OS boundary opens the fixed cgroup.
        from native_trace import validate_registration
        checked = validate_registration(registration)
        super().__init__(checked, callbacks, timeout=timeout,
                         kernel=kernel if kernel is not None else ExchangeKernel(checked))

    def refresh_expected(self, force=False):
        expected = super().refresh_expected(force=force)
        if expected is not None:
            require(expected.keys() == EXPECTATION, 'publisher-expectation-shape')
            require(type(expected['snapshot']) is str and len(expected['snapshot']) <= 255
                    and SNAPSHOT.fullmatch(expected['snapshot']), 'publisher-snapshot-format')
            require(type(expected['checker_path']) is str and CHECKER.fullmatch(expected['checker_path']),
                    'publisher-checker-path')
            require(type(expected['checker_sha256']) is str and HEX64.fullmatch(expected['checker_sha256']),
                    'publisher-checker-digest')
        return expected

    def publisher(self, tid, expected):
        task = self.validate_known(tid)
        require(expected is not None and self.kernel.command(tid) == checker_command(expected), 'publisher-command-differs')
        require(task['uids'] == (0,) * 4 and task['gids'] == (0,) * 4, 'publisher-not-root')
        require(self.kernel.executable(tid)['sha256'] == expected['checker_sha256'], 'publisher-executable-differs')
        return task

    def select_syscall_target(self, tid, expected):
        if expected is not None and self.kernel.command(tid) == checker_command(expected):
            self.publisher(tid, expected)
            self.syscall_tgids.add(tid)
            self.event('admitted-database-publisher-syscalls-enabled', tgid=tid,
                       checker_sha256=expected['checker_sha256'])

    def syscall_event(self, tid, syscall):
        if syscall['op'] == 'entry':
            self.entries[tid] = syscall
            if self.known[tid]['tgid'] in self.syscall_tgids and exchange_arguments(syscall) is not None:
                return {'stage': 'entry', 'entry': dict(syscall)}
            return None
        entry = self.entries.pop(tid, None)
        if entry is not None and self.known[tid]['tgid'] in self.syscall_tgids and exchange_arguments(entry) is not None:
            successful_exchange(entry, syscall)
            return {'stage': 'exit', 'entry': entry, 'exit': dict(syscall)}
        return None

    def missing_boundary_reason(self):
        return 'no-exact-native-database-exchange-boundary'

    def exchange_snapshot(self, tid, arguments, stage):
        value = self.snapshot()
        value['publisher'] = {'pid': self.known[tid]['tgid'], 'tid': tid}
        value['exchange'] = {'number': RENAMEAT2, 'old_dirfd': arguments['old_dirfd'],
                             'new_dirfd': arguments['new_dirfd'], 'old_name': 'celikpanel.db',
                             'new_name': 'celikpanel.db', 'flags': RENAME_EXCHANGE, 'arch': AMD64,
                             'stage': stage, 'entry_exit_matched': stage == 'exit',
                             'result': 0 if stage == 'exit' else None, 'is_error': False}
        return value

    def held(self, tid, expected, entry, arguments, directories, trace, stage):
        self.freeze()
        require(self.refresh_expected(force=True) == expected, 'publisher-admission-changed')
        self.publisher(tid, expected)
        self.kernel.exchange_names(tid, arguments)
        require(self.kernel.exchange_directories(tid, arguments) == directories, 'exchange-directory-fd-changed')
        current = self.kernel.syscall(tid)
        if stage == 'entry':
            require(current == entry and self.entries.get(tid) == entry, 'exchange-entry-stop-changed')
        else:
            successful_exchange(entry, current)
        require(self.exchange_snapshot(tid, arguments, stage) == trace, 'exchange-task-inventory-changed')

    def observe_boundary(self, tid, boundary):
        require(boundary['stage'] == 'entry', 'exchange-entry-was-not-held')
        expected = self.refresh_expected(force=True)
        self.publisher(tid, expected)
        entry = boundary['entry']
        arguments = exchange_arguments(entry)
        require(arguments is not None, 'exchange-entry-unmatched')
        self.freeze()
        self.kernel.exchange_names(tid, arguments)
        directories = self.kernel.exchange_directories(tid, arguments)
        before = self.exchange_snapshot(tid, arguments, 'entry')
        before_sha = proof_digest(before)
        self.held(tid, expected, entry, arguments, directories, before, 'entry')
        self.callbacks.revalidate('before-exchange-entry-proof', before)
        entry_proof = self.callbacks.exchange_entry(before)
        require(proof_digest(before) == before_sha, 'exchange-entry-trace-mutated')
        require(type(entry_proof) is dict and entry_proof.get('status') == 'verified'
                and entry_proof.get('operation_id') == self.registration['operation_id']
                and entry_proof.get('trace_sha256') == before_sha, 'exchange-entry-not-independently-authorized')
        entry_sha = proof_digest(entry_proof)
        self.held(tid, expected, entry, arguments, directories, before, 'entry')
        self.event('database-exchange-entry-held', trace_sha256=before_sha, entry_proof_sha256=entry_sha)
        # Every sibling remains at its exact stop. No signal is fabricated or
        # swallowed: an intervening signal/event refuses the cut and cleanup
        # forwards its real delivery as in the WAL lifecycle.
        require(tid not in self.signals, 'exchange-entry-has-pending-signal')
        self.resume(tid)
        got, status = self.kernel.wait({tid}, min(self.deadline, self.kernel.now() + 5))
        require(got == tid, 'exchange-exit-wrong-task')
        matched = self.consume(tid, status)
        require(os.WIFSTOPPED(status) and status >> 16 == 0 and os.WSTOPSIG(status) == SYSCALL_STOP
                and matched is not None and matched['stage'] == 'exit' and matched['entry'] == entry,
                'exchange-exit-was-not-held')
        after = self.exchange_snapshot(tid, arguments, 'exit')
        signature = proof_digest(after)
        require(after['tasks'] == before['tasks'], 'exchange-task-inventory-changed')
        self.held(tid, expected, entry, arguments, directories, after, 'exit')
        self.callbacks.revalidate('before-cut-proof', after)
        verified = self.callbacks.authorize_cut(after, entry_proof)
        require(proof_digest(after) == signature and proof_digest(entry_proof) == entry_sha,
                'exchange-proof-was-mutated')
        require(type(verified) is dict and verified.get('status') == 'verified'
                and verified.get('operation_id') == self.registration['operation_id']
                and verified.get('trace_sha256') == signature and verified.get('entry_proof_sha256') == entry_sha,
                'exchange-cut-not-independently-authorized')
        self.held(tid, expected, entry, arguments, directories, after, 'exit')
        self.callbacks.revalidate('immediately-before-cut', after)
        require(proof_digest(after) == signature and proof_digest(entry_proof) == entry_sha,
                'exchange-final-proof-was-mutated')
        self.held(tid, expected, entry, arguments, directories, after, 'exit')
        self.cut_called = True
        receipt = self.callbacks.perform_cut(verified)
        require(type(receipt) is dict and receipt.get('status') == 'cut-sent'
                and receipt.get('operation_id') == self.registration['operation_id']
                and receipt.get('trace_sha256') == signature, 'cut-receipt-unconfirmed')
        self.event('authorized-controller-cut-sent', trace_sha256=signature)
        return {'status': 'cut-sent', 'trace': after, 'trace_sha256': signature,
                'entry_trace': before, 'entry_proof': entry_proof, 'entry_proof_sha256': entry_sha,
                'independent_proof': verified, 'cut_receipt': receipt,
                'limits': ['successful-atomic-namespace-exchange-before-receipt',
                           'directory-fsync-not-yet-observed-not-power-loss-durability',
                           'automatic-recovery-terminal-proof-is-separate']}


def trace_database_publication(registration, callbacks, *, timeout=900):
    """Only a registered disposable-lab controller supplies these capabilities."""
    return ExchangeTrace(registration, callbacks, timeout=timeout).run()
