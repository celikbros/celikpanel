#!/usr/bin/env python3
"""Read-only proof of the native DB exchange before its publication receipt.

No attach, signal, SQLite open, write, receipt creation or product authority.
The independent tracer supplies real renameat2 entry/exit and stopped tasks.
All paths come from fixed roots and the exact admitted operation.
"""
from __future__ import annotations
import copy
import json
import os
from pathlib import Path
import re
import stat

import importlib.util
import sys

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('exchange_wal_readers', HERE / 'guest_wal_checkpoint.py')
w = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = w
spec.loader.exec_module(w)
Inconclusive, require = w.Inconclusive, w.require
SCHEMA = 'celikpanel/lab-database-exchange-checkpoint/v1'
SELECTION = Path('/var/lib/celikpanel-release-state/recovery-runtime.v1')
RUNTIMES = Path('/usr/libexec/celikpanel/recovery-runtimes/v1')
LAUNCHER = Path('/usr/libexec/celikpanel/recovery')
KIT_MODES = {n: 0o755 for n in ('bin/recovery', 'bin/panel-checker', 'bin/agent-checker',
             'bin/schema17-bridge', 'update.sh', 'rollback.sh', 'deploy/recovery/runtime-entry.sh')}
KIT_MODES.update({n: 0o644 for n in ('deploy/release-transaction-guard.sh', 'deploy/release-unit-transition.sh',
                 'deploy/release-recovery-foundation.sh', 'deploy/panel-tls-snapshot.sh', 'deploy/release-recovery-observation.sh')})
KIT_MODES['runtime.manifest'] = 0o600
DB = 'celikpanel.db'
BUILD = re.compile(r'\.build-[0-9a-f]{32}\Z')
ADMISSION = ['schema', 'snapshot', 'transaction_token_sha256', 'material_sha256',
             'snapshot_manifest_sha256', 'candidate_panel_sha256', 'parent', 'before',
             'root', 'transaction', 'authority', 'work', 'initial']
PUBLICATION = ['schema', 'admission_sha256', 'seal_sha256', 'build', 'directory', 'before', 'after']
ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'HOME': '/root', 'USER': 'root',
       'LOGNAME': 'root', 'SHELL': '/bin/bash', 'LANG': 'C', 'LC_ALL': 'C',
       'CELIKPANEL_DATA_DIR': '/var/lib/celikpanel',
       'CELIKPANEL_AGENT_STATE_DIR': '/var/lib/celikpanel-agent-private',
       'CELIKPANEL_MUTATION_LOCK': '/run/celikpanel/service-mutation.lock'}
LIMITS = ['read-only-fixture-observation-not-product-authority',
          'actual-rename-syscall-causality-supplied-by-exact-native-tracer',
          'no-original-SQLite-open-no-row-data-output',
          'exchange-visible-before-receipt-not-power-loss-durability-or-recovery-success']


def digest(value):
    return w.sha(json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True,
                             allow_nan=False).encode())


def record(path, keys, schema, budget):
    proof, raw = w._read(path, budget, {0}, 1048576, raw=True, private=True)
    value = w.probe.strict_object(raw)
    require(list(value) == keys and value['schema'] == schema, 'record-shape')
    require(raw == (json.dumps(value, separators=(',', ':'), ensure_ascii=False,
                               allow_nan=False) + '\n').encode(), 'record-not-canonical')
    return value, proof


def directory_value(value):
    require(type(value) is dict and list(value) == ['dev', 'ino', 'mode', 'uid', 'gid']
            and all(type(n) is int and n >= 0 for n in value.values())
            and value['dev'] > 0 and value['ino'] > 0, 'record-directory-shape')


def file_value(value, uid, gid):
    w.valid_product_file(value, uid, gid)
    require(list(value) == ['identity', 'sha256']
            and list(value['identity']) == ['dev', 'ino', 'mode', 'uid', 'gid', 'links', 'size', 'mtime', 'ctime']
            and all(list(value['identity'][k]) == ['Sec', 'Nsec'] for k in ('mtime', 'ctime')),
            'record-file-order')


