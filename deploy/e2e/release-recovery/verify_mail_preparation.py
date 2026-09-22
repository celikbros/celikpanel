#!/usr/bin/env python3
"""Verify bounded native preparation evidence; not an attestation or enrollment."""
import hashlib
import json
from pathlib import Path
import re
import sys
from verify_mail_contract import require, HEX

def verify(r):
    require(r.get('schema') == 'celikpanel/native-mail-preparation/v1', 'unknown schema')
    require(re.fullmatch(r'[0-9a-f]{40}', r.get('source_commit', '')), 'source absent')
    for field in ['test_sha256', 'cli_sha256']: require(re.fullmatch(HEX, r.get(field, '')), 'executable hash absent')
    require(r.get('scope') == {'native_cli': True, 'native_sigkill': True, 'inherited_lock': True, 'owner_content_preserved': True, 'production_enrollment': False, 'power_loss': False}, 'scope differs')
    require(set(r['logs']) == {'mail-preparation-tests.log', 'mail-preparation-cli.log'}, 'sequence incomplete')
    for item in r['logs'].values():
        require(len(item['text']) < 32768 and hashlib.sha256(item['text'].encode()).hexdigest() == item['sha256'], 'log changed')
        require('PRIVATE KEY' not in item['text'], 'private evidence')
    tests = r['logs']['mail-preparation-tests.log']['text']
    for test in ['MailRenewalPreparationWithInheritedLock', 'MailRenewalPreparationResumesAfterSIGKILL', 'FirewallPreparationWithInheritedLock', 'FirewallPreparationResumesAfterSIGKILL']:
        require('--- PASS: Test'+test+' (' in tests, 'native test missing')
    for phase in ['file_renew', 'file_celikpanel-mail-host-cert', 'stage_durable', 'published', 'parent_durable']:
        require('--- PASS: TestMailRenewalPreparationResumesAfterSIGKILL/'+phase+' (' in tests, 'cut missing')
    cli = r['logs']['mail-preparation-cli.log']['text']
    require('native_cli_without_lock=refused no_generation=true' in cli, 'lock refusal absent')
    require(re.search('native_cli_prepare=verified generation='+HEX+' repeat_inode=preserved', cli), 'idempotent publication absent')
    require('native_cli_owner_edit=refused preserved=true workload_configuration=unchanged installed_units_and_hook=unchanged ledger=unchanged older_generations=retained management=absent' in cli, 'preservation absent')
    require(cli.endswith('active\nactive\nactive\n'), 'native workloads not active')
    return {'preparation': 'verified', 'production_enrollment': 'not established'}

if __name__ == '__main__': print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text())), sort_keys=True))
