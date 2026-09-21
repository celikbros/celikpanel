#!/usr/bin/env python3
"""Read-only verification of saved bound-worker boot-wait evidence.

This verifies a disposable test, not product mutation authority. It never
contacts a guest, starts recovery, or upgrades any installed panel.
"""
import argparse
import datetime as dt
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import sys

HERE=Path(__file__).resolve().parent

def module(name):
    spec=importlib.util.spec_from_file_location('verify_wait_'+name,HERE/(name+'.py'))
    value=importlib.util.module_from_spec(spec);sys.modules[spec.name]=value;spec.loader.exec_module(value);return value

wait=module('guest_boot_wait');bound=module('bound_worker_reboot')

def require(value,message):
    if not value:raise ValueError(message)


def check_terminal_state(proof, terminal, intent, boot, readers):
    op=intent['operation_id'];cli=terminal['cli']
    expected={'schema':'celikpanel-recovery-status/v1','request_id':op,'observation':'known',
              'phase':'recovered','terminal_proof':'rollback_verified','reason':'rollback_verified'}
    require(terminal['operation_id']==op and terminal['boot_id']==boot,'terminal request/boot differs')
    require(all(cli.get(k)==v for k,v in expected.items()) and 'waiting_for' not in cli,'terminal not verified or stale wait exposed')
    require(terminal['active_transaction_absent'] is True and terminal['wait_is_not_exposed'] is True,'terminal descriptor/wait differs')
    for key in ('binding_sha256','manifest_sha256'):
        require(terminal[key]==proof['material'][key],'native recovery material changed')
    require(terminal['stale_wait_sha256']==proof['sample']['hint_sha256'],'stale wait was not retained unchanged')
    for name in ('agent','panel'):
        service=terminal['services'][name]
        require(service['properties']['ActiveState']=='active' and service['properties']['SubState']=='running','coordinator not running')
        require(service['installed_sha256']==service['running_sha256']==intent['baseline'][name+'_sha256'],'baseline executable not restored')
    require(terminal['timer']['ActiveState']=='active','native recovery timer unavailable')
    require(any(row.get('cli')==cli and row.get('http',{}).get('body')==dict(cli,panel_state='ready') and row.get('http',{}).get('status')==200
                and row.get('anonymous_status')==401 for row in readers),'authenticated reader disagreement')


def check_terminal(proof, terminal, intent, boot, readers, journal):
    check_terminal_state(proof, terminal, intent, boot, readers)
    cli=terminal['cli']
    boot_hex=boot.replace('-','')
    deferrals=[row for row in journal if row.get('_BOOT_ID')==boot_hex and row.get('MESSAGE','').startswith(wait.WAIT_MESSAGE)]
    completions=[row for row in journal if row.get('_BOOT_ID')==boot_hex and 'Rollback complete / ' in row.get('MESSAGE','')]
    require(deferrals and completions,'current boot deferral or rollback journal absent')
    earlier,later=deferrals[0],completions[-1]
    require(int(earlier['__MONOTONIC_TIMESTAMP'])<int(later['__MONOTONIC_TIMESTAMP']),'rollback did not follow deferral')
    require(earlier.get('_SYSTEMD_INVOCATION_ID') and later.get('_SYSTEMD_INVOCATION_ID')
            and earlier['_SYSTEMD_INVOCATION_ID']!=later['_SYSTEMD_INVOCATION_ID'],'same-invocation success is not timer retry')
    require(dt.datetime.fromisoformat(cli['observed_at'].replace('Z','+00:00')) >=
            dt.datetime.fromisoformat(proof['sample']['cli']['observed_at'].replace('Z','+00:00')),'terminal time predates wait')
    return {'wait_invocation':earlier['_SYSTEMD_INVOCATION_ID'],'rollback_invocation':later['_SYSTEMD_INVOCATION_ID']}


