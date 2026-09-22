import hashlib
import json
from pathlib import Path
import re
import sys
from verify_mail_contract import require, HEX

def verify(r):
    require(r.get('schema')=='celikpanel/native-mail-unselected-boot/v1','schema differs')
    require(re.fullmatch('[0-9a-f]{40}',r.get('source_commit','')),'source missing')
    for key in ['binary_sha256','test_sha256']:require(re.fullmatch(HEX,r.get(key,'')),'binary identity missing')
    require(r.get('lab')==dict(cell_id='release-recovery__3688d130a0721786',node='debian13',local_qemu_loopback=True),'lab differs')
    require(r.get('scope')==dict(automatic_timer_dispatch=True,management_absent=True,graceful_reboot=True,power_loss=False,production_enrollment=False,cross_build_adoption=False,old_application_rollback=False),'scope differs')
    before,result=r['before'],r['result'];kit=r['kit_manifest']
    require(before['schema']=='celikpanel-mail-boot-pending/v1' and result['schema']=='celikpanel-mail-boot-result/v1','producer schema differs')
    require(before['commit']==result['commit']==r['source_commit'],'source differs')
    require(kit['schema']=='celikpanel-mail-renewal-runtime/v1' and kit['binary_sha256']==r['binary_sha256'],'selected executable differs')
    require(kit['ledger_version']==kit['plan_version']==1 and kit['receipt_schema']=='mail-host-certificate-receipt/v1','artifact contract differs')
    for key in ['generation','service_sha256','timer_sha256','hook_sha256']:require(re.fullmatch(HEX,kit.get(key,'')),'kit identity missing')
    require(before['generation']==result['generation']==kit['generation'],'native generation differs')
    for key in ['trial','request','owner']:
        require(re.fullmatch('[0-9a-f]{32}',before.get(key,'')) and before[key]==result[key]==before['cut'][key],'exact operation differs')
    require(before['cut']['build']==r['source_commit'] and before['cut']['point']=='intent','fault identity differs')
    for key in ['old_leaf','new_leaf','before_sha256']:require(re.fullmatch(HEX,before.get(key,'')),'material missing')
    require(before['old_leaf']==result['leaf_before']==before['cut']['old_leaf'] and before['new_leaf']==result['leaf_after']==before['cut']['new_leaf'] and result['leaf_before']!=result['leaf_after'],'leaf chain differs')
    require(before['served_before']=={'smtp587':result['leaf_before'],'imap993':result['leaf_before']} and result['served']=={'smtp587':result['leaf_after'],'imap993':result['leaf_after']},'trusted listeners differ')
    require(type(before['attempt_before']) is int and before['attempt_before']==1 and type(result['attempt']) is int and result['attempt']==2,'attempt budget differs')
    for key in ['management_absent','prior_jobs_unchanged','native_config_unchanged','old_generation_preserved','before_image_unchanged','unselected_stage_preserved','timer_enabled_active']:require(result.get(key) is True,'preservation missing')
    for key in ['boot_before','boot_after']:require(re.fullmatch('[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}',result.get(key,'')),'boot identity missing')
    require(before['boot_id']==result['boot_before'] and result['boot_before']!=result['boot_after'],'reboot missing')
    require(bool(before['retained_stages']),'stage missing')
    for name,ino,digest in before['retained_stages']:require(re.fullmatch(r'\.panel-cert-[0-9a-f]{32}',name) and type(ino) is int and ino>0 and re.fullmatch(HEX,digest),'stage identity differs')
    unit=result['native_unit'];require('Result=success' in unit and 'ExecMainStatus=0' in unit and '/'+kit['generation']+'/renew' in unit,'native result differs')
    events=result['native_journal'];require(len(events)>=3,'native journal missing')
    require(all(e['_BOOT_ID']==result['boot_after'].replace('-','') for e in events),'journal is from another boot')
    messages=[e['MESSAGE'] for e in events]
    require(any(x.startswith('Starting celikpanel-mail-renewal.service') for x in messages) and any(x.startswith('Finished celikpanel-mail-renewal.service') for x in messages),'native automatic execution missing')
    require(not any('Failed' in x or 'failed' in x for x in messages),'unresolved native failure')
    require(set(r['logs'])=={'kill','enrollment','first_preparation','second_preparation'},'journal inventory differs')
    for item in r['logs'].values():require(hashlib.sha256(item['text'].encode()).hexdigest()==item['sha256'],'log hash differs')
    kill=r['logs']['kill']['text'];require('status=9/KILL' in kill and 'TestMailRenewalDisposableVMKillBeforeSelection' in kill and before['trial'] in kill,'actual bound kill missing')
    enrollment=r['logs']['enrollment']['text'];require('native_loaded_forward=verified' in enrollment and 'target='+kit['generation'] in enrollment and 'commit='+r['source_commit'] in enrollment,'native dispatch selection missing')
    require(len(r['retained_preparations'])==2 and all(x.get('accepted') is False for x in r['retained_preparations']),'preparation failures concealed')
    return {'automatic_boot_continuation':'verified','attempt':2,'production_enrollment':'not established'}

if __name__=='__main__':print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text())),sort_keys=True))
