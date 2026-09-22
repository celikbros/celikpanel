import hashlib
import json
from pathlib import Path
import re
import sys
from verify_mail_contract import require, HEX

UNITS=['celikpanel-mail-renewal.service','celikpanel-mail-renewal.timer']

def verify(r):
    require(r.get('schema')=='celikpanel/native-mail-parent/v1','schema differs')
    require(re.fullmatch('[0-9a-f]{40}',r.get('source_commit','')),'source missing')
    for key in ['test_sha256','recovery_sha256','archive_sha256']:
        require(re.fullmatch(HEX,r.get(key,'')),'artifact missing')
    require(r.get('scope')==dict(native_enablement=True,actual_systemd_reload=True,double_process_interruption=True,ordered_inverse=True,management_absent=True,timer_start=True,wants_parent_creation=True,mail_workload=False,production_dispatch=False,whole_update_rollback=False,power_loss=False,automatic_no_work_success=True),'scope differs')
    text=r['log']['text'];require(len(text)<32768 and hashlib.sha256(text.encode()).hexdigest()==r['log']['sha256'],'log differs')
    require('PRIVATE KEY' not in text and '--- FAIL:' not in text,'unsafe/failed final proof')
    cuts=[]
    for phase in ['forward','rollback']:
        require('phase='+phase+'-cut returncode=-9' in text,'process cut missing')
        match=re.findall('native_activity_cut=activity_'+phase+'_acted operation=([0-9a-f]{32}) previous= target=('+HEX+') capture=('+HEX+')',text)
        require(len(match)==1,'cut identity missing');cuts.append(match[0])
    require(cuts[0]==cuts[1],'operation changed')
    op,target,capture=cuts[0];result=r['result']
    require(result['operation']==op and result['target']==target and r['runtime_manifest']['generation']==target,'target differs')
    for key in ['native_files_absent','management_absent','timer_enablement_absent','wants_parent_preserved']:
        require(result.get(key) is True,'final preservation missing')
    require(set(result['phases'])=={'forward-cut','rollback-cut','recover'},'phase missing')
    def observe(values,loaded,active=False):
        require(set(values)==set(UNITS),'unit inventory differs')
        for unit,item in values.items():
            expected=dict(LoadState='loaded' if loaded else 'not-found',FragmentPath='/etc/systemd/system/'+unit if loaded else '',DropInPaths='',NeedDaemonReload='no',ActiveState='active' if active and unit.endswith('.timer') else 'inactive',UnitFileState=('enabled' if unit.endswith('.timer') else 'static') if loaded else '')
            require(item['properties']==expected,'native state differs')
            require(item['code']==0 if loaded else item['code'] in [0,4],'native lookup unverified')
    observe(result['initial'],False)
    for phase,values in result['phases'].items():
        observe(values,phase!='recover',phase=='forward-cut')
        match=re.findall(r'native_observation phase='+phase+r' (\{[^\n]+\})',text)
        require(len(match)==1 and json.loads(match[0])==values,'native log/result differs')
    require('phase=recover returncode=0' in text and '--- PASS: TestMailActivityDisposableVMTransition' in text,'recovery missing')
    require('native_activity_inverse=verified native_units=absent original_inodes=restored operation='+op+' previous= target='+target in text,'ordered inverse missing')
    require('parent_activity_inverse=verified native_files=absent native_units=absent native_no_work_invocation=verified management=absent wants_parent=created_and_retained' in text,'final proof absent')
    invocation=result['native_no_work_invocation']
    require(invocation['ActiveState']=='inactive' and invocation['Result']=='success' and invocation['ExecMainStatus']=='0' and int(invocation['ExecMainStartTimestampMonotonic'])>result['invocation_after_monotonic']>0,'fresh native invocation unverified')
    match=re.findall(r'native_no_work_invocation=(\{[^\n]+\})',text)
    require(len(match)==1 and json.loads(match[0])==invocation,'native invocation log differs')
    parent=result['parent_publication']
    require(parent['schema']=='celikpanel-mail-renewal-parent/v1' and parent['existing'] is False and parent['capture_sha256']==capture and re.fullmatch('[0-9a-f]{32}',parent['nonce']),'parent publication not bound')
    identity=parent['directory']
    require(identity['dev']>0 and identity['ino']>0 and identity['mode']==0o40755 and identity['uid']==0 and identity['gid']==0,'parent identity unsafe')
    match=re.findall(r'parent_publication=(\{[^\n]+\})',text)
    require(len(match)==1 and json.loads(match[0])==parent,'parent publication log differs')
    require(result['fixture_private_parent_preexisting'] is True,'private parent preparation scope missing')
    return {'native_parent_and_timer_inverse':'verified','automatic_no_work_success':'verified','mail_workload':'not established'}

if __name__=='__main__':print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text())),sort_keys=True))
