#!/usr/bin/env python3
"""Enroll the pinned, genuinely built J predecessor only in a registered lab VM.

This is a predecessor-enrollment fixture, not a prior full installed update or
signed Agent admission. No selector/launcher/receipt is fabricated by the fixture.
"""
from __future__ import annotations
import argparse
try:
    import fcntl
except ImportError:  # Parsing/offline tests remain importable on Windows.
    fcntl = None
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tarfile
import time

HERE = Path(__file__).resolve().parent

def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    value = importlib.util.module_from_spec(spec)
    sys.modules[name] = value
    spec.loader.exec_module(value)
    return value

probe = module('predecessor_probe', 'guest_probe.py')
archive_tools = module('predecessor_archive', 'candidate_archive.py')
SCHEMA = 'celikpanel/runtime-predecessor-intent/v1'
PROVENANCE = 'genuine-pinned-predecessor-enrollment-not-prior-full-update'
COMMIT = 'bc0ebc051de0fc542c5f90e7c0bbb519a5c0f111'
TREE = '76492d58a7b9c1b86e2282f0e6d090127b71bd5a'
ARCHIVE_SHA = '4d5bde282e7d2342a4b42930f7749104c658b933cf8908da71db3279cf7542c1'
BASELINE = {'agent': 'e8e69c520cf4112a5c60380378b8347021aaa035cdaacd820c9927436beff078',
            'panel': '568a7b15f20e5666fced0dcec77f52e83e596efd6e0969d3bf05508faa485d05'}
PRIVATE = Path('/root/celikpanel-release-recovery-lab')
TRANSACTION = Path('/var/lib/celikpanel-release-transaction')
SELECTION = Path('/var/lib/celikpanel-release-state/recovery-runtime.v1')
RUNTIMES = Path('/usr/libexec/celikpanel/recovery-runtimes/v1')
LAUNCHER = Path('/usr/libexec/celikpanel/recovery')
ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'HOME': '/root', 'LC_ALL': 'C'}
FILES = ('bin/agent-checker', 'bin/panel-checker', 'bin/recovery', 'bin/schema17-bridge',
         'deploy/panel-tls-snapshot.sh', 'deploy/recovery/runtime-entry.sh',
         'deploy/release-recovery-foundation.sh', 'deploy/release-recovery-observation.sh',
         'deploy/release-transaction-guard.sh', 'deploy/release-unit-transition.sh', 'rollback.sh', 'update.sh')
EXECUTABLE = {'bin/agent-checker', 'bin/panel-checker', 'bin/recovery', 'bin/schema17-bridge',
              'deploy/recovery/runtime-entry.sh', 'rollback.sh', 'update.sh'}


def names(operation):
    if not isinstance(operation, str) or not probe.HEX32.fullmatch(operation):
        raise ValueError('invalid predecessor operation')
    prefix = PRIVATE / ('runtime-predecessor-' + operation)
    return {'unit': 'celikpanel-lab-runtime-predecessor-' + operation + '.service',
            'plan': Path(str(prefix) + '.json'), 'archive': Path(str(prefix) + '.tar.gz'),
            'stage': Path(str(prefix) + '.stage'), 'proof': Path(str(prefix) + '.stage.json'),
            'attempt': Path(str(prefix) + '.attempt.json'), 'result': Path(str(prefix) + '.result.json'),
            'log': Path(str(prefix) + '.log')}


def trusted_chain(path):
    if not path.is_absolute() or str(path) != os.path.normpath(str(path)):
        raise ValueError('noncanonical fixture path')
    for item in (path, *path.parents):
        info = item.lstat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or info.st_mode & 0o7022:
            raise ValueError('untrusted fixture ancestry')


