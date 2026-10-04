#!/usr/bin/env python3
"""One native kit-promotion cut in registered disposable VMs only.

The predecessor is genuine pinned enrollment on signed Alpha75, not a prior
full update. Explicit owner-resume is separate from automatic recovery evidence.
No mode retries an update, manufactures a selector or accepts a production host.
"""
from __future__ import annotations
import argparse
import base64
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import shlex
import subprocess
import sys
import time

HERE = Path(__file__).resolve().parent

def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    value = importlib.util.module_from_spec(spec); sys.modules[name] = value; spec.loader.exec_module(value)
    return value

local = module('promotion_local_controller', 'local_candidate_trial.py')
guest = module('promotion_fault_guest', 'guest_runtime_promotion_fault.py')
trial = local.trial; lab = local.lab
INTENT = 'runtime-promotion-fault-intent.json'
ARM = 'runtime-promotion-arm-attempt.json'
DISPATCH = 'runtime-promotion-dispatch-attempt.json'
RESUME = 'runtime-promotion-owner-resume-attempt.json'
ASSETS = ('guest_runtime_predecessor.py', 'guest_runtime_promotion_fault.py')


def encoded(value): return (json.dumps(value, sort_keys=True) + '\n').encode()

def path(root, node, name): return root / 'evidence' / node / name


def helper_argv(intent, mode):
    identity = intent['identity']
    return ['/usr/bin/python3', '-I', str(guest.old.PRIVATE / 'guest_runtime_promotion_fault.py'), '--mode', mode,
            '--lab-nonce', identity['nonce'], '--vm-uuid', identity['vm_uuid'], '--cell-id', identity['cell_id'],
            '--node', identity['node'], '--operation-id', intent['operation_id']]


def prepare(root, record, plan, node, archive, digest, predecessor_result):
    local.assert_absent(root, node, INTENT, ARM, DISPATCH, RESUME)
    expected_parent = path(root, node, '').resolve()
    if (predecessor_result.parent.resolve() != expected_parent
            or not re.fullmatch(r'runtime-predecessor-collection-[0-9]+\.result\.json', predecessor_result.name)):
        raise ValueError('provide one exact collected predecessor result from this registered node')
    predecessor_raw = trial.read_private(path(root, node, 'runtime-predecessor-intent.json'), 4 << 20)
    result_raw = trial.read_private(predecessor_result, 4 << 20)
    predecessor = json.loads(predecessor_raw); result = json.loads(result_raw)
    guest.validate_predecessor(predecessor, result, trial.identity(record, plan, node))
    candidate = guest.local.archive_tools.inspect_archive(archive, digest)
    previous = predecessor['candidate']['files']['recovery-runtime/runtime.manifest']
    if candidate['files'].get('recovery-runtime/runtime.manifest') == previous:
        raise ValueError('promotion requires a different committed candidate kit')
    intent = local.prepare(root, record, plan, node, archive, digest, 'candidate-installed')
    spec = {'schema': guest.SCHEMA, 'identity': intent['identity'], 'operation_id': intent['operation_id'],
            'checkpoint': 'new-launcher-old-selection', 'action': 'kill', 'candidate': intent['candidate'],
            'created_at': trial.now(), 'predecessor_intent': predecessor, 'predecessor_result': result,
            'predecessor_intent_sha256': hashlib.sha256(predecessor_raw).hexdigest(),
            'predecessor_result_evidence': {'path': str(predecessor_result), 'sha256': hashlib.sha256(result_raw).hexdigest()},
            'limits': ['predecessor-enrollment-not-prior-full-update', 'owner-resume-not-automatic-foundation-proof']}
    guest.validate_spec(spec, intent)
    saved = trial.save(root, node, INTENT, encoded(spec))
    # Reserve ordinary controller admission too; only this separate watcher is valid.
    trial.save(root, node, local.ARM, encoded({'owner': 'runtime-promotion-trial', 'identity': intent['identity'],
                                            'operation_id': intent['operation_id']}))
    assets = {}
    for name in ASSETS:
        uploaded, sha = lab.put_file(root, record, plan, node, HERE / name, name)
        assets[name] = {'path': uploaded, 'sha256': sha}
    uploaded, sha = lab.put_file(root, record, plan, node, Path(saved['path']), guest.names(intent['operation_id'])['plan'].name)
    if uploaded != str(guest.names(intent['operation_id'])['plan']) or sha != saved['sha256']:
        raise ValueError('uploaded promotion intent differs')
    trial.save(root, node, 'runtime-promotion-assets.json', encoded(assets))
    print(json.dumps({'action': 'promotion-prepared-not-started', 'node': node, 'operation_id': intent['operation_id'],
                      'previous': previous, 'target': candidate['files']['recovery-runtime/runtime.manifest']}), flush=True)
    return intent, spec


