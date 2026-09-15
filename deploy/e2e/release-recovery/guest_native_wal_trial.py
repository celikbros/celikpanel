#!/usr/bin/env python3
"""Registered disposable guest gate and unchanged-update WAL fault controller.

No installed-host or arbitrary executable/PID mode. The only gate surrounds the
unchanged bootstrap invocation. It does not alter product SQL, files or receipts.
"""
from __future__ import annotations
import argparse
import base64
import copy
import hashlib
import importlib.util
import json
import os
import re
from pathlib import Path
import signal
import stat
import subprocess
import sys
import time

HERE=Path(__file__).resolve().parent

def module(name, filename):
    spec=importlib.util.spec_from_file_location(name,HERE/filename)
    value=importlib.util.module_from_spec(spec);sys.modules[name]=value;spec.loader.exec_module(value);return value

local=module('native_wal_local','guest_local_candidate.py')
probe=local.probe
PRIVATE=local.PRIVATE
SCHEMA='celikpanel/native-wal-trial/v1'
REQUIRED_HELPERS = frozenset((
    'guest_database_checkpoint.py', 'baseline_profiles.py', 'candidate_archive.py',
    'guest_probe.py', 'guest_port_fault.py', 'guest_update_kill.py',
    'guest_local_candidate.py', 'guest_recovery_fault.py', 'guest_recovery_handoff.py',
    'guest_candidate_data_fault.py', 'guest_forward_completion_fault.py',
    'guest_native_wal_trial.py', 'guest_wal_checkpoint.py', 'wal_frames.py',
    'wal_migration_identity.py', 'native_trace.py', 'trace_fixture.py',
))


RECOVERY_BOUNDARY = 'database-exchange-recovery-reboot'
BOUNDARIES = ('wal', 'database-exchange', RECOVERY_BOUNDARY)
EXCHANGE_HELPERS = REQUIRED_HELPERS | {'guest_exchange_checkpoint.py', 'exchange_trace.py'}


def profile(boundary='wal'):
    if boundary == 'wal':
        return {'prefix': 'native-wal', 'schema': SCHEMA,
                'fault': 'physical-uncommitted-wal-after-successful-native-pwrite64',
                'gate_schema': 'celikpanel/native-wal-gate/v1',
                'release_schema': 'celikpanel/native-wal-gate-release/v1',
                'tracer_unit': 'celikpanel-lab-wal-trace-', 'helpers': REQUIRED_HELPERS}
    if boundary == 'database-exchange':
        return {'prefix': 'native-database-exchange',
                'schema': 'celikpanel/native-database-exchange-trial/v1',
                'fault': 'database-exchange-after-successful-native-renameat2-before-receipt',
                'gate_schema': 'celikpanel/native-database-exchange-gate/v1',
                'release_schema': 'celikpanel/native-database-exchange-gate-release/v1',
                'tracer_unit': 'celikpanel-lab-database-exchange-trace-', 'helpers': EXCHANGE_HELPERS}
    if boundary == RECOVERY_BOUNDARY:
        value = profile('database-exchange')
        value.update(prefix='native-exchange-recovery-reboot',
                     schema='celikpanel/native-exchange-recovery-reboot-trial/v1',
                     fault='native-database-exchange-kill-then-payload-restored-reboot',
                     gate_schema='celikpanel/native-exchange-recovery-reboot-gate/v1',
                     release_schema='celikpanel/native-exchange-recovery-reboot-gate-release/v1',
                     tracer_unit='celikpanel-lab-exchange-recovery-trace-',
                     helpers=EXCHANGE_HELPERS | {'guest_exchange_recovery_handoff.py'},
                     recovery_fault={'action': 'reboot', 'checkpoint': 'payload_restored'})
        return value
    raise ValueError('unsupported native boundary')


def boundary_of(args):
    boundary = getattr(args, 'boundary', 'wal')
    profile(boundary)
    return boundary


def validate_helpers(helpers, boundary='wal'):
    if not isinstance(helpers, dict) or set(helpers) != profile(boundary)['helpers']:
        raise ValueError('native helper inventory is incomplete or unexpected')
    if any(not isinstance(digest, str) or not local.shared.HEX64.fullmatch(digest)
           for digest in helpers.values()):
        raise ValueError('native helper digest is malformed')
    return helpers


