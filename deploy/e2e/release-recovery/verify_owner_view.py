#!/usr/bin/env python3
"""Offline native owner-view proof; never reads private access credentials."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import stat
import sys
spec=importlib.util.spec_from_file_location('owner_guidance_verify',Path(__file__).with_name('verify_budget_guidance.py'))
g=importlib.util.module_from_spec(spec);sys.modules[spec.name]=g;spec.loader.exec_module(g)
require=g.require


def check(samples,result,binary_sha):
    admitted,ready,closed=samples['admitted'],samples['ready'],samples['closed']
    before=admitted['before'];op=result['request_id'];require(before['operation_id']==op,'admission request differs')
    for value in (ready,closed):
        require(value['operation_id']==op and value['boot_id']==before['boot_id'],'view operation/boot differs')
        for name in ('agent','panel'):
            require(value['services'][name]['MainPID']=='0' and value['services'][name]['ActiveState'] in ('inactive','failed'),'normal coordinator not stopped')
    require(ready['identity']==before['identity'],'view VM differs')
    require(ready['runtime_sha256']==result['selected_runtime_sha256'],'wrong selected runtime')
    require(ready['entry_sha256']==ready['running_sha256']==admitted['entry_sha256']==binary_sha,'different installed or running reader')
    require([ready[k] for k in ('anonymous_http','wrong_code_http','foreign_origin_http','authorized_http')]==[401,401,403,200],'native authorization differs')
    status=ready['response']['status']
    require(status['request_id']==op and status['observation']=='known' and status['phase']=='recovery_required'
            and status['automatic_recovery']=='paused_retry_limit' and status['terminal_proof']=='none','wrong native pause result')
    require(closed['exit_code']==0 and closed['stdout_bytes']==closed['stderr_bytes']==0,'view did not close cleanly/private output leaked')
    require(closed['automatic_receipts']==before['receipts']==result['automatic_receipts_preserved'],'view changed budget')


def verify(directory,op):
    result=g.verify(directory,op);refs=result['evidence_sha256'];samples={}
    def load(relative):
        fd=os.open(Path(directory)/relative,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
        with os.fdopen(fd,'rb') as source:
            info=os.fstat(source.fileno());require(stat.S_ISREG(info.st_mode) and info.st_nlink==1 and info.st_size<=65536,'unsafe view evidence')
            raw=source.read(65537);require(len(raw)<=65536 and g.v.w.wait.probe.metadata(info)==g.v.w.wait.probe.metadata(os.fstat(source.fileno())),'view evidence changed')
        refs[relative]=g.v.w.wait.sha(raw);return g.v.w.wait.probe.strict_object(raw)
    for phase in ('admitted','ready','closed'):
        samples[phase]=load('budget-collected/owner-view-'+op+'.'+phase+'.json')
    files=load('budget-candidate-files.json');check(samples,result,files['recovery-runtime/bin/recovery'])
    browser=load('owner-view-browser.json')
    require(browser['operation_id']==op and browser['identity']==samples['ready']['identity'],'browser request/VM differs')
    rows=browser['results'];require(len(rows)==4 and {(r['lang'],r['width']) for r in rows}=={('en',1440),('en',390),('tr',1440),('tr',390)},'browser matrix incomplete')
    for row in rows:
        require(all(row.get(k) is True for k in ('realHTTP','nativeObservation','offlineRetained','lockClears','reloadLocks','noStorage','noOverflow')),'browser checks not passed')
    result.update(schema='celikpanel/native-owner-view-acceptance/v1',owner_view_while_panel_agent_stopped=True,
                  native_owner_view_http=True,ssh_forwarded_browser=True,normal_same_address_access=False)
    result['limitations']=[x for x in result['limitations'] if x!='No browser or HTTP access proof while exhausted']
    result['limitations'].append('Browser access is explicitly owner-started over SSH; automatic normal-address access remains open')
    return result

if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--evidence-dir',required=True);p.add_argument('--operation-id',required=True);a=p.parse_args()
    print(json.dumps(verify(a.evidence_dir,a.operation_id),sort_keys=True,indent=2))
