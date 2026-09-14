#!/usr/bin/env python3
"""Disposable completion cut: an informational journal hint is never authority.

Only the separately opted-in local fixture may hold loopback 2083, freeze its
exact updater, quarantine retained test data and kill that updater. Product
coordinators are observed, never signalled. Recovery is left to native OnFailure.
"""
from __future__ import annotations
import errno
import hashlib
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import re
import socket
import stat
import subprocess
import sys
import time

HERE = Path(__file__).resolve().parent
def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    value = importlib.util.module_from_spec(spec); sys.modules[name] = value; spec.loader.exec_module(value); return value

kill = module('completion_kill', 'guest_update_kill.py')
data = module('completion_data', 'guest_candidate_data_fault.py')
shared, probe = kill.shared, kill.probe
CHECKPOINT = 'CELIKPANEL_UPDATE_CHECKPOINT database_verified_before_start'
BOUNDARY = 'completion-database-verified'
MATERIAL = Path('/var/lib/celikpanel-release-state/recovery-material/v1')
ENV = {**kill.ENV, 'HOME': '/root', 'USER': 'root', 'LOGNAME': 'root',
       'CELIKPANEL_DATA_DIR': '/var/lib/celikpanel'}



def validate_events(raw, identity, operation):
    if len(raw) > 1048576 or raw and not raw.endswith(b"\n"):
        raise ValueError('completion event stream is incomplete or oversized')
    events = [probe.strict_object(line) for line in raw.splitlines()]
    kinds = [event.get('event') for event in events]
    expected = ['armed']
    if 'completion_port_held' in kinds: expected.append('completion_port_held')
    expected += ['worker_frozen', 'completion_database_verified_checkpoint',
                 'candidate_data_fault_applied', 'kill_requested', 'kill_sent']
    progress = kinds[:-1] if kinds and kinds[-1] == 'released' else kinds
    if progress != expected[:len(progress)] or kinds and not progress:
        raise ValueError('completion stream is missing its required prior proof')
    for event in events:
        if (event.get('schema') != kill.SCHEMA or event.get('identity') != identity
                or event.get('operation_id') != operation or not isinstance(event.get('at'), str)):
            raise ValueError('completion event identity differs')
    if events and events[0].get('boundary') != BOUNDARY:
        raise ValueError('completion stream has a different fault boundary')
    return events


def completion_snapshot(state, expected=None):
    marker = state.get('transaction')
    if (state['worker'].get('ActiveState') != 'active' or not marker
            or marker.get('phase') != 'completion.pending' or marker.get('operation') != 'update'):
        raise kill.MissedCheckpoint('exact-completion-checkpoint-missed')
    if expected is not None and marker.get('snapshot') != expected:
        raise kill.MissedCheckpoint('completion-snapshot-changed')
    if state.get('update_lock_exclusive') is not True:
        raise kill.MissedCheckpoint('completion-updater-exclusive-lock-unproved')
    return marker['snapshot']


def checkpoint_hint(raw, identity):
    if len(raw) > 262144: raise kill.MissedCheckpoint('checkpoint-journal-bound')
    matches = []
    for line in raw.splitlines():
        value = probe.strict_object(line)
        message = value.get('MESSAGE')
        if isinstance(message, list) and all(type(v) is int and 0 <= v < 256 for v in message):
            message = bytes(message).decode('utf-8', errors='strict')
        if message != CHECKPOINT: continue
        if (value.get('_SYSTEMD_UNIT') != identity['unit']
                or value.get('_SYSTEMD_INVOCATION_ID') != identity['invocation_id']
                or value.get('_BOOT_ID', '').replace('-', '') != identity['boot_id'].replace('-', '')
                or not str(value.get('_PID', '')).isdigit()
                or not str(value.get('__REALTIME_TIMESTAMP', '')).isdigit()):
            continue
        matches.append({'unit': identity['unit'], 'invocation_id': identity['invocation_id'],
                        'boot_id': identity['boot_id'], 'pid': int(value['_PID']),
                        'timestamp_us': value['__REALTIME_TIMESTAMP'], 'message': CHECKPOINT,
                        'authority': 'informational-journal-hint-only'})
    if len(matches) > 1: raise kill.MissedCheckpoint('checkpoint-journal-ambiguous')
    return matches[0] if matches else None


