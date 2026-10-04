import hashlib
import json
from pathlib import Path
import re
import sys
from verify_mail_contract import require, HEX, one

def verify(r):
    require(r.get('schema') == 'celikpanel/native-mail-hook-preservation/v1', 'schema differs')
    require(re.fullmatch('[0-9a-f]{40}', r.get('source_commit', '')) and re.fullmatch(HEX, r.get('test_sha256', '')), 'source identity absent')
    require(r.get('scope') == {'native_hook_preservation': True, 'owner_edit_refusal': True, 'legacy_parent_metadata_preserved': True, 'trusted_mail_listeners': True, 'production_enrollment': False, 'panel_rollback': False}, 'scope differs')
    names = {'mail-hook-preserve.log', 'mail-hook-observation-metadata.log', 'mail-hook-native-preserve.log', 'mail-hook-owner.log'}
    require(set(r['logs']) == names, 'evidence missing')
    logs = {}
    for n, item in r['logs'].items():
        require(len(item['text']) < 32768 and hashlib.sha256(item['text'].encode()).hexdigest() == item['sha256'], 'log differs')
        require('PRIVATE KEY' not in item['text'], 'private evidence'); logs[n] = item['text']
    require('runtime_unsafe_metadata' in logs['mail-hook-preserve.log'] and '--- FAIL:' in logs['mail-hook-preserve.log'], 'earlier refusal omitted')
    require('root celikpanel renewal-hooks' in logs['mail-hook-observation-metadata.log'] and 'root celikpanel deploy' in logs['mail-hook-observation-metadata.log'], 'legacy parent layout missing')
    native = logs['mail-hook-native-preserve.log']
    generation = one('independent_hook_preserved generation=('+HEX+') loaded_schedule=verified', native)
    require('--- PASS: TestMailRenewalDisposableVMHookPreservation' in native and 'UnitFileState=enabled' in native and 'NeedDaemonReload=no' in native, 'native schedule missing')
    owner = logs['mail-hook-owner.log']
    require('fixture_owner_hook_edit=explicit before_image=retained' in owner and 'owner_hook_refused bytes_inode_mtime=preserved' in owner, 'owner refusal missing')
    require('independent_hook_preserved generation='+generation+' loaded_schedule=verified' in owner, 'restored enrollment differs')
    raw = one(r'fixture_owner_resolution=explicit hook_bytes=restored native_config_units_ledger=unchanged trusted_listeners=(\{[^\n]+\}) management=absent', owner)
    served = json.loads(raw); require(set(served) == {'587','993'} and len(set(served.values())) == 1 and all(re.fullmatch(HEX,v) for v in served.values()), 'trusted listeners differ')
    return {'native_hook': 'preserved', 'production_enrollment': 'not established'}

if __name__ == '__main__': print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text())), sort_keys=True))
