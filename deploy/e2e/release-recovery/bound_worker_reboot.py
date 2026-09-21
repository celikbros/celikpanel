#!/usr/bin/env python3
"""One guarded QMP reset after a real, bound update worker's recovery checkpoint.

This is an opt-in disposable test fault. No local-bootstrap start is inferred,
no product evidence is written, and no guest update or recovery is started here.
"""
from __future__ import annotations
import argparse
import hashlib
import importlib.util
import json
import re
from pathlib import Path
import subprocess
import sys
import time

HERE = Path(__file__).resolve().parent

def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    value = importlib.util.module_from_spec(spec); sys.modules[spec.name] = value; spec.loader.exec_module(value)
    return value

native = module('bound_host_recovery_fault', 'recovery_fault_trial.py')
kill = module('bound_host_kill_events', 'arm_update_kill.py')
worker = module('bound_host_worker', 'guest_bound_worker.py')
lab, trial = native.lab, native.trial
START_SCHEMA = 'celikpanel/bound-worker-start/v1'
ATTEMPT_SCHEMA = 'celikpanel/bound-worker-reboot-attempt/v1'


def encoded(value): return (json.dumps(value, sort_keys=True) + '\n').encode()
def sha(raw): return hashlib.sha256(raw).hexdigest()
def names(operation):
    if not trial.HEX32.fullmatch(operation): raise ValueError('invalid exact worker operation')
    return {'intent': 'bound-worker-' + operation + '.json',
            'start': 'bound-worker-start-' + operation + '.json',
            'stdout': 'bound-worker-start-' + operation + '.stdout.jsonl',
            'attempt': 'bound-worker-reboot-attempt-' + operation + '.json',
            'result': 'bound-worker-reboot-result-' + operation + '.json'}


def validate_start(intent, intent_raw, receipt, raw):
    required = {'schema', 'identity', 'operation_id', 'intent_sha256', 'mode', 'exit_code', 'stdout_sha256'}
    if (set(receipt) != required or receipt['schema'] != START_SCHEMA or receipt['identity'] != intent['identity']
            or receipt['operation_id'] != intent['operation_id'] or receipt['intent_sha256'] != sha(intent_raw)
            or receipt['mode'] != 'start' or type(receipt['exit_code']) is not int or receipt['exit_code'] != 0
            or receipt['stdout_sha256'] != sha(raw) or len(raw) > 131072 or not raw.endswith(b'\n')):
        raise ValueError('genuine driver start receipt differs')
    events = [worker.probe.strict_object(line) for line in raw.splitlines()]
    if [event.get('event') for event in events] != ['reviewed', 'accepted']:
        raise ValueError('real driver did not confirm one reviewed accepted start')
    target, baseline = intent['target'], intent['baseline']
    for event in events:
        if (event.get('schema') != 'celikpanel-release-recovery-update/v1'
                or event.get('cell_id') != intent['identity']['cell_id'] or event.get('node') != intent['identity']['node']
                or event.get('request_id') != intent['operation_id'] or event.get('target_version') != target['version']
                or event.get('target_commit') != target['commit'] or not trial.HEX64.fullmatch(event.get('target_archive_sha256', ''))):
            raise ValueError('driver event operation identity differs')
    request = events[0].get('request', {})
    if (request.get('request_id') != intent['operation_id'] or request.get('target_version') != target['version']
            or request.get('target_commit') != target['commit'] or request.get('target_sequence') != '82'
            or request.get('target_os') != 'linux' or request.get('target_arch') != 'amd64'
            or request.get('target_archive_sha256') != events[0]['target_archive_sha256']
            or not re.fullmatch(r'[1-9][0-9]{0,9}', request.get('target_archive_size', ''))
            or int(request['target_archive_size']) > 2147483648
            or request.get('expected_current_version') != baseline['version']
            or request.get('expected_current_commit') != baseline['commit']
            or events[1]['target_archive_sha256'] != events[0]['target_archive_sha256']
            or events[1].get('status') not in ('queued', 'running')
            or not trial.HEX64.fullmatch(events[0].get('manifest_sha256', ''))):
        raise ValueError('accepted update is not the reviewed current-producer transition')
    return {'receipt_sha256': sha(encoded(receipt)), 'stdout_sha256': sha(raw),
            'manifest_sha256': events[0]['manifest_sha256'], 'archive_sha256': events[0]['target_archive_sha256'],
            'archive_size': request['target_archive_size']}


