#!/usr/bin/env python3
"""Prepare and enroll the pinned predecessor in registered disposable VM fixtures.

No production target, arbitrary predecessor, retry, update or recovery action is
available. The real predecessor enrollment CLI is the only product mutation.
"""
from __future__ import annotations
import argparse
import base64
import hashlib
import importlib.util
import json
from pathlib import Path
import secrets
import shlex
import subprocess
import sys
import time

HERE = Path(__file__).resolve().parent

def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    value = importlib.util.module_from_spec(spec)
    sys.modules[name] = value
    spec.loader.exec_module(value)
    return value

trial = module('predecessor_trial_shared', 'update_trial.py')
guest = module('predecessor_guest', 'guest_runtime_predecessor.py')
lab = trial.lab
INTENT = 'runtime-predecessor-intent.json'
STAGE = 'runtime-predecessor-stage.json'
START = 'runtime-predecessor-start-attempt.json'
ASSETS = ('guest_probe.py', 'candidate_archive.py', 'guest_runtime_predecessor.py')


def encoded(value):
    return (json.dumps(value, sort_keys=True) + '\n').encode()


def evidence_path(root, node, name):
    return root / 'evidence' / node / name


def absent(root, node, *names):
    for name in names:
        path = evidence_path(root, node, name)
        if path.exists() or path.is_symlink():
            raise ValueError('saved predecessor/update operation exists; collect, do not repeat')


def validate_baseline(value):
    trial.validate_baseline(value)
    if value.get('installed_artifacts') != guest.BASELINE or value.get('running_artifacts') != guest.BASELINE:
        raise ValueError('baseline is not the exact signed Alpha75 producer')


def helper_argv(intent, mode):
    ident = intent['identity']
    return ['/usr/bin/python3', '-I', str(guest.PRIVATE / 'guest_runtime_predecessor.py'), '--mode', mode,
            '--lab-nonce', ident['nonce'], '--vm-uuid', ident['vm_uuid'], '--cell-id', ident['cell_id'],
            '--node', ident['node'], '--operation-id', intent['operation_id']]


def load_intent(root, record, plan, node):
    value = json.loads(trial.read_private(evidence_path(root, node, INTENT), 4 * 1024 * 1024))
    guest.validate_plan(value, trial.identity(record, plan, node), value.get('operation_id'))
    return value


def prepare(root, record, plan, node, archive):
    absent(root, node, INTENT, STAGE, START, 'local-candidate-intent.json', 'local-candidate-start-attempt.json',
           'update-intent.json', 'update-start-attempt.json')
    candidate = guest.archive_tools.inspect_archive(archive, guest.ARCHIVE_SHA)
    guest.validate_candidate(candidate)
    source = guest.archive_tools.verify_committed_source(candidate, trial.REPOSITORY)
    baseline_raw = trial.read_private(root / ('baseline-' + node + '-baseline-install-result.json'))
    validate_baseline(json.loads(baseline_raw))
    seed_raw = trial.read_private(evidence_path(root, node, 'seed.stdout.jsonl'))
    trial.validate_seed(seed_raw, record, node)
    operation = secrets.token_hex(16)
    paths = guest.names(operation)
    intent = {'schema': guest.SCHEMA, 'identity': trial.identity(record, plan, node), 'operation_id': operation,
              'provenance': guest.PROVENANCE, 'created_at': trial.now(), 'candidate': candidate,
              'baseline_artifacts': guest.BASELINE, 'baseline_evidence_sha256': hashlib.sha256(baseline_raw).hexdigest(),
              'seed_evidence_sha256': hashlib.sha256(seed_raw).hexdigest(), 'committed_source_proof': source,
              'archive_path': str(paths['archive']), 'source_root': str(paths['stage'] / candidate['root_name'])}
    guest.validate_plan(intent, intent['identity'], operation)
    saved = trial.save(root, node, INTENT, encoded(intent))
    assets = {}
    for name in ASSETS:
        path, sha = lab.put_file(root, record, plan, node, HERE / name, name)
        assets[name] = {'path': path, 'sha256': sha}
    path, sha = lab.put_file(root, record, plan, node, archive, paths['archive'].name)
    if path != str(paths['archive']) or sha != guest.ARCHIVE_SHA:
        raise ValueError('uploaded predecessor archive differs')
    lab.put_file(root, record, plan, node, Path(saved['path']), paths['plan'].name)
    trial.save(root, node, 'runtime-predecessor-assets.json', encoded(assets))
    result = lab.guarded_script(root, record, plan, node, shlex.join(helper_argv(intent, 'stage')), timeout=180)
    value = json.loads(result.stdout)
    if (value.get('schema') != 'celikpanel/runtime-predecessor-stage/v1' or value.get('identity') != intent['identity']
            or value.get('operation_id') != operation or value.get('files') != candidate['files']
            or value.get('root') != intent['source_root']):
        raise ValueError('native predecessor stage proof differs')
    trial.save(root, node, STAGE, encoded(value))
    print(json.dumps({'action': 'predecessor-prepared-not-enrolled', 'node': node, 'operation_id': operation,
                      'archive_sha256': guest.ARCHIVE_SHA, 'commit': guest.COMMIT, 'provenance': guest.PROVENANCE}), flush=True)
    return intent