def load(root, record, plan, node):
    intent = local.load_intent(root, record, plan, node)
    spec = json.loads(trial.read_private(path(root, node, INTENT), 4 << 20)); guest.validate_spec(spec, intent)
    reservation = json.loads(trial.read_private(path(root, node, local.ARM)))
    if reservation != {'owner': 'runtime-promotion-trial', 'identity': intent['identity'], 'operation_id': intent['operation_id']}:
        raise ValueError('ordinary updater fault reservation differs')
    return intent, spec


def read_guest(root, record, plan, node, intent):
    result = lab.guarded_script(root, record, plan, node, shlex.join(helper_argv(intent, 'collect')), timeout=30)
    if len(result.stdout) > 6 << 20: raise ValueError('promotion collection too large')
    state = json.loads(result.stdout)
    if (state.get('schema') != 'celikpanel/runtime-promotion-collection/v1' or state.get('identity') != intent['identity']
            or state.get('operation_id') != intent['operation_id']): raise ValueError('promotion collection identity differs')
    raw = base64.b64decode(state['events_base64'], validate=True)
    if len(raw) > 1048576: raise ValueError('promotion events too large')
    values = [guest.probe.strict_object(line) for line in raw.splitlines()]
    for item in values:
        if item.get('schema') != guest.EVENT_SCHEMA or item.get('identity') != intent['identity'] or item.get('operation_id') != intent['operation_id']:
            raise ValueError('promotion event identity differs')
    if state['events'] != [item['event'] for item in values]: raise ValueError('promotion event list differs')
    return state, raw, values


def launch_argv(intent):
    return ['systemd-run', '--unit=' + guest.names(intent['operation_id'])['fault'], '--no-block',
            '--property=Type=exec', '--property=RuntimeMaxSec=650', '--property=TimeoutStopSec=10', '--property=UMask=0077',
            '--property=ExecStopPost=-' + shlex.join(helper_argv(intent, 'cleanup')), *helper_argv(intent, 'fault')]


def submit(root, record, plan, node, intent, label, argv):
    # Caller saves an exclusive durable attempt before invoking this function.
    try:
        done = lab.guarded_script(root, record, plan, node, shlex.join(argv), timeout=100)
        out, err, code, unknown = done.stdout, done.stderr, done.returncode, False
    except subprocess.CalledProcessError as exc: out, err, code, unknown = exc.stdout or '', exc.stderr or '', exc.returncode, False
    except subprocess.TimeoutExpired as exc: out, err, code, unknown = exc.stdout or '', exc.stderr or '', None, True
    refs = {}; truncated = False
    for name, value in (('stdout', out), ('stderr', err)):
        raw = value.encode() if isinstance(value, str) else value
        truncated |= len(raw) > 4 << 20
        refs[name] = trial.save(root, node, label + '.' + name + '.txt', raw[:4 << 20])
    result = {'identity': intent['identity'], 'operation_id': intent['operation_id'], 'argv': argv, 'exit_code': code,
              'transport_unknown': unknown, 'truncated': truncated, 'evidence': refs}
    trial.save(root, node, label + '.result.json', encoded(result))
    if code != 0 or unknown or truncated: raise ValueError('native submission unconfirmed; collect only, never retry')
    return result


def require_armed(state, values, intent):
    if ([item['event'] for item in values] != ['armed'] or state.get('fault', {}).get('ActiveState') != 'active'
            or state.get('worker', {}).get('LoadState') != 'not-found'):
        raise ValueError('exact promotion watcher is not armed for a fresh worker')


def arm(root, record, plan, node, intent, spec):
    local.assert_absent(root, node, ARM, local.START, 'update-start-attempt.json')
    state, raw, _ = read_guest(root, record, plan, node, intent)
    if raw or state['worker'].get('LoadState') != 'not-found' or state['fault'].get('LoadState') != 'not-found':
        raise ValueError('prior promotion fault or native worker exists')
    trial.save(root, node, ARM, encoded({'identity': intent['identity'], 'operation_id': intent['operation_id'],
                                       'at': trial.now(), 'argv': launch_argv(intent), 'intent_sha256': hashlib.sha256(encoded(spec)).hexdigest()}))
    submit(root, record, plan, node, intent, 'runtime-promotion-arm', launch_argv(intent))
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        state, raw, values = read_guest(root, record, plan, node, intent)
        if values:
            require_armed(state, values, intent)
            trial.save(root, node, 'runtime-promotion-armed.jsonl', raw)
            print(json.dumps({'action': 'promotion-fault-armed', 'node': node, 'operation_id': intent['operation_id']}), flush=True)
            return
        time.sleep(.2)
    raise TimeoutError('promotion armed event unconfirmed; collect without rearming')


