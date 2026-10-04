#!/usr/bin/env python3
"""One unchanged native migrator WAL interruption in registered disposable QEMU.

Stages an explicitly pinned unpublished archive, attaches only the exact gated
lab update, and preserves each attempt. Does not install on existing user hosts.
"""
from __future__ import annotations
import argparse
import base64
import hashlib
import importlib.util
import json
from pathlib import Path
import shlex
import subprocess
import sys
import time

HERE=Path(__file__).resolve().parent

def module(name,filename):
    s=importlib.util.spec_from_file_location(name,HERE/filename);m=importlib.util.module_from_spec(s);sys.modules[name]=m;s.loader.exec_module(m);return m

local=module('native_wal_host_local','local_candidate_trial.py')
guest=module('native_wal_host_guest','guest_native_wal_trial.py')
lab=local.lab
INTENT='native-wal-intent.json'
START='native-wal-start-attempt.json'
ARM='native-wal-arm-attempt.json'
ASSETS=(*local.ASSETS,'guest_native_wal_trial.py','guest_wal_checkpoint.py','wal_frames.py','wal_migration_identity.py',
        'waltrace/native_trace.py','waltrace/trace_fixture.py')


def assets_for(boundary='wal'):
    guest.profile(boundary)
    assets = ASSETS if boundary == 'wal' else (*ASSETS, 'guest_exchange_checkpoint.py', 'waltrace/exchange_trace.py')
    return (*assets, 'guest_exchange_recovery_handoff.py') if boundary == guest.RECOVERY_BOUNDARY else assets


def host_names(boundary='wal'):
    prefix = guest.profile(boundary)['prefix']
    return tuple(prefix + suffix for suffix in ('-intent.json', '-start-attempt.json', '-arm-attempt.json'))


def load(root,record,plan,node,*,boundary='wal'):
    selected=guest.profile(boundary);intent_name,_,_=host_names(boundary)
    value=json.loads(local.trial.read_private(local.evidence_path(root,node,intent_name),4*1024*1024))
    if (value.get('schema')!=selected['schema'] or value.get('fault')!=selected['fault']
        or value.get('identity')!=local.trial.identity(record,plan,node)):raise ValueError('native intent differs')
    guest.validate_helpers(value.get('helpers'),boundary)
    intent=local.load_intent(root,record,plan,node)
    if value['operation_id']!=intent['operation_id'] or value['local_intent_sha256']!=hashlib.sha256(local.trial.read_private(local.evidence_path(root,node,local.INTENT),4*1024*1024)).hexdigest():raise ValueError('local intent changed')
    if intent.get('recovery_fault') != selected.get('recovery_fault'):
        raise ValueError('native recovery fault differs from fixed profile')
    return value,intent


def argv(value,mode,*,boundary='wal'):
    guest.profile(boundary)
    i=value['identity']
    command = ['/usr/bin/python3','-I',str(guest.PRIVATE/'guest_native_wal_trial.py'),'--mode',mode,
            '--lab-nonce',i['nonce'],'--vm-uuid',i['vm_uuid'],'--cell-id',i['cell_id'],'--node',i['node'],'--operation-id',value['operation_id']]
    if boundary != 'wal':command += ['--boundary',boundary]
    return command


def read(root,record,plan,node,value,*,boundary='wal'):
    selected=guest.profile(boundary)
    r=lab.guarded_script(root,record,plan,node,shlex.join(argv(value,'collect',boundary=boundary)),timeout=30)
    if len(r.stdout)>48*1024*1024:raise ValueError('collection exceeds bound')
    data=json.loads(r.stdout)
    if data.get('schema')!=selected['schema'] or data.get('identity')!=value['identity'] or data.get('operation_id')!=value['operation_id']:raise ValueError('collection identity differs')
    raw=base64.b64decode(data['events_base64'],validate=True)
    if len(raw)>16*1024*1024 or (raw and not raw.endswith(b'\n')):raise ValueError('partial native events')
    events=[json.loads(l) for l in raw.splitlines()]
    kinds=[]
    for e in events:
        if e.get('schema')!=selected['schema'] or e.get('identity')!=value['identity'] or e.get('operation_id')!=value['operation_id']:raise ValueError('event identity differs')
        kinds.append(e.get('event'))
    validate_event_order(kinds, data.get('result'),boundary=boundary)
    return data,raw,events


