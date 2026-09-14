#!/usr/bin/env python3
"""Guarded disposable recovery-unit kill or host-reboot checkpoint handoff.

Never starts update/recovery, edits product data, or reboots a guest itself.
The separate host controller may reset only its registered QEMU after collecting
reboot_ready. Missing durable proof means unavailable, never an improvised fault.
"""
from __future__ import annotations
import argparse
import datetime as dt
import hashlib
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import re
import signal
import stat
import subprocess
import sys
import time
import uuid

SPEC = importlib.util.spec_from_file_location('recovery_fault_shared', Path(__file__).with_name('guest_update_kill.py'))
shared = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(shared)
probe = shared.probe
files = shared.shared
ENV = shared.ENV
UNIT = 'celikpanel-release-recovery.service'
CHECKPOINT_ROOT = Path('/var/lib/celikpanel-recovery-checkpoints')
RUNTIME_ROOT = Path('/usr/libexec/celikpanel/recovery-runtimes/v1')
PRIVATE_ROOT = Path('/root/celikpanel-release-recovery-lab')
TRANSACTION_ROOT = Path('/var/lib/celikpanel-release-transaction')
SCHEMA = 'celikpanel/recovery-fault/v1'
INTENT_SCHEMA = 'celikpanel/recovery-fault-intent/v1'
CHECKPOINT_SCHEMA = 'celikpanel/recovery-checkpoint/v1'
CHECKPOINTS = ('restore_admitted', 'payload_restored', 'units_reloaded', 'runtime_verified', 'schedulers_restored')
PHASES = ('active', 'completion.pending', 'scheduler-restore.pending')
FIELDS = ('Id', 'LoadState', 'ActiveState', 'SubState', 'MainPID', 'ControlGroup', 'InvocationID', 'FreezerState')


class Unavailable(Exception):
    pass


def private_read(path: Path, maximum=16384) -> bytes:
    probe.protected_parents(path, {0})
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1:
        raise Unavailable('unsafe-private-evidence')
    return probe.bounded_file(path, maximum)


def validate_intent(value, identity, operation):
    required = {'schema', 'identity', 'operation_id', 'action', 'snapshot', 'snapshot_manifest_sha256',
                'transaction_token_sha256', 'runtime_manifest_sha256', 'checkpoint', 'worker_executable_sha256', 'worker_command'}
    if (not isinstance(value, dict) or set(value) != required or value['schema'] != INTENT_SCHEMA
            or value['identity'] != identity or value['operation_id'] != operation or not files.HEX32.fullmatch(operation)
            or value['action'] not in ('kill', 'reboot') or value['checkpoint'] not in CHECKPOINTS
            or not isinstance(value['snapshot'], str) or not files.SNAPSHOT.fullmatch(value['snapshot'])):
        raise Unavailable('invalid-exact-recovery-fault-intent')
    for key in ('snapshot_manifest_sha256', 'transaction_token_sha256', 'runtime_manifest_sha256', 'worker_executable_sha256'):
        if not isinstance(value[key], str) or not files.HEX64.fullmatch(value[key]):
            raise Unavailable('invalid-recovery-fault-digest')
    root = RUNTIME_ROOT / value['runtime_manifest_sha256']
    allowed = [ ['/bin/bash', str(root / path)] for path in ('deploy/recovery/runtime-entry.sh', 'deploy/release-recovery-runner.sh') ]
    if value['worker_command'] not in allowed:
        raise Unavailable('unexpected-recovery-worker-command')
    return value


def parse_marker(raw):
    match = re.fullmatch(rb'version=1\ntoken=([0-9a-f]{64})\noperation=(update|rollback)\nsnapshot=([^\n]+)\n', raw)
    if not match:
        raise Unavailable('noncanonical-transaction-marker')
    snapshot = match[3].decode('ascii')
    if not files.SNAPSHOT.fullmatch(snapshot):
        raise Unavailable('noncanonical-transaction-snapshot')
    return {'snapshot': snapshot, 'transaction_operation': match[2].decode(),
            'transaction_token_sha256': hashlib.sha256(match[1]).hexdigest()}


