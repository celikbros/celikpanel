import hashlib
import json
from pathlib import Path
import re
import sys
from verify_mail_contract import require, HEX

def verify(r):
    require(r.get('schema')=='celikpanel/native-mail-unselected/v1','schema differs')
    require(re.fullmatch('[0-9a-f]{40}',r.get('source_commit','')),'source missing')
    for key in ['binary_sha256','test_sha256']:require(re.fullmatch(HEX,r.get(key,'')),'binary identity missing')
    require(r.get('lab')==dict(cell_id='release-recovery__3688d130a0721786',node='debian13',local_qemu_loopback=True),'lab differs')
    require(r.get('scope')==dict(before_ledger_cut=True,leased_cut=True,unselected_intent_cut=True,terminal_write_cut=True,fresh_processes=True,management_absent=True,automatic_timer_dispatch=False,production_enrollment=False,cross_build_adoption=False,reboot=False,power_loss=False),'scope differs')
    result=r['result']
    require(result.get('schema')=='celikpanel-mail-unselected-native/v1' and result.get('commit')==r['source_commit'],'producer differs')
    for key in ['management_absent','native_timer_paused_for_faults','native_timer_restored']:require(result.get(key) is True,'runtime scope missing')
    rows=result['trials'];require(len(rows)==3 and [row['point'] for row in rows]==['before_ledger','leased','intent'],'cuts differ')
    require(len({row['request'] for row in rows})==3 and len({row['trial'] for row in rows})==3,'operation identities overlap')
    expected_logs=set();previous=None
    for row,attempt in zip(rows,[1,2,2]):
        for key in ['trial','request','owner']:require(re.fullmatch('[0-9a-f]{32}',row.get(key,'')),'exact identity missing')
        for key in ['old_leaf','new_leaf','before_sha256']:require(re.fullmatch(HEX,row.get(key,'')),'material proof missing')
        require(row['new_leaf']!=row['old_leaf'] and (previous is None or row['old_leaf']==previous),'selection chain differs');previous=row['new_leaf']
        require(type(row['attempt']) is int and row['attempt']==attempt,'execution budget differs')
        for key in ['before_selection_unchanged','config_unchanged','prior_jobs_unchanged']:require(row.get(key) is True,'owner preservation missing')
        require(row['served']=={'smtp587':row['new_leaf'],'imap993':row['new_leaf']},'trusted listeners differ')
        terminal=row['point']=='intent';require(row['terminal_cut'] is terminal,'terminal cut missing')
        stages=row['retained_stages'];require(bool(stages)==terminal,'unselected stage retention differs')
        for name,ino,digest in stages:require(re.fullmatch(r'\.panel-cert-[0-9a-f]{32}',name) and type(ino) is int and ino>0 and re.fullmatch(HEX,digest),'retained stage identity missing')
        for point in [row['point']]+(['terminal'] if terminal else []):
            name='celikpanel-mail-unselected-'+row['trial']+'-'+point+'.log';expected_logs.add(name);text=r['journals'][name]['text']
            require('Finished with result: signal' in text and 'code=killed, status=9/KILL' in text,'actual process kill missing')
            test='TestMailRenewalDisposableVMKillUnselectedTerminal' if point=='terminal' else 'TestMailRenewalDisposableVMKillBeforeSelection'
            require('=== RUN   '+test in text,'wrong native fault executor')
        name='celikpanel-mail-unselected-resume-'+row['trial']+'.log';expected_logs.add(name);text=r['journals'][name]['text']
        require('Finished with result: success' in text and 'code=exited, status=0/SUCCESS' in text and 'mail-unselected-renew-v2 --process-pending' in text,'fresh helper continuation missing')
    require(set(r['journals'])==expected_logs,'unexpected/missing native journal')
    for item in r['journals'].values():
        text=item['text'];require(len(text)<16000 and hashlib.sha256(text.encode()).hexdigest()==item['sha256'],'journal bytes differ')
        require('PRIVATE KEY' not in text and 'Traceback' not in text,'unsafe or failed accepted evidence')
    require(len(r['retained_preparations'])==2 and r['retained_preparations'][1]['result']=='not_accepted_as_native_proof','preparation failures concealed')
    return {'unselected_recovery':'verified','actual_sigkills':4,'production_enrollment':'not established'}

if __name__=='__main__':print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text())),sort_keys=True))