def selected(plan, budget, full=False):
    files = plan['candidate']['files']
    expected = {k[len('recovery-runtime/'):]: v for k, v in files.items() if k.startswith('recovery-runtime/')}
    require(set(expected) == set(KIT_MODES)
            and all(type(v) is str and w.HEX64.fullmatch(v) for v in expected.values()), 'kit-pins-unavailable')
    manifest_sha = expected['runtime.manifest']
    selector, raw = w._read(SELECTION, budget, {0}, 160, raw=True, private=True)
    require(raw == ('format=celikpanel-recovery-selection-v1\nruntime=' + manifest_sha + '\n').encode(), 'selected-kit-differs')
    root = RUNTIMES / manifest_sha
    root_id = w._directory(root, {0}, 0, 0, 0o700)
    checker_path = root / 'bin/panel-checker'
    checker = w._read(checker_path, budget, {0}, 128 * 1024 ** 2)
    require(checker['sha256'] == expected['bin/panel-checker']
            and checker['identity']['mode'] == stat.S_IFREG | 0o755
            and checker['identity']['gid'] == 0, 'checker-pin-differs')
    result = {'root': str(root), 'root_identity': root_id, 'manifest_sha256': manifest_sha,
              'selector': selector, 'checker_path': str(checker_path), 'checker': checker}
    if full:
        inventory, directories = w.tree(root, budget, {0}, 100)
        require(set(inventory) == set(expected)
                and all(inventory[n]['sha256'] == h for n, h in expected.items()), 'kit-inventory-differs')
        manifest, raw = w._read(root / 'runtime.manifest', budget, {0}, 4096, raw=True, private=True)
        canonical = ('format=celikpanel-recovery-runtime-v1\nprotocol=1\nsnapshot=6\n' +
                     ''.join(expected[n] + '  ' + n + '\n' for n in sorted(expected) if n != 'runtime.manifest')).encode()
        require(raw == canonical and manifest['sha256'] == manifest_sha, 'kit-manifest-differs')
        for n, p in inventory.items():
            mode = KIT_MODES[n]
            require(p['identity']['mode'] == stat.S_IFREG | mode and p['identity']['gid'] == 0, 'kit-file-metadata')
        require(all(d['mode'] == stat.S_IFDIR | 0o700 and d['uid'] == d['gid'] == 0 for d in directories.values()), 'kit-directory-metadata')
        launcher = w._read(LAUNCHER, budget, {0}, 128 * 1024 ** 2)
        require(launcher['sha256'] == expected['bin/recovery'] and launcher['identity']['mode'] == stat.S_IFREG | 0o755
                and launcher['identity']['gid'] == 0, 'launcher-differs')
        result.update(inventory=inventory, directories=directories, launcher=launcher)
    return result


def writer_expected(args, plan, worker_start_ticks, tick=lambda: None):
    """Immutable non-authorizing selected-checker hint; no publication required."""
    w.validate_plan(args, plan)
    require(type(worker_start_ticks) is int and worker_start_ticks > 1, 'worker-start-hint-invalid')
    budget = w.Budget(tick)
    tx = w.transaction(budget, allow_absent=True, hint=True)
    if tx is None:
        return None
    kit = selected(plan, budget)
    return {'operation_id': args.operation_id, 'token_sha256': tx['transaction_token_sha256'],
            'worker_start_ticks': worker_start_ticks, 'snapshot': tx['snapshot'],
            'checker_path': kit['checker_path'], 'checker_sha256': kit['checker']['sha256']}