def material_proof(snapshot, token_hash, candidate_manifest, tick):
    root = MATERIAL / hashlib.sha256(snapshot.encode()).hexdigest()
    fd = data.open_dir(root)
    try:
        data.check_dir(fd, private=True)
        metadata = data.file_proof(fd, 'material.json', tick)
        if metadata['mode'] != 0o600 or metadata['size'] > 4 * 1024 * 1024:
            raise kill.MissedCheckpoint('completion-material-record-bound')
        recordfd = os.open('material.json', data.FLAGS, dir_fd=fd)
        with os.fdopen(recordfd, 'rb') as stream: raw = stream.read(4 * 1024 * 1024 + 1)
        if hashlib.sha256(raw).hexdigest() != metadata['sha256']:
            raise kill.MissedCheckpoint('completion-material-record-changed')
        value = probe.strict_object(raw)
        if (value.get('schema') not in ('celikpanel/recovery-material/v1', 'celikpanel/recovery-material/v2')
                or value.get('snapshot') != snapshot or value.get('transaction_token_sha256') != token_hash
                or value.get('candidate_manifest_sha256') != candidate_manifest
                or not shared.HEX64.fullmatch(value.get('data_manifest_sha256', ''))):
            raise kill.MissedCheckpoint('completion-material-tuple-differs')
        datafd = data.open_dir(root / 'data')
        try:
            inventory = data.inventory(datafd, tick)
            manifestfd = os.open('SHA256SUMS', data.FLAGS, dir_fd=datafd)
            with os.fdopen(manifestfd, 'rb') as stream: sums = stream.read(32769)
            if len(sums) > 32768 or hashlib.sha256(sums).hexdigest() != value['data_manifest_sha256']:
                raise kill.MissedCheckpoint('completion-material-manifest-differs')
            rows = {}
            for line in sums.decode().splitlines():
                match = re.fullmatch(r'([0-9a-f]{64})  (\./[^\x00\r\n]+)', line)
                if not match: raise kill.MissedCheckpoint('completion-material-manifest-invalid')
                name = match[2][2:]; path = PurePosixPath(name)
                if path.is_absolute() or '..' in path.parts or str(path) != name or name in rows or name == 'SHA256SUMS':
                    raise kill.MissedCheckpoint('completion-material-manifest-path')
                rows[name] = match[1]
            if not rows or rows != {k: v for k, v in inventory.items() if k != 'SHA256SUMS'}:
                raise kill.MissedCheckpoint('completion-material-inventory-differs')
        finally: os.close(datafd)
        data.same_dir(root, fd)
        if data.file_proof(fd, 'material.json', tick) != metadata:
            raise kill.MissedCheckpoint('completion-material-record-changed')
        return {'root': str(root), 'schema': value['schema'], 'record_sha256': metadata['sha256'],
                'snapshot': snapshot, 'snapshot_manifest_sha256': value['snapshot_manifest_sha256'],
                'transaction_token_sha256': token_hash, 'candidate_manifest_sha256': candidate_manifest,
                'data_manifest_sha256': value['data_manifest_sha256'], 'data_inventory': inventory}
    finally: os.close(fd)


