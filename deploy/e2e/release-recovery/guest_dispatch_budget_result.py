#!/usr/bin/env python3
"""Read native budget proof, or admit one explicit retry in the sealed VM only."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys

HERE=Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('budget_result_fault',HERE/'guest_dispatch_budget.py')
f=importlib.util.module_from_spec(spec);sys.modules[spec.name]=f;spec.loader.exec_module(f)
ROOT=f.ROOT

def validated(operation):
    intent,raw=f.boot.guarded_intent(operation)
    recovery=f.fault.probe.strict_object(f.fault.private_read(ROOT/('recovery-fault-'+operation+'.json')))
    f.fault.validate_intent(recovery,intent['identity'],operation)
    config=f.fault.probe.strict_object(f.boot.read(ROOT/('budget-cuts-'+operation+'.json')))
    if (config.get('intent_sha256')!=f.boot.sha(raw) or config.get('identity')!=intent['identity']
            or config.get('armed_boot_id')==f.boot.boot_id() or config.get('attempts')!=[2,3]):
        raise ValueError('wrong sealed reboot/cut intent')
    events=[f.fault.probe.strict_object(line) for line in f.boot.read(ROOT/('budget-cuts-'+operation+'.jsonl'),1048576).splitlines()]
    for attempt in (2,3):
        rows=[e for e in events if e.get('attempt')==attempt]
        if ([e.get('event') for e in rows]!=['armed','freeze_requested','freeze_observed','checkpoint_verified','kill_requested','kill_sent','released']
                or any(e.get('identity')!=intent['identity'] or e.get('operation_id')!=operation for e in rows)
                or rows[-1].get('kill_sent') is not True or rows[-1].get('checkpoint_verified') is not True
                or rows[3].get('checkpoint',{}).get('snapshot')!=recovery['snapshot']
                or rows[3].get('worker',{}).get('boot_id')!=f.boot.boot_id()):
            raise ValueError('both native recovery cuts are not confirmed')
    return intent,recovery

def cli(operation):
    cp=f.boot.run(['/usr/libexec/celikpanel/recovery','status','--request-id',operation,'--json'])
    if cp.returncode:raise ValueError('native CLI status unavailable')
    return f.fault.probe.strict_object(cp.stdout)

def sample(operation,phase):
    intent,recovery=validated(operation);snapshot=recovery['snapshot'];directory=f.BUDGET/snapshot
    receipts={}
    for name in sorted(directory.iterdir()):
        if name.name not in ('1','2','3') and not name.name.startswith('owner.'):
            raise ValueError('unexpected budget evidence')
        raw=f.fault.private_read(name,1024)
        f.validate_receipt(raw,snapshot,int(name.name) if name.name in ('1','2','3') else 'owner')
        receipts[name.name]={'sha256':f.boot.sha(raw),'bytes':len(raw)}
    if any(name not in receipts for name in ('1','2','3')):raise ValueError('three reservations not retained')
    status=cli(operation)
    expected={'schema':'celikpanel-recovery-status/v1','request_id':operation,'observation':'known'}
    expected.update({'phase':'recovery_required','reason':'recovery_incomplete','terminal_proof':'none'} if phase=='exhausted'
                    else {'phase':'recovered','reason':'rollback_verified','terminal_proof':'rollback_verified'})
    if any(status.get(k)!=v for k,v in expected.items()):raise ValueError('native status does not confirm requested result')
    marker=None
    if phase=='exhausted':
        marker=f.fault.read_transaction()
        if marker['snapshot']!=snapshot or any(name.startswith('owner.') for name in receipts):raise ValueError('different operation or owner retry already present')
    else:
        if len(receipts)!=4 or sum(name.startswith('owner.') for name in receipts)!=1:raise ValueError('owner admission not exact')
        for name in ('active','quiesce.pending','completion.pending','scheduler-restore.pending'):
            p=f.boot.TRANSACTION/name
            if p.exists() or p.is_symlink():raise ValueError('terminal marker remains')
    if not f.boot.lock_free():raise ValueError('transaction lock not released')
    result={'schema':'celikpanel/native-budget-result/v1','phase':phase,'operation_id':operation,
            'at':f.fault.probe.utc_now(),'identity':intent['identity'],'boot_id':f.boot.boot_id(),
            'snapshot':snapshot,'receipts':receipts,'cli':status,'marker':marker,'lock_free':True}
    cp=f.boot.run(['journalctl','-b','-u','celikpanel-release-recovery.service','--no-pager','-o','json','-n','200'])
    if cp.returncode or len(cp.stdout)>1048576:raise ValueError('native journal unavailable')
    rows=[f.fault.probe.strict_object(line) for line in cp.stdout.splitlines()]
    result['paused_messages']=[row['MESSAGE'] for row in rows if 'three automatic' in row.get('MESSAGE','').lower() or '3 automatic' in row.get('MESSAGE','').lower() or 'attempts' in row.get('MESSAGE','').lower()]
    return result

def owner_retry(operation):
    before=sample(operation,'exhausted')
    f.boot.once(ROOT/('budget-owner-retry-'+operation+'.admitted.json'),f.boot.encoded(before))
    # Root CLI is the supported owner path. No edit to evidence or budgets.
    cp=subprocess.run(['/usr/libexec/celikpanel/recovery','recover','--retry','--snapshot',before['snapshot']],capture_output=True,timeout=240,env=f.boot.ENV)
    f.boot.once(ROOT/('budget-owner-retry-'+operation+'.stdout'),cp.stdout)
    f.boot.once(ROOT/('budget-owner-retry-'+operation+'.stderr'),cp.stderr)
    f.boot.once(ROOT/('budget-owner-retry-'+operation+'.result.json'),f.boot.encoded({'exit_code':cp.returncode,'operation_id':operation,'stdout_sha256':f.boot.sha(cp.stdout),'stderr_sha256':f.boot.sha(cp.stderr)}))
    if cp.returncode:raise ValueError('owner retry did not confirm success')
    result=sample(operation,'terminal')
    if any(result['receipts'][name]!=before['receipts'][name] for name in ('1','2','3')):raise ValueError('automatic reservations changed')
    f.boot.once(ROOT/('budget-terminal-'+operation+'.json'),f.boot.encoded(result))
    return result

def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('command',choices=('exhausted','terminal','owner-retry'));p.add_argument('--operation-id',required=True);a=p.parse_args()
    if a.command=='owner-retry':result=owner_retry(a.operation_id)
    else:
        result=sample(a.operation_id,a.command)
        f.boot.once(ROOT/('budget-'+a.command+'-'+a.operation_id+'-'+str(__import__('time').time_ns())+'.json'),f.boot.encoded(result))
    print(json.dumps(result,sort_keys=True))
if __name__=='__main__':main()
