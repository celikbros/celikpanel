#!/usr/bin/env python3
"""Validate bounded native evidence, not production enrollment or attestation."""
import hashlib
import json
from pathlib import Path
import re
import sys
from verify_mail_contract import HEX, UUID, one, require


def digest(raw): return hashlib.sha256(raw).hexdigest()


def verify(r):
    require(r.get('schema') == 'celikpanel/native-mail-recovery-budget/v1', 'unknown evidence schema')
    for field in ['source_commit', 'producer_commit']: require(re.fullmatch(r'[0-9a-f]{40}', r.get(field, '')), 'source absent')
    require(r.get('scope') == {'selected_recovery_budget': True, 'actual_reload_failure': True, 'native_boot_timer_retry': True, 'same_operation_owner_retry': True, 'installed_management_absent': True, 'fixture_ca': True, 'production_enrollment': False, 'preselection_retry': False, 'power_loss': False}, 'unsupported scope')
    names = {'mail-budget-source7.log', 'mail-budget-kill.log', 'mail-budget-kit.log', 'mail-budget-attempt2.log', 'mail-budget-boot.log', 'mail-budget-exhausted.log', 'mail-budget-owner.log'}
    require(set(r['logs']) == names, 'incomplete native sequence')
    logs = {}
    for name, item in r['logs'].items():
        text = item['text']; require(len(text) < 32768 and digest(text.encode()) == item['sha256'], 'log digest differs')
        require('PRIVATE KEY' not in text and 'nonce=' not in text, 'private evidence present'); logs[name] = text
    old, leaf = one(r'kit_fixture_source_prepared old=(' + HEX + ') new=(' + HEX + ')', logs['mail-budget-source7.log'])
    require(old != leaf, 'new source absent')
    killed = logs['mail-budget-kill.log']
    require('Result=signal' in killed and 'ExecMainStatus=9' in killed and 'status=9/KILL' in killed, 'actual kill missing')
    require('component=mail-renewal\nversion=dev\ncommit=' + r['producer_commit'] in killed, 'producer source differs')
    request, selected = one(r'native_sigkill_selected_before_reload=verified request=([0-9a-f]{32}) selected=(' + HEX + ')', killed)
    require(selected == leaf and 'queue=retained configuration=unchanged' in killed, 'selected evidence missing')
    before, boot = logs['mail-budget-attempt2.log'], logs['mail-budget-boot.log']
    for text, prefix, attempt in [(before, 'native_failed_reload_retained', 2), (boot, 'automatic_boot_failed_reload_retained', 3)]:
        require(prefix + ' request=' + request + ' attempt=' + str(attempt) + ' status=cancelling queue=unchanged configuration=unchanged foreign_history=preserved' in text, 'attempt identity or retained evidence differs')
        require('Result=exit-code' in text and 'ExecMainStatus=1' in text, 'native failure not established')
    require('ExecReload={ path=/usr/bin/false' in before and 'ActiveState=active' in before, 'native reload fault absent')
    require('before_reboot=attempt2; native_reload_fault=retained; timer=enabled_but_stopped' in before, 'reboot prerequisites differ')
    require(one(r'(?m)^(' + UUID + ')$', before) != one(r'(?m)^(' + UUID + ')$', boot), 'reboot missing')
    require('LastTriggerUSec=' in boot and 'LastTriggerUSec=\n' not in boot and 'Started celikpanel-mail-renewal.timer' in boot and 'Starting celikpanel-mail-renewal.service' in boot, 'automatic boot retry absent')
    refused = logs['mail-budget-exhausted.log']
    require('budget_exhausted_and_wrong_owner=refused request=' + request + ' attempt=3 ledger=unchanged queue=unchanged native_reload=not_repeated configuration=unchanged' in refused, 'exhaustion changed state')
    require('paused after three recorded execution attempts for operation ' + request in refused and '--retry-selected ' + request + ' for one explicit attempt' in refused, 'actionable continuation missing')
    require(refused.count('ExecMainStatus=1') == 2 and 'celikpanel-mail-budget-wrong-owner.service' in refused, 'refusal outcomes differ')
    owner = logs['mail-budget-owner.log']
    require('fixture_owner_reload_resolution=explicit; fault_evidence=retained' in owner and '--retry-selected ' + request in owner, 'explicit owner resolution missing')
    got, actual, served = one(r'explicit_budget_owner_recovery=verified request=([0-9a-f]{32}) leaf=(' + HEX + r') served=(\{[^\n]+\}) attempt=4 queue=acknowledged foreign_history=preserved config_bytes_and_mtime=preserved management=absent', owner)
    require(got == request and actual == leaf and json.loads(served) == {'587': leaf, '993': leaf}, 'completion or native listeners differ')
    require('Result=success' in owner and 'ExecMainStatus=0' in owner, 'owner attempt not completed')
    m = r['manifest']; require(m['schema'] == 'celikpanel-mail-renewal-runtime/v1' and m['ledger_version'] == 1 and m['plan_version'] == 1 and m['receipt_schema'] == 'mail-host-certificate-receipt/v1', 'consumer contract differs')
    kit = Path(__file__).resolve().parents[3] / 'internal/mailrenewalkit'; templates = {n: (kit / f).read_text() for n, f in [('service', 'renewal.service'), ('timer', 'renewal.timer'), ('hook', 'deploy-hook')]}
    require(re.fullmatch(HEX, m['binary_sha256']), 'helper digest absent')
    generation = digest((m['schema'] + '\n' + m['binary_sha256'] + '\n' + '\n'.join(digest(templates[n].encode()) for n in ('service', 'timer', 'hook')) + '\n').encode())
    require(m['generation'] == generation, 'generation mismatch')
    for n, text in templates.items(): require(m[n + '_sha256'] == digest(text.replace('@RENEW@', '/usr/libexec/celikpanel/mail-renewal/' + generation + '/renew').encode()), 'native template differs')
    require('budget_recovery_fixture_kit=installed old_generation=retained generation=' + generation in logs['mail-budget-kit.log'], 'fixture kit differs')
    require('component=mail-renewal\nversion=dev\ncommit=' + r['source_commit'] in logs['mail-budget-kit.log'], 'consumer source differs')
    require(m['binary_sha256'] + '  /usr/libexec/celikpanel/mail-renewal/' + generation + '/renew' in boot, 'postboot executable differs')
    return {'selected_recovery_budget': 'verified across reboot', 'explicit_owner_attempt': 4, 'request': request, 'production_enrollment': 'not established'}


if __name__ == '__main__': print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text())), sort_keys=True))
