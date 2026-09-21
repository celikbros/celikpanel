#!/usr/bin/env python3
"""Require genuine owner-admission interruption and subsequent exact rollback."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import stat
import sys
spec=importlib.util.spec_from_file_location('owner_interruption_budget',Path(__file__).with_name('verify_dispatch_budget.py'))
v=importlib.util.module_from_spec(spec);sys.modules[spec.name]=v;spec.loader.exec_module(v)
require=v.require


def check(value,result):
    require(value['schema']=='celikpanel/native-owner-interruption/v1','wrong interruption schema')
    before=value['before'];after=value['after'];later=value['later'];op=result['request_id']
    require(before['operation_id']==op and before['receipts']==result['automatic_receipts_preserved'],'wrong original operation/budget')
    for current in (after,later):
        require(current['operation_id']==op and current['identity']==before['identity'] and current['boot_id']==before['boot_id'],'foreign owner observation')
        require(current['marker']==before['marker'] and current['lock_free'] is True,'pending operation/lock changed')
        require(len(current['receipts'])==4 and all(current['receipts'].get(n)==before['receipts'][n] for n in ('1','2','3')),'owner interruption replenished automatic budget')
        status=current['cli']
        require(status.get('request_id')==op and status.get('observation')=='known' and status.get('phase')=='recovery_required'
                and status.get('terminal_proof')=='none' and status.get('automatic_recovery')=='paused_retry_limit','interruption falsely terminal/unknown')
    require(after['receipts']==later['receipts'],'timer or polling added owner retry')
    from datetime import datetime
    require((datetime.fromisoformat(later['at'])-datetime.fromisoformat(after['at'])).total_seconds()>=30,'no natural timer observation interval')
    events=value['events'];require([e['event'] for e in events]==['armed','freeze_requested','freeze_observed','checkpoint_verified','kill_requested','kill_sent','released'],'inconclusive owner cut')
    require(all(e['operation_id']==op and e['identity']==before['identity'] for e in events),'cut operation/VM differs')
    require(events[0]['checkpoint']=='owner_admission_published','wrong cut boundary')
    proof=events[3];worker=proof['worker'];unit='celikpanel-lab-owner-interrupt-'+op+'.service'
    require(worker['unit']==unit and worker['cgroup']=='/system.slice/'+unit and worker['boot_id']==before['boot_id'],'wrong owner process')
    require(all(e.get('worker')==worker for e in (events[1],events[2],events[4],events[5])),'cut process identity changed')
    require(proof['snapshot_proof']['snapshot']==before['snapshot'],'wrong restored snapshot')
    require(proof['owner_receipts']==after['receipts'] and proof['transaction']==before['marker'],'cut not bound to published owner admission')
    require(proof['snapshot_proof']['runtime']['runtime_manifest_sha256']==result['selected_runtime_sha256'],'wrong recovery runtime')
    require(events[-1]['kill_sent'] is True and events[-1]['checkpoint_verified'] is True,'kill not proved')
    require(value['unit_result']=={'Result':'signal','ExecMainCode':'2','ExecMainStatus':'9','MainPID':'0'},'owner process not killed by SIGKILL')
    return after['receipts']


def verify(directory,operation):
    result=v.verify(directory,operation,owner_receipt_count=2)
    name='budget-collected/owner-interrupt-'+operation+'.json'
    fd=os.open(Path(directory)/name,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    with os.fdopen(fd,'rb') as source:
        info=os.fstat(source.fileno());require(stat.S_ISREG(info.st_mode) and info.st_nlink==1 and info.st_size<=131072,'unsafe owner proof')
        raw=source.read(131073);require(len(raw)<=131072 and v.w.wait.probe.metadata(info)==v.w.wait.probe.metadata(os.fstat(source.fileno())),'owner proof changed')
    value=v.w.wait.probe.strict_object(raw);old=check(value,result)
    terminal=v.w.wait.probe.strict_object((Path(directory)/('budget-collected/budget-terminal-'+operation+'.json')).read_bytes())
    require(all(terminal['receipts'].get(n)==digest for n,digest in old.items()),'interrupted owner admission lost')
    result['evidence_sha256'][name]=v.w.wait.sha(raw)
    result.update(schema='celikpanel/native-owner-interruption-acceptance/v1',interrupted_owner_admission_preserved=True,
                  automatic_retry_after_owner_cut=False,second_explicit_owner_continuation='rollback_verified',
                  owner_cut_boundary='owner_admission_published')
    result['limitations'].append('Owner cut is after admission publication, not every rollback checkpoint or SSH disconnect mode')
    return result

if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--evidence-dir',required=True);p.add_argument('--operation-id',required=True);a=p.parse_args()
    print(json.dumps(verify(a.evidence_dir,a.operation_id),sort_keys=True,indent=2))
