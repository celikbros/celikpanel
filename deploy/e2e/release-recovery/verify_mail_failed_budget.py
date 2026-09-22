import hashlib
import json
from pathlib import Path
import re
import sys
from verify_mail_contract import require, HEX, one

def verify(r):
    require(r.get('schema')=='celikpanel/native-mail-failed-budget/v1','schema differs')
    require(re.fullmatch('[0-9a-f]{40}',r.get('source_commit','')),'source missing')
    require(re.fullmatch(HEX,r.get('binary_sha256','')),'binary missing')
    require(r.get('scope')==dict(terminal_failed_retry=True,fresh_processes=True,native_owner_config_conflict=True,same_operation_owner_retry=True,management_absent=True,automatic_timer_dispatch=False,preselection_interruption=False,production_enrollment=False,power_loss=False),'scope differs')
    text=r['log']['text']
    require(len(text)<32768 and hashlib.sha256(text.encode()).hexdigest()==r['log']['sha256'],'log differs')
    require('PRIVATE KEY' not in text and 'Traceback' not in text,'unsafe/failed evidence')
    attempts=re.findall(r'failed_attempt=([123]) request=([0-9a-f]{32}) selected=unchanged owner_conflict=preserved queue=preserved',text)
    require(len(attempts)==3 and [a for a,_ in attempts]==['1','2','3'] and len({i for _,i in attempts})==1,'bounded attempts differ')
    request=attempts[0][1]
    require('--retry-failed '+request in text,'owner continuation missing')
    require('automatic_budget=exhausted wrong_owner=refused ledger=unchanged trusted_old_listeners=verified' in text,'exhaustion/owner proof missing')
    raw=one(r'explicit_failed_retry=verified request='+request+r' attempt=4 leaf=('+HEX+r') trusted_listeners=',text)
    listeners=json.loads(one(r'trusted_listeners=(\{[^\n]+?\}) original_native_inodes=restored foreign_history=preserved queue=acknowledged management=absent timer=restored',text))
    require(listeners=={'587':raw,'993':raw},'trusted listener leaf differs')
    return {'failed_retry_budget':'verified','production_enrollment':'not established'}

if __name__=='__main__':print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text())),sort_keys=True))
