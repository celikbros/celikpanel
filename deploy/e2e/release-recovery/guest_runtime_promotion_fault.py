#!/usr/bin/env python3
"""Fault one exact disposable updater in the native launcher/selector swap window.

The fixture observes product evidence; it never creates promotion evidence, writes
selectors, changes kits, or retries an update. A missed window is inconclusive.
"""
from __future__ import annotations
import argparse
import base64
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
import time

HERE = Path(__file__).resolve().parent

def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    value = importlib.util.module_from_spec(spec); sys.modules[name] = value; spec.loader.exec_module(value)
    return value

local = module('promotion_local_guest', 'guest_local_candidate.py')
old = module('promotion_predecessor_guest', 'guest_runtime_predecessor.py')
probe = old.probe
SCHEMA = 'celikpanel/runtime-promotion-fault/v1'
EVENT_SCHEMA = 'celikpanel/runtime-promotion-fault-event/v1'
PROMOTIONS = Path('/var/lib/celikpanel-release-state/recovery-promotions/v1')
RECORD_KEYS = ('schema', 'nonce', 'previous', 'target', 'mode', 'old_launcher', 'new_launcher', 'old_selection', 'new_selection')
IDENTITY_KEYS = ('dev', 'ino', 'mode', 'uid', 'gid', 'size', 'mtime_sec', 'mtime_nsec', 'ctime_sec', 'ctime_nsec', 'sha256')
CAPABILITY = [str(old.LAUNCHER), 'verify-material-support', '--layout', 'snapshot-name-sha256-v1']

class Unavailable(ValueError):
    pass


def names(operation):
    local.names(operation)
    prefix = old.PRIVATE / ('runtime-promotion-' + operation)
    return {'plan': Path(str(prefix) + '.json'), 'events': Path(str(prefix) + '.jsonl'),
            'dispatch': Path(str(prefix) + '.dispatch.json'), 'dispatch_attempt': Path(str(prefix) + '.dispatch-attempt.json'),
            'resume': Path(str(prefix) + '.resume.json'), 'resume_attempt': Path(str(prefix) + '.resume-attempt.json'),
            'fault': 'celikpanel-lab-promotion-fault-' + operation + '.service'}


def selection(digest):
    return ('format=celikpanel-recovery-selection-v1\nruntime=' + digest + '\n').encode()


def parse_record(raw, previous, target):
    value = probe.strict_object(raw)
    if (tuple(value) != RECORD_KEYS or value['schema'] != 'celikpanel/recovery-promotion/v1'
            or not probe.HEX32.fullmatch(value.get('nonce', '')) or value.get('previous') != previous
            or value.get('target') != target or previous == target or value.get('mode') != '--normal'
            or not probe.HEX64.fullmatch(previous) or not probe.HEX64.fullmatch(target)):
        raise Unavailable('promotion-record-binding-differs')
    for key in RECORD_KEYS[5:]:
        item = value[key]; is_launcher = key.endswith('launcher')
        if not isinstance(item, dict) or tuple(item) != IDENTITY_KEYS:
            raise Unavailable('promotion-file-identity-fields-differ')
        if (any(type(item[k]) is not int for k in IDENTITY_KEYS[:-1]) or item['dev'] <= 0 or item['ino'] <= 0
                or item['mode'] != stat.S_IFREG | (0o755 if is_launcher else 0o600)
                or item['uid'] != 0 or item['gid'] != 0 or not 0 < item['size'] <= (128 << 20 if is_launcher else 160)
                or not 0 <= item['mtime_nsec'] < 1000000000 or not 0 <= item['ctime_nsec'] < 1000000000
                or not probe.HEX64.fullmatch(item.get('sha256', ''))):
            raise Unavailable('promotion-file-identity-invalid')
    for prefix, digest in (('old', previous), ('new', target)):
        item = value[prefix + '_selection']; raw_selection = selection(digest)
        if item['size'] != len(raw_selection) or item['sha256'] != hashlib.sha256(raw_selection).hexdigest():
            raise Unavailable('promotion-selection-bytes-differ')
    for kind in ('launcher', 'selection'):
        if all(value['old_' + kind][k] == value['new_' + kind][k] for k in ('dev', 'ino')):
            raise Unavailable('promotion-inodes-not-distinct')
    if raw != (json.dumps(value, separators=(',', ':'), ensure_ascii=False) + '\n').encode():
        raise Unavailable('promotion-record-not-canonical')
    return value


