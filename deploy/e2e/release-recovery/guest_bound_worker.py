#!/usr/bin/env python3
"""Exact real-worker cut for a fresh current-producer recovery observation lab.

Only a nonce/DMI-guarded disposable VM is accepted. This observer never creates
worker state, observations, bindings or transactions. It may freeze/kill only the
proved self-update unit; ordinary services and workload data are never changed.
"""
from __future__ import annotations
import argparse
import datetime as dt
import grp
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import sys

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location('bound_worker_kill', HERE / 'guest_update_kill.py')
base = importlib.util.module_from_spec(SPEC); SPEC.loader.exec_module(base)
probe, files = base.probe, base.shared
SCHEMA = 'celikpanel/bound-worker-fault-intent/v1'
OBSERVATIONS = Path('/var/lib/celikpanel-recovery-observations')
BINDINGS = Path('/var/lib/celikpanel-release-state/recovery-observation-bindings')
WORKERS = Path('/var/lib/celikpanel-release-state/self-update')
HEX40 = re.compile(r'[0-9a-f]{40}\Z')


def protected_read(path, maximum, mode=0o600, gid=0):
    probe.protected_parents(path, {0})
    info = path.lstat()
    if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_gid != gid
            or stat.S_IMODE(info.st_mode) != mode or info.st_nlink != 1):
        raise probe.ProbeError('unsafe bound-worker evidence')
    raw = probe.bounded_file(path, maximum)
    if probe.metadata(path.lstat()) != probe.metadata(info):
        raise probe.ProbeError('bound-worker evidence changed')
    return raw


def validate_intent(value, identity, operation):
    if (not isinstance(value, dict) or set(value) != {'schema', 'identity', 'operation_id', 'baseline', 'target', 'recovery_fault'}
            or value['schema'] != SCHEMA or value['identity'] != identity
            or value['operation_id'] != operation or not files.HEX32.fullmatch(operation)):
        raise probe.ProbeError('bound-worker intent identity differs')
    for role, version in (('baseline', 'v0.1.0-alpha.81'), ('target', 'v0.1.0-alpha.82')):
        item = value[role]
        if (not isinstance(item, dict) or set(item) != {'version', 'commit', 'agent_sha256', 'panel_sha256'}
                or item['version'] != version or not HEX40.fullmatch(item.get('commit', ''))
                or any(not files.HEX64.fullmatch(item.get(key, '')) for key in ('agent_sha256', 'panel_sha256'))):
            raise probe.ProbeError('unsupported current-producer artifact identity')
    if value['baseline']['commit'] == value['target']['commit']:
        raise probe.ProbeError('candidate source commit must be distinct')
    if any(value['baseline'][key] == value['target'][key] for key in ('agent_sha256', 'panel_sha256')):
        raise probe.ProbeError('candidate artifacts must be distinct')
    if value['recovery_fault'] not in (None, {'action': 'reboot', 'checkpoint': 'payload_restored'}):
        raise probe.ProbeError('unsupported bound-worker recovery fault')
    return value


def fixed_record(raw, keys):
    if len(raw) > 2048 or not raw.endswith(b'\n'):
        raise probe.ProbeError('noncanonical record framing')
    try: lines = raw.decode('ascii').splitlines()
    except UnicodeError: raise probe.ProbeError('non-ASCII record') from None
    if len(lines) != len(keys): raise probe.ProbeError('record field count differs')
    values = {}
    for line, key in zip(lines, keys):
        if not line.startswith(key + '='): raise probe.ProbeError('record fields differ')
        values[key] = line[len(key) + 1:]
    if raw != ''.join(key + '=' + values[key] + '\n' for key in keys).encode():
        raise probe.ProbeError('record framing differs')
    return values


