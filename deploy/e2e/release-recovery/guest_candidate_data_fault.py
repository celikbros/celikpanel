#!/usr/bin/env python3
"""Quarantine three retained-candidate files in a guarded disposable VM only.

This is a test oracle, never recovery authority. A durable attempt is consumed
before the first rename; interrupted attempts are preserved and never resumed.
"""
from __future__ import annotations
import base64
import ctypes
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import sys

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location('candidate_data_handoff', HERE / 'guest_recovery_handoff.py')
hand = importlib.util.module_from_spec(SPEC); sys.modules[SPEC.name] = hand; SPEC.loader.exec_module(hand)
fault = hand.fault
OPTION = 'quarantine-fixed-three'
SCHEMA = 'celikpanel/lab-candidate-data-fault/v1'
TARGETS = ('rollback.sh', 'bin/agent', 'web/dist/index.html')
RELEASES = Path('/var/backups/celikpanel/releases')
QUARANTINE = Path('/var/backups/celikpanel/.lab-candidate-data-faults')
PROTECTED = (Path('/opt/celikpanel/bin/agent'), Path('/opt/celikpanel/bin/panel'),
             Path('/opt/celikpanel/web/index.html'))
MAX_FILE = 128 * 1024 * 1024
FLAGS = os.O_RDONLY | getattr(os, 'O_CLOEXEC', 0) | getattr(os, 'O_NOFOLLOW', 0) | getattr(os, 'O_NONBLOCK', 0)


def encoded(value): return (json.dumps(value, sort_keys=True) + '\n').encode()


def identity(info):
    return {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid,
            'gid': info.st_gid, 'mode': stat.S_IMODE(info.st_mode), 'links': info.st_nlink,
            'size': info.st_size, 'mtime_ns': info.st_mtime_ns, 'ctime_ns': info.st_ctime_ns}


def check_dir(fd, private=False):
    info = os.fstat(fd)
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0
            or stat.S_IMODE(info.st_mode) & 0o022 or (private and stat.S_IMODE(info.st_mode) != 0o700)):
        raise ValueError('candidate-data-unsafe-directory')
    return info


def open_dir(path):
    if not path.is_absolute() or '..' in path.parts: raise ValueError('candidate-data-invalid-path')
    fd = os.open('/', FLAGS | os.O_DIRECTORY)
    try:
        check_dir(fd)
        for part in path.parts[1:]:
            next_fd = os.open(part, FLAGS | os.O_DIRECTORY, dir_fd=fd)
            os.close(fd); fd = next_fd; check_dir(fd)
        return fd
    except BaseException:
        os.close(fd); raise


def same_dir(path, fd):
    fresh = open_dir(path)
    try:
        a, b = os.fstat(fresh), os.fstat(fd)
        if (a.st_dev, a.st_ino) != (b.st_dev, b.st_ino): raise ValueError('candidate-data-directory-replaced')
    finally: os.close(fresh)


def file_proof(parent, name, tick=lambda: None):
    fd = os.open(name, FLAGS, dir_fd=parent)
    try:
        before = os.fstat(fd)
        if (not stat.S_ISREG(before.st_mode) or before.st_uid != 0 or before.st_gid != 0
                or before.st_nlink != 1 or stat.S_IMODE(before.st_mode) & 0o022 or before.st_size > MAX_FILE):
            raise ValueError('candidate-data-unsafe-file')
        digest = hashlib.sha256(); consumed = 0
        while True:
            tick(); chunk = os.read(fd, 1048576)
            if not chunk: break
            consumed += len(chunk)
            if consumed > MAX_FILE: raise ValueError('candidate-data-growing-file-bound')
            digest.update(chunk)
        after = os.fstat(fd)
        current = os.stat(name, dir_fd=parent, follow_symlinks=False)
        if identity(before) != identity(after) or before.st_ctime_ns != after.st_ctime_ns or identity(after) != identity(current):
            raise ValueError('candidate-data-file-changed')
        return {**identity(after), 'sha256': digest.hexdigest()}
    finally: os.close(fd)


def inventory(root, tick):
    result = {}; entries = 0
    def visit(fd, prefix):
        nonlocal entries
        for name in sorted(os.listdir(fd)):
            tick()
            entries += 1
            if entries > 4096: raise ValueError('candidate-data-inventory-bound')
            info = os.stat(name, dir_fd=fd, follow_symlinks=False)
            relative = prefix + name
            if stat.S_ISDIR(info.st_mode):
                child = os.open(name, FLAGS | os.O_DIRECTORY, dir_fd=fd)
                try: check_dir(child); visit(child, relative + '/')
                finally: os.close(child)
            elif stat.S_ISREG(info.st_mode): result[relative] = file_proof(fd, name, tick)['sha256']
            else: raise ValueError('candidate-data-inventory-unsafe')
    visit(root, '')
    return result