def read_record(previous, target):
    # Genuine absence of any ancestor is a not-yet-observed window, not authority.
    current = Path('/')
    for component in (*PROMOTIONS.parts[1:], 'current'):
        current /= component
        try: info = current.lstat()
        except FileNotFoundError: return None
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid or info.st_gid or info.st_mode & 0o7022
                or (current in (PROMOTIONS, PROMOTIONS / 'current') and stat.S_IMODE(info.st_mode) != 0o700)):
            raise Unavailable('promotion-ancestry-unsafe')
    try: raw, proof = old.read_file(current / 'intent.json', 16384, 0o600)
    except FileNotFoundError: return None
    value = parse_record(raw, previous, target)
    listing = list(current.iterdir())
    if len(listing) > 64: raise Unavailable('promotion-inventory-too-large')
    for path in listing:
        if path.name == 'intent.json': continue
        if path.name == 'committed.json': raise Unavailable('promotion-already-committed')
        if not re.fullmatch(r'\.committed-[0-9a-f]{32}', path.name): raise Unavailable('promotion-inventory-unknown')
        old.read_file(path, 16384, 0o600)
    return {'record': value, 'file': proof}


def metadata_matches(info, expected, moved=False):
    return (stat.S_ISREG(info.st_mode) and info.st_nlink == 1
            and (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid, info.st_size,
                 info.st_mtime_ns) == (expected['dev'], expected['ino'], expected['mode'], expected['uid'],
                 expected['gid'], expected['size'], expected['mtime_sec'] * 1000000000 + expected['mtime_nsec'])
            and (moved or info.st_ctime_ns == expected['ctime_sec'] * 1000000000 + expected['ctime_nsec']))


def pair_paths(record):
    return ((old.LAUNCHER, record['new_launcher'], True),
            (Path(str(old.LAUNCHER) + '.promotion-' + record['nonce']), record['old_launcher'], True),
            (old.SELECTION, record['old_selection'], False),
            (Path(str(old.SELECTION) + '.promotion-' + record['nonce']), record['new_selection'], False))


def mixed_pair(proof, full=False):
    result = {}
    for path, expected, moved in pair_paths(proof['record']):
        old.trusted_chain(path.parent)
        if not metadata_matches(path.lstat(), expected, moved): return None
        if full:
            _, actual = old.read_file(path, 128 << 20, stat.S_IMODE(expected['mode']))
            if actual['sha256'] != expected['sha256'] or not metadata_matches(path.lstat(), expected, moved):
                raise Unavailable('promotion-pair-changed')
            result[str(path)] = actual
    return result if full else True


def runtime_inventory(candidate):
    expected = {name: candidate['files']['recovery-runtime/' + name] for name in (*old.FILES, 'runtime.manifest')}
    digest = expected['runtime.manifest']
    root = old.RUNTIMES / digest
    if stat.S_IMODE(root.lstat().st_mode) != 0o700: raise Unavailable('runtime-root-not-private')
    proof = old.tree_proof(root, expected, private=True)
    raw, manifest = old.read_file(root / 'runtime.manifest', 4096, 0o600)
    canonical = b'format=celikpanel-recovery-runtime-v1\nprotocol=1\nsnapshot=6\n' + ''.join(expected[name] + '  ' + name + '\n' for name in sorted(old.FILES)).encode()
    if raw != canonical or manifest['sha256'] != digest: raise Unavailable('runtime-manifest-differs')
    return {'manifest_sha256': digest, 'inventory': proof, 'manifest': manifest}


def retained_source(candidate, tick=lambda: None):
    # The genuine bootstrap moves SOURCE_ROOT into this immutable namespace
    # before update.sh starts. The original staging pathname must not be reused.
    old.trusted_chain(local.RELEASES)
    roots = []
    for index, entry in enumerate(local.RELEASES.iterdir()):
        tick()
        if index >= 10000: raise Unavailable('retained-release-directory-bound')
        if re.fullmatch(re.escape(candidate['commit'][:12]) + r'-[0-9a-f]{24}', entry.name): roots.append(entry)
    if len(roots) != 1: raise Unavailable('exact-retained-candidate-root-unavailable')
    return local.verify_tree(roots[0], candidate, tick)