class CompletionNative:
    def __init__(self, native):
        self.native = native; self.socket = None; self.plan = native.plan; self.args = native.args
        if self.plan['boundary'] != BOUNDARY or self.plan.get('candidate_data_fault') != data.OPTION or 'recovery_fault' in self.plan:
            raise kill.MissedCheckpoint('completion-plan-not-explicit')

    def __getattr__(self, name): return getattr(self.native, name)

    def observe(self):
        state = self.native.observe()
        state['update_lock_exclusive'] = self.native.owns_transaction_lock(state['worker'])
        return state

    def maybe_hold_port(self, state):
        if self.socket is not None: return False
        marker = state.get('transaction')
        if not marker or marker.get('operation') != 'update' or marker.get('phase') not in ('active', 'completion.pending'):
            return False
        if state.get('update_lock_exclusive') is not True: return False
        worker = self.native.worker_identity()
        panel = shared.unit_state('celikpanel-panel.service')
        if panel['MainPID'] != '0' or panel['ActiveState'] not in ('inactive', 'failed'): return False
        listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        try:
            listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            listener.bind(('127.0.0.1', 2083)); listener.listen(1)
        except OSError as exc:
            listener.close()
            if exc.errno == errno.EADDRINUSE: return False
            raise
        if self.native.worker_identity() != worker or not self.native.owns_transaction_lock(self.native.properties()):
            listener.close()
            raise kill.MissedCheckpoint('completion-worker-changed-during-port-admission')
        self.socket = listener
        return True

    def release_port(self):
        if self.socket is not None: self.socket.close(); self.socket = None

    def hint(self, identity):
        result = subprocess.run(['/usr/bin/journalctl', '--no-pager', '--output=json',
                                 '_SYSTEMD_UNIT=' + identity['unit'],
                                 '_SYSTEMD_INVOCATION_ID=' + identity['invocation_id'], '-n', '200'],
                                capture_output=True, timeout=3, env=kill.ENV)
        if result.returncode: raise kill.MissedCheckpoint('checkpoint-journal-unavailable')
        return checkpoint_hint(result.stdout, identity)

    def coordinators(self, tick):
        result = {}
        for name in ('agent', 'panel'):
            tick(); unit = 'celikpanel-' + name + '.service'
            before = data.hand.properties(unit); pid = before['MainPID']
            if before['ActiveState'] in ('inactive', 'failed') and pid == '0':
                result[name] = {'state': before['ActiveState'], 'main_pid': 0}; continue
            if before['ActiveState'] not in ('active', 'activating') or not pid.isdigit() or int(pid) <= 1:
                raise kill.MissedCheckpoint('completion-coordinator-state-unknown')
            proc = Path('/proc') / pid
            start = kill.process_start(probe.bounded_file(proc / 'stat', 16384, virtual=True).decode())
            group = '/system.slice/' + unit
            if (before['ControlGroup'] != group or not shared.HEX32.fullmatch(before['InvocationID'])
                    or probe.bounded_file(proc / 'cgroup', 4096, virtual=True) != ('0::' + group + '\n').encode()
                    or probe.bounded_file(proc / 'cmdline', 16384, virtual=True) != ('/opt/celikpanel/bin/' + name + '\0').encode()):
                raise kill.MissedCheckpoint('completion-coordinator-identity-unknown')
            # /proc/<pid>/exe deliberately follows the kernel executable link.
            with (proc / 'exe').open('rb') as stream:
                before_exe = os.fstat(stream.fileno())
                if not stat.S_ISREG(before_exe.st_mode) or before_exe.st_size > 128 * 1024 * 1024:
                    raise kill.MissedCheckpoint('completion-coordinator-executable-bound')
                h = hashlib.sha256()
                for chunk in iter(lambda: stream.read(1048576), b''):
                    tick(); h.update(chunk)
                if probe.metadata(before_exe) != probe.metadata(os.fstat(stream.fileno())):
                    raise kill.MissedCheckpoint('completion-coordinator-executable-changed')
                digest = h.hexdigest()
            if digest != self.plan['candidate']['files']['bin/' + name]:
                raise kill.MissedCheckpoint('completion-coordinator-not-candidate')
            after = data.hand.properties(unit)
            if before != after or kill.process_start(probe.bounded_file(proc / 'stat', 16384, virtual=True).decode()) != start:
                raise kill.MissedCheckpoint('completion-coordinator-changed')
            result[name] = {'state': before['ActiveState'], 'main_pid': int(pid), 'start_ticks': start,
                            'invocation_id': before['InvocationID'], 'cgroup': group, 'running_sha256': digest}
        return result

    def completion_proof(self, snapshot, identity, tick):
        proof = self.native.full_proof(snapshot, tick)
        transaction = data.fault.read_transaction()
        if transaction['transaction_phase'] != 'completion.pending' or transaction['transaction_operation'] != 'update' or transaction['snapshot'] != snapshot:
            raise kill.MissedCheckpoint('completion-token-phase-differs')
        runtime, selection = data.hand.selection()
        runtime_proof = data.fault.verify_runtime(runtime, tick)
        material = material_proof(snapshot, transaction['transaction_token_sha256'], self.plan['candidate']['manifest_sha256'], tick)
        if material['schema'] != 'celikpanel/recovery-material/v2':
            raise kill.MissedCheckpoint('completion-material-v2-required')
        if material['snapshot_manifest_sha256'] != proof['manifest_sha256']:
            raise kill.MissedCheckpoint('completion-material-snapshot-differs')
        checker = data.fault.RUNTIME_ROOT / runtime / 'bin/panel-checker'
        checker_hash = shared.digest_file(checker, tick)
        tick()
        result = subprocess.run([str(checker), '--check-completed-update-database-wal-aware'],
                                stdin=subprocess.DEVNULL, capture_output=True, timeout=12, env=ENV)
        tick()
        if result.returncode: raise kill.MissedCheckpoint('completion-readonly-database-check-failed')
        if data.hand.selection() != (runtime, selection) or data.fault.verify_runtime(runtime, tick) != runtime_proof:
            raise kill.MissedCheckpoint('completion-runtime-changed')
        self.native.revalidate(identity)
        if data.fault.read_transaction() != transaction:
            raise kill.MissedCheckpoint('completion-transaction-changed')
        proof.update({'phase': 'completion.pending', 'transaction': transaction,
                      'runtime_proof': runtime_proof, 'material': material,
                      'database_readonly_checker': {'path': str(checker), 'sha256': checker_hash,
                         'flag': '--check-completed-update-database-wal-aware', 'exit_code': 0,
                         'stdout_sha256': hashlib.sha256(result.stdout).hexdigest(),
                         'stderr_sha256': hashlib.sha256(result.stderr).hexdigest()},
                      'coordinators': self.coordinators(tick)})
        return proof

    def verify_after_fault(self, proof, identity, tick):
        self.native.revalidate(identity)
        if data.fault.read_transaction() != proof['transaction']:
            raise kill.MissedCheckpoint('completion-transaction-changed-after-data-fault')
        if material_proof(proof['snapshot'], proof['transaction']['transaction_token_sha256'],
                          self.plan['candidate']['manifest_sha256'], tick) != proof['material']:
            raise kill.MissedCheckpoint('completion-material-changed-after-data-fault')
        # Native coordinators may have completed a start; any running process is
        # still required to be the exact installed candidate, never killed here.
        return self.coordinators(tick)


