#!/usr/bin/env python3
"""One unpublished native update and automatic-recovery trial in a registered VM.

This path tests native update/recovery, not signed agent admission. It never
retries a start, repairs a guest, signs an artifact, or accepts production hosts.
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

HERE=Path(__file__).resolve().parent

def module(name,filename):
    spec=importlib.util.spec_from_file_location(name,HERE/filename)
    value=importlib.util.module_from_spec(spec);sys.modules[name]=value;spec.loader.exec_module(value);return value

trial=module('local_trial_shared','update_trial.py')
guest=module('local_trial_guest','guest_local_candidate.py')
events_tools=module('local_trial_events','arm_update_kill.py')
lab=trial.lab
INTENT='local-candidate-intent.json'
STAGE='local-candidate-stage.json'
START='local-candidate-start-attempt.json'
ARM='local-candidate-kill-intent.json'
ASSETS=('candidate_archive.py','guest_probe.py','guest_port_fault.py','guest_update_kill.py','guest_local_candidate.py','guest_recovery_fault.py','guest_recovery_handoff.py','guest_candidate_data_fault.py')

def encoded(value):return (json.dumps(value,sort_keys=True)+'\n').encode()

def evidence_path(root,node,name):return root/'evidence'/node/name

def assert_absent(root,node,*names):
    for name in names:
        path=evidence_path(root,node,name)
        if path.exists() or path.is_symlink():raise ValueError('saved '+name+' exists; collect the original operation without retrying')

def load_intent(root,record,plan,node):
    value=json.loads(trial.read_private(evidence_path(root,node,INTENT),4*1024*1024))
    operation=value.get('operation_id','');guest.names(operation)
    guest.validate_plan(value,trial.identity(record,plan,node),operation)
    return value

def helper_argv(intent,mode):
    ident=intent['identity']
    return ['python3','-I',str(guest.PRIVATE/'guest_local_candidate.py'),'--mode',mode,'--lab-nonce',ident['nonce'],'--vm-uuid',ident['vm_uuid'],'--cell-id',ident['cell_id'],'--node',ident['node'],'--operation-id',intent['operation_id']]

def read_guest(root,record,plan,node,intent):
    result=lab.guarded_script(root,record,plan,node,shlex.join(helper_argv(intent,'collect')),timeout=30)
    if len(result.stdout)>2*1024*1024:raise ValueError('local collection exceeds bound')
    state=json.loads(result.stdout)
    if state.get('schema')!='celikpanel/local-candidate-collection/v1' or state.get('identity')!=intent['identity'] or state.get('operation_id')!=intent['operation_id']:raise ValueError('local collection identity differs')
    raw=base64.b64decode(state['fault_jsonl_base64'],validate=True)
    events=events_tools.validate_events(raw,intent['identity'],intent['operation_id'])
    return state,raw,events

def validate_stage(value,intent):
    if (value.get('schema')!='celikpanel/local-candidate-stage/v1' or value.get('identity')!=intent['identity'] or value.get('operation_id')!=intent['operation_id']
            or value.get('root')!=intent['source_root'] or value.get('manifest_sha256')!=intent['candidate']['manifest_sha256']
            or value.get('verified_files')!=len(intent['candidate']['files']) or not trial.HEX64.fullmatch(value.get('bash_sha256',''))):raise ValueError('local candidate stage proof differs')
    return value

def prepare(root,record,plan,node,archive,digest,boundary,recovery_fault=None,candidate_data_fault=None):
    assert_absent(root,node,INTENT,START,ARM,'update-intent.json','update-start-attempt.json')
    candidate=guest.archive_tools.inspect_archive(archive,digest)
    source_proof=guest.archive_tools.verify_committed_source(candidate,trial.REPOSITORY)
    baseline_raw=trial.read_private(root/('baseline-'+node+'-baseline-install-result.json'))
    baseline=json.loads(baseline_raw);trial.validate_baseline(baseline)
    seed_raw=trial.read_private(evidence_path(root,node,'seed.stdout.jsonl'));trial.validate_seed(seed_raw,record,node)
    operation=secrets.token_hex(16)
    intent={'schema':guest.SCHEMA,'provenance':'unpublished-local-build-not-signed-agent-admission','identity':trial.identity(record,plan,node),'operation_id':operation,
            'created_at':trial.now(),'boundary':boundary,'candidate':candidate,'committed_source_proof':source_proof,'baseline_artifacts':baseline['installed_artifacts'],
            'baseline_evidence_sha256':hashlib.sha256(baseline_raw).hexdigest(),'seed_evidence_sha256':hashlib.sha256(seed_raw).hexdigest(),
            'archive_path':str(guest.PRIVATE/('local-candidate-'+operation+'.tar.gz')),
            'source_root':str(guest.RELEASES/('.download.lab-'+operation)/'payload'/candidate['root_name'])}
    if recovery_fault is not None:
        intent['recovery_fault']=recovery_fault
    if candidate_data_fault is not None:
        intent['candidate_data_fault']=candidate_data_fault
    guest.validate_plan(intent,intent['identity'],operation)
    customization=evidence_path(root,node,'owner-unit-baseline.json')
    if customization.exists() or customization.is_symlink():
        raw=trial.read_private(customization)
        intent['owner_unit_baseline_evidence']={'path':str(customization),'sha256':hashlib.sha256(raw).hexdigest()}
    trial.exercise.capture(root,record,plan,node,'local-before-update',intent['created_at'])
    saved=trial.save(root,node,INTENT,encoded(intent))
    assets={}
    for name in ASSETS:
        path,sha=lab.put_file(root,record,plan,node,HERE/name,name);assets[name]={'path':path,'sha256':sha}
    path,sha=lab.put_file(root,record,plan,node,archive,Path(intent['archive_path']).name)
    if path!=intent['archive_path'] or sha!=digest:raise ValueError('uploaded local artifact differs')
    lab.put_file(root,record,plan,node,Path(saved['path']),'local-candidate-'+operation+'.json')
    trial.save(root,node,'local-candidate-assets.json',encoded(assets))
    result=lab.guarded_script(root,record,plan,node,shlex.join(helper_argv(intent,'stage')),timeout=180)
    stage=validate_stage(json.loads(result.stdout),intent)
    trial.save(root,node,STAGE,encoded(stage))
    print(json.dumps({'action':'local-candidate-prepared-not-started','node':node,'operation_id':operation,'candidate_commit':candidate['commit'],'archive_sha256':digest,'provenance':intent['provenance']}),flush=True)
    return intent

def launch_argv(intent,mode):
    paths=guest.names(intent['operation_id'])
    if mode=='arm':
        return ['systemd-run','--unit='+paths['fault'],'--no-block','--property=RuntimeMaxSec=650','--property=TimeoutStopSec=10','--property=UMask=0077',
                '--property=ExecStopPost=-/usr/bin/systemctl thaw '+paths['worker'],*helper_argv(intent,'fault')]
    if mode!='start':raise ValueError('unknown local launch mode')
    return ['systemd-run','--unit='+paths['worker'],'--no-block','--property=Type=exec','--property=UMask=0077','--property=OnFailure=celikpanel-release-recovery.service',
            '/bin/bash',intent['source_root']+'/bootstrap-prebuilt-update.sh','--normal']

def launch_once(root,record,plan,node,intent,mode):
    label='local-candidate-'+mode
    try:
        result=lab.guarded_script(root,record,plan,node,shlex.join(launch_argv(intent,mode)),timeout=30)
        stdout,stderr,code,error=result.stdout,result.stderr,result.returncode,None
    except subprocess.CalledProcessError as exc:stdout,stderr,code,error=exc.stdout or '',exc.stderr or '',exc.returncode,None
    except subprocess.TimeoutExpired as exc:stdout,stderr,code,error=exc.stdout or '',exc.stderr or '',None,'transport-outcome-unknown'
    refs={};truncated=False
    for name,data in (('stdout',stdout),('stderr',stderr)):
        raw=data.encode() if isinstance(data,str) else data
        truncated=truncated or len(raw)>131072
        refs[name]=trial.save(root,node,label+'.'+name+'.txt',raw[:131072])
    outcome={'identity':intent['identity'],'operation_id':intent['operation_id'],'exit_code':code,'error':error,'truncated':truncated,'evidence':refs}
    trial.save(root,node,label+'.result.json',encoded(outcome))
    if code!=0 or error or truncated:raise ValueError('native '+mode+' not confirmed; collect same operation, never retry')

def require_armed(state,events,intent):
    paths=guest.names(intent['operation_id'])
    if [e['event'] for e in events]!=['armed'] or state['states'][paths['fault']].get('ActiveState')!='active' or state['states'][paths['worker']].get('LoadState')!='not-found' or state.get('transaction') is not None:raise ValueError('exact local fault is not armed before a fresh start')

def arm(root,record,plan,node,intent):
    assert_absent(root,node,START,ARM,'update-start-attempt.json')
    validate_stage(json.loads(trial.read_private(evidence_path(root,node,STAGE))),intent)
    state,raw,events=read_guest(root,record,plan,node,intent);paths=guest.names(intent['operation_id'])
    if raw or state['states'][paths['fault']].get('LoadState')!='not-found' or state['states'][paths['worker']].get('LoadState')!='not-found' or state.get('transaction') is not None:raise ValueError('prior native operation or fault evidence exists')
    trial.save(root,node,ARM,encoded({'identity':intent['identity'],'operation_id':intent['operation_id'],'created_at':trial.now(),'intent_sha256':hashlib.sha256(trial.read_private(evidence_path(root,node,INTENT),4*1024*1024)).hexdigest()}))
    launch_once(root,record,plan,node,intent,'arm')
    deadline=time.monotonic()+10
    while time.monotonic()<deadline:
        state,raw,events=read_guest(root,record,plan,node,intent)
        if events:
            require_armed(state,events,intent)
            trial.save(root,node,'local-candidate-armed.jsonl',raw)
            print(json.dumps({'action':'local-candidate-kill-armed','node':node,'operation_id':intent['operation_id']}),flush=True);return
        time.sleep(.2)
    raise TimeoutError('native armed event unconfirmed; collect without rearming')

def start(root,record,plan,node,intent):
    assert_absent(root,node,START,'update-start-attempt.json')
    arm_intent=json.loads(trial.read_private(evidence_path(root,node,ARM)))
    if arm_intent.get('identity')!=intent['identity'] or arm_intent.get('operation_id')!=intent['operation_id'] or arm_intent.get('intent_sha256')!=hashlib.sha256(trial.read_private(evidence_path(root,node,INTENT),4*1024*1024)).hexdigest():raise ValueError('local armed intent differs')
    state,raw,events=read_guest(root,record,plan,node,intent);require_armed(state,events,intent)
    trial.save(root,node,START,encoded({'identity':intent['identity'],'operation_id':intent['operation_id'],'started_at':trial.now(),'argv':launch_argv(intent,'start'),'provenance':intent['provenance']}))
    launch_once(root,record,plan,node,intent,'start')
    print(json.dumps({'action':'local-candidate-start-submitted-once','node':node,'operation_id':intent['operation_id'],'recovery_success':'unconfirmed'}),flush=True)

def collect(root,record,plan,node,intent):
    state,raw,events=read_guest(root,record,plan,node,intent)
    label='local-candidate-collection-'+str(time.time_ns())
    refs={'events':trial.save(root,node,label+'.jsonl',raw),'state':trial.save(root,node,label+'.state.json',encoded(state))}
    worker=guest.names(intent['operation_id'])['worker']
    argv=['journalctl','--no-pager','--output=json','-u',worker,'-u','celikpanel-release-recovery.service','--since',intent['created_at'],'-n','200']
    result=lab.guarded_script(root,record,plan,node,shlex.join(argv),timeout=30)
    journal=result.stdout.encode();refs['journal']=trial.save(root,node,label+'.journal.jsonl',journal[:262144])
    trial.exercise.capture(root,record,plan,node,label,intent['created_at'])
    summary={'action':'local-candidate-collected','node':node,'operation_id':intent['operation_id'],'events':[e['event'] for e in events],'states':state['states'],'transaction':state['transaction'],
             'journal_truncated':len(journal)>262144,'evidence':refs,'provenance':intent['provenance'],'recovery_success':'not-inferred-by-controller'}
    print(json.dumps(summary,sort_keys=True),flush=True);return summary

def main(argv=None):
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work-root',required=True);parser.add_argument('--node',choices=('arch','debian13'),default='arch')
    parser.add_argument('--mode',choices=('prepare','arm','start','collect'),required=True);parser.add_argument('--execute',action='store_true')
    parser.add_argument('--archive',type=Path);parser.add_argument('--archive-sha256');parser.add_argument('--boundary',choices=('candidate-installed','require-unit-reload'),default='require-unit-reload')
    parser.add_argument('--candidate-data-fault',choices=('quarantine-fixed-three',))
    args=parser.parse_args(argv)
    if args.mode!='collect' and not args.execute:parser.error('mutation requires --execute and the registered disposable VM')
    if args.mode=='prepare' and (args.archive is None or args.archive_sha256 is None):parser.error('prepare requires exact local archive and SHA256')
    if args.mode!='prepare' and args.candidate_data_fault is not None:parser.error('candidate data fault must be sealed during prepare')
    root=lab.checked_root(args.work_root);record,plan=lab.load(root);lab.process_guard(plan['nodes'][args.node])
    if args.mode=='prepare':return prepare(root,record,plan,args.node,args.archive,args.archive_sha256,args.boundary,candidate_data_fault=args.candidate_data_fault)
    intent=load_intent(root,record,plan,args.node)
    return {'arm':arm,'start':start,'collect':collect}[args.mode](root,record,plan,args.node,intent)

if __name__=='__main__':
    try:main()
    except (ValueError,OSError,TimeoutError,subprocess.SubprocessError) as exc:
        print('local candidate controller refused: '+type(exc).__name__+': '+str(exc),file=sys.stderr);sys.exit(2)