def verify(directory, operation):
    require(re.fullmatch('[0-9a-f]{32}',operation),'invalid request')
    directory=Path(directory).resolve(strict=True);refs={}
    def raw(name):
        require(Path(name).name==name,'evidence path must be a basename')
        fd=os.open(directory/name,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
        with os.fdopen(fd,'rb') as source:
            info=os.fstat(source.fileno())
            require(stat.S_ISREG(info.st_mode) and info.st_nlink==1 and info.st_size<=1048576,'unsafe/oversized evidence')
            value=source.read(1048577)
            require(len(value)<=1048576 and wait.probe.metadata(info)==wait.probe.metadata(os.fstat(source.fileno())),'evidence changed')
        refs[name]=wait.sha(value);return value
    def obj(name):return wait.probe.strict_object(raw(name))
    def lines(name):return [wait.probe.strict_object(row) for row in raw(name).splitlines()]
    intent_raw=raw('bound-worker-'+operation+'.json');intent=wait.probe.strict_object(intent_raw)
    bound.worker.validate_intent(intent,intent['identity'],operation)
    accepted=bound.validate_start(intent,intent_raw,obj('bound-worker-start-'+operation+'.json'),raw('bound-worker-start-'+operation+'.stdout.jsonl'))
    reset=obj('bound-worker-reboot-result-'+operation+'.json')
    require(reset['operation_id']==operation and reset['identity']==intent['identity'] and reset['start']==accepted,'reset admission differs')
    require(reset['action']=='registered-QEMU-reset-submitted-once' and reset['command']=='system_reset','actual reset not recorded')
    collections={}
    for name,reference in reset['evidence'].items():
        value=raw(Path(reference['path']).name);require(wait.sha(value)==reference['sha256'],'cut evidence digest differs')
        collections[name]=value
    cut=[wait.probe.strict_object(row) for row in collections['worker_cut'].splitlines()]
    association=bound.bind_cut(intent,cut,wait.probe.strict_object(collections['record']))
    require(association==reset['worker_cut'],'genuine worker association differs')
    before=obj('boot-wait-before-boot.json');after=obj('boot-wait-after-boot.json')
    config=obj('boot-wait-'+operation+'.json')
    wait.verify_boot(config,intent,intent_raw,after['boot_id'])
    require(before['operation_id']==after['operation_id']==operation and before['boot_id']==reset['before_boot_id']==config['armed_boot_id'],'boot provenance differs')
    require(before['helper_sha256']==config['script_sha256'],'observer identity differs')
    events=lines('boot-wait-'+operation+'.jsonl')
    require(all(e.get('schema')==wait.SCHEMA and e.get('operation_id')==operation and e.get('boot_id')==after['boot_id'] for e in events),'observer event identity differs')
    names=[e['event'] for e in events]
    require(names[0]=='boot-job-started' and names[-2:]==['native-wait-verified','boot-job-released']
            and all(n=='sample-unavailable' for n in names[1:-2]),'not one successful bounded boot job')
    proof=events[-2]
    require(events[-1]['reason']=='real-native-deferral-observed','boot job timed out')
    require(proof['material']==events[0]['material'],'transaction changed across deferral')
    for key in ('snapshot','binding_sha256'):
        require(proof['material'][key]==association[key],'wait is not bound to actual worker snapshot')
    require(proof['material']['manifest_sha256']==cut[2]['manifest_sha256'],'wait snapshot manifest differs')
    wait.validate_sample(proof['sample'],intent,after['boot_id'])
    terminal=obj('boot-wait-terminal.json');readers=lines('bound-reader-postboot.jsonl');journal=lines('boot-wait-native-journal.jsonl')
    retry=check_terminal(proof,terminal,intent,after['boot_id'],readers,journal)
    floor=raw('boot-wait-floor-foundation.txt').decode('ascii')
    require(floor.count('sequence=82\n')==2 and 'release-commit='+intent['target']['commit']+'\n' in floor,'monotonic floor/foundation differs')
    return {'schema':'celikpanel/native-boot-wait-acceptance/v1','result':'scoped-checks-passed','request_id':operation,
            'platform':'Debian 13 / amd64 / disposable QEMU','baseline':intent['baseline'],'target':intent['target'],
            'faults':['exact genuine update-worker SIGKILL','QMP reset at native rollback payload_restored'],
            'fixture_boot_job_max_seconds':config['max_seconds'],'helper_sha256':config['script_sha256'],
            'wait':{'observed_at':proof['sample']['cli']['observed_at'],'readiness':'starting','readiness_exit':1,
                    'phase':'recovering','terminal_proof':'none','waiting_for':'starting','lock_released':True,
                    'transaction_binding_manifest_unchanged':True,'previous_failure':proof['sample']['status']['previous_failure']},
            'retry':retry,'terminal':terminal['cli'],'stale_wait_retained_but_not_exposed':True,
            'coordinators_restored_to_baseline':True,'authenticated_http':200,'anonymous_http':401,
            'sequence_floor_after_rollback':82,'foundation_sequence_after_rollback':82,'evidence_sha256':refs,
            'limitations':['Fixture signing only; no production signing/distribution proof',
                           'CLI wait only; no HTTP/browser wait access during boot',
                           'No prior failure was recorded during this wait; it was recorded before terminal rollback',
                           'initializing and stopping waits remain unproved natively',
                           'No schema transition or hosted workload continuity acceptance in this case',
                           'Full P0 fault matrix remains open']}


def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--evidence-dir',required=True);parser.add_argument('--operation-id',required=True)
    args=parser.parse_args();print(json.dumps(verify(args.evidence_dir,args.operation_id),indent=2,sort_keys=True))


if __name__=='__main__':
    try:main()
    except (ValueError,OSError,KeyError,IndexError,wait.probe.ProbeError) as exc:
        print('boot wait acceptance refused: '+str(exc),file=sys.stderr);raise SystemExit(2)