def names(operation, boundary='wal'):
    local.names(operation)
    selected = profile(boundary)
    return {'worker':'celikpanel-self-update-'+operation+'.service',
            'tracer':selected['tracer_unit']+operation+'.service',
            **{key:PRIVATE/(selected['prefix']+'-'+operation+suffix) for key,suffix in
               (('intent','.json'),('gate','.gate.json'),('release','.release.json'),
                ('result','.result.json'),('events','.events.jsonl'))}}


def private_raw(path):
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK|os.O_CLOEXEC)
    try:
        info=os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_uid!=0 or info.st_gid!=0 or info.st_nlink!=1 or stat.S_IMODE(info.st_mode)!=0o600 or info.st_size>16*1024*1024:
            raise ValueError('private record metadata differs')
        raw=os.read(fd,16*1024*1024+1)
        if len(raw)!=info.st_size or file_identity(os.fstat(fd))!=file_identity(info) or file_identity(path.lstat())!=file_identity(info):
            raise ValueError('private record changed')
        return raw
    finally:os.close(fd)


def private_record(path):
    raw = private_raw(path)
    return probe.strict_object(raw), raw


def private_json(path):
    return private_record(path)[0]


def load(args):
    boundary=boundary_of(args);selected=profile(boundary)
    identity=probe.guard_guest(args);paths=names(args.operation_id,boundary)
    local.trusted_chain(PRIVATE)
    if stat.S_IMODE(PRIVATE.lstat().st_mode)!=0o700:raise ValueError('private root mode differs')
    value=private_json(paths['intent'])
    if (value.get('schema')!=selected['schema'] or value.get('identity')!=identity
        or value.get('operation_id')!=args.operation_id
        or value.get('fault')!=selected['fault']
        or value.get('provenance')!='unpublished-local-build-not-signed-agent-admission'):
        raise ValueError('native boundary intent differs')
    plan,plan_raw=private_record(local.names(args.operation_id)['plan'])
    local.validate_plan(plan,identity,args.operation_id)
    if plan.get('recovery_fault') != selected.get('recovery_fault'):
        raise ValueError('native recovery fault differs from fixed profile')
    if hashlib.sha256(plan_raw).hexdigest()!=value['local_intent_sha256']:
        raise ValueError('local intent changed')
    seeded=value['populated_seed']
    if not local.shared.HEX32.fullmatch(seeded['seed_id']):raise ValueError('seed identity malformed')
    seedroot=PRIVATE/('populated-baseline-'+seeded['seed_id'])
    installed=private_json(seedroot/'installed.json');manifest,manifest_raw=private_record(seedroot/'manifest.json')
    if installed!=seeded['installed'] or installed.get('identity')!=identity or hashlib.sha256(manifest_raw).hexdigest()!=seeded['manifest_sha256']:
        raise ValueError('populated seed proof changed')
    for filename,digest in validate_helpers(value.get('helpers'),boundary).items():
        if Path(filename).name!=filename:raise ValueError('helper path differs')
        p=PRIVATE/filename
        fd=os.open(p,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK|os.O_CLOEXEC)
        try:
            info=os.fstat(fd)
            if not stat.S_ISREG(info.st_mode) or info.st_uid!=0 or info.st_gid!=0 or info.st_nlink!=1 or stat.S_IMODE(info.st_mode)!=0o600 or info.st_size>1048576:
                raise ValueError('helper metadata differs')
            raw=os.read(fd,1048577)
            if (len(raw)!=info.st_size or hashlib.sha256(raw).hexdigest()!=digest or file_identity(os.fstat(fd))!=file_identity(info)
                or file_identity(p.lstat())!=file_identity(info)):raise ValueError('helper bytes differ')
        finally:os.close(fd)
    return value,plan,paths


def executable(pid):
    fd=os.open('/proc/'+str(pid)+'/exe',os.O_RDONLY|os.O_CLOEXEC)
    try:
        info=os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or not 0<info.st_size<=32*1024*1024:raise ValueError('executable unavailable')
        digest=hashlib.sha256()
        while True:
            raw=os.read(fd,1048576)
            if not raw:break
            digest.update(raw)
        return {'sha256':digest.hexdigest(),'device':info.st_dev,'inode':info.st_ino}
    finally:os.close(fd)