def database_state(plan, tx, budget, uid, gid):
    base = w.MIGRATIONS / tx['transaction_token_sha256']
    authority, work = base / 'authority', base / 'work'
    admission, ap = record(authority / 'admission.json', ADMISSION, 'celikpanel/database-migration-admission/v1', budget)
    dirs = {'parent': w._directory(w.PARENT, {uid}, uid, gid, 0o750),
            'root': w._directory(w.MIGRATIONS, {uid}, 0, gid, 0o710),
            'transaction': w._directory(base, {uid}, 0, gid, 0o710),
            'authority': w._directory(authority, {uid}, 0, 0, 0o700)}
    for key, value in dirs.items():
        directory_value(admission[key]); require(admission[key] == value, 'admission-directory-differs')
    directory_value(admission['work'])
    require(admission['work']['uid'] == uid and admission['work']['gid'] == gid
            and admission['work']['mode'] == stat.S_IFDIR | 0o700, 'admitted-work-metadata')
    dirs['work'] = w._directory(work, {uid}, 0, 0, 0o700)
    require(dirs['work'] == dict(admission['work'], uid=0, gid=0), 'sealed-work-directory-differs')
    file_value(admission['before'], uid, gid); file_value(admission['initial'], uid, gid)
    snap = w.snapshot_proof(tx, plan['candidate'], budget)
    material = w.checkpoint.forward.material_proof(tx['snapshot'], tx['transaction_token_sha256'], plan['candidate']['manifest_sha256'], budget)
    expected = {'snapshot': tx['snapshot'], 'transaction_token_sha256': tx['transaction_token_sha256'],
                'candidate_panel_sha256': plan['candidate']['files']['bin/panel'],
                'material_sha256': material['record_sha256'], 'snapshot_manifest_sha256': snap['manifest_sha256']}
    require(all(admission[k] == v for k, v in expected.items())
            and material['schema'] == 'celikpanel/recovery-material/v3'
            and material['snapshot_manifest_sha256'] == snap['manifest_sha256'], 'admission-material-snapshot-differs')
    before = admission['before']; flat = {k: before['identity'][k] for k in ('dev', 'ino', 'mode', 'uid', 'gid', 'links', 'size')}
    for name in ('mtime', 'ctime'):
        flat[name + '_sec'] = before['identity'][name]['Sec']; flat[name + '_nsec'] = before['identity'][name]['Nsec']
    parent = dict(admission['parent'], links=0, size=0, mtime_sec=0, mtime_nsec=0, ctime_sec=0, ctime_nsec=0)
    require(material['database_before'] == {'file': flat, 'parent': parent, 'sha256': before['sha256'],
                                           'attribute_counts': {'attributes': 0, 'parent_attributes': 0}}
            and admission['initial']['sha256'] == snap['database']['sha256'], 'material-before-or-initial-differs')
    seal, sp = record(authority / 'seal.json', ['schema', 'admission_sha256', 'files'], 'celikpanel/database-migration-seal/v1', budget)
    require(seal['admission_sha256'] == ap['sha256'] and type(seal['files']) is dict
            and list(seal['files']) == sorted(seal['files']) and DB in seal['files']
            and set(seal['files']) <= {DB, DB + '-wal', DB + '-shm'}, 'seal-shape-or-link')
    require(w._names(work, {uid}, 4) == list(seal['files']), 'sealed-work-inventory')
    work_files = {}
    for n, value in seal['files'].items():
        file_value(value, uid, gid)
        p = w._read(work / n, budget, {uid})
        require(w.product_file(p) == value, 'sealed-work-changed'); work_files[n] = p
    pub, pp = record(authority / 'publication.json', PUBLICATION, 'celikpanel/database-publication-intent/v1', budget)
    require(pub['admission_sha256'] == ap['sha256'] and pub['seal_sha256'] == sp['sha256']
            and type(pub['build']) is str and BUILD.fullmatch(pub['build']) and pub['before'] == before, 'publication-link-differs')
    file_value(pub['before'], uid, gid); file_value(pub['after'], uid, gid); directory_value(pub['directory'])
    require(pub['after']['identity']['dev'] == before['identity']['dev'] == dirs['parent']['dev']
            and pub['after']['identity']['ino'] != before['identity']['ino'], 'publication-not-two-same-filesystem-inodes')
    build = authority / pub['build']; dirs['build'] = w._directory(build, {uid}, 0, 0, 0o700)
    require(dirs['build'] == pub['directory'] and dirs['build']['dev'] == dirs['parent']['dev'], 'build-directory-differs')
    require(w._names(build, {uid}, 2) == [DB], 'build-inventory-differs')
    # This first-exchange oracle is narrower than general product retry grammar.
    # Any receipt, partial record or extra build makes this checkpoint unknown.
    require(w._names(authority, {uid}, 64) == sorted(['admission.json', 'seal.json', 'publication.json', pub['build']]), 'receipts-present-or-authority-not-initial')
    for suffix in ('-wal', '-shm', '-journal'):
        try:
            w._read(w.PARENT / (DB + suffix), budget, {uid})
        except FileNotFoundError:
            continue
        raise Inconclusive('canonical-sidecar-present')
    canonical, staged = w._read(w.PARENT / DB, budget, {uid}), w._read(build / DB, budget, {uid})
    file_value(w.product_file(canonical), uid, gid); file_value(w.product_file(staged), uid, gid)
    return {'admission': admission, 'seal': seal, 'publication': pub,
            'records': {'admission.json': ap, 'seal.json': sp, 'publication.json': pp},
            'snapshot': snap, 'material': material, 'directories': dirs, 'work_files': work_files,
            'canonical': dict(canonical, path=str(w.PARENT / DB)), 'build': dict(staged, path=str(build / DB)),
            'receipts_absent': ['published.json', 'restoration.json', 'restored.json']}