def rename_noreplace(source_parent, source, target_parent, target):
    # Never overwrite an intervening owner-created quarantine object.
    native = ctypes.CDLL(None, use_errno=True)
    rename = getattr(native, 'renameat2', None)
    if rename is None: raise ValueError('candidate-data-atomic-noreplace-unavailable')
    rename.argtypes = (ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint)
    rename.restype = ctypes.c_int
    if rename(source_parent, os.fsencode(source), target_parent, os.fsencode(target), 1) != 0:
        errno = ctypes.get_errno()
        raise OSError(errno, 'candidate-data-quarantine-rename-refused')


def save_once(parent, name, value):
    raw = encoded(value)
    fd = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=parent)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(raw); stream.flush(); os.fsync(stream.fileno())
    os.fsync(parent)
    return hashlib.sha256(raw).hexdigest()


def apply(args, plan, proof, worker, tick, revalidate, *, after_step=lambda _: None):
    if sys.platform != 'linux': raise ValueError('candidate-data-linux-required')
    actual_identity = fault.probe.guard_guest(args)
    operation = args.operation_id
    if (plan.get('candidate_data_fault') != OPTION or plan.get('identity') != actual_identity
            or plan.get('operation_id') != operation or not fault.files.HEX32.fullmatch(operation)):
        raise ValueError('candidate-data-opt-in-or-identity-missing')
    retained = proof.get('retained_candidate', {})
    root = Path(retained.get('root', ''))
    candidate = plan['candidate']
    if (root.parent != RELEASES or not re.fullmatch(re.escape(candidate['commit'][:12]) + '-[0-9a-f]{24}', root.name)
            or retained.get('manifest_sha256') != candidate['manifest_sha256']
            or retained.get('verified_files') != len(candidate['files'])
            or any(name not in candidate['files'] for name in TARGETS)):
        raise ValueError('candidate-data-retained-proof-mismatch')
    transaction = fault.read_transaction()
    runtime, selection = hand.selection()
    phase = 'completion.pending' if plan.get('boundary') == 'completion-database-verified' else 'active'
    if phase == 'completion.pending' and (proof.get('phase') != phase or proof.get('transaction') != transaction
            or proof.get('database_readonly_checker', {}).get('exit_code') != 0
            or proof.get('material', {}).get('transaction_token_sha256') != transaction.get('transaction_token_sha256')):
        raise ValueError('candidate-data-completion-proof-missing')
    if (transaction.get('transaction_operation') != 'update' or transaction.get('transaction_phase') != phase
            or transaction.get('snapshot') != proof.get('snapshot')):
        raise ValueError('candidate-data-active-transaction-mismatch')
    def verify_live():
        tick(); revalidate(worker)
        if fault.read_transaction() != transaction or hand.selection()[1] != selection:
            raise ValueError('candidate-data-live-proof-changed')
    verify_live()
    runtime_proof = fault.verify_runtime(runtime, tick)
    snapshot = fault.files.verify_snapshot(proof['snapshot'], tick)
    if snapshot is None or snapshot['manifest_sha256'] != proof.get('manifest_sha256'):
        raise ValueError('candidate-data-snapshot-mismatch')
    opened = []
    try:
        rootfd = open_dir(root); opened.append(rootfd); check_dir(rootfd, private=True)
        if inventory(rootfd, tick) != candidate['files']: raise ValueError('candidate-data-original-inventory-mismatch')
        targets = []
        for relative in TARGETS:
            path = root / relative; parent = open_dir(path.parent); opened.append(parent)
            old = file_proof(parent, path.name, tick)
            if old['sha256'] != candidate['files'][relative]: raise ValueError('candidate-data-original-hash-mismatch')
            targets.append((relative, path, parent, old))
        protected = {}
        for path in (*PROTECTED, *(Path(p) for p in proof.get('installed_unit_helpers', {}))):
            parent = open_dir(path.parent); opened.append(parent)
            protected[str(path)] = (parent, file_proof(parent, path.name, tick))
        anchor = open_dir(QUARANTINE.parent); opened.append(anchor)
        try: os.mkdir(QUARANTINE.name, 0o700, dir_fd=anchor); os.fsync(anchor)
        except FileExistsError: pass
        qbase = open_dir(QUARANTINE); opened.append(qbase); check_dir(qbase, private=True)
        if os.fstat(qbase).st_dev != os.fstat(rootfd).st_dev: raise ValueError('candidate-data-cross-filesystem')
        verify_live(); same_dir(root, rootfd); same_dir(QUARANTINE, qbase)
        os.mkdir(operation, 0o700, dir_fd=qbase); os.fsync(qbase)  # consumed even if interrupted next
        qpath = QUARANTINE / operation
        qfd = open_dir(qpath); opened.append(qfd); check_dir(qfd, private=True)
        intent = {'schema': SCHEMA, 'identity': actual_identity, 'operation_id': operation,
                  'option': OPTION, 'worker': worker, 'transaction': transaction,
                  'snapshot_proof': snapshot, 'runtime_proof': runtime_proof,
                  'retained_root': str(root), 'candidate_manifest_sha256': candidate['manifest_sha256'],
                  'files': {r: before for r, _, _, before in targets},
                  'protected': {path: item[1] for path, item in protected.items()},
                  'quarantine': str(qpath), 'authority': 'fixture-oracle-only'}
        intent_sha = save_once(qfd, 'intent.json', intent); after_step('intent_durable')
        for index, (relative, path, parent, before) in enumerate(targets):
            verify_live(); same_dir(root, rootfd); same_dir(path.parent, parent); same_dir(qpath, qfd)
            if file_proof(parent, path.name, tick) != before: raise ValueError('candidate-data-target-changed')
            destination = str(index) + '.original'
            save_once(qfd, 'pending-' + str(index) + '.json', {'relative': relative, 'before': before})
            rename_noreplace(parent, path.name, qfd, destination)
            os.fsync(parent); os.fsync(qfd); after_step('renamed-' + str(index))
            moved = file_proof(qfd, destination, tick)
            if any(moved[key] != value for key, value in before.items() if key != 'ctime_ns'):
                raise ValueError('candidate-data-quarantine-differs')
            save_once(qfd, 'moved-' + str(index) + '.json', {'relative': relative, 'original': destination, 'before': before, 'after': moved})
        verify_live(); same_dir(root, rootfd); same_dir(qpath, qfd)
        expected = {name: digest for name, digest in candidate['files'].items() if name not in TARGETS}
        if inventory(rootfd, tick) != expected: raise ValueError('candidate-data-unexpected-remaining-difference')
        for path, (parent, before) in protected.items():
            same_dir(Path(path).parent, parent)
            if file_proof(parent, Path(path).name, tick) != before: raise ValueError('candidate-data-protected-state-changed')
        if fault.verify_runtime(runtime, tick) != runtime_proof or fault.files.verify_snapshot(proof['snapshot'], tick) != snapshot:
            raise ValueError('candidate-data-independent-evidence-changed')
        verify_live()
        result = {'schema': SCHEMA, 'identity': actual_identity, 'operation_id': operation,
                  'status': 'applied', 'quarantine': str(qpath), 'intent_sha256': intent_sha,
                  'removed': list(TARGETS), 'manifest_unchanged': True, 'protected_state_unchanged': True,
                  'snapshot_manifest_sha256': snapshot['manifest_sha256'], 'runtime_manifest_sha256': runtime,
                  'remaining_inventory_sha256': hashlib.sha256(encoded(expected)).hexdigest()}
        result_sha = save_once(qfd, 'applied.json', result); after_step('applied_durable')
        return {**result, 'applied_sha256': result_sha}
    finally:
        for fd in reversed(opened): os.close(fd)