def identity(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid,
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def read_file(path, limit, mode=None, kernel=False):
    flags = os.O_RDONLY | os.O_CLOEXEC | os.O_NONBLOCK
    if not kernel:
        trusted_chain(path.parent)
        flags |= os.O_NOFOLLOW
    with os.fdopen(os.open(path, flags), 'rb') as stream:
        before = os.fstat(stream.fileno())
        if (not stat.S_ISREG(before.st_mode) or before.st_uid != 0 or before.st_nlink != 1
                or before.st_size > limit or (not kernel and (before.st_gid != 0 or before.st_mode & 0o7022))
                or (mode is not None and stat.S_IMODE(before.st_mode) != mode)):
            raise ValueError('unsafe bounded predecessor file')
        raw = stream.read(limit + 1)
        if len(raw) != before.st_size or identity(before) != identity(os.fstat(stream.fileno())):
            raise ValueError('predecessor file changed')
    return raw, {'sha256': hashlib.sha256(raw).hexdigest(), 'bytes': len(raw), 'device': before.st_dev,
                 'inode': before.st_ino, 'mode': stat.S_IMODE(before.st_mode), 'uid': before.st_uid, 'gid': before.st_gid,
                 'links': before.st_nlink, 'mtime_ns': before.st_mtime_ns, 'ctime_ns': before.st_ctime_ns}


def private_json(path):
    return probe.strict_object(read_file(path, 4 * 1024 * 1024, 0o600)[0])


def fsync_dir(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def save_once(path, value):
    trusted_chain(path.parent)
    raw = (json.dumps(value, sort_keys=True) + '\n').encode()
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as out:
        out.write(raw); out.flush(); os.fsync(out.fileno())
    fsync_dir(path.parent)
    return hashlib.sha256(raw).hexdigest()


def validate_candidate(candidate):
    if (not isinstance(candidate, dict) or candidate.get('archive_sha256') != ARCHIVE_SHA
            or candidate.get('commit') != COMMIT or candidate.get('tree') != TREE
            or candidate.get('root_name') != 'celikpanel-v0.1.0-alpha.81'
            or candidate.get('provenance') != 'unpublished-local-build-not-signed-agent-admission'):
        raise ValueError('predecessor is not the exact J artifact')
    for name in (*FILES, 'runtime.manifest'):
        digest = candidate.get('files', {}).get('recovery-runtime/' + name, '')
        if not probe.HEX64.fullmatch(digest):
            raise ValueError('pinned predecessor kit inventory incomplete')
    return candidate


def validate_plan(plan, expected, operation):
    paths = names(operation)
    if (plan.get('schema') != SCHEMA or plan.get('identity') != expected or plan.get('operation_id') != operation
            or plan.get('provenance') != PROVENANCE or plan.get('baseline_artifacts') != BASELINE
            or plan.get('archive_path') != str(paths['archive'])
            or plan.get('source_root') != str(paths['stage'] / 'celikpanel-v0.1.0-alpha.81')):
        raise ValueError('predecessor intent differs from registered fixture')
    validate_candidate(plan.get('candidate'))
    return paths


def absent(path):
    # Missing ancestors are handled by the caller; symlinks and all other
    # errors are present/unsafe, never silently interpreted as absence.
    try:
        path.lstat()
    except FileNotFoundError:
        return True
    return False


def no_selected_runtime():
    for target in (SELECTION, LAUNCHER):
        trusted_chain(target.parent)
        if not absent(target):
            raise ValueError('predecessor fixture already has a selector or launcher')
    if not absent(RUNTIMES):
        trusted_chain(RUNTIMES)
        if list(RUNTIMES.iterdir()):
            raise ValueError('predecessor fixture contains prior runtime evidence')


def empty_boundary():
    trusted_chain(TRANSACTION)
    if stat.S_IMODE(TRANSACTION.stat().st_mode) != 0o700:
        raise ValueError('transaction root is not private')
    if set(path.name for path in TRANSACTION.iterdir()) != {'transaction.lock'}:
        raise ValueError('existing or unknown transaction blocks predecessor enrollment')
    read_file(TRANSACTION / 'transaction.lock', 0, 0o600)


def unit_properties(unit):
    fields = ('Id', 'LoadState', 'ActiveState', 'SubState', 'MainPID', 'InvocationID', 'ControlGroup')
    argv = ['/usr/bin/systemctl', 'show', unit]
    for field in fields:
        argv.extend(('-p', field))
    result = subprocess.run(argv, capture_output=True, text=True, env=ENV, timeout=5, check=True)
    if len(result.stdout) > 16384:
        raise ValueError('unit properties exceed bound')
    values = dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)
    if set(values) != set(fields) or values['Id'] != unit:
        raise ValueError('unit properties differ')
    return values


def baseline():
    result = {}
    for name, expected in BASELINE.items():
        unit = 'celikpanel-' + name + '.service'
        before = unit_properties(unit)
        pid = before.get('MainPID', '')
        if before.get('ActiveState') != 'active' or not pid.isdigit() or int(pid) <= 1:
            raise ValueError('genuine Alpha75 coordinator is not active')
        installed = read_file(Path('/opt/celikpanel/bin') / name, 128 << 20, 0o755)[1]
        running = read_file(Path('/proc') / pid / 'exe', 128 << 20, 0o755, kernel=True)[1]
        if installed['sha256'] != expected or running['sha256'] != expected or unit_properties(unit) != before:
            raise ValueError('current coordinator differs from genuine Alpha75')
        result[name] = {'unit': before, 'installed': installed, 'running': running}
    return result


def tree_proof(root, expected, private=False):
    trusted_chain(root)
    actual = {}
    deadline = time.monotonic() + 30
    count = 0
    for parent, directories, files in os.walk(root, followlinks=False):
        if time.monotonic() > deadline:
            raise ValueError('predecessor inventory time limit')
        for name in directories:
            count += 1
            if count > 20000 or time.monotonic() > deadline:
                raise ValueError('predecessor directory inventory limit')
            path = Path(parent) / name
            trusted_chain(path)
            if private and stat.S_IMODE(path.stat().st_mode) != 0o700:
                raise ValueError('installed kit directory is not private')
        for name in files:
            count += 1
            if count > 20000:
                raise ValueError('predecessor inventory count limit')
            path = Path(parent) / name
            relative = path.relative_to(root).as_posix()
            mode = (0o755 if relative in EXECUTABLE else 0o600 if relative == 'runtime.manifest' else 0o644) if private else None
            raw, proof = read_file(path, 128 << 20, mode)
            del raw
            actual[relative] = proof['sha256']
    if actual != expected:
        raise ValueError('predecessor full inventory changed')
    return {'root': str(root), 'verified_files': len(actual), 'files': actual}


def stage(plan, paths):
    no_selected_runtime(); empty_boundary(); before = baseline()
    candidate = archive_tools.inspect_archive(paths['archive'], ARCHIVE_SHA)
    if candidate != plan['candidate']:
        raise ValueError('guest archive differs from sealed predecessor')
    root = Path(plan['source_root'])
    paths['stage'].mkdir(mode=0o700); root.mkdir(mode=0o700)
    with tarfile.open(paths['archive'], 'r:gz') as bundle:
        for item in bundle:
            relative = archive_tools.member_path(item.name, candidate['root_name'])
            if not relative:
                continue
            target = root / relative
            if item.isdir():
                target.mkdir(mode=0o755, parents=True, exist_ok=True)
                continue
            if not item.isfile():
                raise ValueError('special predecessor archive member')
            target.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
            with os.fdopen(os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as out, bundle.extractfile(item) as source:
                for chunk in iter(lambda: source.read(1048576), b''):
                    out.write(chunk)
                out.flush(); os.fchmod(out.fileno(), 0o755 if item.mode & 0o111 else 0o644); os.fsync(out.fileno())
    proof = tree_proof(root, candidate['files'])
    for parent, _, _ in os.walk(root, topdown=False):
        fsync_dir(Path(parent))
    fsync_dir(paths['stage']); fsync_dir(PRIVATE)
    if baseline() != before:
        raise ValueError('baseline changed during predecessor staging')
    proof.update(schema='celikpanel/runtime-predecessor-stage/v1', identity=plan['identity'], operation_id=plan['operation_id'], baseline=before)
    save_once(paths['proof'], proof)
    return proof


def runtime_proof(candidate):
    raw, selector = read_file(SELECTION, 160, 0o600)
    digest = candidate['files']['recovery-runtime/runtime.manifest']
    if raw != ('format=celikpanel-recovery-selection-v1\nruntime=' + digest + '\n').encode():
        raise ValueError('native predecessor selector differs from pinned kit')
    expected = {name: candidate['files']['recovery-runtime/' + name] for name in (*FILES, 'runtime.manifest')}
    root = RUNTIMES / digest
    if stat.S_IMODE(root.lstat().st_mode) != 0o700:
        raise ValueError('selected predecessor root is not private')
    inventory = tree_proof(root, expected, private=True)
    manifest, _ = read_file(root / 'runtime.manifest', 4096, 0o600)
    canonical = b'format=celikpanel-recovery-runtime-v1\nprotocol=1\nsnapshot=6\n' + ''.join(expected[name] + '  ' + name + '\n' for name in sorted(FILES)).encode()
    if manifest != canonical:
        raise ValueError('predecessor runtime manifest schema differs')
    _, launcher = read_file(LAUNCHER, 128 << 20, 0o755)
    if launcher['sha256'] != expected['bin/recovery']:
        raise ValueError('native predecessor launcher does not match selected binary')
    return {'manifest_sha256': digest, 'selector': selector, 'launcher': launcher, 'inventory': inventory,
            'protocol': 1, 'snapshot_version': 6}


def native_commands(plan):
    binary = str(Path(plan['source_root']) / 'recovery-runtime/bin/recovery')
    source = str(Path(plan['source_root']) / 'recovery-runtime')
    return ([binary, 'enroll-runtime', '--source', source, '--transaction-fd', '9'],
            [str(LAUNCHER), 'verify-material-support', '--layout', 'snapshot-name-sha256-v1'],
            [str(LAUNCHER), 'verify-compatibility', '--mode', '--normal'])


def enroll(plan, paths):
    staged = private_json(paths['proof'])
    if (staged.get('identity') != plan['identity'] or staged.get('operation_id') != plan['operation_id']
            or staged.get('files') != plan['candidate']['files'] or staged.get('root') != plan['source_root']):
        raise ValueError('predecessor stage proof does not match enrollment')
    empty_boundary(); no_selected_runtime(); before = baseline()
    tree_proof(Path(plan['source_root']), plan['candidate']['files'])
    save_once(paths['attempt'], {'schema': 'celikpanel/runtime-predecessor-attempt/v1', 'identity': plan['identity'],
                                'operation_id': plan['operation_id'], 'started_at': probe.utc_now(), 'commands': native_commands(plan)})
    result = {'schema': 'celikpanel/runtime-predecessor-result/v1', 'identity': plan['identity'], 'operation_id': plan['operation_id'],
              'provenance': PROVENANCE, 'before': before, 'commands': [], 'status': 'unavailable', 'started_at': probe.utc_now()}
    original = os.open(TRANSACTION / 'transaction.lock', os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
    try:
        info = os.fstat(original)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or stat.S_IMODE(info.st_mode) != 0o600
                or info.st_nlink != 1 or info.st_size != 0):
            raise ValueError('unsafe native enrollment lock')
        fcntl.flock(original, fcntl.LOCK_EX | fcntl.LOCK_NB)
        os.dup2(original, 9, inheritable=True)
        with os.fdopen(os.open(paths['log'], os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as output:
            empty_boundary(); no_selected_runtime()
            if baseline() != before:
                raise ValueError('baseline changed before locked enrollment')
            for command in native_commands(plan):
                result['commands'].append({'argv': command, 'exit_code': None})
                completed = subprocess.run(command, stdin=subprocess.DEVNULL, stdout=output, stderr=subprocess.STDOUT,
                                           env=ENV, pass_fds=(9,), timeout=90)
                result['commands'][-1]['exit_code'] = completed.returncode
                output.flush(); os.fsync(output.fileno())
                if completed.returncode != 0:
                    raise ValueError('native predecessor command failed')
            result['runtime'] = runtime_proof(plan['candidate'])
            empty_boundary()
            result['after'] = baseline()
            if result['after'] != before:
                raise ValueError('coordinator changed during predecessor enrollment')
            result['status'] = 'verified'
    except Exception as exc:
        result['error_type'] = type(exc).__name__
    finally:
        if original != 9:
            os.close(original)
        try:
            os.close(9)
        except OSError:
            pass
        result['finished_at'] = probe.utc_now()
        if not absent(paths['log']):
            result['log'] = read_file(paths['log'], 1048576, 0o600)[1]
        save_once(paths['result'], result)
    return result


def collect(plan, paths):
    import base64
    result = {'schema': 'celikpanel/runtime-predecessor-collection/v1', 'identity': plan['identity'],
              'operation_id': plan['operation_id'], 'artifacts': {}}
    for name in ('proof', 'attempt', 'result', 'log'):
        if absent(paths[name]):
            continue
        raw, proof = read_file(paths[name], 4 * 1024 * 1024 if name != 'log' else 1048576, 0o600)
        result['artifacts'][name] = {'proof': proof, 'base64': base64.b64encode(raw).decode()}
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--mode', choices=('stage', 'enroll', 'collect'), required=True)
    for key in ('lab-nonce', 'vm-uuid', 'cell-id', 'node', 'operation-id'):
        parser.add_argument('--' + key, required=True)
    args = parser.parse_args()
    expected = probe.guard_guest(args)
    paths = names(args.operation_id)
    trusted_chain(PRIVATE)
    if stat.S_IMODE(PRIVATE.stat().st_mode) != 0o700:
        raise ValueError('private fixture root changed')
    plan = private_json(paths['plan']); validate_plan(plan, expected, args.operation_id)
    value = {'stage': stage, 'enroll': enroll, 'collect': collect}[args.mode](plan, paths)
    print(json.dumps(value, sort_keys=True))
    return 0 if args.mode != 'enroll' or value['status'] == 'verified' else 1


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ValueError, OSError, subprocess.SubprocessError, probe.ProbeError) as exc:
        print('predecessor fixture refused: ' + type(exc).__name__, file=sys.stderr)
        sys.exit(2)
