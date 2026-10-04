#!/usr/bin/env python3
"""Exact disposable updater-to-recovery fault handoff; no product recovery authority."""
from __future__ import annotations
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import time

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("recovery_handoff_fault", HERE / "guest_recovery_fault.py")
fault = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(fault)
SELECTION = Path('/var/lib/celikpanel-release-state/recovery-runtime.v1')
ENV = fault.ENV
SCHEMA = 'celikpanel/recovery-fault-handoff/v1'


def names(operation):
    if not fault.files.HEX32.fullmatch(operation): raise fault.Unavailable('invalid-handoff-operation')
    return {'unit': 'celikpanel-lab-recovery-fault-' + operation + '.service',
            'intent': fault.PRIVATE_ROOT / ('recovery-fault-' + operation + '.json'),
            'events': fault.PRIVATE_ROOT / ('recovery-fault-' + operation + '.jsonl')}


def encoded(value): return (json.dumps(value, sort_keys=True) + '\n').encode()


def save_once(path, value):
    raw = encoded(value)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(raw); stream.flush(); os.fsync(stream.fileno())
    parent = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try: os.fsync(parent)
    finally: os.close(parent)
    return hashlib.sha256(raw).hexdigest()


def selection():
    raw = fault.private_read(SELECTION, 160)
    match = re.fullmatch(rb'format=celikpanel-recovery-selection-v1\nruntime=([0-9a-f]{64})\n', raw)
    if not match: raise fault.Unavailable('selected-runtime-not-canonical')
    return match[1].decode(), raw


def properties(unit):
    result = subprocess.run(['/usr/bin/systemctl', 'show', unit, '-p', 'Id', '-p', 'LoadState', '-p', 'ActiveState',
                             '-p', 'MainPID', '-p', 'InvocationID', '-p', 'ControlGroup'],
                            capture_output=True, text=True, timeout=2, env=ENV)
    values = {}
    for line in result.stdout.splitlines():
        key, sep, value = line.partition('=')
        if not sep or key in values: raise fault.Unavailable('handoff-unit-properties-invalid')
        values[key] = value
    if (set(values) != {'Id', 'LoadState', 'ActiveState', 'MainPID', 'InvocationID', 'ControlGroup'}
            or values['Id'] != unit or result.returncode not in (0, 1)
            or result.returncode == 1 and values['LoadState'] != 'not-found'):
        raise fault.Unavailable('handoff-unit-properties-unavailable')
    return values


def helper_argv(identity, operation, cleanup=False):
    argv = ['/usr/bin/python3', '-I', str(fault.PRIVATE_ROOT / 'guest_recovery_fault.py'), '--execute']
    for flag, value in (('lab-nonce', identity['nonce']), ('vm-uuid', identity['vm_uuid']),
                        ('cell-id', identity['cell_id']), ('node', identity['node']), ('operation-id', operation)):
        argv.extend(('--' + flag, value))
    if cleanup: argv.append('--cleanup')
    return argv


def launch_argv(identity, operation):
    paths = names(operation)
    return ['systemd-run', '--unit=' + paths['unit'], '--no-block', '--property=Type=exec',
            '--property=RuntimeMaxSec=650', '--property=TimeoutStopSec=5', '--property=UMask=0077',
            '--property=ExecStopPost=-' + shlex.join(helper_argv(identity, operation, True)),
            *helper_argv(identity, operation)]


def read_events(identity, operation):
    try: raw = fault.private_read(names(operation)['events'], 1048576)
    except FileNotFoundError: return b'', []
    events = []
    for line in raw.splitlines():
        event = fault.probe.strict_object(line)
        if (event.get('schema') != fault.SCHEMA or event.get('identity') != identity
                or event.get('operation_id') != operation): raise fault.Unavailable('recovery-event-identity-differs')
        events.append(event)
    if raw and not raw.endswith(b'\n'): raise fault.Unavailable('recovery-event-incomplete')
    return raw, events


