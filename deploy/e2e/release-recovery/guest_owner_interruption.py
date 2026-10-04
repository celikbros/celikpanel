#!/usr/bin/env python3
"""Interrupt one explicitly admitted owner continuation in a sealed test VM.

Cut boundary is the published owner receipt, not a rollback checkpoint. No
product evidence is edited and the normal timer remains enabled throughout.
"""
import argparse
import importlib.util
import json
import os
import re
from pathlib import Path
import subprocess
import sys
import time
spec=importlib.util.spec_from_file_location('owner_interrupt_budget',Path(__file__).with_name('guest_dispatch_budget_result.py'))
r=importlib.util.module_from_spec(spec);sys.modules[spec.name]=r;spec.loader.exec_module(r)
f=r.f.fault;b=r.f.boot


def receipts(snapshot):
    result={}
    for p in sorted((r.f.BUDGET/snapshot).iterdir()):
        if p.name not in ('1','2','3') and not re.fullmatch(r'owner\.[A-Za-z0-9]+',p.name):
            raise ValueError('unexpected admission evidence')
        raw=f.private_read(p,1024)
        r.f.validate_receipt(raw,snapshot,int(p.name) if p.name in ('1','2','3') else 'owner')
        result[p.name]={'sha256':b.sha(raw),'bytes':len(raw)}
    return result


def pending(operation,before,owners):
    r.validated(operation)
    marker=f.read_transaction();now=receipts(before['snapshot']);status=r.cli(operation)
    if marker['snapshot']!=before['snapshot'] or marker['transaction_token_sha256']!=before['marker']['transaction_token_sha256']:
        raise ValueError('different pending transaction')
    if len(now)!=3+owners or any(now.get(n)!=before['receipts'][n] for n in ('1','2','3')):
        raise ValueError('admission history changed')
    if not b.lock_free():raise ValueError('owner lock remains held')
    if (status.get('request_id')!=operation or status.get('observation')!='known' or status.get('phase')!='recovery_required'
            or status.get('terminal_proof')!='none' or status.get('automatic_recovery')!='paused_retry_limit'):
        raise ValueError('exact paused operation not observed')
    return {'operation_id':operation,'identity':before['identity'],'boot_id':b.boot_id(),'at':f.probe.utc_now(),
            'marker':marker,'receipts':now,'cli':status,'lock_free':True}


class OwnerNative(f.Native):
    def __init__(self,intent,before):super().__init__(intent);self.before=before
    def observe(self):
        try:worker=self.worker_identity()
        except f.probe.ProbeError as exc:raise f.Unavailable('process-probe-unknown') from exc
        marker=f.read_transaction()
        if marker!=self.before['marker']:raise f.Unavailable('transaction-changed')
        try:current=receipts(self.intent['snapshot'])
        except (FileNotFoundError,ValueError):raise f.Unavailable('receipt-not-ready')
        if len(current)!=4 or any(current.get(n)!=self.before['receipts'][n] for n in ('1','2','3')):
            raise f.Unavailable('one-owner-admission-not-published')
        owner=next(n for n in current if n.startswith('owner.'))
        record=r.f.validate_receipt(f.private_read(r.f.BUDGET/self.intent['snapshot']/owner,1024),self.intent['snapshot'],'owner')
        if record['token_sha256']!=marker['transaction_token_sha256'] or record['operation']!=marker['transaction_operation']:
            raise f.Unavailable('owner-admission-token-differs')
        if b.lock_free():raise f.Unavailable('owner-does-not-hold-lock')
        return {'worker':worker,'transaction':marker,'owner_receipts':current}