def read_transaction():
    present = []
    for phase in PHASES:
        try:
            value = parse_marker(private_read(TRANSACTION_ROOT / phase, 4096))
        except FileNotFoundError:
            continue
        present.append((phase, value))
    if not present:
        raise Unavailable('transaction-not-observed')
    if len(present) == 2 and [p[0] for p in present] == ['completion.pending', 'scheduler-restore.pending'] and present[0][1] == present[1][1]:
        present = present[:1]
    if len(present) != 1:
        raise Unavailable('ambiguous-transaction-markers')
    for other in ('quiesce.pending',):
        if (TRANSACTION_ROOT / other).exists() or (TRANSACTION_ROOT / other).is_symlink():
            raise Unavailable('unexpected-transaction-phase')
    return dict(present[0][1], transaction_phase=present[0][0])


def validate_checkpoint(value, intent, worker, transaction):
    required = {'schema', 'snapshot', 'transaction_token_sha256', 'transaction_operation', 'transaction_phase',
                'checkpoint', 'sequence', 'observed_at', 'recovery_unit', 'invocation_id', 'main_pid',
                'main_start_ticks', 'boot_id', 'runtime_manifest_sha256'}
    if not isinstance(value, dict) or set(value) != required or value['schema'] != CHECKPOINT_SCHEMA:
        raise Unavailable('checkpoint-record-missing-or-unsupported')
    for field in ('snapshot', 'transaction_token_sha256', 'runtime_manifest_sha256', 'checkpoint'):
        if value[field] != intent[field]:
            raise Unavailable('checkpoint-intent-mismatch')
    if value['transaction_operation'] not in ('update', 'rollback') or value['transaction_phase'] not in PHASES:
        raise Unavailable('checkpoint-transaction-phase-invalid')
    if any(value[field] != transaction[field] for field in ('snapshot', 'transaction_token_sha256', 'transaction_operation', 'transaction_phase')):
        raise Unavailable('checkpoint-live-transaction-mismatch')
    if type(value['sequence']) is not int or value['sequence'] <= 0 or value['sequence'] > 1000000:
        raise Unavailable('checkpoint-sequence-invalid')
    try:
        stamp = dt.datetime.fromisoformat(value['observed_at'].replace('Z', '+00:00'))
        if stamp.utcoffset() != dt.timedelta(0) or stamp > dt.datetime.now(dt.timezone.utc) + dt.timedelta(seconds=2):
            raise ValueError()
    except (ValueError, TypeError, AttributeError):
        raise Unavailable('checkpoint-time-invalid')
    for source, target in (('recovery_unit', 'unit'), ('invocation_id', 'invocation_id'), ('main_pid', 'pid'),
                           ('main_start_ticks', 'start_ticks'), ('boot_id', 'boot_id')):
        if value[source] != worker[target]:
            raise Unavailable('checkpoint-worker-identity-mismatch')
    if type(value['main_pid']) is not int or value['main_pid'] <= 1:
        raise Unavailable('checkpoint-main-pid-invalid')
    return value


def verify_runtime(digest, tick=lambda: None):
    root = RUNTIME_ROOT / digest
    manifest = root / 'runtime.manifest'
    raw = private_read(manifest, 4096)
    if hashlib.sha256(raw).hexdigest() != digest:
        raise Unavailable('runtime-manifest-mismatch')
    lines = raw.decode('ascii').splitlines()
    if lines[:3] != ['format=celikpanel-recovery-runtime-v1', 'protocol=1', 'snapshot=6']:
        raise Unavailable('runtime-manifest-protocol-unsupported')
    expected = {}
    for line in lines[3:]:
        match = re.fullmatch(r'([0-9a-f]{64})  ([^\x00\r\n]+)', line)
        if not match: raise Unavailable('runtime-manifest-entry-invalid')
        name = match[2]; relative = PurePosixPath(name)
        if relative.is_absolute() or '..' in relative.parts or str(relative) != name or name in expected or name == 'runtime.manifest':
            raise Unavailable('runtime-manifest-path-ambiguous')
        expected[name] = match[1]
    if not {'bin/recovery', 'rollback.sh', 'update.sh', 'deploy/recovery/runtime-entry.sh'} <= expected.keys():
        raise Unavailable('runtime-manifest-incomplete')
    actual = set()
    walked = 0
    for parent, dirs, names in os.walk(root, followlinks=False):
        tick()
        walked += 1 + len(dirs) + len(names)
        if walked > 100: raise Unavailable('runtime-inventory-exceeded-bound')
        probe.protected_identity(Path(parent), directory=True)
        for name in dirs: probe.protected_identity(Path(parent) / name, directory=True)
        for name in names:
            path = Path(parent) / name
            probe.protected_identity(path)
            if path != manifest: actual.add(path.relative_to(root).as_posix())
    if actual != set(expected): raise Unavailable('runtime-inventory-mismatch')
    for name, checksum in expected.items():
        tick()
        if files.digest_file(root / name, tick) != checksum:
            raise Unavailable('runtime-payload-mismatch')
    if private_read(manifest, 4096) != raw: raise Unavailable('runtime-manifest-changed')
    return {'runtime_manifest_sha256': digest, 'verified_files': len(expected)}