def make_intent(args, identity, proof, transaction, runtime, bash_sha):
    if (transaction.get('snapshot') != proof.get('snapshot') or transaction.get('transaction_phase') != 'active'
            or transaction.get('transaction_operation') != 'update'):
        raise fault.Unavailable('handoff-is-not-exact-active-update')
    value = {'schema': fault.INTENT_SCHEMA, 'identity': identity, 'operation_id': args.operation_id,
             'action': args.recovery_action, 'checkpoint': args.recovery_checkpoint,
             'snapshot': proof['snapshot'], 'snapshot_manifest_sha256': proof['manifest_sha256'],
             'transaction_token_sha256': transaction['transaction_token_sha256'],
             'runtime_manifest_sha256': runtime, 'worker_executable_sha256': bash_sha,
             'worker_command': ['/bin/bash', str(fault.RUNTIME_ROOT / runtime / 'deploy/recovery/runtime-entry.sh')]}
    return fault.validate_intent(value, identity, args.operation_id)


def helper_identity(identity, operation):
    unit = names(operation)['unit']
    before = properties(unit)
    if (before['LoadState'] != 'loaded' or before['ActiveState'] not in ('active', 'activating')
            or not before['MainPID'].isdigit() or int(before['MainPID']) <= 1
            or not fault.files.HEX32.fullmatch(before['InvocationID'])
            or before['ControlGroup'] != '/system.slice/' + unit):
        raise fault.Unavailable('exact-handoff-helper-not-running')
    proc = Path('/proc') / before['MainPID']
    start = fault.shared.process_start(fault.probe.bounded_file(proc / 'stat', 16384, virtual=True).decode())
    command = fault.probe.bounded_file(proc / 'cmdline', 8192, virtual=True)
    expected = helper_argv(identity, operation)
    allowed = [b'\0'.join(part.encode() for part in [executable, *expected[1:]]) + b'\0'
               for executable in ('python3', '/usr/bin/python3')]
    if command not in allowed or fault.probe.bounded_file(proc / 'cgroup', 4096, virtual=True).decode() != '0::' + before['ControlGroup'] + '\n':
        raise fault.Unavailable('handoff-helper-command-or-cgroup-differs')
    binary = fault.probe.hash_file(proc / 'exe', proc_executable=True)
    python_sha = fault.files.digest_file(Path('/usr/bin/python3').resolve())
    if binary.get('status') != 'ok' or binary.get('sha256') != python_sha:
        raise fault.Unavailable('handoff-helper-executable-differs')
    if properties(unit) != before or fault.shared.process_start(fault.probe.bounded_file(proc / 'stat', 16384, virtual=True).decode()) != start:
        raise fault.Unavailable('handoff-helper-identity-changed')
    return dict(before, helper_proof={'identity': identity, 'operation_id': operation, 'start_ticks': start,
                                     'command_sha256': hashlib.sha256(command).hexdigest(), 'executable_sha256': python_sha})


def cancel(operation, expected):
    actual = properties(names(operation)['unit'])
    if actual['MainPID'] == '0': return 'exited'
    if any(actual[field] != expected[field] for field in ('MainPID', 'InvocationID', 'ControlGroup')):
        return 'identity-changed'
    proof = expected.get('helper_proof')
    if not proof or proof.get('operation_id') != operation: return 'identity-unavailable'
    if helper_identity(proof['identity'], operation) != expected: return 'identity-changed'
    result = subprocess.run(['/usr/bin/systemctl', 'stop', names(operation)['unit']], capture_output=True, timeout=8, env=ENV)
    return 'stopped' if result.returncode == 0 else 'unknown'


def cancel_admission_unknown(identity, operation):
    # Discovery is read-only and requires the entire planned command/VM identity,
    # cgroup, invocation, PID/start time and interpreter proof before one stop.
    try: expected = helper_identity(identity, operation)
    except (fault.Unavailable, fault.probe.ProbeError, OSError): return 'identity-unavailable'
    return cancel(operation, expected)