def interrupt(operation):
    before=r.sample(operation,'exhausted');_,intent=r.validated(operation)
    b.once(b.ROOT/('budget-owner-retry-'+operation+'.admitted.json'),b.encoded(before))
    # Fixed disposable unit isolates the supported CLI and its descendants. Its
    # identity/command/executable/cgroup is rechecked before freeze and SIGKILL.
    f.UNIT='celikpanel-lab-owner-interrupt-'+operation+'.service'
    intent=dict(intent,action='kill',checkpoint='owner_admission_published',
                worker_command=['/bin/bash',str(f.RUNTIME_ROOT/intent['runtime_manifest_sha256']/'deploy/recovery/runtime-entry.sh'),
                                '--owner-retry','--snapshot',before['snapshot']])
    native=OwnerNative(intent,before)
    unit=f.UNIT
    cp=b.run(['systemctl','show',unit,'-p','LoadState','--value'])
    if cp.returncode or cp.stdout.strip()!=b'not-found':raise ValueError('owner unit already exists')
    cp=b.run(['systemd-run','--quiet','--no-block','--unit='+unit,'--property=Type=exec','--property=UMask=0077',
              '--property=RuntimeMaxSec=240','/usr/libexec/celikpanel/recovery','recover','--retry','--snapshot',before['snapshot']])
    if cp.returncode:raise ValueError('owner CLI not started')
    events=[]
    path=b.ROOT/('owner-interrupt-'+operation+'.events.jsonl')
    fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    with os.fdopen(fd,'w') as out:
        def emit(event,**fields):
            value={'event':event,'operation_id':operation,'identity':before['identity'],'at':f.probe.utc_now(),**fields}
            events.append(value);out.write(json.dumps(value,sort_keys=True)+'\n');out.flush();os.fsync(out.fileno())
        if f.run_fault(intent,emit,native)!=0:raise ValueError('owner receipt cut inconclusive')
    # Observe a natural timer cycle. Do not dispatch a recovery while checking.
    end=time.monotonic()+75;after=None
    while time.monotonic()<end:
        try:after=pending(operation,before,1);break
        except (ValueError,f.Unavailable,FileNotFoundError):time.sleep(.5)
    if after is None:raise ValueError('interrupted owner operation did not return to honest pause')
    time.sleep(32)
    later=pending(operation,before,1)
    if later['receipts']!=after['receipts']:raise ValueError('polling or timer added a retry')
    cp=b.run(['systemctl','show',unit,'-p','Result','-p','ExecMainCode','-p','ExecMainStatus','-p','MainPID'])
    result={'schema':'celikpanel/native-owner-interruption/v1','before':before,'after':after,'later':later,
            'events':events,'unit_result':dict(x.split('=',1) for x in cp.stdout.decode().splitlines())}
    b.once(b.ROOT/('owner-interrupt-'+operation+'.json'),b.encoded(result))
    return {'operation_id':operation,'cut':'confirmed','automatic_budget_unchanged':True,'paused_after_owner_cut':True}


def resume(operation):
    evidence=f.probe.strict_object(f.private_read(b.ROOT/('owner-interrupt-'+operation+'.json'),131072))
    before=evidence['before'];latest=pending(operation,before,1)
    if latest['receipts']!=evidence['later']['receipts']:raise ValueError('interrupted evidence changed')
    b.once(b.ROOT/('owner-resume-'+operation+'.admitted.json'),b.encoded(latest))
    cp=subprocess.run(['/usr/libexec/celikpanel/recovery','recover','--retry','--snapshot',before['snapshot']],capture_output=True,timeout=240,env=b.ENV)
    for suffix,raw in [('stdout',cp.stdout),('stderr',cp.stderr)]:b.once(b.ROOT/('budget-owner-retry-'+operation+'.'+suffix),raw)
    b.once(b.ROOT/('budget-owner-retry-'+operation+'.result.json'),b.encoded({'exit_code':cp.returncode,'operation_id':operation,'stdout_sha256':b.sha(cp.stdout),'stderr_sha256':b.sha(cp.stderr)}))
    if cp.returncode:raise ValueError('second explicit owner continuation failed')
    current=receipts(before['snapshot']);status=r.cli(operation)
    if len(current)!=5 or any(current.get(n)!=value for n,value in latest['receipts'].items()):raise ValueError('previous admissions lost or changed')
    if status.get('request_id')!=operation or status.get('observation')!='known' or status.get('terminal_proof')!='rollback_verified' or status.get('phase')!='recovered':raise ValueError('terminal proof absent')
    for name in ('active','quiesce.pending','completion.pending','scheduler-restore.pending'):
        p=b.TRANSACTION/name
        if p.exists() or p.is_symlink():raise ValueError('terminal marker remains')
    if not b.lock_free():raise ValueError('terminal lock remains')
    result=dict(before,phase='terminal',at=f.probe.utc_now(),receipts=current,cli=status,marker=None)
    b.once(b.ROOT/('budget-terminal-'+operation+'.json'),b.encoded(result))
    return {'operation_id':operation,'terminal':status,'owner_receipts':2}

if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('command',choices=('interrupt','resume'));p.add_argument('--operation-id',required=True);a=p.parse_args()
    print(json.dumps((interrupt if a.command=='interrupt' else resume)(a.operation_id),sort_keys=True))
