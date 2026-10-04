import json
import re
from pathlib import Path
import sys
from verify_mail_contract import require, HEX

def verify(r):
    require(r.get('schema')=='celikpanel/native-agent-compatibility/v1','schema differs')
    require(re.fullmatch('[0-9a-f]{40}',r.get('source_commit','')),'source missing')
    require(r.get('lab')=={'cell_id':'release-recovery__3688d130a0721786','local_qemu_loopback':True},'lab differs')
    require(r.get('scope')==dict(read_only_native_admission=True,private_resource_sigkill=True,debian_mail_workload=True,arch_mail_workload=False,production_enrollment=False,whole_application_rollback=False),'scope inflated')
    deb,arch=r['debian13'],r['arch']
    require(deb['schema']=='celikpanel-native-agent-compatibility/v1' and arch['schema']=='celikpanel-native-agent-compatibility-absent/v1','producer differs')
    require(deb['source_commit']==arch['source_commit']==r['source_commit'],'source binding differs')
    require(deb['files']==arch['files'] and set(deb['files'])=={'bin/agent','bin/agent-native-contract.json','bin/recovery','bin/publication.test'},'artifact inventory differs')
    for digest in deb['files'].values():require(re.fullmatch(HEX,digest),'artifact digest missing')
    c=deb['contract'];require(c==dict(schema='celikpanel-agent-native-contract/v1',source_commit=r['source_commit'],agent_sha256=deb['files']['bin/agent'],mail_hook_policy='preserve-independent-v1'),'declaration differs')
    require(re.fullmatch(HEX,deb['generation']),'native helper identity missing')
    require(set(deb['cases'])=={'supported','unmarked','edited-agent','edited-contract','linked-agent'},'native cases missing')
    for name,item in deb['cases'].items():
        require(item['exit']==(0 if name=='supported' else 3),'native refusal differs')
        require(item['stdout']=='','unexpected public output')
        require(re.fullmatch(HEX,item['agent_sha256']),'Agent identity missing')
        if name=='supported':require(item['stderr']=='' and item['agent_sha256']==c['agent_sha256'],'supported claim differs')
        else:require('same operation' in item['stderr'] and 'keep the current renewal files' in item['stderr'],'actionable refusal missing')
    require(deb['strict_candidate_exit']==0,'candidate verification missing')
    require(deb['workload_before']==deb['workload_after'] and len(deb['workload_before'])>=12,'workload material changed')
    require(deb['served']==dict(smtp=deb['leaf_sha256'],imap=deb['leaf_sha256']) and re.fullmatch(HEX,deb['leaf_sha256']),'trusted listener proof differs')
    require(deb['timer_enabled_active'] is True,'renewal schedule lost')
    require(arch['absent_before']==arch['absent_after'] and len(arch['absent_before'])==6,'absent native state changed')
    require(arch['wants_before']==arch['wants_after'] and len(arch['wants_before'])==6,'shared wants parent changed')
    require(arch['native_enrollment'] is False,'Arch enrollment invented')
    require(set(arch['cases'])=={'supported','unmarked','edited-agent'},'absence cases missing')
    for name,item in arch['cases'].items():
        require(item['compatibility_exit']==0,'legacy installation blocked by absent declaration')
        require(item['candidate_exit']==(0 if name=='supported' else 3),'candidate gate skipped when hook absent')
    for node in [deb,arch]:
        require(node['management_absent'] is True,'management absence missing')
        require(re.fullmatch('[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}',node['boot_id']),'boot identity missing')
        log=node['publication_log'];require(log.rstrip().endswith('PASS'),'publication test did not finish')
        for name in ['TestAgentDeclarationTravelsWithAtomicPublicationAndInverse','TestAgentDeclarationEditAfterForwardPublicationBlocksInverse','TestAgentDeclarationMaterialRollbackWithoutCandidate']:
            require('--- PASS: '+name in log,'publication boundary missing')
        for case in ['complete','stage_file_written','intent_durable','exchange_done','receipt_durable']:
            for suffix in ['', '-prior-contract']:
                require('--- PASS: TestAgentDeclarationTravelsWithAtomicPublicationAndInverse/'+case+suffix+' ' in log,'old/new interruption case missing')
    return {'native_admission':'verified','resource_inverse':'verified on Debian and Arch','production_enrollment':'not established'}

if __name__=='__main__':print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text())),sort_keys=True))