def arm(args, updater_identity, proof, tick, revalidate):
    identity = fault.probe.guard_guest(args)
    paths = names(args.operation_id)
    if properties(paths['unit'])['LoadState'] != 'not-found': raise fault.Unavailable('handoff-unit-already-exists')
    if any(path.exists() or path.is_symlink() for path in (paths['intent'], paths['events'])):
        raise fault.Unavailable('handoff-evidence-already-exists')
    tick(); revalidate(updater_identity)
    transaction = fault.read_transaction()
    runtime, selection_raw = selection()
    runtime_proof = fault.verify_runtime(runtime, tick)
    bash_sha = fault.files.digest_file(Path('/bin/bash').resolve(), tick)
    intent = make_intent(args, identity, proof, transaction, runtime, bash_sha)
    tick(); revalidate(updater_identity)
    if fault.read_transaction() != transaction or selection()[1] != selection_raw:
        raise fault.Unavailable('handoff-proof-changed')
    intent_sha = save_once(paths['intent'], intent)
    deadline = time.monotonic() + 10
    unit = None
    try:
        launch = subprocess.run(launch_argv(identity, args.operation_id), capture_output=True, timeout=8, env=ENV)
        if launch.returncode: raise fault.Unavailable('recovery-fault-launch-unconfirmed')
        while time.monotonic() < deadline:
            tick(); revalidate(updater_identity)
            if fault.read_transaction() != transaction or selection()[1] != selection_raw:
                raise fault.Unavailable('handoff-proof-changed-after-arm')
            unit = properties(paths['unit'])
            raw, events = read_events(identity, args.operation_id)
            if events:
                if ([event['event'] for event in events] != ['armed']
                        or events[0].get('action') != intent['action'] or events[0].get('checkpoint') != intent['checkpoint']
                        or unit['ActiveState'] != 'active'
                        or not unit['MainPID'].isdigit() or int(unit['MainPID']) <= 1
                        or not fault.files.HEX32.fullmatch(unit['InvocationID'])
                        or unit['ControlGroup'] != '/system.slice/' + paths['unit']):
                    raise fault.Unavailable('exact-recovery-fault-not-armed')
                unit = helper_identity(identity, args.operation_id)
                return {'schema': SCHEMA, 'identity': identity, 'operation_id': args.operation_id,
                        'intent': intent, 'intent_sha256': intent_sha, 'armed_events_sha256': hashlib.sha256(raw).hexdigest(),
                        'unit': unit, 'updater': updater_identity, 'runtime_proof': runtime_proof}
            time.sleep(0.03)
        raise fault.Unavailable('recovery-fault-arm-timeout')
    except BaseException:
        try: cancel_admission_unknown(identity, args.operation_id)
        except Exception: pass  # preserve unavailable admission; never launch again
        raise


def collect(identity, operation, require_reboot=False):
    intent = fault.validate_intent(fault.probe.strict_object(fault.private_read(names(operation)['intent'])), identity, operation)
    raw, events = read_events(identity, operation)
    import base64
    result = {'schema': SCHEMA, 'identity': identity, 'operation_id': operation, 'intent': intent,
              'unit': properties(names(operation)['unit']), 'events_base64': base64.b64encode(raw).decode(),
              'events_sha256': hashlib.sha256(raw).hexdigest(), 'reboot_proof': None}
    if require_reboot:
        if intent['action'] != 'reboot' or not events or events[-1].get('event') != 'reboot_ready':
            raise fault.Unavailable('reboot-ready-not-observed')
        native = fault.Native(intent)
        observation = native.observe()
        event = events[-1]
        if (observation['worker'] != event.get('worker') or observation['checkpoint_sha256'] != event.get('checkpoint_sha256')
                or not native.frozen()):
            raise fault.Unavailable('reboot-ready-proof-changed')
        result['reboot_proof'] = observation
    return result


def main(argv=None):
    import argparse
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('lab-nonce', 'vm-uuid', 'cell-id', 'node', 'operation-id'):
        parser.add_argument('--' + name, required=True)
    parser.add_argument('--mode', choices=('collect', 'reboot-proof'), required=True)
    args = parser.parse_args(argv)
    identity = fault.probe.guard_guest(args)
    print(json.dumps(collect(identity, args.operation_id, args.mode == 'reboot-proof'), sort_keys=True))


if __name__ == '__main__':
    try: main()
    except Exception as exc:
        import sys
        print('recovery handoff read refused: ' + type(exc).__name__, file=sys.stderr)
        raise SystemExit(2)
