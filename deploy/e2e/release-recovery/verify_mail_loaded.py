import hashlib
import json
from pathlib import Path
import re
import sys
from verify_mail_contract import require, HEX, one

def verify(r):
    require(r.get('schema') == 'celikpanel/native-mail-loaded-transition/v1', 'schema differs')
    require(re.fullmatch('[0-9a-f]{40}', r.get('source_commit', '')), 'source missing')
    for key in ['test_sha256','agent_test_sha256','recovery_sha256']:
        require(re.fullmatch(HEX,r.get(key,'')), 'binary identity missing')
    require(r.get('scope') == dict(existing_schedule_forward_inverse=True,actual_systemd_reload=True,interrupted_reload_recovery=True,owner_preference_preserved=True,bootstrap_enrollment=False,production_dispatch=False,whole_update_rollback=False,power_loss=False), 'scope differs')
    text=r['log']['text']
    require(len(text)<32768 and hashlib.sha256(text.encode()).hexdigest()==r['log']['sha256'],'log differs')
    require('PRIVATE KEY' not in text and '--- FAIL:' not in text,'unsafe/failed evidence')
    require('--- PASS: TestMailRenewalDisposableVMHookPreservation' in text,'shared observer not exercised')
    cuts=[]
    for phase in ['forward','rollback']:
        require('phase='+phase+'-cut returncode=-9' in text,'process kill missing')
        match=re.findall('native_loaded_cut=loaded_'+phase+'_reloaded operation=([0-9a-f]{32}) previous=('+HEX+') target=('+HEX+') capture=('+HEX+')',text)
        require(len(match)==1,'cut identity absent');cuts.append(match[0])
    require(cuts[0]==cuts[1],'operation changed')
    op,previous,target,capture=cuts[0]
    require(previous!=target and r['runtime_manifest']['generation']==target,'target differs')
    for phase,generation in [('forward-cut',target),('rollback-cut',previous),('recover',previous)]:
        require('loaded_exec phase='+phase+' generation='+generation+' verified=yes' in text,'actual loaded command differs')
    require('native_loaded_inverse=verified timer_preference=preserved original_inodes=restored operation='+op+' previous='+previous+' target='+target in text,'inverse proof missing')
    require('phase=recover returncode=0' in text and '--- PASS: TestMailLoadedDisposableVMTransition' in text,'recovery missing')
    raw=one(r'loaded_native_files=original_inodes_restored configuration_ledger=unchanged management=absent trusted_listeners=(\{[^\n]+\})',text);listeners=json.loads(raw)
    require(set(listeners)=={'587','993'} and len(set(listeners.values()))==1 and all(re.fullmatch(HEX,v) for v in listeners.values()),'trusted listeners differ')
    require(text.count('NeedDaemonReload=no')==2 and 'UnitFileState=enabled' in text and 'ActiveState=active' in text,'final native schedule missing')
    return {'existing_loaded_schedule':'verified','production_dispatch':'not established'}

if __name__=='__main__':print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text())),sort_keys=True))