def load(root, record, plan, node, operation):
    selected = names(operation)
    evidence = root / 'evidence' / node
    intent_raw = trial.read_private(evidence / selected['intent'])
    intent = worker.probe.strict_object(intent_raw)
    expected = trial.identity(record, plan, node)
    worker.validate_intent(intent, expected, operation)
    if intent['recovery_fault'] != {'action': 'reboot', 'checkpoint': 'payload_restored'}:
        raise ValueError('sealed worker intent does not authorize recovery reboot fault')
    receipt = worker.probe.strict_object(trial.read_private(evidence / selected['start']))
    start_raw = trial.read_private(evidence / selected['stdout'])
    start = validate_start(intent, intent_raw, receipt, start_raw)
    # Target archive/manifest provenance also belongs to this VM's sealed local
    # origin. The actual Agent still owns signed admission and durable operation state.
    origin = worker.probe.strict_object(trial.read_private(root / 'worker-origin' / 'worker-origin-intent.json'))
    validate_origin(intent, start, origin, expected)
    return intent, start


def validate_origin(intent, start, origin, expected):
    if (origin.get('schema') != 'celikpanel/worker-fixture-origin/v1'
            or origin.get('identity') != {'schema': lab.SCHEMA, **expected}
            or origin.get('target', {}).get('version') != intent['target']['version']
            or origin.get('target', {}).get('commit') != intent['target']['commit']
            or origin.get('target', {}).get('archive_sha256') != start['archive_sha256']
            or origin.get('target', {}).get('archive_size') != start['archive_size']
            or origin.get('files', {}).get('worker-origin-manifest', {}).get('sha256') != start['manifest_sha256']):
        raise ValueError('real start differs from the sealed fixture origin')


def bind_cut(intent, events, recovery):
    expected = ['armed', 'worker_frozen', 'candidate_installed_checkpoint', 'recovery_fault_armed', 'kill_requested', 'kill_sent', 'released']
    if [event.get('event') for event in events] != expected:
        raise ValueError('real worker cut is not conclusively finished')
    if any(e.get('identity') != intent['identity'] or e.get('operation_id') != intent['operation_id'] for e in events):
        raise ValueError('worker cut event identity differs')
    released = events[-1]
    if released.get('kill_sent') is not True or released.get('checkpoint_verified') is not True or released.get('reason') != 'exact-update-unit-killed':
        raise ValueError('real worker cut outcome is unconfirmed')
    checkpoint, handoff = events[2], events[3].get('handoff', {})
    worker_identity = events[1].get('worker')
    bound = checkpoint.get('bound_worker', {})
    exact = recovery['intent']
    native.hand.fault.validate_intent(exact, intent['identity'], intent['operation_id'])
    if (checkpoint.get('phase') != 'active' or checkpoint.get('worker') != worker_identity
            or events[4].get('worker') != worker_identity or events[5].get('worker') != worker_identity
            or not isinstance(worker_identity, dict)
            or worker_identity.get('running_executable_sha256') != intent['baseline']['agent_sha256']
            or checkpoint.get('installed_artifacts') != {name: intent['target'][name + '_sha256'] for name in ('agent', 'panel')}
            or type(checkpoint.get('verified_files')) is not int or checkpoint['verified_files'] <= 0
            or bound.get('request_id') != intent['operation_id'] or bound.get('target_commit') != intent['target']['commit']
            or bound.get('phase') != 'running' or bound.get('terminal_proof') != 'none'
            or bound.get('snapshot') != checkpoint.get('snapshot') or bound.get('snapshot') != exact['snapshot']
            or bound.get('transaction_token_sha256') != exact['transaction_token_sha256']
            or checkpoint.get('manifest_sha256') != exact['snapshot_manifest_sha256']
            or handoff.get('schema') != native.hand.SCHEMA or handoff.get('identity') != intent['identity']
            or handoff.get('operation_id') != intent['operation_id'] or handoff.get('updater') != worker_identity
            or handoff.get('intent') != exact or handoff.get('intent_sha256') != sha(native.hand.encoded(exact))
            or {key: exact[key] for key in ('action', 'checkpoint')} != intent['recovery_fault']):
        raise ValueError('recovery checkpoint is not bound to the actual worker cut')
    for key in ('binding_sha256', 'observation_sha256', 'worker_state_sha256', 'transaction_token_sha256'):
        if not trial.HEX64.fullmatch(bound.get(key, '')): raise ValueError('bound worker provenance is incomplete')
    return {'snapshot': exact['snapshot'], 'transaction_token_sha256': exact['transaction_token_sha256'],
            'binding_sha256': bound['binding_sha256'], 'handoff_intent_sha256': handoff['intent_sha256']}