def start(root, record, plan, node, intent, spec):
    local.assert_absent(root, node, local.START, 'update-start-attempt.json')
    attempt = json.loads(trial.read_private(path(root, node, ARM)))
    if (attempt.get('identity') != intent['identity'] or attempt.get('operation_id') != intent['operation_id']
            or attempt.get('intent_sha256') != hashlib.sha256(encoded(spec)).hexdigest()): raise ValueError('sealed promotion arm differs')
    state, _, values = read_guest(root, record, plan, node, intent); require_armed(state, values, intent)
    trial.save(root, node, local.START, encoded({'identity': intent['identity'], 'operation_id': intent['operation_id'],
                                              'owner': 'runtime-promotion-trial', 'at': trial.now(), 'argv': local.launch_argv(intent, 'start')}))
    local.launch_once(root, record, plan, node, intent, 'start')
    print(json.dumps({'action': 'promotion-update-submitted-once', 'node': node, 'operation_id': intent['operation_id'], 'outcome': 'unconfirmed'}), flush=True)


def command_once(root, record, plan, node, intent, spec, mode):
    name = DISPATCH if mode == 'dispatch-read' else RESUME
    local.assert_absent(root, node, name)
    trial.read_private(path(root, node, local.START))
    argv = helper_argv(intent, mode)
    trial.save(root, node, name, encoded({'identity': intent['identity'], 'operation_id': intent['operation_id'],
                                        'at': trial.now(), 'argv': argv, 'owner_action': mode == 'owner-resume'}))
    return submit(root, record, plan, node, intent, 'runtime-promotion-' + mode, argv)


def collect(root, record, plan, node, intent, spec):
    state, raw, values = read_guest(root, record, plan, node, intent)
    label = 'runtime-promotion-collection-' + str(time.time_ns())
    refs = {'events': trial.save(root, node, label + '.jsonl', raw), 'state': trial.save(root, node, label + '.state.json', encoded(state))}
    for key, item in state.get('artifacts', {}).items():
        if key not in ('dispatch_attempt', 'dispatch', 'resume_attempt', 'resume'): raise ValueError('unknown promotion artifact')
        data = base64.b64decode(item['base64'], validate=True)
        if len(data) > 4 << 20 or hashlib.sha256(data).hexdigest() != item['proof']['sha256'] or len(data) != item['proof']['bytes']:
            raise ValueError('promotion artifact bytes differ')
        refs[key] = trial.save(root, node, label + '.' + key + '.json', data)
    argv = ['journalctl', '--no-pager', '--output=json', '-u', guest.local.names(intent['operation_id'])['worker'],
            '-u', guest.names(intent['operation_id'])['fault'], '-u', 'celikpanel-release-recovery.service',
            '--since', intent['created_at'], '-n', '250']
    done = lab.guarded_script(root, record, plan, node, shlex.join(argv), timeout=30)
    journal = done.stdout.encode(); refs['journal'] = trial.save(root, node, label + '.journal.jsonl', journal[:524288])
    trial.exercise.capture(root, record, plan, node, label, intent['created_at'])
    summary = {'action': 'promotion-evidence-collected', 'identity': intent['identity'], 'operation_id': intent['operation_id'],
               'events': [item['event'] for item in values], 'worker': state['worker'], 'fault': state['fault'],
               'journal_truncated': len(journal) > 524288, 'evidence': refs,
               'automatic_resume': 'not-established-by-predecessor-enrollment-fixture', 'native_outcome': 'not-inferred'}
    trial.save(root, node, label + '.summary.json', encoded(summary)); print(json.dumps(summary, sort_keys=True), flush=True)
    return summary


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work-root', required=True); parser.add_argument('--node', required=True, choices=('arch', 'debian13'))
    parser.add_argument('--mode', required=True, choices=('prepare', 'arm', 'start', 'collect', 'dispatch-read', 'owner-resume'))
    parser.add_argument('--execute', action='store_true'); parser.add_argument('--archive', type=Path)
    parser.add_argument('--archive-sha256'); parser.add_argument('--predecessor-result', type=Path)
    args = parser.parse_args(argv)
    if args.mode not in ('collect', 'dispatch-read') and not args.execute:
        parser.error('mutation requires --execute and an exact registered disposable VM')
    if args.mode == 'prepare' and (args.archive is None or args.archive_sha256 is None or args.predecessor_result is None):
        parser.error('prepare requires exact candidate archive/SHA and one collected predecessor result')
    if args.mode != 'prepare' and any(value is not None for value in (args.archive, args.archive_sha256, args.predecessor_result)):
        parser.error('artifact and predecessor inputs must be sealed at preparation only')
    root = lab.checked_root(args.work_root); record, plan = lab.load(root); lab.process_guard(plan['nodes'][args.node])
    if args.mode == 'prepare': return prepare(root, record, plan, args.node, args.archive, args.archive_sha256, args.predecessor_result)
    intent, spec = load(root, record, plan, args.node)
    if args.mode in ('dispatch-read', 'owner-resume'): return command_once(root, record, plan, args.node, intent, spec, args.mode)
    return {'arm': arm, 'start': start, 'collect': collect}[args.mode](root, record, plan, args.node, intent, spec)

if __name__ == '__main__':
    try: main()
    except (ValueError, OSError, TimeoutError, subprocess.SubprocessError, guest.probe.ProbeError) as exc:
        print('promotion controller refused: ' + type(exc).__name__ + ': ' + str(exc), file=sys.stderr); sys.exit(2)