def gate(args,value,plan,paths):
    selected=profile(boundary_of(args))
    if paths['release'].exists() or paths['result'].exists():raise ValueError('prior release or result exists')
    native=local.LocalNative(args,plan,private_json(local.names(args.operation_id)['proof']))
    native.unit=paths['worker'];props=native.properties();pid=os.getpid()
    if props.get('MainPID')!=str(pid) or props.get('ControlGroup')!='/system.slice/'+paths['worker']:
        raise ValueError('gate is not exact registered systemd MainPID')
    start=local.kill.process_start(Path('/proc/self/stat').read_text())
    ex=executable(pid)
    proof={'schema':selected['gate_schema'],'identity':value['identity'],'operation_id':args.operation_id,
           'pid':pid,'start_ticks':start,'boot_id':Path('/proc/sys/kernel/random/boot_id').read_text().strip(),
           'unit':paths['worker'],'invocation_id':props['InvocationID'],'executable':ex}
    local.save_private(paths['gate'],proof)
    deadline=time.monotonic()+90
    while time.monotonic()<deadline:
        if paths['release'].exists():
            permit=private_json(paths['release'])
            if permit!={'schema':selected['release_schema'],'operation_id':args.operation_id,'gate':proof}:
                raise ValueError('gate release differs')
            probe.guard_guest(args)
            if hashlib.sha256(Path(plan['source_root'],'bootstrap-prebuilt-update.sh').read_bytes()).hexdigest()!=plan['candidate']['files']['bootstrap-prebuilt-update.sh']:
                raise ValueError('bootstrap changed')
            if os.environ.get('INVOCATION_ID')!=props['InvocationID']:raise ValueError('gate invocation environment differs')
            os.execve('/bin/bash',['/bin/bash',plan['source_root']+'/bootstrap-prebuilt-update.sh','--normal'],
                      {'INVOCATION_ID':os.environ['INVOCATION_ID'],'PATH':'/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin','HOME':'/root','LANG':'C.UTF-8'})
        time.sleep(.025)
    raise TimeoutError('native gate expired without release')


def file_identity(info):
    return {'dev':info.st_dev,'ino':info.st_ino,'mode':info.st_mode,'uid':info.st_uid,'gid':info.st_gid,
            'links':info.st_nlink,'size':info.st_size,'mtime_ns':info.st_mtime_ns,'ctime_ns':info.st_ctime_ns}