def validate_predecessor(intent, result, identity):
    old.validate_plan(intent, identity, intent.get('operation_id'))
    if (result.get('schema') != 'celikpanel/runtime-predecessor-result/v1' or result.get('identity') != identity
            or result.get('operation_id') != intent['operation_id'] or result.get('provenance') != old.PROVENANCE
            or result.get('status') != 'verified' or result.get('before') != result.get('after')):
        raise Unavailable('verified-native-predecessor-result-required')
    for name, digest in old.BASELINE.items():
        actual = result.get('before', {}).get(name, {})
        if any(actual.get(kind, {}).get('sha256') != digest for kind in ('installed', 'running')):
            raise Unavailable('predecessor-baseline-is-not-genuine75')
    commands = result.get('commands', [])
    if commands != [{'argv': command, 'exit_code': 0} for command in old.native_commands(intent)]:
        raise Unavailable('predecessor-native-command-proof-differs')
    expected = {name: intent['candidate']['files']['recovery-runtime/' + name] for name in (*old.FILES, 'runtime.manifest')}
    runtime = result.get('runtime', {})
    if (runtime.get('manifest_sha256') != expected['runtime.manifest'] or runtime.get('inventory', {}).get('files') != expected
            or runtime.get('launcher', {}).get('sha256') != expected['bin/recovery']
            or runtime.get('selector', {}).get('sha256') != hashlib.sha256(selection(expected['runtime.manifest'])).hexdigest()):
        raise Unavailable('predecessor-native-inventory-proof-differs')


def validate_spec(spec, plan):
    if (spec.get('schema') != SCHEMA or spec.get('identity') != plan['identity'] or spec.get('operation_id') != plan['operation_id']
            or spec.get('checkpoint') != 'new-launcher-old-selection' or spec.get('action') != 'kill'
            or spec.get('candidate') != plan['candidate'] or 'recovery_fault' in plan or 'candidate_data_fault' in plan):
        raise Unavailable('promotion-fixture-intent-differs')
    validate_predecessor(spec['predecessor_intent'], spec['predecessor_result'], plan['identity'])
    old_digest = spec['predecessor_intent']['candidate']['files']['recovery-runtime/runtime.manifest']
    new_digest = plan['candidate']['files']['recovery-runtime/runtime.manifest']
    if old_digest == new_digest: raise Unavailable('candidate-runtime-is-not-new')
    return old_digest, new_digest


class Native(local.LocalNative):
    def __init__(self, args, plan, stage, spec):
        super().__init__(args, plan, stage); self.spec = spec
        self.previous, self.target = validate_spec(spec, plan)

    def checkpoint(self):
        old.empty_boundary()
        proof = read_record(self.previous, self.target)
        if proof is None: return None
        return proof if mixed_pair(proof) else None

    def proof(self, checkpoint, tick):
        tick(); self.revalidate(self.frozen_identity); old.empty_boundary()
        if not self.owns_transaction_lock(self.properties()): raise Unavailable('updater-exclusive-lock-not-proven')
        current = self.checkpoint()
        if current != checkpoint: raise Unavailable('promotion-record-changed-under-freeze')
        pair = mixed_pair(current, full=True)
        if pair is None: raise Unavailable('promotion-window-missed-under-freeze')
        self.proof_phase = 'previous-runtime'; tick(); previous = runtime_inventory(self.spec['predecessor_intent']['candidate'])
        self.proof_phase = 'target-runtime'; tick(); target = runtime_inventory(self.plan['candidate'])
        self.proof_phase = 'retained-candidate'; tick(); source = retained_source(self.plan['candidate'], tick)
        if (current['record']['old_launcher']['sha256'] != previous['inventory']['files']['bin/recovery']
                or current['record']['new_launcher']['sha256'] != target['inventory']['files']['bin/recovery']):
            raise Unavailable('promotion-launcher-runtime-binding-differs')
        self.proof_phase = 'coordinator-baseline'; baseline = old.baseline(); tick()
        self.revalidate(self.frozen_identity)
        if self.checkpoint() != checkpoint or mixed_pair(checkpoint, full=True) != pair:
            raise Unavailable('promotion-full-proof-changed')
        if not self.owns_transaction_lock(self.properties()): raise Unavailable('updater-exclusive-lock-lost')
        return {'checkpoint': checkpoint, 'pair': pair, 'previous_runtime': previous, 'target_runtime': target,
                'retained_candidate': source, 'baseline': baseline, 'transaction_markers': 'absent', 'exclusive_update_lock': True}

    def safe_thaw(self, identity):
        props = self.properties()
        boot = Path('/proc/sys/kernel/random/boot_id').read_text().strip()
        if (props.get('Id') != identity['unit'] or props.get('InvocationID') != identity['invocation_id']
                or boot != identity['boot_id'] or props.get('ControlGroup') not in (identity['cgroup'], '')):
            return 'refused-replacement-identity'
        if props.get('MainPID') not in ('0', str(identity['pid'])): return 'refused-replacement-process'
        if props.get('MainPID') != '0' and self.worker_identity() != identity: return 'refused-changed-process'
        return 'thawed' if self.thaw() == 0 else 'thaw-unconfirmed'