class Native:
    def __init__(self, intent):
        self.intent = intent

    def properties(self):
        argv = ['/usr/bin/systemctl', 'show', UNIT]
        for name in FIELDS: argv.extend(('-p', name))
        observed = subprocess.run(argv, stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=2, env=ENV)
        result = {}
        for line in observed.stdout.splitlines():
            key, sep, value = line.partition('=')
            if not sep or key in result: raise Unavailable('unit-properties-malformed')
            result[key] = value
        if set(result) != set(FIELDS) or observed.returncode != 0 or result['LoadState'] != 'loaded':
            raise Unavailable('unit-properties-unavailable')
        return result

    def worker_identity(self, *, cleanup=False):
        values = self.properties()
        if (values['Id'] != UNIT or values['ActiveState'] not in ('active', 'activating') or values['SubState'] not in ('running', 'start')
                or not values['MainPID'].isdigit() or int(values['MainPID']) <= 1 or values['ControlGroup'] != '/system.slice/' + UNIT
                or not files.HEX32.fullmatch(values['InvocationID'])):
            raise Unavailable('exact-recovery-worker-not-running')
        proc = Path('/proc') / values['MainPID']
        start = shared.process_start(probe.bounded_file(proc / 'stat', 16384, virtual=True).decode())
        command = probe.bounded_file(proc / 'cmdline', 8192, virtual=True)
        if not cleanup and command != b'\0'.join(part.encode() for part in self.intent['worker_command']) + b'\0':
            raise Unavailable('recovery-command-mismatch')
        if probe.bounded_file(proc / 'cgroup', 4096, virtual=True).decode() != '0::' + values['ControlGroup'] + '\n':
            raise Unavailable('recovery-cgroup-mismatch')
        binary = probe.hash_file(proc / 'exe', proc_executable=True)
        if binary.get('status') != 'ok' or not cleanup and binary.get('sha256') != self.intent['worker_executable_sha256']:
            raise Unavailable('recovery-executable-mismatch')
        boot = probe.bounded_file(Path('/proc/sys/kernel/random/boot_id'), 128, virtual=True).decode().strip()
        if str(uuid.UUID(boot)) != boot:
            raise Unavailable('boot-id-invalid')
        after = self.properties()
        if any(after[key] != values[key] for key in ('Id', 'MainPID', 'InvocationID', 'ControlGroup', 'ActiveState', 'SubState')) or shared.process_start(probe.bounded_file(proc / 'stat', 16384, virtual=True).decode()) != start:
            raise Unavailable('recovery-identity-changed')
        return {'unit': UNIT, 'pid': int(values['MainPID']), 'start_ticks': start, 'invocation_id': values['InvocationID'],
                'cgroup': values['ControlGroup'], 'boot_id': boot, 'running_executable_sha256': binary['sha256'],
                'command_sha256': hashlib.sha256(command).hexdigest()}

    def observe(self):
        worker = self.worker_identity()
        transaction = read_transaction()
        raw = private_read(CHECKPOINT_ROOT / (self.intent['transaction_token_sha256'] + '.json'))
        checkpoint = validate_checkpoint(probe.strict_object(raw), self.intent, worker, transaction)
        return {'worker': worker, 'transaction': transaction, 'checkpoint': checkpoint,
                'checkpoint_sha256': hashlib.sha256(raw).hexdigest()}

    def freeze(self):
        self.command('freeze')
        if self.properties()['FreezerState'] != 'frozen':
            raise Unavailable('recovery-freeze-not-confirmed')
        # Cleanup follows the invocation actually frozen, even if restart raced
        # with systemctl. This identity never admits a kill or reboot.
        actual = self.worker_identity(cleanup=True)
        if self.properties()['FreezerState'] != 'frozen':
            raise Unavailable('recovery-freeze-result-changed')
        return actual

    def command(self, action):
        result = subprocess.run(['/usr/bin/systemctl', action, UNIT], stdin=subprocess.DEVNULL, capture_output=True, timeout=5, env=ENV)
        if result.returncode: raise Unavailable('recovery-' + action + '-unconfirmed')

    def thaw(self, worker):
        # Do not thaw a newly started invocation of the fixed recovery unit.
        try:
            current = self.worker_identity(cleanup=True)
        except (Unavailable, OSError):
            values = self.properties()
            if values['MainPID'] == '0' and values['InvocationID'] == worker['invocation_id']:
                self.command('thaw')
                return 'old-empty-unit-thawed'
            if values['MainPID'] == '0': return 'unit-exited'
            return 'identity-unavailable'
        if current != worker: return 'identity-changed'
        self.command('thaw')
        return 'thawed'

    def proof(self, observation, tick):
        tick()
        current = self.observe()
        if current != observation or self.properties()['FreezerState'] != 'frozen':
            raise Unavailable('frozen-recovery-checkpoint-changed')
        snapshot = files.verify_snapshot(self.intent['snapshot'], tick)
        if snapshot is None or snapshot['manifest_sha256'] != self.intent['snapshot_manifest_sha256']:
            raise Unavailable('exact-complete-snapshot-not-verified')
        runtime_proof = verify_runtime(self.intent['runtime_manifest_sha256'], tick)
        tick()
        if self.observe() != observation or self.properties()['FreezerState'] != 'frozen':
            raise Unavailable('checkpoint-changed-after-full-proof')
        return dict(snapshot, runtime=runtime_proof)

    def kill(self):
        result = subprocess.run(['/usr/bin/systemctl', 'kill', '--kill-whom=all', '--signal=KILL', UNIT],
                                stdin=subprocess.DEVNULL, capture_output=True, timeout=5, env=ENV)
        if result.returncode: raise Unavailable('recovery-kill-result-unknown')


