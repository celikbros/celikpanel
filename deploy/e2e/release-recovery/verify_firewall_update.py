#!/usr/bin/env python3
"""Verify bounded native update/automatic rollback evidence, never mutate a host."""
import json
from pathlib import Path
import re
import sys

SHA=re.compile('[a-f0-9]{64}\\Z')
UUID=re.compile('[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}\\Z')
UNIT='celikpanel-firewall-restore.service'

def require(value,reason):
    if not value:raise ValueError(reason)

def verify_fault(record,rollback):
    fault=rollback['fault'];checkpoint=fault['checkpoint'];helper=checkpoint['firewall_unit']
    require(fault['events']==['armed','worker_frozen','candidate_installed_checkpoint','kill_requested','kill_sent','released'],'fault sequence')
    require(fault['signal']=='SIGKILL' and fault['scope']=='exact-update-unit-cgroup','fault scope')
    require(fault['released']['checkpoint_verified'] is True and fault['released']['kill_sent'] is True and fault['released']['reason']=='exact-update-unit-killed','fault terminal')
    require(checkpoint['operation_id']==rollback['after']['request_id']==helper['operation_id'],'fault operation')
    for field in ('cell_id','node','vm_uuid'):
        require(checkpoint['identity'][field]==rollback['after']['identity'][field]==fault['released']['identity'][field],'fault guest identity')
    bound=checkpoint['bound_worker']
    require(bound['request_id']==helper['operation_id'] and bound['target_commit']==record['fixture_commit'] and bound['snapshot']==checkpoint['snapshot'] and bound['phase']=='running' and bound['terminal_proof']=='none','accepted worker binding')
    require(checkpoint['worker']['boot_id']==rollback['after']['boot_id'],'same pre-reboot fault')
    require(checkpoint['worker']['unit']=='celikpanel-self-update-'+helper['operation_id']+'.service','fault worker')
    require(checkpoint['worker']['running_executable_sha256']==record['baseline']['agent_sha256'],'original worker')
    require('-to-'+record['fixture_commit']+'-' in checkpoint['snapshot'] and checkpoint['verified_files']>0 and SHA.fullmatch(checkpoint['manifest_sha256']),'complete snapshot')
    require(SHA.fullmatch(fault['raw_evidence_sha256']),'private evidence binding')
    return helper,checkpoint


def verify_case(record,key,case,helper,checkpoint):
    phase,terminal=('succeeded','update_verified') if key=='updated' else ('recovered','rollback_verified')
    before=case['before'];after=case['after'];boot=case['boot'];generation=helper['generation']
    require(UUID.fullmatch(after['boot_id']) and UUID.fullmatch(boot['boot_id']) and after['boot_id']!=boot['boot_id'],'actual distinct boot')
    require(after['identity']==boot['identity'] and 'nonce' not in after['identity'] and UUID.fullmatch(after['identity']['vm_uuid']),'guest identity')
    for stage,observation in [('after',after),('boot',boot)]:
        require(observation['schema']=='celikpanel/native-firewall-update/v1','observation schema')
        require(observation['request_id']==after['request_id'],'exact operation')
        status=observation['operation_status']
        require(observation['operation_status_exit']==0 and status['request_id']==after['request_id'] and status['observation']=='known' and status['phase']==phase and status['terminal_proof']==terminal and status['reason']==terminal,'terminal proof')
        if key=='rollback':require(status['previous_failure']=='update_failed','known failure preserved')
        require(observation['https_http_code']=='200','native HTTPS')
        require(observation['policy']['sha256']==before['policy_sha256'] and observation['policy']['mode']==0o600 and observation['policy']['uid']==0,'saved policy')
        require(observation['tables']==before['tables'] and set(before['tables'])=={'celikpanel_fw','celikpanel_lab_other'},'native tables preserved')
        for name in ('agent','panel'):
            u=observation['units']['celikpanel-'+name+'.service']
            require(u['ActiveState']=='active' and u['SubState']=='running' and u['Result']=='success' and u['UnitFileState']=='enabled','management runtime')
            expected=checkpoint['installed_artifacts'][name] if key=='updated' else record['baseline'][name+'_sha256']
            require(observation['binaries'][name]['sha256']==expected,'exact application payload')
        unit=observation['units'][UNIT];enabled=case['before_enablement'] if key=='updated' and stage=='after' else 'enabled'
        require(unit['ActiveState']=='active' and unit['SubState']=='exited' and unit['Result']=='success' and unit['UnitFileState']==enabled,'native firewall state')
        require(set(observation['generations'])=={generation},'retained helper')
        for name,digest,mode in [('restore',helper['helper_sha256'],0o755),('runtime.manifest',helper['manifest_sha256'],0o644),(UNIT,helper['unit_sha256'],0o644)]:
            proof=observation['generations'][generation][name]
            require(proof=={'sha256':digest,'mode':mode,'uid':0,'gid':0},'immutable helper proof')
        expected=helper['unit_sha256'] if key=='updated' else before['unit_sha256']
        require(observation['unit']=={'sha256':expected,'mode':0o644,'uid':0,'gid':0},'intended unit')
    require(before['unit_sha256']!=helper['unit_sha256'],'real unit transition')


def verify(record):
    require(record['schema']=='celikpanel/native-firewall-update-acceptance/v1','schema')
    require(record['fixture_only_changes']==['deploy/release-sequence-policy'],'fixture source delta')
    require(SHA.fullmatch(record['archive_sha256']) and len(record['source_commit'])==40 and len(record['fixture_commit'])==40,'source identity')
    forward,rollback=record['updated'],record['rollback']
    require(forward['before_enablement']=='disabled' and forward['explicit_boot_enablement'] is True,'forward boot scope')
    require(rollback['before']['unit_enabled']=='enabled','rollback baseline enablement')
    require(forward['after']['request_id']!=rollback['after']['request_id'],'distinct operations')
    require(forward['after']['identity']['vm_uuid']!=rollback['after']['identity']['vm_uuid'],'distinct guests')
    require(forward['after']['identity']['node']==rollback['after']['identity']['node']=='debian13','Debian identity')
    helper,checkpoint=verify_fault(record,rollback)
    for key in ('updated','rollback'):verify_case(record,key,record[key],helper,checkpoint)
    boots=2;platforms=['Debian 13']
    if 'arch_rollback' in record:
        arch=record['arch_rollback']
        require(arch['after']['identity']['node']=='arch' and arch['before']['unit_enabled']=='enabled','Arch baseline')
        require(arch['after']['request_id'] not in (forward['after']['request_id'],rollback['after']['request_id']),'distinct Arch operation')
        helper,checkpoint=verify_fault(record,arch);verify_case(record,'rollback',arch,helper,checkpoint)
        boots+=1;platforms.append('Arch')
    return {'update':'verified','automatic_rollback':'verified','boots':boots,'platforms':platforms,'production_UI_admission':False}

if __name__=='__main__':
    print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text())),sort_keys=True))
