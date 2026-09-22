import hashlib
import json
from pathlib import Path
import re
import sys
from verify_mail_contract import require, HEX

UNITS = ['celikpanel-mail-renewal.service', 'celikpanel-mail-renewal.timer']

def verify(r):
    require(r.get('schema') == 'celikpanel/native-mail-bootstrap-loaded/v1', 'schema differs')
    for key in ['source_commit', 'test_source_commit']:
        require(re.fullmatch('[0-9a-f]{40}', r.get(key, '')), 'source missing')
    for key in ['test_sha256', 'recovery_sha256', 'archive_sha256']:
        require(re.fullmatch(HEX, r.get(key, '')), 'artifact identity missing')
    require(r.get('scope') == dict(initial_idle_unit_loading=True, actual_systemd_reload=True, double_process_interruption=True, positive_absence_restored=True, management_absent=True, timer_activation=False, mail_workload=False, production_dispatch=False, whole_update_rollback=False, power_loss=False), 'scope differs')
    def text(item):
        value=item['text']
        require(len(value)<32768 and hashlib.sha256(value.encode()).hexdigest()==item['sha256'], 'log differs')
        require('PRIVATE KEY' not in value, 'secret evidence')
        return value
    log=text(r['log'])
    require('--- FAIL:' not in log, 'failed final evidence')
    refusals=r['preparation_refusals']
    require(len(refusals)==2, 'preparation history lost')
    require('runtime_changed' in text(refusals[0]), 'source-mode refusal lost')
    require('fixture marker missing' in text(refusals[1]), 'fixture-guard refusal lost')
    require(all('native_bootstrap_loaded_cut=' not in text(x) for x in refusals), 'refusal had unaccounted native mutation')
    cuts=[]
    for phase in ['forward','rollback']:
        require('phase='+phase+'-cut returncode=-9' in log, 'process kill missing')
        matches=re.findall('native_bootstrap_loaded_cut=loaded_'+phase+'_reloaded operation=([0-9a-f]{32}) previous= target=('+HEX+') capture=('+HEX+')',log)
        require(len(matches)==1, 'cut identity absent');cuts.append(matches[0])
    require(cuts[0]==cuts[1], 'operation changed')
    op,target,capture=cuts[0]
    result=r['result']
    require(result['operation']==op and result['target']==target and r['runtime_manifest']['generation']==target, 'target differs')
    for key in ['native_files_absent','management_absent','timer_enablement_absent']:
        require(result.get(key) is True, 'restoration incomplete')
    require(set(result['phases'])=={'forward-cut','rollback-cut','recover'}, 'phase missing')
    def observe(values,loaded):
        require(set(values)==set(UNITS), 'unit inventory differs')
        for unit,item in values.items():
            expected=dict(LoadState='loaded' if loaded else 'not-found',FragmentPath='/etc/systemd/system/'+unit if loaded else '',DropInPaths='',NeedDaemonReload='no',ActiveState='inactive',UnitFileState=('disabled' if unit.endswith('.timer') else 'static') if loaded else '')
            require(item['properties']==expected, 'native state differs')
            require(item['code']==0 if loaded else item['code'] in [0,4], 'native status unverified')
    observe(result['initial'],False)
    for phase,values in result['phases'].items():
        observe(values,phase=='forward-cut')
        matches=re.findall(r'native_observation phase='+phase+r' (\{[^\n]+\})',log)
        require(len(matches)==1 and json.loads(matches[0])==values, 'native observation/result differs')
    require('phase=recover returncode=0' in log and '--- PASS: TestMailBootstrapLoadedDisposableVMTransition' in log, 'recovery missing')
    require('native_bootstrap_inverse=verified native_units=absent original_inodes=restored operation='+op+' previous= target='+target in log, 'inverse proof absent')
    require('bootstrap_inverse=verified native_files=absent native_units=absent timer=never_enabled management=absent' in log, 'final absence missing')
    return {'initial_idle_loading_and_compensation':'verified','timer_activation':'not established'}

if __name__=='__main__': print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text())),sort_keys=True))