def bound_proof(intent, snapshot, transaction_raw, observation_raw, binding_raw, state_raw):
    operation, target, baseline = intent['operation_id'], intent['target'], intent['baseline']
    transaction = fixed_record(transaction_raw, ('version', 'token', 'operation', 'snapshot'))
    observation = fixed_record(observation_raw, ('schema', 'request_id', 'target_commit', 'phase', 'terminal_proof', 'reason', 'observed_at', 'previous_failure'))
    binding = fixed_record(binding_raw, ('schema', 'request_id', 'target_commit', 'snapshot', 'update_token'))
    state = probe.strict_object(state_raw)
    if (transaction['version'] != '1' or transaction['operation'] != 'update'
            or transaction['snapshot'] != snapshot or not files.HEX64.fullmatch(transaction['token'])
            or not files.SNAPSHOT.fullmatch(snapshot) or '-to-' + target['commit'] + '-' not in snapshot):
        raise probe.ProbeError('transaction is not exact active target')
    expected_binding = {'schema': 'celikpanel-recovery-binding/v1', 'request_id': operation,
                        'target_commit': target['commit'], 'snapshot': snapshot, 'update_token': transaction['token']}
    if binding != expected_binding: raise probe.ProbeError('immutable observation binding differs')
    if (observation['schema'] != 'celikpanel-recovery-observation/v1' or observation['request_id'] != operation
            or observation['target_commit'] != target['commit'] or observation['phase'] != 'running'
            or observation['terminal_proof'] != 'none' or observation['reason'] != 'update_running'
            or observation['previous_failure'] != 'none'):
        raise probe.ProbeError('initial worker observation is not confirmed')
    try: observed = dt.datetime.strptime(observation['observed_at'], '%Y-%m-%dT%H:%M:%SZ')
    except ValueError: raise probe.ProbeError('worker observation time invalid') from None
    if observed.strftime('%Y-%m-%dT%H:%M:%SZ') != observation['observed_at']:
        raise probe.ProbeError('worker observation time noncanonical')
    if (state.get('version') != 1 or state.get('request_id') != operation or state.get('status') != 'running'
            or state.get('target_version') != target['version'] or state.get('target_commit') != target['commit']
            or state.get('expected_current_version') != baseline['version']
            or state.get('expected_current_commit') != baseline['commit']
            or state.get('target_os') != 'linux' or state.get('target_arch') != 'amd64'
            or state.get('error')):
        raise probe.ProbeError('real worker accepted tuple differs')
    # Tokens and raw worker state stay private. This is observation provenance,
    # not product mutation authority and never a replacement for native locks.
    return {'request_id': operation, 'target_commit': target['commit'], 'snapshot': snapshot,
            'transaction_token_sha256': hashlib.sha256(transaction['token'].encode()).hexdigest(),
            'binding_sha256': hashlib.sha256(binding_raw).hexdigest(),
            'observation_sha256': hashlib.sha256(observation_raw).hexdigest(),
            'worker_state_sha256': hashlib.sha256(state_raw).hexdigest(), 'phase': 'running', 'terminal_proof': 'none'}