def capture_checkpoint(operation,proof,prior_wal):
    token=proof['transaction']['transaction_token_sha256']
    if not local.shared.HEX64.fullmatch(token):raise ValueError('capture token malformed')
    target=PRIVATE/('native-wal-'+operation+'-capture')
    work=Path('/var/lib/celikpanel/.release-db-migrations')/token/'work'
    expected=proof['database']['work_files']
    if not {'celikpanel.db','celikpanel.db-wal'}<=expected.keys() or not expected.keys()<={'celikpanel.db','celikpanel.db-wal','celikpanel.db-shm'}:
        raise ValueError('capture inventory differs')
    source=os.open(work,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW|os.O_CLOEXEC)
    try:
        directory={k:v for k,v in file_identity(os.fstat(source)).items() if k in ('dev','ino','mode','uid','gid')}
        if directory!=proof['database']['work_directory']:raise ValueError('capture work directory changed')
        target.mkdir(mode=0o700)
        captured={}
        for name,want in expected.items():
            fd=os.open(name,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK|os.O_CLOEXEC,dir_fd=source)
            try:
                info=os.fstat(fd)
                if file_identity(info)!=want['identity'] or not stat.S_ISREG(info.st_mode) or info.st_nlink!=1 or info.st_size>256*1024*1024:
                    raise ValueError('capture input changed')
                out=os.open(target/name,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
                digest=hashlib.sha256();size=0
                with os.fdopen(out,'wb') as stream:
                    while True:
                        raw=os.read(fd,1048576)
                        if not raw:break
                        size+=len(raw)
                        if size>info.st_size:raise ValueError('capture grew')
                        stream.write(raw);digest.update(raw)
                    stream.flush();os.fsync(stream.fileno())
                if (size!=info.st_size or digest.hexdigest()!=want['sha256'] or file_identity(os.fstat(fd))!=want['identity']
                    or file_identity(os.stat(name,dir_fd=source,follow_symlinks=False))!=want['identity']):raise ValueError('capture input changed after copy')
                captured[name]={'sha256':digest.hexdigest(),'bytes':size,'source':want}
            finally:os.close(fd)
        out=os.open(target/'prior.wal',os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
        with os.fdopen(out,'wb') as stream:stream.write(prior_wal);stream.flush();os.fsync(stream.fileno())
        answer={'schema':'celikpanel/native-wal-raw-capture/v1','operation_id':operation,'directory':str(target),
                'files':captured,'prior_wal_sha256':hashlib.sha256(prior_wal).hexdigest(),'scope':'copies-only-no-SQLite-open-on-source'}
        local.save_private(target/'capture.json',answer)
        directoryfd=os.open(target,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
        try:os.fsync(directoryfd)
        finally:os.close(directoryfd)
        return answer
    finally:os.close(source)



DATABASE_PARENT = Path('/var/lib/celikpanel')


def capture_exchange_checkpoint(operation, proof, *, boundary='database-exchange'):
    """Copy only the exact observed pair. Never open a source with SQLite."""
    local.names(operation)
    token = proof['transaction']['transaction_token_sha256']
    if not isinstance(token, str) or not local.shared.HEX64.fullmatch(token):
        raise ValueError('capture token malformed')
    database = proof['database']
    authority = DATABASE_PARENT / '.release-db-migrations' / token / 'authority'
    build = Path(database['build']['path'])
    if (build.name != 'celikpanel.db' or build.parent.parent != authority
        or not re.fullmatch(r'\.build-[0-9a-f]{32}', build.parent.name)
        or database['canonical']['path'] != str(DATABASE_PARENT / 'celikpanel.db')):
        raise ValueError('exchange capture paths differ')
    if boundary not in ('database-exchange', RECOVERY_BOUNDARY):
        raise ValueError('exchange capture profile differs')
    target = PRIVATE / (profile(boundary)['prefix'] + '-' + operation + '-capture')
    handles = []
    pins = []
    def directory(path, expected):
        parent = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
        handles.append(parent)
        for part in path.parts[1:]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent)
            handles.append(child)
            info = os.fstat(child)
            if not stat.S_ISDIR(info.st_mode) or info.st_mode & 0o022:
                raise ValueError('exchange capture directory is unsafe')
            pins.append((parent, part, child, file_identity(info)))
            parent = child
        observed = {k: v for k, v in file_identity(os.fstat(parent)).items() if k in ('dev', 'ino', 'mode', 'uid', 'gid')}
        if observed != expected:
            raise ValueError('exchange capture directory changed')
        return parent
    try:
        sources = {
            'canonical.db': (directory(DATABASE_PARENT, database['directories']['parent']), database['canonical']),
            'retained-before.db': (directory(build.parent, database['directories']['build']), database['build']),
        }
        target.mkdir(mode=0o700)
        captured = {}
        for name, (parent, want) in sources.items():
            fd = os.open('celikpanel.db', os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=parent)
            try:
                info = os.fstat(fd)
                if (file_identity(info) != want['identity'] or not stat.S_ISREG(info.st_mode)
                    or info.st_nlink != 1 or not 0 < info.st_size <= 256 * 1024 * 1024):
                    raise ValueError('exchange capture input changed')
                out = os.open(target / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
                digest = hashlib.sha256()
                size = 0
                with os.fdopen(out, 'wb') as stream:
                    while True:
                        raw = os.read(fd, 1048576)
                        if not raw:
                            break
                        size += len(raw)
                        if size > info.st_size:
                            raise ValueError('exchange capture grew')
                        stream.write(raw)
                        digest.update(raw)
                    stream.flush()
                    os.fsync(stream.fileno())
                if (size != info.st_size or digest.hexdigest() != want['sha256']
                    or file_identity(os.fstat(fd)) != want['identity']
                    or file_identity(os.stat('celikpanel.db', dir_fd=parent, follow_symlinks=False)) != want['identity']):
                    raise ValueError('exchange capture input changed after copy')
                captured[name] = {'sha256': digest.hexdigest(), 'bytes': size, 'source': copy.deepcopy(want)}
            finally:
                os.close(fd)
        # A first input must still be the same pair member after copying the second.
        for parent, want in sources.values():
            fd = os.open('celikpanel.db', os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=parent)
            try:
                if file_identity(os.fstat(fd)) != want['identity']:
                    raise ValueError('exchange capture pair changed')
                digest = hashlib.sha256()
                size = 0
                while True:
                    raw = os.read(fd, 1048576)
                    if not raw:
                        break
                    size += len(raw)
                    if size > want['identity']['size']:
                        raise ValueError('exchange capture pair grew')
                    digest.update(raw)
                if (size != want['identity']['size'] or digest.hexdigest() != want['sha256']
                    or file_identity(os.fstat(fd)) != want['identity']
                    or file_identity(os.stat('celikpanel.db', dir_fd=parent, follow_symlinks=False)) != want['identity']):
                    raise ValueError('exchange capture pair changed after copy')
            finally:
                os.close(fd)
        for parent, name, child, before in pins:
            if (file_identity(os.fstat(child)) != before
                or file_identity(os.stat(name, dir_fd=parent, follow_symlinks=False)) != before):
                raise ValueError('exchange capture ancestor changed')
        answer = {'schema': 'celikpanel/native-database-exchange-raw-capture/v1',
                  'operation_id': operation, 'directory': str(target), 'files': captured,
                  'scope': 'copies-only-no-SQLite-open-on-source'}
        local.save_private(target / 'capture.json', answer)
        fd = os.open(target, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
        return answer
    finally:
        for fd in reversed(handles):
            os.close(fd)


def make_callbacks(args, value, plan, paths, native, gateproof, tracer, checkpoint, emit, trace_fd):
    boundary = boundary_of(args)
    selected = profile(boundary)
    prefix = selected['prefix'] + '-' + args.operation_id
    class Callbacks:
        def record(self,event):
            raw=(json.dumps(event,sort_keys=True)+'\n').encode()
            written=os.write(trace_fd,raw)
            if written!=len(raw):raise OSError('short trace evidence write')
            os.fsync(trace_fd)
        def revalidate(self,stage,proof):
            if probe.guard_guest(args)!=value['identity']:raise ValueError('guest identity changed')
            props=native.properties()
            if props.get('MainPID')!=str(gateproof['pid']) or props.get('InvocationID')!=gateproof['invocation_id'] or props.get('ControlGroup')!='/system.slice/'+paths['worker']:
                raise ValueError('native worker changed')
            if local.kill.process_start(Path('/proc',str(gateproof['pid']),'stat').read_text())!=gateproof['start_ticks']:
                raise ValueError('worker start changed')
        def release_start_gate(self,proof):
            self.revalidate('release',proof)
            if executable(gateproof['pid'])!=gateproof['executable']:raise ValueError('gate executable changed')
            local.save_private(paths['release'],{'schema':selected['release_schema'],'operation_id':args.operation_id,'gate':gateproof})
            emit('gate_released',gate=gateproof)
        def writer_expected(self):
            return checkpoint.writer_expected(args,plan,int(gateproof['start_ticks']))
        def exchange_entry(self, trace_snapshot):
            if boundary == 'wal':
                raise ValueError('exchange callback is unavailable in WAL profile')
            self.revalidate('exchange-entry', trace_snapshot)
            proof = checkpoint.inspect_entry(args, plan, native.worker_identity(), trace_snapshot)
            if (proof.get('status') != 'verified' or proof.get('operation_id') != args.operation_id
                or proof.get('trace_sha256') != tracer.proof_digest(trace_snapshot)):
                local.save_private(PRIVATE / (prefix + '.entry-checkpoint-refused.json'), proof)
                raise ValueError('database exchange entry checkpoint not verified')
            self.entry_proof = copy.deepcopy(proof)
            emit('database_exchange_entry_verified', checkpoint=proof, trace=trace_snapshot)
            return proof
        def authorize_cut(self,trace_snapshot,prior_proof):
            self.revalidate('authorize',trace_snapshot)
            worker=native.worker_identity()
            if boundary == 'wal':
                proof=checkpoint.inspect(args,plan,worker,trace_snapshot,prior_wal=prior_proof)
            else:
                if not hasattr(self, 'entry_proof') or prior_proof != self.entry_proof:
                    raise ValueError('database exchange entry proof changed')
                proof=checkpoint.inspect(args,plan,worker,trace_snapshot,prior_proof)
            if proof.get('status')!='verified':
                local.save_private(PRIVATE/(prefix+'.checkpoint-refused.json'),proof)
                raise ValueError('native checkpoint not verified')
            if boundary != 'wal' and (
                proof.get('operation_id') != args.operation_id
                or proof.get('trace_sha256') != tracer.proof_digest(trace_snapshot)
                or proof.get('entry_proof_sha256') != tracer.proof_digest(prior_proof)
                or prior_proof != self.entry_proof):
                raise ValueError('database exchange proof binding changed')
            self.cut_trace=copy.deepcopy(trace_snapshot)
            self.cut_prior=copy.deepcopy(prior_proof)
            self.cut_base=copy.deepcopy(proof)
            if boundary == 'wal':
                proof['capture']=capture_checkpoint(args.operation_id,proof,prior_proof)
                proof.update({'status':'verified','operation_id':args.operation_id,'trace_sha256':tracer.proof_digest(trace_snapshot),'prior_wal_sha256':hashlib.sha256(prior_proof).hexdigest()})
                event='wal_checkpoint_verified'
            else:
                proof['capture']=capture_exchange_checkpoint(args.operation_id,proof,boundary=boundary)
                event='database_exchange_checkpoint_verified'
            emit(event,checkpoint=proof,trace=trace_snapshot)
            return proof
        def perform_cut(self,verified_proof):
            self.recovery_handoff = None
            try:
                return self.perform_exact_cut(verified_proof)
            except BaseException:
                if self.recovery_handoff is not None:
                    self.recovery_handoff_cleanup = self.recovery_helper.cancel(args.operation_id, self.recovery_handoff)
                raise
        def perform_exact_cut(self,verified_proof):
            self.revalidate('cut',verified_proof)
            if boundary == 'wal':
                latest=checkpoint.inspect(args,plan,native.worker_identity(),self.cut_trace,prior_wal=self.cut_prior)
            else:
                if self.cut_prior != self.entry_proof:
                    raise ValueError('database exchange entry proof changed before kill')
                latest=checkpoint.inspect(args,plan,native.worker_identity(),self.cut_trace,self.cut_prior)
            if latest!=self.cut_base:raise ValueError('final checkpoint authority changed')
            self.revalidate('final-cut',verified_proof)
            if boundary == RECOVERY_BOUNDARY:
                helper = module('exchange_recovery_handoff', 'guest_exchange_recovery_handoff.py')
                def exact():
                    self.revalidate('recovery-handoff', verified_proof)
                    now = checkpoint.inspect(args, plan, native.worker_identity(), self.cut_trace, self.cut_prior)
                    if now != self.cut_base:
                        raise ValueError('exchange proof changed during recovery handoff')
                self.recovery_helper = helper
                handoff = helper.arm(args, plan, native.worker_identity(), self.cut_base, exact)
                self.recovery_handoff = handoff
                emit('recovery_fault_armed', handoff=handoff)
                exact()
            emit('kill_requested',scope='exact-update-unit-cgroup',signal='SIGKILL')
            native.kill()
            receipt={'status':'cut-sent','trace_sha256':verified_proof['trace_sha256'],'scope':'exact-update-unit-cgroup','signal':'SIGKILL','operation_id':args.operation_id}
            if boundary != 'wal':receipt['entry_proof_sha256']=tracer.proof_digest(self.entry_proof)
            emit('kill_sent',receipt=receipt)
            return receipt
    return Callbacks()


def trace(args,value,plan,paths):
    boundary=boundary_of(args);selected=profile(boundary)
    tracer_name='native_trace.py' if boundary=='wal' else 'exchange_trace.py'
    tracer_path=tracer_name if (HERE/tracer_name).exists() else 'waltrace/'+tracer_name
    tracer=module('native_boundary_tracer',tracer_path)
    checkpoint=module('native_boundary_checkpoint','guest_wal_checkpoint.py' if boundary=='wal' else 'guest_exchange_checkpoint.py')
    if paths['events'].exists() or paths['result'].exists():raise ValueError('trace was already attempted')
    fd=os.open(paths['events'],os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
    with os.fdopen(fd,'w') as stream:
        def emit(event,**fields):
            stream.write(json.dumps({'schema':selected['schema'],'event':event,'identity':value['identity'],
                'operation_id':args.operation_id,'at':probe.utc_now(),**fields},sort_keys=True)+'\n')
            stream.flush();os.fsync(stream.fileno())
        emit('armed')
        deadline=time.monotonic()+60
        while not paths['gate'].exists():
            if time.monotonic()>=deadline:raise TimeoutError('gate did not appear')
            time.sleep(.025)
        gateproof=private_json(paths['gate'])
        if (gateproof.get('schema') != selected['gate_schema'] or gateproof.get('identity')!=value['identity']
            or gateproof.get('operation_id')!=args.operation_id or gateproof.get('unit')!=paths['worker']):
            raise ValueError('gate identity differs')
        native=local.LocalNative(args,plan,private_json(local.names(args.operation_id)['proof']))
        native.unit=paths['worker']
        registration={'operation_id':args.operation_id,'unit':paths['worker'],'worker_pid':gateproof['pid'],
            'worker_start_ticks':int(gateproof['start_ticks']),'boot_id':gateproof['boot_id'],
            'gate_executable_sha256':gateproof['executable']['sha256'],
            'gate_executable_device':gateproof['executable']['device'],'gate_executable_inode':gateproof['executable']['inode']}
        trace_fd=os.open(PRIVATE/(selected['prefix']+'-'+args.operation_id+'.trace.jsonl'),os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
        callbacks=make_callbacks(args,value,plan,paths,native,gateproof,tracer,checkpoint,emit,trace_fd)
        driver=tracer.trace_native if boundary=='wal' else tracer.trace_database_publication
        try:result=driver(registration,callbacks,timeout=600)
        except Exception as exc:
            result={'status':'inconclusive','reason':'native-trace-'+type(exc).__name__}
        finally:os.close(trace_fd)
        if hasattr(callbacks, 'recovery_handoff_cleanup'):
            result['recovery_handoff_cleanup'] = callbacks.recovery_handoff_cleanup
        local.save_private(paths['result'],result)
        emit('trace_finished',result=result)
        return 0 if result.get('status')=='cut-sent' else 2


def collect(args,value,plan,paths):
    selected=profile(boundary_of(args))
    answer={'schema':selected['schema'],'identity':value['identity'],'operation_id':args.operation_id,'states':{}}
    for unit in (paths['worker'],paths['tracer'],'celikpanel-release-recovery.service','celikpanel-agent.service','celikpanel-panel.service'):
        r=subprocess.run(['/usr/bin/systemctl','show',unit,'-p','Id','-p','LoadState','-p','ActiveState','-p','SubState','-p','MainPID','-p','Result','-p','InvocationID','-p','OnFailure'],capture_output=True,text=True,timeout=5,env=local.kill.ENV)
        answer['states'][unit]=dict(l.split('=',1) for l in r.stdout.splitlines() if '=' in l)
    for key in ('gate','release','result'):
        answer[key]=private_json(paths[key]) if paths[key].exists() else None
    if paths['events'].exists():
        raw=private_raw(paths['events'])
        answer['events_base64']=base64.b64encode(raw).decode()
    else:answer['events_base64']=''
    answer['transaction']=local.shared.transaction()
    print(json.dumps(answer,sort_keys=True))


def main(argv=None):
    parser=argparse.ArgumentParser(description=__doc__)
    for key in ('lab-nonce','vm-uuid','cell-id','node','operation-id'):parser.add_argument('--'+key,required=True)
    parser.add_argument('--mode',choices=('gate','trace','collect'),required=True)
    parser.add_argument('--boundary',choices=BOUNDARIES,default='wal')
    args=parser.parse_args(argv);value,plan,paths=load(args)
    return {'gate':gate,'trace':trace,'collect':collect}[args.mode](args,value,plan,paths)

if __name__=='__main__':
    try:sys.exit(main())
    except (OSError,ValueError,TimeoutError,probe.ProbeError) as exc:
        print('native WAL fixture refused: '+type(exc).__name__,file=sys.stderr);sys.exit(2)