def trace_shape(trace, stage):
    require(type(trace) is dict and set(trace) == {'tracer_pid', 'tasks', 'publisher', 'exchange'}
            and type(trace['tracer_pid']) is int and trace['tracer_pid'] == os.getpid(), 'exchange-trace-shape')
    p, e = trace['publisher'], trace['exchange']
    require(type(p) is dict and set(p) == {'pid', 'tid'} and all(type(n) is int and n > 1 for n in p.values()), 'publisher-shape')
    require(type(e) is dict and set(e) == {'number', 'old_dirfd', 'new_dirfd', 'old_name', 'new_name', 'flags', 'arch', 'stage', 'entry_exit_matched', 'result', 'is_error'}
            and all(type(e[k]) is int for k in ('number', 'old_dirfd', 'new_dirfd', 'flags', 'arch'))
            and e['number'] == 316 and e['arch'] == 0xc000003e and e['flags'] == 2
            and 0 <= e['old_dirfd'] < 1048576 and 0 <= e['new_dirfd'] < 1048576 and e['old_dirfd'] != e['new_dirfd']
            and e['old_name'] == e['new_name'] == DB and e['stage'] == stage and e['is_error'] is False
            and e['entry_exit_matched'] is (stage == 'exit')
            and (e['result'] is None if stage == 'entry' else type(e['result']) is int and e['result'] == 0), 'exchange-syscall-not-proved')


def publisher_proof(trace, worker, kit, state, budget, uid):
    p, e = trace['publisher'], trace['exchange']; pid, tid = p['pid'], p['tid']
    require(any(t['tid'] == tid and t['tgid'] == pid for t in trace['tasks']), 'publisher-not-admitted-task')
    status = w.proc_status(tid, budget, {0, uid}); root = w.PROC / str(pid)
    require(tuple(status['Uid'].split()) == ('0',) * 4 and tuple(status['Gid'].split()) == ('0',) * 4, 'publisher-not-root')
    command = (kit['checker_path'] + '\0--publish-update-database=' + state['admission']['snapshot'] + '\0').encode()
    require(w._virtual(root / 'cmdline', budget, {0}) == command, 'publisher-command-differs')
    raw = w._virtual(root / 'environ', budget, {0}); environment = {}
    require(raw.endswith(b'\0'), 'publisher-environment-malformed')
    for value in raw[:-1].split(b'\0'):
        key, sep, val = value.partition(b'=')
        require(sep and key not in environment, 'publisher-environment-malformed'); environment[key] = val
    require(environment == {k.encode(): v.encode() for k, v in ENV.items()}, 'publisher-environment-differs')
    require(w._virtual(root / 'cgroup', budget, {0}, 4096) == ('0::' + worker['cgroup'] + '\n').encode(), 'publisher-cgroup-differs')
    exe = w.executable(pid, budget, {0}, kit['checker']['sha256'])
    require(exe == kit['checker'], 'publisher-not-selected-checker-inode')
    descriptors = {}
    for key, path, want in [('old_dirfd', w.PARENT, state['directories']['parent']),
                            ('new_dirfd', Path(state['build']['path']).parent, state['directories']['build'])]:
        fd_path = w.PROC / str(tid) / 'fd' / str(e[key])
        # Only a kernel-owned FD link is traversed, after its fixed target matches.
        require(os.readlink(fd_path) == str(path), 'exchange-dirfd-target-differs')
        first = w.directory_id(fd_path.stat())
        require(first == want and w._directory(path, {0, uid}) == want
                and os.readlink(fd_path) == str(path) and w.directory_id(fd_path.stat()) == first, 'exchange-dirfd-identity-differs')
        descriptors[key] = {'fd': e[key], 'path': str(path), 'identity': first}
    return {'pid': pid, 'tid': tid, 'start_ticks': w.proc_start(pid, budget, {0}),
            'thread_start_ticks': w.proc_start(tid, budget, {0}), 'executable': exe, 'descriptors': descriptors}


def file_without_ctime(value):
    value = copy.deepcopy(value); value.pop('path', None); value['identity'].pop('ctime_ns')
    return value


