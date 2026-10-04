#!/usr/bin/env python3
"""Supplement native budget acceptance with exact producer-to-CLI hint proof."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import stat
import sys
spec=importlib.util.spec_from_file_location('guidance_budget_verify',Path(__file__).with_name('verify_dispatch_budget.py'))
v=importlib.util.module_from_spec(spec);sys.modules[spec.name]=v;spec.loader.exec_module(v)
require=v.require


def check(value,operation,target,phase):
    require(value['schema']=='celikpanel/native-budget-guidance/v1' and value['operation_id']==operation and value['phase']==phase,'guidance identity differs')
    raw=value['status_raw'].encode('ascii');status=v.w.wait.record(raw,('schema','request_id','target_commit','phase','terminal_proof','reason','observed_at','previous_failure'))
    hint=v.w.wait.record(value['automatic_raw'].encode('ascii'),('schema','request_id','observation_identity','observation_sha256','automatic_recovery'))
    require(status['schema']=='celikpanel-recovery-observation/v1' and status['request_id']==operation and status['target_commit']==target,'status binding differs')
    require(hint['schema']=='celikpanel-recovery-automatic/v1' and hint['request_id']==operation and hint['automatic_recovery']=='paused_retry_limit','unsupported hint')
    cli=value['cli'];require(cli['observation']=='known' and cli['request_id']==operation,'unknown or foreign CLI')
    for key in ('phase','terminal_proof','reason','observed_at'):
        require(cli.get(key)==status[key],'CLI does not match native status')
    require(cli.get('previous_failure','none')==status['previous_failure'],'prior failure differs')
    if phase=='exhausted':
        require(status['phase']=='recovery_required' and status['terminal_proof']=='none' and status['reason']=='recovery_incomplete','not exhausted')
        require(hint['observation_identity']==value['status_identity'] and hint['observation_sha256']==v.w.wait.sha(raw),'hint not bound to exact native status')
        require(cli.get('automatic_recovery')=='paused_retry_limit','CLI did not read bound hint')
        require(set(value['receipts'])=={'1','2','3'} and value['lock_free'] is True,'budget or lock differs')
        require('all three attempts' in value['cli_text']['en'] and 'one-time same-operation retry' in value['cli_text']['en'],'English action absent')
        require('tek seferlik' in value['cli_text']['tr'] and 'Sunucu sahibi' in value['cli_text']['tr'],'Turkish action absent')
        for text in value['cli_text'].values():
            require(operation in text and 'sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50' in text,'request or inspection command absent')
    else:
        require(status['phase']=='recovered' and status['terminal_proof']=='rollback_verified' and status['reason']=='rollback_verified','terminal proof absent')
        require(hint['observation_identity']!=value['status_identity'] and hint['observation_sha256']!=v.w.wait.sha(raw),'retained hint not stale')
        require(not cli.get('automatic_recovery') and 'all three attempts' not in value['cli_text']['en'],'stale pause overrides terminal proof')


def verify(directory,operation):
    result=v.verify(directory,operation);refs=result['evidence_sha256'];samples={}
    for phase in ('exhausted','terminal'):
        name='budget-collected/budget-guidance-'+phase+'-'+operation+'.json'
        fd=os.open(Path(directory)/name,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
        with os.fdopen(fd,'rb') as src:
            info=os.fstat(src.fileno());require(stat.S_ISREG(info.st_mode) and info.st_nlink==1 and info.st_size<=32768,'unsafe guidance evidence')
            raw=src.read(32769);require(len(raw)<=32768 and v.w.wait.probe.metadata(info)==v.w.wait.probe.metadata(os.fstat(src.fileno())),'guidance evidence changed')
        refs[name]=v.w.wait.sha(raw);samples[phase]=v.w.wait.probe.strict_object(raw)
        check(samples[phase],operation,result['target']['commit'],phase)
    before,after=samples['exhausted'],samples['terminal']
    require(before['identity']==after['identity'] and before['boot_id']==after['boot_id'] and before['at']<after['at'],'guidance VM/boot/time differs')
    require(before['receipts']==result['automatic_receipts_preserved'] and after['cli']==result['terminal'],'guidance not from accepted native trial')
    result.update(schema='celikpanel/native-budget-guidance-acceptance/v1',native_hint_bound_to_status=True,native_cli_languages=['en','tr'],retained_pause_ignored_after_rollback=True)
    return result


if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--evidence-dir',required=True);p.add_argument('--operation-id',required=True);a=p.parse_args()
    print(json.dumps(verify(a.evidence_dir,a.operation_id),indent=2,sort_keys=True))
