#!/usr/bin/env python3
"""Read-only supplemental DB evidence inside an already frozen disposable update.

No CLI, path override, fixture authority, SQLite writes or product branch changes.
Incomplete observations stay unavailable; an untouched initial image is a state
observation, not proof that a migration transaction never started.
"""
from __future__ import annotations
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import sqlite3
import stat
import sys
import time

HERE=Path(__file__).resolve().parent

def module(name,filename):
    spec=importlib.util.spec_from_file_location(name,HERE/filename)
    value=importlib.util.module_from_spec(spec);sys.modules[name]=value;spec.loader.exec_module(value);return value

forward=module('database_checkpoint_forward','guest_forward_completion_fault.py')
probe=forward.probe;fault=forward.data.fault
profiles=module('checkpoint_baseline_profiles','baseline_profiles.py')
PARENT=Path('/var/lib/celikpanel')
ROOT=PARENT/'.release-db-migrations'
SNAPSHOTS=forward.shared.SNAPSHOTS
SCHEMA='celikpanel/lab-database-checkpoint/v1'
RECORDS=('admission.json','seal.json','publication.json','published.json','restoration.json','restored.json')
SIDECARS=('-wal','-shm','-journal')
HEX=re.compile(r'[0-9a-f]{64}\Z')

class Unavailable(ValueError):pass

