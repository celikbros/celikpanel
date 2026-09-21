#!/usr/bin/env python3
"""Offline native budget acceptance; never contacts or mutates a server."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import sys
spec=importlib.util.spec_from_file_location('budget_wait_verifier',Path(__file__).with_name('verify_boot_wait.py'))
w=importlib.util.module_from_spec(spec);sys.modules[spec.name]=w;spec.loader.exec_module(w)
require=w.require

def check_budget(before,after,operation,snapshot):
    for value in (before,after):
        require(value.get('schema')=='celikpanel/native-budget-result/v1' and value.get('operation_id')==operation
                and value.get('snapshot')==snapshot and value.get('lock_free') is True,'budget sample identity or lock differs')
        require(value.get('cli',{}).get('request_id')==operation and value['cli'].get('observation')=='known','wrong request or unknown budget')
    require(before['boot_id']==after['boot_id'] and before['identity']==after['identity'],'sample boot or VM differs')
    require(before['phase']=='exhausted' and before['cli'].get('phase')=='recovery_required'
            and before['cli'].get('terminal_proof')=='none' and before['cli'].get('reason')=='recovery_incomplete','budget exhaustion not observed')
    require(before['marker'].get('snapshot')==snapshot and before['marker'].get('transaction_phase')=='active','pending exact transaction absent')
    require(after['phase']=='terminal' and after['marker'] is None and after['cli'].get('phase')=='recovered'
            and after['cli'].get('terminal_proof')=='rollback_verified','owner retry not terminal')
    require(set(before['receipts'])=={'1','2','3'},'exhausted automatic slots differ')
    require(len(after['receipts'])==4 and sum(re.fullmatch(r'owner\.[A-Za-z0-9]+',n) is not None for n in after['receipts'])==1,'not exactly one owner admission')
    require(all(after['receipts'].get(n)==before['receipts'][n] for n in ('1','2','3')),'automatic reservation changed')
    require(len(before['paused_messages'])>=2 and all(x.startswith('Automatic recovery paused after three admitted attempts.') for x in before['paused_messages']),'repeated native deferral not recorded')
    require(before['at']<after['at'],'result predates exhaustion')

def verify(directory,operation):
    require(re.fullmatch('[0-9a-f]{32}',operation),'invalid request')
    directory=Path(directory).resolve(strict=True);refs={}
    def raw(name,collected=False):
        require(Path(name).name==name,'only evidence basename accepted')
        relative=Path('budget-collected')/name if collected else Path(name)
        fd=os.open(directory/relative,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
        with os.fdopen(fd,'rb') as src:
            info=os.fstat(src.fileno());require(stat.S_ISREG(info.st_mode) and info.st_nlink==1 and info.st_size<=4194304,'unsafe evidence')
            value=src.read(4194305);require(len(value)<=4194304 and w.wait.probe.metadata(info)==w.wait.probe.metadata(os.fstat(src.fileno())),'evidence changed')
        refs[str(relative)]=w.wait.sha(value);return value
    def obj(name,collected=False):return w.wait.probe.strict_object(raw(name,collected))
    def lines(name,collected=False):return [w.wait.probe.strict_object(x) for x in raw(name,collected).splitlines()]
    iraw=raw('bound-worker-'+operation+'.json');intent=w.wait.probe.strict_object(iraw)
    w.bound.worker.validate_intent(intent,intent['identity'],operation)
    start=w.bound.validate_start(intent,iraw,obj('bound-worker-start-'+operation+'.json'),raw('bound-worker-start-'+operation+'.stdout.jsonl'))
    reset=obj('bound-worker-reboot-result-'+operation+'.json')
    require(reset['operation_id']==operation and reset['identity']==intent['identity'] and reset['start']==start,'reset admission differs')
    require(reset['action']=='registered-QEMU-reset-submitted-once' and reset['command']=='system_reset','reset not submitted')
    collection={}
    for name,ref in reset['evidence'].items():
        value=raw(Path(ref['path']).name);require(w.wait.sha(value)==ref['sha256'],'reset evidence changed');collection[name]=value
    cut=[w.wait.probe.strict_object(x) for x in collection['worker_cut'].splitlines()]
    association=w.bound.bind_cut(intent,cut,w.wait.probe.strict_object(collection['record']))
    require(association==reset['worker_cut'],'worker cut association differs');snapshot=association['snapshot']
    before=obj('budget-owner-retry-'+operation+'.admitted.json',True);after=obj('budget-terminal-'+operation+'.json',True)
    check_budget(before,after,operation,snapshot)
    require(before['identity']==intent['identity'] and before['boot_id']!=reset['before_boot_id'],'genuine boot change missing')
    config=obj('budget-cuts-'+operation+'.json',True)
    require(config['armed_boot_id']==reset['before_boot_id'] and config['intent_sha256']==w.wait.sha(iraw)
            and config['attempts']==[2,3] and config['identity']==intent['identity'],'cut authorization differs')
    events=lines('budget-cuts-'+operation+'.jsonl',True);invocations=[]
    require(len(events)==14,'unexpected cut event count')
    for attempt in (2,3):
        rows=[r for r in events if r.get('attempt')==attempt]
        require([r.get('event') for r in rows]==['armed','freeze_requested','freeze_observed','checkpoint_verified','kill_requested','kill_sent','released'],'cut not conclusive')
        require(all(r.get('operation_id')==operation and r.get('identity')==intent['identity'] for r in rows),'cut VM/request differs')
        checkpoint=rows[3];worker=checkpoint['worker'];invocations.append(worker['invocation_id'])
        require(worker['boot_id']==before['boot_id'] and checkpoint['checkpoint']['snapshot']==snapshot
                and checkpoint['checkpoint']['checkpoint']=='payload_restored','cut boot/checkpoint differs')
        require(checkpoint['budget_receipts']=={str(n):before['receipts'][str(n)]['sha256'] for n in range(1,attempt+1)},'cut lacks exact published reservations')
        require(rows[-1].get('checkpoint_verified') is True and rows[-1].get('kill_sent') is True
                and rows[-1]['reason']=='exact-recovery-unit-killed','cut outcome unverified')
    require(len(set(invocations))==2,'same recovery invocation counted twice')
    boot_events=lines('boot-wait-'+operation+'.jsonl',True);proof=next(r for r in boot_events if r['event']=='native-wait-verified')
    w.wait.validate_sample(proof['sample'],intent,before['boot_id'])
    terminal=obj('budget-final-native.json');journal=lines('budget-native-journal.jsonl');readers=lines('bound-reader-after-owner.jsonl',True)
    w.check_terminal_state(proof,terminal,intent,before['boot_id'],readers)
    require(terminal['cli']==after['cli'],'terminal CLI changed')
    require('sequence=82\n' in terminal['release_floor'] and 'sequence=82\n' in terminal['foundation']
            and 'release-commit='+intent['target']['commit']+'\n' in terminal['foundation'],'monotonic floor/foundation differs')
    owner=obj('budget-owner-retry-'+operation+'.result.json',True)
    require(owner['exit_code']==0 and owner['operation_id']==operation,'owner command failed')
    for suffix in ('stdout','stderr'):
        require(owner[suffix+'_sha256']==w.wait.sha(raw('budget-owner-retry-'+operation+'.'+suffix,True)),'owner output changed')
    kit=obj('budget-selected-kit.json');files=obj('budget-candidate-files.json');build=obj('budget-candidate-build.json')
    require(build['commit']==intent['target']['commit'] and build['sha256']==start['archive_sha256'],'candidate build differs')
    require(terminal['runtime_selection']=='format=celikpanel-recovery-selection-v1\nruntime='+kit['runtime_manifest_sha256']+'\n','selected runtime changed')
    for name,digest in kit['files'].items():require(digest==files['recovery-runtime/'+name],'new runtime source not used')
    paused=[r for r in journal if r.get('MESSAGE','').startswith('Automatic recovery paused after three admitted attempts.')]
    require(len(paused)>=2,'native pause journal missing')
    admitted=[r for r in journal if r.get('MESSAGE','').startswith('Recovery dispatch admitted:')]
    require([r['MESSAGE'].split('attempt=')[1].split()[0] for r in admitted]==['1','2','3'],'unexpected or missing automatic admission')
    require(admitted[0]['_BOOT_ID']==reset['before_boot_id'].replace('-','')
            and all(r['_BOOT_ID']==before['boot_id'].replace('-','') for r in admitted[1:]),'reservations not split across real reboot')
    return {'schema':'celikpanel/native-dispatch-budget-acceptance/v1','result':'scoped-checks-passed','request_id':operation,
        'platform':terminal['systemd']+' / Debian 13 / amd64 / disposable QEMU','baseline':intent['baseline'],'target':intent['target'],
        'target_archive_sha256':build['sha256'],'selected_runtime_sha256':kit['runtime_manifest_sha256'],
        'automatic_receipts_preserved':before['receipts'],'owner_receipt_count':1,'cut_invocations':invocations,
        'native_pause_observations':len(paused),'exhausted_status':before['cli'],'terminal':after['cli'],
        'authenticated_http':200,'anonymous_http':401,'coordinators_restored_to_baseline':True,
        'floor_and_foundation_sequence':82,'evidence_sha256':refs,
        'limitations':['Fixture signing; production signing/admission not proved','Only after-publication interruption; publication-edge power loss remains open',
            'Interrupted attempts, not repeated deterministic child errors','No browser or HTTP access proof while exhausted',
            'No Arch budget trial, schema migration or workload continuity proof','Full P0.2/P0.3 remain open']}

def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--evidence-dir',required=True);p.add_argument('--operation-id',required=True);a=p.parse_args()
    print(json.dumps(verify(a.evidence_dir,a.operation_id),indent=2,sort_keys=True))
if __name__=='__main__':main()
