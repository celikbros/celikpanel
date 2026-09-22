import hashlib
import json
from pathlib import Path
import re
import sys
from verify_mail_contract import require, HEX, one

def verify(r):
    require(r.get('schema') == 'celikpanel/native-mail-file-transition/v1', 'schema differs')
    require(re.fullmatch('[0-9a-f]{40}', r.get('source_commit', '')), 'source missing')
    for key in ['test_sha256', 'recovery_sha256']:
        require(re.fullmatch(HEX, r.get(key, '')), 'binary identity missing')
    require(r.get('scope') == dict(native_file_exchange=True, interrupted_inverse_exchange=True, original_inodes_restored=True, trusted_mail_listeners=True, native_schedule_activation=False, production_dispatch=False, whole_update_rollback=False, power_loss=False), 'scope differs')
    expected = {'mail-files-native.log', 'mail-files-native-resume.log', 'mail-files-certbot-filter.log', 'mail-files-certbot-inventory.log'}
    require(set(r['logs']) == expected, 'logs missing')
    logs = {}
    for name, item in r['logs'].items():
        require(len(item['text']) < 32768 and hashlib.sha256(item['text'].encode()).hexdigest() == item['sha256'], 'log differs')
        require('PRIVATE KEY' not in item['text'], 'private evidence'); logs[name] = item['text']
    require('explicit target required' in logs['mail-files-native.log'] and '--- FAIL:' in logs['mail-files-native.log'], 'first refusal omitted')
    current = logs['mail-files-native-resume.log']
    require('prior_guard_refusal=preserved native_files=unchanged journal=empty' in current, 'refusal state not verified')
    ids = []
    for phase in ['forward', 'rollback']:
        require('phase='+phase+'-cut returncode=-9' in current, 'actual process kill missing')
        match = re.findall(r'native_cut='+phase+r'_celikpanel-mail-host-cert operation=([0-9a-f]{32}) previous=('+HEX+r') target=('+HEX+r') capture=('+HEX+r')', current)
        require(len(match) == 1, 'cut identity missing'); ids.append(match[0])
    require(ids[0] == ids[1], 'operation changed between cuts')
    op, previous, target, capture = ids[0]
    require(previous != target and r['runtime_manifest']['generation'] == target, 'target differs')
    require('native_inverse=verified original_inodes=restored capture=preserved direction=rollback operation='+op+' previous='+previous+' target='+target in current, 'inverse proof missing')
    require('phase=recover returncode=0' in current and '--- PASS: TestMailFilesDisposableVMTransition' in current, 'recovery did not pass')
    require(current.count('NeedDaemonReload=no') == 2 and 'UnitFileState=enabled' in current and 'ActiveState=active' in current, 'native schedule not observed')
    raw = one(r'native_files=original_inodes_restored configuration_ledger=unchanged management=absent trusted_listeners=(\{[^\n]+\})', current)
    listeners = json.loads(raw)
    require(set(listeners) == {'587','993'} and len(set(listeners.values())) == 1 and all(re.fullmatch(HEX,v) for v in listeners.values()), 'listeners differ')
    require('certbot_hook_inventory=one_live_hook staging_directories=excluded' in logs['mail-files-certbot-inventory.log'], 'stages not excluded')
    return {'native_inverse': 'verified', 'schedule_activation': 'not established'}

if __name__ == '__main__': print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text())), sort_keys=True))
