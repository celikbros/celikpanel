import hashlib
import json
from pathlib import Path
import re
import sys
from verify_mail_contract import require, HEX

UNITS=['celikpanel-mail-renewal.service','celikpanel-mail-renewal.timer']

def verify(r):
    require(r.get('schema')=='celikpanel/native-mail-enable/v1','schema differs')
    require(re.fullmatch('[0-9a-f]{40}',r.get('source_commit','')),'source missing')
    for key in ['test_sha256','recovery_sha256','archive_sha256']:
        require(re.fullmatch(HEX,r.get(key,'')),'artifact missing')
    require(r.get('scope')==dict(native_enablement=True,actual_systemd_reload=True,double_process_interruption=True,ordered_inverse=True,management_absent=True,timer_start=False,wants_parent_creation=False,mail_workload=False,production_dispatch=False,whole_update_rollback=False,power_loss=False),'scope differs')
    text=r['log']['text'];require(len(text)<32768 and hashlib.sha256(text.encode()).hexdigest()==r['log']['sha256'],'log differs')
    require('PRIVATE KEY' not in text and '--- FAIL:' not in text,'unsafe/failed final proof')
    cuts=[]
    for phase in ['forward','rollback']:
        require('phase='+phase+'-cut returncode=-9' in text,'process cut missing')
        match=re.findall('native_enable_cut=enable_'+phase+'_reloaded operation=([0-9a-f]{32}) previous= target=('+HEX+') capture=('+HEX+')',text)
        require(len(match)==1,'cut identity missing');cuts.append(match[0])
    require(cuts[0]==cuts[1],'operation changed')
    op,target,capture=cuts[0];result=r['result']
    require(result['operation']==op and result['target']==target and r['runtime_manifest']['generation']==target,'target differs')
    for key in ['native_files_absent','management_absent','timer_enablement_absent','wants_parent_preserved']:
        require(result.get(key) is True,'final preservation missing')
    require(set(result['phases'])=={'forward-cut','rollback-cut','recover'},'phase missing')
    def observe(values,loaded,enabled=False):
        require(set(values)==set(UNITS),'unit inventory differs')
        for unit,item in values.items():
            expected=dict(LoadState='loaded' if loaded else 'not-found',FragmentPath='/etc/systemd/system/'+unit if loaded else '',DropInPaths='',NeedDaemonReload='no',ActiveState='inactive',UnitFileState=(('enabled' if enabled else 'disabled') if unit.endswith('.timer') else 'static') if loaded else '')
            require(item['properties']==expected,'native state differs')
            require(item['code']==0 if loaded else item['code'] in [0,4],'native lookup unverified')
    observe(result['initial'],False)
    for phase,values in result['phases'].items():
        observe(values,phase!='recover',phase=='forward-cut')
        match=re.findall(r'native_observation phase='+phase+r' (\{[^\n]+\})',text)
        require(len(match)==1 and json.loads(match[0])==values,'native log/result differs')
    require('phase=recover returncode=0' in text and '--- PASS: TestMailEnableDisposableVMTransition' in text,'recovery missing')
    require('native_enable_inverse=verified native_units=absent original_inodes=restored operation='+op+' previous= target='+target in text,'ordered inverse missing')
    require('enable_inverse=verified native_files=absent native_units=absent timer=never_started management=absent wants_parent=preserved' in text,'final proof absent')
    return {'native_enablement_and_inverse':'verified','timer_start':'not established'}

if __name__=='__main__':print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text())),sort_keys=True))