def run_watch(native, emit, clock=time.monotonic, pause=time.sleep, interrupted=lambda: False):
    end = clock() + 600; seen = False; identity = None; freeze_attempted = False
    emit('armed', previous=native.previous, target=native.target)
    try:
        while clock() < end and not interrupted():
            props = native.properties()
            if props.get('ActiveState') != 'active':
                if seen: raise Unavailable('updater-exited-before-promotion-window')
                pause(.02); continue
            seen = True
            checkpoint = native.checkpoint()
            if checkpoint is None: pause(.02); continue
            identity = native.worker_identity()
            if not native.owns_transaction_lock(props): raise Unavailable('updater-exclusive-lock-not-proven')
            emit('freeze_requested', worker=identity, checkpoint=checkpoint)
            freeze_attempted = True; frozen_deadline = clock() + 30
            native.freeze(); native.frozen_identity = identity; native.revalidate(identity)
            def tick():
                if clock() >= frozen_deadline or interrupted(): raise Unavailable('frozen-proof-deadline')
            proof = native.proof(checkpoint, tick); tick(); native.revalidate(identity)
            emit('promotion_checkpoint_verified', worker=identity, proof=proof)
            tick(); native.revalidate(identity)
            if native.checkpoint() != checkpoint or not native.owns_transaction_lock(native.properties()):
                raise Unavailable('promotion-final-admission-changed')
            emit('kill_attempt', worker=identity)
            native.kill()
            emit('kill_sent', worker=identity)
            return 'fault-applied-native-outcome-unconfirmed'
        raise Unavailable('promotion-window-unobserved')
    except (OSError, ValueError, subprocess.SubprocessError, local.kill.MissedCheckpoint, probe.ProbeError) as exc:
        code = str(exc) if isinstance(exc, (Unavailable, local.kill.MissedCheckpoint)) else type(exc).__name__
        emit('inconclusive', reason=code[:120], proof_phase=getattr(native, 'proof_phase', 'checkpoint'))
        return 'inconclusive'
    finally:
        if freeze_attempted and identity is not None:
            try: result = native.safe_thaw(identity)
            except Exception: result = 'thaw-unconfirmed'
            emit('cleanup', result=result, worker=identity)


def events(spec):
    path = names(spec['operation_id'])['events']
    if old.absent(path): return b'', []
    raw, _ = old.read_file(path, 1048576, 0o600)
    values = []
    for line in raw.splitlines():
        item = probe.strict_object(line)
        if item.get('schema') != EVENT_SCHEMA or item.get('identity') != spec['identity'] or item.get('operation_id') != spec['operation_id']:
            raise Unavailable('fault-event-identity-differs')
        values.append(item)
    return raw, values


def native_from(args, expected):
    paths = local.names(args.operation_id)
    plan = old.private_json(paths['plan']); local.validate_plan(plan, expected, args.operation_id)
    spec = old.private_json(names(args.operation_id)['plan']); validate_spec(spec, plan)
    stage = old.private_json(paths['proof'])
    if (stage.get('identity') != expected or stage.get('operation_id') != args.operation_id
            or stage.get('root') != plan['source_root'] or stage.get('manifest_sha256') != plan['candidate']['manifest_sha256']
            or stage.get('verified_files') != len(plan['candidate']['files']) or not probe.HEX64.fullmatch(stage.get('bash_sha256', ''))):
        raise Unavailable('stage-proof-differs')
    return Native(args, plan, stage, spec)