def validate_event_order(kinds, result=None,*,boundary='wal'):
    guest.profile(boundary)
    checkpoints=['wal_checkpoint_verified'] if boundary=='wal' else ['database_exchange_entry_verified','database_exchange_checkpoint_verified']
    handoff = ['recovery_fault_armed'] if boundary == guest.RECOVERY_BOUNDARY else []
    progress=['armed','gate_released',*checkpoints,*handoff,'kill_requested','kill_sent']
    observed=kinds[:-1] if kinds and kinds[-1]=='trace_finished' else kinds
    if kinds and (not observed or observed!=progress[:len(observed)] or len(observed)>len(progress)):
        raise ValueError('native event sequence skips a required proof')
    if result is not None:
        if not isinstance(result, dict) or result.get('status') not in ('cut-sent', 'inconclusive'):
            raise ValueError('native result is not a supported observation')
        if result['status'] == 'cut-sent' and observed != progress:
            raise ValueError('native cut result lacks its complete event proof')
    return kinds


def prepare(root,record,plan,node,archive,digest,*,boundary='wal'):
    selected=guest.profile(boundary);intent_name,_,_=host_names(boundary)
    local.assert_absent(root,node,*(name for b in guest.BOUNDARIES for name in host_names(b)))
    assets_source=assets_for(boundary)
    for source in assets_source:
        if not (HERE/source).is_file():raise ValueError('native fixture asset absent')
    seeded_raw=local.trial.read_private(local.evidence_path(root,node,'populated-seed-result.json'))
    seeded=json.loads(seeded_raw)
    if seeded.get('schema')!='celikpanel/lab-populated-installed/v1' or seeded.get('identity')!=local.trial.identity(record,plan,node):raise ValueError('populated fixture is not verified')
    recovery = {'recovery_fault': selected['recovery_fault']} if 'recovery_fault' in selected else {}
    intent=local.prepare(root,record,plan,node,archive,digest,'candidate-installed',baseline_profile=local.trial.profiles.ALPHA64,**recovery)
    assets={}
    for source in assets_source:
        path,sha=lab.put_file(root,record,plan,node,HERE/source,Path(source).name)
        assets[Path(source).name]=sha
    guest.validate_helpers(assets,boundary)
    value={'schema':selected['schema'],'identity':intent['identity'],'operation_id':intent['operation_id'],
           'created_at':local.trial.now(),'fault':selected['fault'],
           'provenance':intent['provenance'],'local_intent_sha256':hashlib.sha256(local.trial.read_private(local.evidence_path(root,node,local.INTENT),4*1024*1024)).hexdigest(),
           'helpers':assets,'populated_seed':{'seed_id':seeded['seed_id'],'manifest_sha256':seeded['manifest_sha256'],'installed':seeded}}
    ref=local.trial.save(root,node,intent_name,local.encoded(value))
    lab.put_file(root,record,plan,node,Path(ref['path']),guest.names(intent['operation_id'],boundary)['intent'].name)
    print(json.dumps({'action':selected['prefix']+'-prepared','node':node,'operation_id':value['operation_id'],'helpers':len(assets)}),flush=True)
    return value


def launch(root,record,plan,node,value,mode,*,boundary='wal'):
    selected=guest.profile(boundary)
    paths=guest.names(value['operation_id'],boundary)
    command=['systemd-run','--no-block','--property=Type=exec','--property=UMask=0077','--property=TimeoutStopSec=10']
    if mode=='trace':command+=['--unit='+paths['tracer'],'--property=RuntimeMaxSec=660']
    elif mode=='gate':command+=['--unit='+paths['worker'],'--property=OnFailure=celikpanel-release-recovery.service','--property=RuntimeMaxSec=900']
    else:raise ValueError('unsupported native launch')
    command+=argv(value,mode,boundary=boundary)
    label=selected['prefix']+'-'+mode
    try:
        r=lab.guarded_script(root,record,plan,node,shlex.join(command),timeout=30)
        out,err,code,problem=r.stdout,r.stderr,r.returncode,None
    except subprocess.CalledProcessError as exc:out,err,code,problem=exc.stdout or '',exc.stderr or '',exc.returncode,None
    except subprocess.TimeoutExpired as exc:out,err,code,problem=exc.stdout or '',exc.stderr or '',None,'transport-outcome-unknown'
    refs={}
    for suffix,raw in [('stdout',out),('stderr',err)]:
        raw=raw.encode() if isinstance(raw,str) else raw
        refs[suffix]=local.trial.save(root,node,label+'.'+suffix,raw[:131072])
        if len(raw)>131072:problem='transport-output-truncated'
    local.trial.save(root,node,label+'.result.json',local.encoded({'exit_code':code,'problem':problem,'evidence':refs}))
    if code!=0 or problem:raise ValueError('native start outcome not confirmed; collect same attempt')