def run_fault(intent, emit, native, *, clock=time.monotonic, pause=time.sleep, interrupted=lambda: False):
    deadline = clock() + 600
    frozen, killed, ready = False, False, False
    observation, proof, frozen_deadline = None, None, None
    cleanup_worker = None
    reason = 'checkpoint-unavailable'
    def tick():
        if interrupted(): raise Unavailable('signal')
        if clock() >= deadline or frozen_deadline is not None and clock() >= frozen_deadline:
            raise Unavailable('timeout')
    emit('armed', action=intent['action'], checkpoint=intent['checkpoint'], timeout_seconds=600)
    try:
        while observation is None:
            tick()
            try:
                observation = native.observe()
            except (Unavailable, FileNotFoundError):
                pause(0.03)
        tick()
        # Bind again immediately before the freeze; unknown never signals a unit.
        if native.observe() != observation: raise Unavailable('pre-freeze-identity-changed')
        emit('freeze_requested', worker=observation['worker'])
        frozen = True
        frozen_deadline = clock() + 30
        cleanup_worker = native.freeze() or observation['worker']
        emit('freeze_observed', worker=cleanup_worker, scope='cleanup-only')
        proof = native.proof(observation, tick)
        emit('checkpoint_verified', **observation, snapshot_proof=proof)
        tick()
        if native.observe() != observation: raise Unavailable('pre-action-checkpoint-changed')
        if intent['action'] == 'kill':
            emit('kill_requested', scope='exact-recovery-unit-cgroup', worker=observation['worker'])
            native.kill()
            killed = True
            emit('kill_sent', worker=observation['worker'])
            reason = 'exact-recovery-unit-killed'
        else:
            ready = True
            emit('reboot_ready', worker=observation['worker'], checkpoint_sha256=observation['checkpoint_sha256'],
                 frozen_hold_seconds=30, reboot_scope='registered-QEMU-host-controller-only')
            # No guest reboot command. The host must collect this fsynced record,
            # reprove its QEMU identity and checkpoint, then perform one reset.
            while True:
                tick()
                if native.observe() != observation: raise Unavailable('reboot-handoff-identity-changed')
                pause(0.05)
    except Unavailable as exc:
        reason = str(exc)
    except Exception as exc:
        reason = 'observation-error:' + type(exc).__name__
    finally:
        thaw_result = None
        if frozen and observation is not None:
            try: thaw_result = native.thaw(cleanup_worker or observation['worker'])
            except Exception: thaw_result = 'unknown'
        emit('released', reason=reason, checkpoint_verified=proof is not None, kill_sent=killed,
             reboot_ready=ready, reboot_performed='not-observed-by-guest', thaw_result=thaw_result)
    return 0 if killed and proof is not None else 2