def launch_argv(intent):
    return ['systemd-run', '--unit=' + guest.names(intent['operation_id'])['unit'], '--no-block',
            '--property=Type=exec', '--property=RuntimeMaxSec=300', '--property=UMask=0077',
            *helper_argv(intent, 'enroll')]


def enroll(root, record, plan, node, intent):
    absent(root, node, START, 'local-candidate-intent.json', 'local-candidate-start-attempt.json',
           'update-intent.json', 'update-start-attempt.json')
    stage = json.loads(trial.read_private(evidence_path(root, node, STAGE), 4 * 1024 * 1024))
    if (stage.get('identity') != intent['identity'] or stage.get('operation_id') != intent['operation_id']
            or stage.get('files') != intent['candidate']['files']):
        raise ValueError('saved predecessor stage differs')
    argv = launch_argv(intent)
    trial.save(root, node, START, encoded({'identity': intent['identity'], 'operation_id': intent['operation_id'],
                                         'started_at': trial.now(), 'argv': argv}))
    try:
        result = lab.guarded_script(root, record, plan, node, shlex.join(argv), timeout=30)
        stdout, stderr, code, error = result.stdout, result.stderr, result.returncode, None
    except subprocess.CalledProcessError as exc:
        stdout, stderr, code, error = exc.stdout or '', exc.stderr or '', exc.returncode, None
    except subprocess.TimeoutExpired as exc:
        stdout, stderr, code, error = exc.stdout or '', exc.stderr or '', None, 'admission-unknown'
    refs = {}
    for name, raw in (('stdout', stdout), ('stderr', stderr)):
        raw = raw.encode() if isinstance(raw, str) else raw
        refs[name] = trial.save(root, node, 'runtime-predecessor-start.' + name + '.txt', raw[:131072])
    trial.save(root, node, 'runtime-predecessor-start.result.json', encoded({'exit_code': code, 'error': error, 'evidence': refs}))
    if code != 0 or error:
        raise ValueError('predecessor start not confirmed; collect the same operation without retry')
    print(json.dumps({'action': 'predecessor-enrollment-submitted-once', 'node': node,
                      'operation_id': intent['operation_id'], 'result': 'unconfirmed'}), flush=True)


def collect(root, record, plan, node, intent):
    result = lab.guarded_script(root, record, plan, node, shlex.join(helper_argv(intent, 'collect')), timeout=30)
    if len(result.stdout) > 8 * 1024 * 1024:
        raise ValueError('predecessor collection exceeds bound')
    value = json.loads(result.stdout)
    if (value.get('schema') != 'celikpanel/runtime-predecessor-collection/v1'
            or value.get('identity') != intent['identity'] or value.get('operation_id') != intent['operation_id']):
        raise ValueError('predecessor collection identity differs')
    label = 'runtime-predecessor-collection-' + str(time.time_ns())
    refs = {}
    summary = {'action': 'predecessor-collected', 'node': node, 'operation_id': intent['operation_id'],
               'provenance': guest.PROVENANCE, 'status': 'unavailable'}
    for name, item in value.get('artifacts', {}).items():
        if name not in ('proof', 'attempt', 'result', 'log'):
            raise ValueError('unexpected predecessor artifact')
        raw = base64.b64decode(item['base64'], validate=True)
        if len(raw) > 4 * 1024 * 1024 or hashlib.sha256(raw).hexdigest() != item['proof']['sha256'] or len(raw) != item['proof']['bytes']:
            raise ValueError('predecessor evidence digest differs')
        refs[name] = trial.save(root, node, label + '.' + name + ('.txt' if name == 'log' else '.json'), raw)
        if name == 'result':
            terminal = json.loads(raw)
            if terminal.get('identity') != intent['identity'] or terminal.get('operation_id') != intent['operation_id']:
                raise ValueError('predecessor result identity differs')
            summary['status'] = terminal.get('status', 'unavailable')
            summary['error_type'] = terminal.get('error_type')
            summary['runtime_manifest_sha256'] = terminal.get('runtime', {}).get('manifest_sha256')
    summary['evidence'] = refs
    trial.save(root, node, label + '.json', encoded(summary))
    print(json.dumps(summary, sort_keys=True), flush=True)
    return summary


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work-root', required=True)
    parser.add_argument('--node', choices=('arch', 'debian13'), required=True)
    parser.add_argument('--mode', choices=('prepare', 'enroll', 'collect'), required=True)
    parser.add_argument('--archive', type=Path)
    parser.add_argument('--execute', action='store_true')
    args = parser.parse_args(argv)
    if args.mode != 'collect' and not args.execute:
        parser.error('mutation requires --execute and a registered disposable VM')
    if args.mode == 'prepare' and args.archive is None:
        parser.error('prepare requires the exact pinned old J archive path')
    if args.mode != 'prepare' and args.archive is not None:
        parser.error('archive is sealed only during prepare')
    root = lab.checked_root(args.work_root)
    record, plan = lab.load(root)
    lab.process_guard(plan['nodes'][args.node])
    if args.mode == 'prepare':
        return prepare(root, record, plan, args.node, args.archive)
    intent = load_intent(root, record, plan, args.node)
    return {'enroll': enroll, 'collect': collect}[args.mode](root, record, plan, args.node, intent)


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.SubprocessError, guest.probe.ProbeError) as exc:
        print('predecessor controller refused: ' + type(exc).__name__, file=sys.stderr)
        sys.exit(2)