def collect(operation):
    if not fault.files.HEX32.fullmatch(operation): raise ValueError('candidate-data-invalid-operation')
    path = QUARANTINE / operation
    if not path.exists() and not path.is_symlink(): return {'status': 'not-applied'}
    fd = open_dir(path)
    try:
        check_dir(fd, private=True); records = {}
        for name in ('intent.json', 'applied.json', *(f'{kind}-{i}.json' for i in range(3) for kind in ('pending', 'moved'))):
            try:
                metadata = file_proof(fd, name)
                if metadata['size'] > 32768 or metadata['mode'] != 0o600: raise ValueError('candidate-data-record-bound')
                filefd = os.open(name, FLAGS, dir_fd=fd)
                with os.fdopen(filefd, 'rb') as stream: raw = stream.read(32769)
                if hashlib.sha256(raw).hexdigest() != metadata['sha256']: raise ValueError('candidate-data-record-changed')
                records[name] = {'sha256': metadata['sha256'], 'base64': base64.b64encode(raw).decode()}
            except FileNotFoundError: continue
        originals = {}
        for i in range(3):
            try: originals[str(i) + '.original'] = file_proof(fd, str(i) + '.original')
            except FileNotFoundError: pass
        retained = {'status': 'unavailable'}
        if 'intent.json' in records:
            intent = json.loads(base64.b64decode(records['intent.json']['base64']))
            source = Path(intent.get('retained_root', ''))
            if (intent.get('schema') != SCHEMA or intent.get('operation_id') != operation or source.parent != RELEASES
                    or not re.fullmatch(r'[0-9a-f]{12}-[0-9a-f]{24}', source.name)):
                raise ValueError('candidate-data-collected-intent-invalid')
            observed = {}
            for relative in ('SHA256SUMS', *TARGETS):
                parent = None
                try:
                    parent = open_dir((source / relative).parent)
                    observed[relative] = {'status': 'present', 'proof': file_proof(parent, (source / relative).name)}
                except FileNotFoundError: observed[relative] = {'status': 'absent'}
                finally:
                    if parent is not None: os.close(parent)
            retained = {'status': 'observed', 'root': str(source), 'files': observed}
        return {'status': 'recorded', 'quarantine': str(path), 'records': records, 'originals': originals, 'retained': retained,
                'recovery_success': 'not-inferred'}
    finally: os.close(fd)
