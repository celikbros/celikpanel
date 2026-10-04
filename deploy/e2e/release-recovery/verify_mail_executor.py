#!/usr/bin/env python3
"""Verify bounded independent mail executor evidence, not host attestation."""
import hashlib
import json
from pathlib import Path
import re
import sys
from verify_mail_contract import HEX, UUID, one, require


def verify(record):
    require(record.get('schema')=='celikpanel/native-mail-executor/v1','executor schema differs')
    require(re.fullmatch(r'[0-9a-f]{40}',record.get('source_commit','')),'source identity missing')
    helper=record.get('helper_binary_sha256','');test=record.get('test_binary_sha256','')
    require(re.fullmatch(HEX,helper) and re.fullmatch(HEX,test) and helper!=test,'separate binaries not identified')
    expected={'separate_renewal_executable':True,'real_native_mail':True,'fixture_ca_certbot_source':True,'orderly_reboot':True,'installed_management_absent':True,'runtime_absence_refusal':True,'production_enrollment':False,'public_acme':False,'automatic_boot_renewal':False,'power_loss':False,'independent_interrupted_recovery':False}
    require(record.get('scope')==expected,'unsupported executor scope')
    names={'mail-prepare-independent.log','mail-helper-negative.log','mail-helper-native-result.log','mail-helper-replay.log','mail-helper-native-boot.log','mail-helper-runtime-gap.log'}
    require(set(record.get('logs',{}))==names,'executor logs differ')
    logs={}
    for name,item in record['logs'].items():
        text=item['text'];require(len(text)<32768 and hashlib.sha256(text.encode()).hexdigest()==item['sha256'],'executor log digest differs')
        require('PRIVATE KEY' not in text and 'nonce=' not in text,'private executor evidence present')
        logs[name]=text
    prepared=logs['mail-prepare-independent.log']
    require(prepared.startswith(test+'  /root/celikpanel-release-recovery-lab/mail-agent.test\n'),'fixture executable differs')
    require('--- PASS: TestMailHostCertificateDisposableVMPrepareIndependentRenewal (' in prepared and 'Result=success' in prepared and 'ExecMainStatus=0' in prepared and '--- FAIL:' not in prepared,'initial preparation failed')
    lineage,old,new=one(r'independent renewal source prepared; lineage=(celikpanel-mail-[a-f0-9]{24}) selected=('+HEX+') source=('+HEX+')',prepared)
    require(old!=new,'no new source generation')
    before=one(r'(?m)^('+UUID+')$',prepared)
    for name in names-{'mail-prepare-independent.log','mail-helper-native-boot.log'}:
        require(logs[name].startswith(helper+'  /root/celikpanel-release-recovery-lab/mail-renewal\n'),'executed helper differs')
    for name in ('mail-helper-negative.log','mail-helper-native-result.log','mail-helper-replay.log'):
        require(one(r'(?m)^('+UUID+')$',logs[name])==before,'preboot execution differs')
    negative=logs['mail-helper-negative.log']
    observations=[json.loads(line) for line in negative.splitlines() if line.startswith('{')]
    require([(v['args'],v['exit']) for v in observations]==[
        (['--inspect-build-identity'],0),(['--self-update-worker','0'*32],2),(['--initialize-service-mutation-ledger'],2),([],2),(['--internal-service-mutation-supervisor','/run/celikpanel/service-mutation.lock','/bin/sh','-c','true'],125)],'negative entry scope differs')
    require('component=mail-renewal\n' in observations[0]['stdout'],'wrong component entry')
    require('helper_negative_scope=verified; native_config_ledger_selection_unchanged=yes; override_init_write=absent' in negative,'negative scope changed native state')
    result=logs['mail-helper-native-result.log']
    require('installed_agent_absent=yes' in result and 'installed_panel_absent=yes' in result,'installed management present')
    require('/mail-renewal --queue '+lineage in result and '/mail-renewal --process-pending' in result,'helper lifecycle absent')
    require(result.count('Result=success')==2 and result.count('ExecMainStatus=0')==2 and result.count('ActiveState=inactive')==2,'native helper units failed')
    require('native_configuration_bytes_and_mtimes=unchanged' in result,'helper rewrote native configuration')
    request,leaf=one(r'independent_renewal_completed request=([0-9a-f]{32}) leaf=('+HEX+') queue=absent',result)
    require(leaf==new,'completed different source')
    fingerprint=':'.join(new[i:i+2].upper() for i in range(0,64,2))
    pattern=r'(?m)^sha256 Fingerprint=(.*)$'
    require(re.findall(pattern,result)==[fingerprint]*2,'renewed listeners differ')
    require('independent_same_leaf_replay=acknowledged; canonical_ledger=unchanged' in logs['mail-helper-replay.log'],'same-leaf replay changed ledger')
    boot=logs['mail-helper-native-boot.log'];after=one(r'(?m)^('+UUID+')$',boot)
    require(after!=before and boot.splitlines()[1:4]==['active','active','inactive'],'native boot/service state differs')
    require('installed_agent_absent=yes' in boot and 'installed_panel_absent=yes' in boot and 'volatile_management_runtime=absent' in boot,'management/runtime absence not observed')
    require(re.findall(pattern,boot)==[fingerprint]*2,'postboot native listeners differ')
    gap=logs['mail-helper-runtime-gap.log']
    require(one(r'(?m)^('+UUID+')$',gap)==after,'runtime refusal boot differs')
    require('missing_runtime_refusal=verified; pending=retained; ledger_native_config=unchanged' in gap,'missing runtime not preserved')
    return {'separate_executable_renewal':'verified','native_postboot_mail':'verified','operation':request,'production_enrollment':'not established','automatic_boot_renewal':'not established'}

if __name__=='__main__':
    print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text())),sort_keys=True))