def cleanup(intent, identity, operation):
    """ExecStopPost binds the actual frozen invocation; it never admits a fault."""
    raw = private_read(PRIVATE_ROOT / ('recovery-fault-' + operation + '.jsonl'), 1048576)
    requested, expected = None, None
    for line in raw.splitlines():
        event = probe.strict_object(line)
        if event.get('schema') != SCHEMA or event.get('identity') != identity or event.get('operation_id') != operation:
            raise Unavailable('cleanup-event-identity-differs')
        if event.get('event') == 'freeze_requested': requested = event.get('worker')
        if event.get('event') == 'freeze_observed': expected = event.get('worker')
    if requested is None: return 'no-freeze-request'
    native = Native(intent)
    if expected is None:
        # Helper SIGKILL may fall between systemctl freeze and the fsynced result.
        # On this guarded fixture only, prove the currently frozen fixed unit's
        # full process identity afresh. This is thaw authority, never kill authority.
        if native.properties()['FreezerState'] != 'frozen': return 'no-frozen-result'
        expected = native.worker_identity(cleanup=True)
        if expected['boot_id'] != requested.get('boot_id') or native.properties()['FreezerState'] != 'frozen':
            return 'freeze-result-changed'
    if not isinstance(expected, dict) or expected.get('unit') != UNIT: raise Unavailable('cleanup-worker-invalid')
    return native.thaw(expected)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('lab-nonce', 'vm-uuid', 'cell-id', 'node', 'operation-id'):
        parser.add_argument('--' + name, required=True)
    parser.add_argument('--execute', action='store_true')
    parser.add_argument('--cleanup', action='store_true')
    args = parser.parse_args(argv)
    if not args.execute or not files.HEX32.fullmatch(args.operation_id):
        parser.error('exact disposable operation and --execute required')
    identity = probe.guard_guest(args)
    info = PRIVATE_ROOT.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or stat.S_IMODE(info.st_mode) != 0o700:
        raise Unavailable('unsafe-private-fixture-root')
    intent = validate_intent(probe.strict_object(private_read(PRIVATE_ROOT / ('recovery-fault-' + args.operation_id + '.json'))), identity, args.operation_id)
    if args.cleanup:
        print(json.dumps({'cleanup': cleanup(intent, identity, args.operation_id)}))
        return 0
    path = PRIVATE_ROOT / ('recovery-fault-' + args.operation_id + '.jsonl')
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    stopped = False
    def stop(signum, frame):
        nonlocal stopped
        stopped = True
    previous = {sig: signal.signal(sig, stop) for sig in (signal.SIGINT, signal.SIGTERM)}
    try:
        with os.fdopen(fd, 'w') as stream:
            def emit(event, **fields):
                stream.write(json.dumps({'schema': SCHEMA, 'event': event, 'at': probe.utc_now(), 'identity': identity,
                                        'operation_id': args.operation_id, **fields}, sort_keys=True) + '\n')
                stream.flush()
                os.fsync(stream.fileno())
            return run_fault(intent, emit, Native(intent), interrupted=lambda: stopped)
    finally:
        for sig, handler in previous.items(): signal.signal(sig, handler)


if __name__ == '__main__':
    try: raise SystemExit(main())
    except (OSError, probe.ProbeError, Unavailable) as exc:
        print('recovery fault refused: ' + type(exc).__name__, file=sys.stderr)
        raise SystemExit(2)