def identity(info):
    return {'dev':info.st_dev,'ino':info.st_ino,'mode':info.st_mode,'uid':info.st_uid,'gid':info.st_gid,
            'links':info.st_nlink,'size':info.st_size,
            'mtime':{'Sec':info.st_mtime_ns//1000000000,'Nsec':info.st_mtime_ns%1000000000},
            'ctime':{'Sec':info.st_ctime_ns//1000000000,'Nsec':info.st_ctime_ns%1000000000}}

def parent_chain(path):
    result={}
    for parent in reversed(path.parents):
        allowed_panel=(parent==PARENT or (parent.name=='work' and parent.parent.parent==ROOT and HEX.fullmatch(parent.parent.name)))
        owners=probe.database_owners() if allowed_panel else {0}
        info=parent.lstat()
        if stat.S_ISDIR(info.st_mode) and info.st_uid==0 and stat.S_IMODE(info.st_mode)&stat.S_ISVTX:
            result[parent]=(info.st_dev,info.st_ino,info.st_uid,info.st_gid,stat.S_IMODE(info.st_mode))
        else:result[parent]=probe.protected_identity(parent,directory=True,owners=owners)
    return result

def directory(path,owners):
    parent_chain(path)
    info=path.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid not in owners or stat.S_IMODE(info.st_mode)&0o022:
        raise Unavailable('unsafe-directory')
    return {key:identity(info)[key] for key in ('dev','ino','mode','uid','gid')}

def file_proof(path,tick,owners,limit=256*1024*1024,raw=False):
    tick();parents=parent_chain(path)
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK|os.O_CLOEXEC)
    with os.fdopen(fd,'rb') as f:
        before=os.fstat(f.fileno())
        if (not stat.S_ISREG(before.st_mode) or before.st_uid not in owners or before.st_nlink!=1
                or stat.S_IMODE(before.st_mode)&0o022 or before.st_size>limit):raise Unavailable('unsafe-file')
        digest=hashlib.sha256();chunks=[];total=0
        while chunk:=f.read(1048576):
            tick();total+=len(chunk)
            if total>limit:raise Unavailable('file-bound')
            digest.update(chunk)
            if raw:chunks.append(chunk)
        if identity(os.fstat(f.fileno()))!=identity(before) or identity(path.lstat())!=identity(before) or parent_chain(path)!=parents:
            raise Unavailable('file-changed')
    proof={'identity':identity(before),'sha256':digest.hexdigest()}
    return (proof,b''.join(chunks)) if raw else proof

def optional(path,tick,owners,limit=256*1024*1024,raw=False):
    try:return file_proof(path,tick,owners,limit,raw)
    except FileNotFoundError:return None

def inventory(root,tick,owners):
    before=directory(root,owners);names=sorted(p.name for p in root.iterdir())
    if len(names)>64:raise Unavailable('inventory-bound')
    out={}
    for name in names:
        tick();path=root/name
        if not (name in ('celikpanel.db',*[f'celikpanel.db{s}' for s in SIDECARS])):
            raise Unavailable('unexpected-work-entry')
        out[name]=file_proof(path,tick,owners)
    if directory(root,owners)!=before or names!=sorted(p.name for p in root.iterdir()):raise Unavailable('inventory-changed')
    return before,out

def build_inventory(path,tick,owners):
    # Publication staging directory remains root-private; normalized/retired DB
    # payloads legitimately belong to the panel account inside that directory.
    before=directory(path,{0})
    if before['gid']!=0 or stat.S_IMODE(before['mode'])!=0o700:
        raise Unavailable('unsafe-build-directory')
    value=inventory(path,tick,owners)
    if value[0]!=before or directory(path,{0})!=before:
        raise Unavailable('build-directory-changed')
    return value

def history(path,files,migrations,tick):
    # Immutable only when every sidecar is absent. Never pretend main-file-only
    # reads represent WAL state or let SQLite touch the frozen evidence.
    if any('celikpanel.db'+suffix in files for suffix in SIDECARS):
        return {'status':'unavailable','reason':'sidecar-requires-consistent-WAL-observation'}
    expected={row['filename']:row['sha256'] for row in migrations['migrations']}
    connection=None
    try:
        tick();deadline=time.monotonic()+2
        connection=sqlite3.connect(path.as_uri()+'?mode=ro&immutable=1',uri=True,timeout=0,isolation_level=None)
        connection.setlimit(sqlite3.SQLITE_LIMIT_LENGTH,1048576)
        connection.set_progress_handler(lambda:int(time.monotonic()>=deadline),1000)
        connection.execute('PRAGMA query_only=ON');connection.execute('PRAGMA trusted_schema=OFF')
        rows=[]
        for version,name,digest in connection.execute('SELECT version,filename,sha256 FROM schema_migrations ORDER BY version LIMIT 1001'):
            if (type(version)is not int or version!=len(rows)+1 or not isinstance(name,str)
                    or not re.fullmatch(r'[0-9]{3}_[a-z0-9_]+\.sql',name) or int(name[:3])!=version
                    or not isinstance(digest,str) or not HEX.fullmatch(digest)
                    or expected.get(name)!=digest):raise Unavailable('migration-identity-differs')
            rows.append({'version':version,'filename':name,'sha256':digest})
        if not rows or len(rows)>1000:raise Unavailable('migration-history-bound')
        tick();return {'status':'observed','read_mode':'standalone-immutable-read-only','schema_version':len(rows),'migrations':rows,
                       'migration_identities_sha256':hashlib.sha256(json.dumps(rows,sort_keys=True,separators=(',',':')).encode()).hexdigest()}
    except (sqlite3.Error,Unavailable):return {'status':'unavailable','reason':'migration-history-not-verified'}
    finally:
        if connection is not None:connection.close()

def inspect(snapshot_proof,plan,transaction,tick):
    migrations=profiles.validate_candidate_migrations(plan.get('candidate_migration_identities'),plan['candidate'])
    snapshot=snapshot_proof['snapshot'];token=transaction['transaction_token_sha256']
    if (transaction['snapshot']!=snapshot or transaction['transaction_operation']!='update'
            or transaction['transaction_phase'] not in ('active','completion.pending') or not HEX.fullmatch(token)):
        raise Unavailable('transaction-tuple-differs')
    material=forward.material_proof(snapshot,token,plan['candidate']['manifest_sha256'],tick)
    if material['schema']!='celikpanel/recovery-material/v3' or material['snapshot_manifest_sha256']!=snapshot_proof['manifest_sha256']:
        raise Unavailable('database-material-v3-not-verified')
    owners=probe.database_owners();base=ROOT/token;authority=base/'authority';work=base/'work'
    dirs={str(p):directory(p,owners if p==PARENT else {0}) for p in (PARENT,ROOT,base,authority)}
    names=sorted(p.name for p in authority.iterdir())
    if len(names)>64 or any(n not in RECORDS and not re.fullmatch(r'\.record-[0-9a-f]{32}',n) and not re.fullmatch(r'\.build-[0-9a-f]{32}',n) for n in names):
        raise Unavailable('authority-inventory-unknown')
    records={};parsed={};temporary={}
    for name in names:
        if name.startswith('.build-'):
            d,fs=build_inventory(authority/name,tick,owners);temporary[name]={'directory':d,'files':fs}
        elif name.startswith('.record-'):
            temporary[name]=file_proof(authority/name,tick,{0},1048576)
    for name in RECORDS:
        value=optional(authority/name,tick,{0},1048576,True)
        records[name]=None if value is None else value[0]
        if value is not None:
            if stat.S_IMODE(value[0]['identity']['mode'])!=0o600 or value[0]['identity']['gid']!=0:raise Unavailable('record-metadata')
            parsed[name]=probe.strict_object(value[1])
    admission=parsed.get('admission.json')
    if not isinstance(admission,dict) or admission.get('schema')!='celikpanel/database-migration-admission/v1':raise Unavailable('admission-unavailable')
    snapshot_database=file_proof(SNAPSHOTS/snapshot/'celikpanel.db',tick,{0})
    expected={'snapshot':snapshot,'transaction_token_sha256':token,'snapshot_manifest_sha256':snapshot_proof['manifest_sha256'],
              'material_sha256':material['record_sha256'],'candidate_panel_sha256':plan['candidate']['files']['bin/panel']}
    if any(admission.get(k)!=v for k,v in expected.items()):raise Unavailable('admission-tuple-differs')
    if not isinstance(admission.get('initial'),dict) or admission['initial'].get('sha256')!=snapshot_database['sha256']:
        raise Unavailable('initial-snapshot-differs')
    before=admission.get('before',{});bid=before.get('identity',{});flat={k:bid.get(k) for k in ('dev','ino','mode','uid','gid','links','size')}
    for stamp in ('mtime','ctime'):
        flat[stamp+'_sec']=bid.get(stamp,{}).get('Sec');flat[stamp+'_nsec']=bid.get(stamp,{}).get('Nsec')
    material_before=material.get('database_before',{})
    if before.get('sha256')!=material_before.get('sha256') or flat!=material_before.get('file'):
        raise Unavailable('admission-material-before-differs')
    for key,path in (('parent',PARENT),('root',ROOT),('transaction',base),('authority',authority)):
        if admission.get(key)!=dirs[str(path)]:raise Unavailable('admission-directory-differs')
    workdir,files=inventory(work,tick,owners)
    canonical={}
    for suffix in ('',*SIDECARS):
        value=optional(PARENT/('celikpanel.db'+suffix),tick,owners)
        if value is not None:canonical['celikpanel.db'+suffix]=value
    if 'celikpanel.db' not in canonical or 'celikpanel.db' not in files:raise Unavailable('database-absent')
    canonical_history=history(PARENT/'celikpanel.db',canonical,migrations,tick)
    work_history=history(work/'celikpanel.db',files,migrations,tick)
    exact_before=canonical['celikpanel.db']==admission.get('before') and len(canonical)==1
    exact_initial=files['celikpanel.db']==admission.get('initial') and len(files)==1 and workdir==admission.get('work')
    no_publication=all(records[n] is None for n in RECORDS if n!='admission.json') and not temporary
    initial=(exact_before and exact_initial and no_publication and transaction['transaction_phase']=='active'
             and canonical_history.get('schema_version')==38 and work_history.get('schema_version')==38
             and canonical_history.get('migration_identities_sha256')==plan.get('baseline_migration_identities_sha256')
             and work_history.get('migration_identities_sha256')==plan.get('baseline_migration_identities_sha256'))
    # Rehash after immutable history reads; a change does not become a successful
    # observation merely because the initial hash happened to match.
    if inventory(work,tick,owners)!=(workdir,files):raise Unavailable('work-changed')
    again={}
    for suffix in ('',*SIDECARS):
        value=optional(PARENT/('celikpanel.db'+suffix),tick,owners)
        if value is not None:again['celikpanel.db'+suffix]=value
    if again!=canonical:raise Unavailable('canonical-changed')
    for name in RECORDS:
        if optional(authority/name,tick,{0},1048576)!=records[name]:raise Unavailable('record-changed')
    if file_proof(SNAPSHOTS/snapshot/'celikpanel.db',tick,{0})!=snapshot_database:raise Unavailable('snapshot-database-changed')
    for name,want in temporary.items():
        if name.startswith('.build-'):
            d,fs=build_inventory(authority/name,tick,owners);current={'directory':d,'files':fs}
        else:current=file_proof(authority/name,tick,{0},1048576)
        if current!=want:raise Unavailable('temporary-evidence-changed')
    if sorted(p.name for p in authority.iterdir())!=names:raise Unavailable('authority-inventory-changed')
    if any(directory(Path(p),owners if Path(p)==PARENT else {0})!=v for p,v in dirs.items()):raise Unavailable('directory-changed')
    return {'schema':SCHEMA,'status':'observed','classification':'initial-images-before-durable-migration' if initial else 'active-or-completed-migration-state-observed',
            'premigration_acceptance':'PASS-state-only' if initial else 'INCONCLUSIVE','transaction':transaction,'material':material,
            'candidate_migration_identities_sha256':migrations['sha256'],'admission_record_sha256':records['admission.json']['sha256'],'authority_records':records,'authority_entries':names,'temporary_evidence':temporary,'snapshot_database':snapshot_database,
            'directories':dirs,'work_directory':workdir,'work_files':files,'canonical_files':canonical,
            'canonical_matches_admitted_before':exact_before,'work_matches_admitted_initial':exact_initial,'publication_records_absent':no_publication,
            'canonical_migration_history':canonical_history,'work_migration_history':work_history,
            'limits':['observation-only-no-product-authorization','initial-images-do-not-prove-no-BEGIN-ever-occurred','WAL-history-unknown-without-separate-consistent-copy']}

def observe(args,plan,snapshot_proof,tick,native):
    try:
        if probe.guard_guest(args)!=plan['identity']:raise Unavailable('guest-identity-differs')
        worker=native.worker_identity();native.revalidate(worker)
        transaction=fault.read_transaction();value=inspect(snapshot_proof,plan,transaction,tick)
        native.revalidate(worker)
        if fault.read_transaction()!=transaction:raise Unavailable('transaction-changed')
        return value
    except (OSError,ValueError,KeyError,TypeError,probe.ProbeError,fault.Unavailable) as exc:
        return {'schema':SCHEMA,'status':'unavailable','premigration_acceptance':'INCONCLUSIVE','reason':'database-checkpoint-'+type(exc).__name__,
                'limits':['supplementary-observation-never-grants-authority']}