def run_fault(args, emit, native, *, clock=time.monotonic, pause=time.sleep, interrupted=lambda: False):
    deadline = clock() + 600; freeze_deadline = None; frozen = False; killed = False
    proof = None; snapshot = None; saw_worker = False; last = clock()
    def tick():
        nonlocal last
        if interrupted(): raise kill.MissedCheckpoint('signal')
        if clock() >= deadline or freeze_deadline is not None and clock() >= freeze_deadline:
            raise kill.MissedCheckpoint('timeout')
        if frozen and clock() - last >= .2:
            completion_snapshot(native.observe(), snapshot); last = clock()
    emit('armed', operation_id=args.operation_id, timeout_seconds=600, boundary=BOUNDARY)
    try:
        while True:
            tick(); state = native.observe()
            running = state['worker'].get('ActiveState') == 'active'
            if saw_worker and not running: raise kill.MissedCheckpoint('update-unit-exited-before-completion-cut')
            saw_worker = saw_worker or running
            if running and native.maybe_hold_port(state):
                emit('completion_port_held', operation_id=args.operation_id, address='127.0.0.1:2083')
            marker = state.get('transaction')
            if running and marker and marker.get('phase') == 'completion.pending':
                snapshot = completion_snapshot(state)
                identity = native.worker_identity()
                hint = native.hint(identity)
                if hint is not None:
                    frozen = True; freeze_deadline = clock() + 30
                    native.freeze(); native.revalidate(identity)
                    completion_snapshot(native.observe(), snapshot)
                    emit('worker_frozen', operation_id=args.operation_id, worker=identity, snapshot=snapshot)
                    proof = native.completion_proof(snapshot, identity, tick)
                    tick(); native.revalidate(identity); completion_snapshot(native.observe(), snapshot)
                    emit('completion_database_verified_checkpoint', operation_id=args.operation_id,
                         worker=identity, journal_hint=hint, **proof)
                    loss = native.candidate_data_fault(identity, proof, tick)
                    emit('candidate_data_fault_applied', operation_id=args.operation_id, data_fault=loss)
                    coordinators = native.verify_after_fault(proof, identity, tick)
                    tick(); native.revalidate(identity); completion_snapshot(native.observe(), snapshot)
                    emit('kill_requested', operation_id=args.operation_id, worker=identity, snapshot=snapshot,
                         coordinators=coordinators, signal='SIGKILL', scope='exact-update-unit-cgroup')
                    native.kill(); killed = True
                    emit('kill_sent', operation_id=args.operation_id, worker=identity, snapshot=snapshot)
                    reason = 'exact-completion-update-unit-killed'; break
            if running and marker and marker.get('phase') in ('scheduler-restore.pending', 'completion-scheduler'):
                raise kill.MissedCheckpoint('completion-cut-already-passed')
            pause(.03)
    except kill.MissedCheckpoint as exc: reason = str(exc)
    except Exception as exc: reason = 'observation-error:' + type(exc).__name__
    finally:
        port_released = False
        try: native.release_port(); port_released = True
        except Exception: pass
        thaw = None
        if frozen:
            try: thaw = native.thaw()
            except Exception: thaw = 'unknown'
        emit('released', operation_id=args.operation_id, reason=locals().get('reason', 'unknown'),
             checkpoint_verified=proof is not None, kill_sent=killed, thaw_exit=thaw,
             port_released=port_released, boundary=BOUNDARY)
    return 0 if killed and proof is not None else 2