def fault(native):
    spec = native.spec; old.empty_boundary(); old.baseline()
    proof = old.runtime_proof(spec['predecessor_intent']['candidate'])
    if proof != spec['predecessor_result']['runtime']: raise Unavailable('enrolled-predecessor-changed-before-arm')
    if native.properties().get('LoadState') != 'not-found': raise Unavailable('worker-exists-before-arm')
    path = names(spec['operation_id'])['events']; stopped = False
    def stop(signum, frame):
        nonlocal stopped
        stopped = True
    previous = {sig: signal.signal(sig, stop) for sig in (signal.SIGTERM, signal.SIGINT)}
    try:
        with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'w') as stream:
            old.fsync_dir(path.parent)
            def emit(event, **fields):
                stream.write(json.dumps({'schema': EVENT_SCHEMA, 'identity': spec['identity'], 'operation_id': spec['operation_id'],
                                         'at': probe.utc_now(), 'event': event, **fields}, sort_keys=True) + '\n')
                stream.flush(); os.fsync(stream.fileno())
            return run_watch(native, emit, interrupted=lambda: stopped)
    finally:
        for sig, handler in previous.items(): signal.signal(sig, handler)


def cleanup(native):
    _, values = events(native.spec)
    requested = [item for item in values if item['event'] == 'freeze_requested']
    return {'cleanup': native.safe_thaw(requested[-1]['worker']) if requested else 'nothing-frozen'}


def dispatch_read(native):
    spec = native.spec; paths = names(spec['operation_id']); _, values = events(spec)
    sent = [item for item in values if item['event'] == 'kill_sent']
    proved = [item for item in values if item['event'] == 'promotion_checkpoint_verified']
    if len(sent) != 1 or len(proved) != 1 or sent[0]['worker'] != proved[0]['worker']:
        raise Unavailable('exact-promotion-kill-not-confirmed')
    props = native.properties()
    if props.get('ActiveState') in ('active', 'activating', 'deactivating') or props.get('InvocationID') != sent[0]['worker']['invocation_id']:
        raise Unavailable('exact-updater-termination-not-confirmed')
    old.empty_boundary()
    before = native.checkpoint()
    if before != proved[0]['proof']['checkpoint']: raise Unavailable('original-mixed-pair-no-longer-present')
    pair = mixed_pair(before, full=True)
    if pair is None: raise Unavailable('mixed-pair-unavailable')
    previous = runtime_inventory(spec['predecessor_intent']['candidate']); target = runtime_inventory(spec['candidate'])
    old.save_once(paths['dispatch_attempt'], {'identity': spec['identity'], 'operation_id': spec['operation_id'], 'argv': CAPABILITY,
                                            'at': probe.utc_now(), 'checkpoint_sha256': before['file']['sha256']})
    completed = subprocess.run(CAPABILITY, stdin=subprocess.DEVNULL, capture_output=True, env=old.ENV, timeout=60)
    after = native.checkpoint(); after_pair = mixed_pair(after, full=True) if after else None
    result = {'schema': 'celikpanel/runtime-promotion-dispatch/v1', 'identity': spec['identity'], 'operation_id': spec['operation_id'],
              'argv': CAPABILITY, 'exit_code': completed.returncode, 'before': before, 'after': after,
              'pair_unchanged': pair == after_pair, 'previous_runtime': previous, 'target_runtime': target,
              'stdout_sha256': hashlib.sha256(completed.stdout).hexdigest(), 'stderr_sha256': hashlib.sha256(completed.stderr).hexdigest(),
              'stdout_bytes': len(completed.stdout), 'stderr_bytes': len(completed.stderr),
              'stdout_base64': base64.b64encode(completed.stdout[:1048576]).decode(), 'stderr_base64': base64.b64encode(completed.stderr[:1048576]).decode(),
              'logs_truncated': max(len(completed.stdout), len(completed.stderr)) > 1048576,
              'status': 'verified' if completed.returncode == 0 and before == after and pair == after_pair and max(len(completed.stdout), len(completed.stderr)) <= 1048576 else 'unavailable',
              'attribution': 'native-fixed-launcher-command-old-selected-executable-check-not-full-rollback'}
    old.save_once(paths['dispatch'], result)
    return result