def reboot(root, record, plan, node, operation, execute=False):
    if not execute: raise ValueError('disposable QMP reset requires execute=True')
    checked = lab.checked_root(root)
    if checked != root or lab.load(root) != (record, plan): raise ValueError('registered lab identity changed')
    lab.process_guard(plan['nodes'][node])
    selected = names(operation)
    intent, start = load(root, record, plan, node, operation)
    evidence = root / 'evidence' / node
    for name in ('attempt', 'result'):
        path = evidence / selected[name]
        if path.exists() or path.is_symlink(): raise ValueError('reset was already attempted; never retry')
    deadline = time.monotonic() + 600
    while time.monotonic() < deadline:
        try: recovery, raw, events = native.read_guest(root, record, plan, node, intent)
        except subprocess.CalledProcessError:
            time.sleep(.1); continue  # the subordinate intent does not predate the verified updater cut
        if events and events[-1].get('event') == 'released': raise ValueError('recovery reset checkpoint missed')
        if events and events[-1].get('event') == 'reboot_ready': break
        time.sleep(.1)
    else: raise TimeoutError('bound recovery reset checkpoint unavailable')
    state, cut_raw = kill.shared.read_guest(root, record, plan, node, operation, 'update-kill')
    cut_events = kill.validate_events(cut_raw, intent['identity'], operation)
    cut = bind_cut(intent, cut_events, recovery)
    expected = native.qemu_identity(plan['nodes'][node])
    connection = native.QMP(plan['nodes'][node], expected)
    try:
        recovery, raw, events = native.read_guest(root, record, plan, node, intent, reboot_proof=True)
        proof_at = time.monotonic()
        if bind_cut(intent, cut_events, recovery) != cut: raise ValueError('cut binding changed')
        proof = recovery.get('reboot_proof')
        if not proof or proof['worker']['boot_id'] != events[-1]['worker']['boot_id']:
            raise ValueError('exact frozen recovery reboot proof unavailable')
        refs = native.save_collection(root, node, recovery, raw)
        refs['worker_cut'] = trial.save(root, node, 'bound-worker-reboot-cut-' + operation + '.jsonl', cut_raw)
        attempt = {'schema': ATTEMPT_SCHEMA, 'identity': intent['identity'], 'operation_id': operation,
                   'created_at': trial.now(), 'qemu': expected, 'checkpoint_sha256': proof['checkpoint_sha256'],
                   'before_boot_id': proof['worker']['boot_id'], 'start': start, 'worker_cut': cut,
                   'evidence': refs, 'command': 'system_reset', 'scope': 'registered-disposable-QEMU-only'}
        trial.save(root, node, selected['attempt'], encoded(attempt))
        if time.monotonic() - proof_at > 3: raise ValueError('frozen checkpoint proof expired before reset')
        connection.reset()
        result = dict(attempt, action='registered-QEMU-reset-submitted-once', reboot_verified=False, recovery_success='unconfirmed')
        trial.save(root, node, selected['result'], encoded(result))
        print(json.dumps({'action': result['action'], 'node': node, 'operation_id': operation, 'recovery_success': 'unconfirmed'}), flush=True)
        return result
    finally: connection.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work-root', required=True); parser.add_argument('--node', choices=('debian13', 'arch'), required=True)
    parser.add_argument('--operation-id', required=True); parser.add_argument('--execute', action='store_true')
    args = parser.parse_args()
    root = lab.checked_root(args.work_root); record, plan = lab.load(root)
    reboot(root, record, plan, args.node, args.operation_id, args.execute)


if __name__ == '__main__':
    try: main()
    except (ValueError, OSError, TimeoutError, subprocess.SubprocessError) as exc:
        print('bound-worker reset refused: ' + type(exc).__name__, file=sys.stderr)
        raise SystemExit(2)