class Native(base.Native):
    def __init__(self, args, intent):
        super().__init__(args)
        self.intent = intent
        self.bound = None

    def worker_identity(self):
        before = self.properties()
        base.validate_worker_fields(before, self.args.operation_id)
        proc = Path('/proc') / before['MainPID']
        start = base.process_start(probe.bounded_file(proc / 'stat', 16384, virtual=True).decode())
        command = probe.bounded_file(proc / 'cmdline', 4096, virtual=True)
        expected = b'/opt/celikpanel/bin/agent\0--self-update-worker\0' + self.args.operation_id.encode() + b'\0'
        if command != expected or probe.bounded_file(proc / 'cgroup', 4096, virtual=True).decode() != '0::' + before['ControlGroup'] + '\n':
            raise base.MissedCheckpoint('bound-worker-command-or-cgroup-differs')
        executable = probe.hash_file(proc / 'exe', proc_executable=True)
        if executable.get('status') != 'ok' or executable.get('sha256') != self.intent['baseline']['agent_sha256']:
            raise base.MissedCheckpoint('bound-worker-executable-differs')
        # This hidden production mode exits before sockets, managers or background
        # tasks; it inspects the actual retained executable, including after unlink.
        result = subprocess.run([str(proc / 'exe'), '--inspect-build-identity'], capture_output=True, timeout=3, env=base.ENV)
        expected_build = ('version=' + self.intent['baseline']['version'] + '\ncommit=' + self.intent['baseline']['commit'] + '\n').encode()
        if result.returncode or result.stdout != expected_build or result.stderr:
            raise base.MissedCheckpoint('bound-worker-build-identity-differs')
        after = self.properties()
        if any(before[k] != after[k] for k in ('Id', 'MainPID', 'InvocationID', 'ControlGroup', 'ActiveState')) or start != base.process_start(probe.bounded_file(proc / 'stat', 16384, virtual=True).decode()):
            raise base.MissedCheckpoint('bound-worker-identity-changed')
        return {'unit': self.unit, 'pid': int(before['MainPID']), 'start_ticks': start,
                'invocation_id': before['InvocationID'], 'cgroup': before['ControlGroup'],
                'running_executable_sha256': executable['sha256'],
                'boot_id': Path('/proc/sys/kernel/random/boot_id').read_text().strip()}

    def read_binding(self, snapshot):
        gid = grp.getgrnam('celikpanel').gr_gid
        return bound_proof(self.intent, snapshot,
                           protected_read(files.TRANSACTIONS / 'active', 4096),
                           protected_read(OBSERVATIONS / (self.args.operation_id + '.status'), 2048, 0o640, gid),
                           protected_read(BINDINGS / (snapshot + '.binding'), 1024),
                           protected_read(WORKERS / (self.args.operation_id + '.json'), 16384))

    def full_proof(self, snapshot, tick):
        proof = super().full_proof(snapshot, tick)
        before = self.read_binding(snapshot)
        for name in ('agent', 'panel'):
            if files.digest_file(files.SNAPSHOTS / snapshot / 'bin' / name, tick) != self.intent['baseline'][name + '_sha256']:
                raise base.MissedCheckpoint('snapshot-baseline-artifact-differs')
        if self.read_binding(snapshot) != before:
            raise base.MissedCheckpoint('bound-worker-provenance-changed')
        self.bound = before
        return dict(proof, bound_worker=before)

    def kill(self):
        if self.bound is None or self.read_binding(self.bound['snapshot']) != self.bound:
            raise base.MissedCheckpoint('bound-worker-provenance-changed-before-kill')
        super().kill()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('lab-nonce', 'vm-uuid', 'cell-id', 'node', 'operation-id'):
        parser.add_argument('--' + name, required=True)
    parser.add_argument('--execute', action='store_true')
    args = parser.parse_args(argv)
    if not args.execute or not files.HEX32.fullmatch(args.operation_id):
        parser.error('exact disposable operation and --execute required')
    identity = probe.guard_guest(args)
    root = files.PRIVATE_ROOT
    info = root.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or stat.S_IMODE(info.st_mode) != 0o700:
        raise probe.ProbeError('unsafe bound-worker fixture root')
    intent = validate_intent(probe.strict_object(protected_read(root / ('bound-worker-' + args.operation_id + '.json'), 8192)), identity, args.operation_id)
    args.candidate_agent, args.candidate_panel = intent['target']['agent_sha256'], intent['target']['panel_sha256']
    fault = intent['recovery_fault']
    args.recovery_action = fault['action'] if fault else None
    args.recovery_checkpoint = fault['checkpoint'] if fault else None
    fd = os.open(root / ('update-kill-' + args.operation_id + '.jsonl'), os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    stopped = False
    def stop(signum, frame):
        nonlocal stopped
        stopped = True
    previous = {sig: signal.signal(sig, stop) for sig in (signal.SIGINT, signal.SIGTERM)}
    try:
        with os.fdopen(fd, 'w') as stream:
            def emit(event, **fields):
                stream.write(json.dumps({'schema': base.SCHEMA, 'event': event, 'at': probe.utc_now(), 'identity': identity, **fields}, sort_keys=True) + '\n')
                stream.flush(); os.fsync(stream.fileno())
            return base.run_kill(args, emit, Native(args, intent), interrupted=lambda: stopped)
    finally:
        for sig, handler in previous.items(): signal.signal(sig, handler)


if __name__ == '__main__':
    try: raise SystemExit(main())
    except (OSError, probe.ProbeError) as exc:
        print('bound-worker fault refused: ' + type(exc).__name__, file=sys.stderr)
        raise SystemExit(2)