def pair(state, stage, entry=None):
    canonical, build, publication = state['canonical'], state['build'], state['publication']
    if stage == 'entry':
        require(w.product_file(canonical) == publication['before'] and w.product_file(build) == publication['after'], 'preexchange-pair-differs')
    else:
        require(file_without_ctime(canonical) == file_without_ctime(entry['database']['build'])
                and file_without_ctime(build) == file_without_ctime(entry['database']['canonical']), 'postexchange-pair-differs')
        before, after = copy.deepcopy(entry['database']), copy.deepcopy(state)
        for value in (before, after):
            value.pop('canonical'); value.pop('build')
        require(before == after, 'exchange-authority-changed')


def _inspect(args, plan, worker_identity, trace, stage, entry, tick):
    guest = w.validate_plan(args, plan); trace_shape(trace, stage)
    budget = w.Budget(tick); uid, gid = w.account()
    worker = w.worker_proof(args, plan, worker_identity, budget, uid)
    # Adapt only field names: stopped_inventory verifies actual kernel tasks,
    # never reads writer/write or creates a fake freezer/stop result.
    stopped_trace = {'tracer_pid': trace['tracer_pid'], 'tasks': trace['tasks'], 'writer': trace['publisher'], 'write': trace['exchange']}
    tasks = w.stopped_inventory(worker, stopped_trace, budget, uid)
    locks = w.exclusive_lock(worker, tasks, budget, uid)
    tx = w.transaction(budget); kit = selected(plan, budget, full=True)
    state = database_state(plan, tx, budget, uid, gid)
    publisher = publisher_proof(trace, worker, kit, state, budget, uid)
    require(any(lock['pid'] == publisher['pid'] and lock['fd'] == 9 for lock in locks), 'publisher-fd9-lock-unproved')
    if stage == 'exit':
        require(type(entry) is dict and entry.get('schema') == SCHEMA and entry.get('status') == 'verified'
                and entry.get('classification') == 'database-exchange-entry-held'
                and entry.get('operation_id') == args.operation_id, 'entry-proof-unavailable')
        pair(entry['database'], 'entry')
        expected_exchange = dict(entry['exchange'], stage='exit', entry_exit_matched=True, result=0)
        require(trace['exchange'] == expected_exchange and entry['identity'] == guest
                and entry['worker'] == worker and entry['transaction'] == tx and entry['kit'] == kit
                and entry['publisher'] == publisher and entry['tasks'] == tasks
                and entry['exclusive_locks'] == locks, 'entry-exit-identity-differs')
    pair(state, stage, entry)
    require(database_state(plan, tx, budget, uid, gid) == state and selected(plan, budget, full=True) == kit
            and publisher_proof(trace, worker, kit, state, budget, uid) == publisher
            and w.transaction(budget) == tx and w.exclusive_lock(worker, tasks, budget, uid) == locks
            and w.stopped_inventory(worker, stopped_trace, budget, uid) == tasks
            and w.worker_proof(args, plan, worker_identity, budget, uid) == worker, 'final-exchange-proof-changed')
    result = {'schema': SCHEMA, 'status': 'verified', 'classification': 'database-exchange-entry-held' if stage == 'entry' else 'database-exchanged-before-receipt-held',
              'identity': guest, 'operation_id': args.operation_id, 'worker': worker, 'transaction': tx,
              'tasks': tasks, 'exclusive_locks': locks, 'kit': kit, 'publisher': publisher,
              'exchange': dict(trace['exchange']), 'trace_sha256': digest(trace), 'database': state, 'limits': list(LIMITS)}
    if stage == 'exit':
        result['entry_proof_sha256'] = digest(entry)
    return result


def inspect_entry(args, plan, worker, trace, *, tick=lambda: None):
    return _result(args, plan, worker, trace, 'entry', None, tick)


def inspect(args, plan, worker, trace, entry_proof, *, tick=lambda: None):
    return _result(args, plan, worker, trace, 'exit', entry_proof, tick)


def _result(args, plan, worker, trace, stage, entry, tick):
    try:
        return _inspect(args, plan, worker, trace, stage, entry, tick)
    except (OSError, ValueError, KeyError, TypeError, IndexError, AttributeError, w.probe.ProbeError,
            w.checkpoint.forward.kill.MissedCheckpoint) as error:
        reason = str(error) if isinstance(error, Inconclusive) and re.fullmatch(r'[a-z0-9-]{1,96}', str(error)) else type(error).__name__
        return {'schema': SCHEMA, 'status': 'inconclusive', 'reason': reason, 'limits': list(LIMITS)}