def owner_resume(native):
    # Explicit owner command, not evidence of automatic legacy-foundation dispatch.
    spec = native.spec; paths = names(spec['operation_id'])
    dispatch = old.private_json(paths['dispatch'])
    if (dispatch.get('identity') != spec['identity'] or dispatch.get('operation_id') != spec['operation_id']
            or dispatch.get('status') != 'verified' or not dispatch.get('pair_unchanged')):
        raise Unavailable('verified-old-selected-dispatch-required')
    if native.properties().get('ActiveState') in ('active', 'activating', 'deactivating'):
        raise Unavailable('updater-still-running')
    old.empty_boundary(); before = native.checkpoint()
    if before != dispatch['after'] or mixed_pair(before, full=True) is None:
        raise Unavailable('owner-resume-original-promotion-differs')
    runtime_inventory(spec['predecessor_intent']['candidate']); runtime_inventory(spec['candidate'])
    baseline_before = old.baseline(); argv = [str(old.LAUNCHER), 'recover']
    old.save_once(paths['resume_attempt'], {'identity': spec['identity'], 'operation_id': spec['operation_id'],
                                          'at': probe.utc_now(), 'argv': argv, 'owner_action': True,
                                          'checkpoint_sha256': before['file']['sha256']})
    completed = subprocess.run(argv, stdin=subprocess.DEVNULL, capture_output=True, env=old.ENV, timeout=90)
    result = {'schema': 'celikpanel/runtime-promotion-owner-resume/v1', 'identity': spec['identity'], 'operation_id': spec['operation_id'],
              'argv': argv, 'owner_action': True, 'exit_code': completed.returncode, 'status': 'unavailable',
              'before': before, 'stdout_base64': base64.b64encode(completed.stdout[:1048576]).decode(),
              'stderr_base64': base64.b64encode(completed.stderr[:1048576]).decode(),
              'stdout_sha256': hashlib.sha256(completed.stdout).hexdigest(), 'stderr_sha256': hashlib.sha256(completed.stderr).hexdigest(),
              'logs_truncated': max(len(completed.stdout), len(completed.stderr)) > 1048576,
              'automatic_resume': 'not-established'}
    try:
        old.empty_boundary(); result['baseline_before'] = baseline_before; result['baseline_after'] = old.baseline()
        result['selected_target'] = old.runtime_proof(spec['candidate'])
        result['preserved_previous_runtime'] = runtime_inventory(spec['predecessor_intent']['candidate'])
        if completed.returncode == 0 and baseline_before == result['baseline_after'] and not result['logs_truncated']:
            result['status'] = 'verified'
    except (ValueError, OSError, subprocess.SubprocessError) as exc:
        result['error_type'] = type(exc).__name__
    old.save_once(paths['resume'], result)
    return result

def collect(native):
    raw, values = events(native.spec)
    observer = local.kill.Native(native.args); observer.unit = names(native.spec['operation_id'])['fault']
    result = {'schema': 'celikpanel/runtime-promotion-collection/v1', 'identity': native.spec['identity'],
              'operation_id': native.spec['operation_id'], 'events_base64': base64.b64encode(raw).decode(),
              'events': [item['event'] for item in values], 'worker': native.properties(), 'fault': observer.properties(), 'artifacts': {}}
    for name in ('dispatch_attempt', 'dispatch', 'resume_attempt', 'resume'):
        path = names(native.spec['operation_id'])[name]
        if not old.absent(path):
            data, proof = old.read_file(path, 4 << 20, 0o600)
            result['artifacts'][name] = {'base64': base64.b64encode(data).decode(), 'proof': proof}
    return result


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--mode', required=True, choices=('fault', 'cleanup', 'collect', 'dispatch-read', 'owner-resume'))
    for key in ('lab-nonce', 'vm-uuid', 'cell-id', 'node', 'operation-id'): parser.add_argument('--' + key, required=True)
    args = parser.parse_args(argv)
    expected = probe.guard_guest(args); old.trusted_chain(old.PRIVATE)
    if stat.S_IMODE(old.PRIVATE.stat().st_mode) != 0o700: raise Unavailable('fixture-root-not-private')
    native = native_from(args, expected)
    value = {'fault': fault, 'cleanup': cleanup, 'collect': collect, 'dispatch-read': dispatch_read, 'owner-resume': owner_resume}[args.mode](native)
    print(json.dumps(value, sort_keys=True), flush=True)

if __name__ == '__main__':
    try: main()
    except (ValueError, OSError, subprocess.SubprocessError, probe.ProbeError) as exc:
        print('promotion fixture refused: ' + type(exc).__name__, file=sys.stderr); sys.exit(2)