def arm(root,record,plan,node,value,*,boundary='wal'):
    selected=guest.profile(boundary);_,start_name,arm_name=host_names(boundary)
    local.assert_absent(root,node,arm_name,start_name)
    data,raw,events=read(root,record,plan,node,value,boundary=boundary);paths=guest.names(value['operation_id'],boundary)
    if raw or data['transaction'] or any(data['states'][paths[k]].get('LoadState')!='not-found' for k in ('worker','tracer')):raise ValueError('prior operation exists')
    local.trial.save(root,node,arm_name,local.encoded(value))
    launch(root,record,plan,node,value,'trace',boundary=boundary)
    deadline=time.monotonic()+10
    while time.monotonic()<deadline:
        data,raw,events=read(root,record,plan,node,value,boundary=boundary)
        if [e['event'] for e in events]==['armed'] and data['states'][paths['tracer']].get('ActiveState')=='active':
            print(json.dumps({'action':selected['prefix']+'-armed','node':node,'operation_id':value['operation_id']}),flush=True);return
        time.sleep(.1)
    raise TimeoutError('native tracer not armed; never rearm')


def start(root,record,plan,node,value,*,boundary='wal'):
    selected=guest.profile(boundary);_,start_name,arm_name=host_names(boundary)
    local.assert_absent(root,node,start_name)
    if json.loads(local.trial.read_private(local.evidence_path(root,node,arm_name),4*1024*1024))!=value:raise ValueError('native arm intent differs')
    data,raw,events=read(root,record,plan,node,value,boundary=boundary);paths=guest.names(value['operation_id'],boundary)
    if [e['event'] for e in events]!=['armed'] or data['states'][paths['tracer']].get('ActiveState')!='active' or data['states'][paths['worker']].get('LoadState')!='not-found' or data['transaction']:raise ValueError('fresh armed state absent')
    local.trial.save(root,node,start_name,local.encoded({'intent':value,'at':local.trial.now(),'entrypoint':argv(value,'gate',boundary=boundary)}))
    launch(root,record,plan,node,value,'gate',boundary=boundary)
    print(json.dumps({'action':selected['prefix']+'-started-once','node':node,'operation_id':value['operation_id']}),flush=True)


def collect(root,record,plan,node,value,*,boundary='wal'):
    selected=guest.profile(boundary)
    data,raw,events=read(root,record,plan,node,value,boundary=boundary)
    label=selected['prefix']+'-collection-'+str(time.time_ns())
    local.trial.save(root,node,label+'.state.json',local.encoded(data))
    local.trial.save(root,node,label+'.jsonl',raw)
    paths=guest.names(value['operation_id'],boundary)
    command=['journalctl','--no-pager','--output=json','-u',paths['worker'],'-u',paths['tracer'],'-u','celikpanel-release-recovery.service','--since',value['created_at'],'-n','300']
    journal=lab.guarded_script(root,record,plan,node,shlex.join(command),timeout=30).stdout.encode()
    if len(journal)>4*1024*1024:raise ValueError('journal exceeds bound')
    local.trial.save(root,node,label+'.journal.jsonl',journal)
    print(json.dumps({'action':selected['prefix']+'-collected','node':node,'label':label,'events':[e['event'] for e in events],
                     'trace_result':data['result'],'states':data['states']}),flush=True)


def main(argv=None,*,boundary='wal'):
    guest.profile(boundary)
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--work-root',required=True);p.add_argument('--node',choices=('arch','debian13'),required=True)
    p.add_argument('--mode',choices=('prepare','arm','start','collect','reboot') if boundary==guest.RECOVERY_BOUNDARY else ('prepare','arm','start','collect'),required=True)
    p.add_argument('--archive',type=Path);p.add_argument('--archive-sha256')
    p.add_argument('--execute',action='store_true')
    a=p.parse_args(argv)
    if a.mode != 'collect' and not a.execute:
        p.error('mutation requires --execute and a registered disposable VM')
    root=lab.checked_root(a.work_root);record,plan=lab.load(root);lab.process_guard(plan['nodes'][a.node])
    if a.mode=='prepare':
        if not a.archive or not a.archive_sha256:p.error('prepare requires archive and exact hash')
        return prepare(root,record,plan,a.node,a.archive,a.archive_sha256,boundary=boundary)
    value,intent=load(root,record,plan,a.node,boundary=boundary)
    if a.mode=='reboot':
        recovery=module('native_exchange_reboot_host','recovery_fault_trial.py')
        return recovery.reboot(root,record,plan,a.node,intent,native_boundary=boundary)
    return {'arm':arm,'start':start,'collect':collect}[a.mode](root,record,plan,a.node,value,boundary=boundary)

if __name__=='__main__':
    try:main()
    except (OSError,ValueError,TimeoutError,subprocess.SubprocessError) as exc:
        print('native WAL controller refused: '+type(exc).__name__+': '+str(exc),file=sys.stderr);sys.exit(2)
