#!/usr/bin/env python3
"""Validate bounded native selected-renewal evidence, not release attestation."""
import hashlib
import json
from pathlib import Path
import re
import sys
from verify_mail_contract import HEX, UUID, one, require


def digest(raw): return hashlib.sha256(raw).hexdigest()


def verify(r):
    require(r.get('schema') == 'celikpanel/native-mail-selected-recovery/v1', 'unknown evidence schema')
    source = r.get('source_commit', '')
    require(re.fullmatch(r'[0-9a-f]{40}', source), 'source missing')
    require(r.get('scope') == {'actual_sigkill_after_selection': True, 'same_operation_recovery': True, 'native_boot_timer_recovery': True, 'stopped_service_preserved': True, 'native_configuration_preserved': True, 'installed_management_absent': True, 'fixture_ca': True, 'production_enrollment': False, 'power_loss': False, 'preselection_recovery': False, 'bounded_retry': False}, 'unsupported claim')
    names = {'mail-selected-source5.log', 'mail-selected-kill-observe.log', 'mail-selected-kill-proof2.log', 'mail-selected-refuse.log', 'mail-selected-recover.log', 'mail-selected-kit.log', 'mail-selected-source6.log', 'mail-selected-boot-kill.log', 'mail-selected-boot-proof2.log'}
    require(set(r['logs']) == names, 'native sequence incomplete')
    logs = {}
    for name, item in r['logs'].items():
        text = item['text']; require(len(text) < 32768 and digest(text.encode()) == item['sha256'], 'log digest differs')
        require('PRIVATE KEY' not in text and 'nonce=' not in text, 'private evidence present'); logs[name] = text
    old, fifth = one(r'kit_fixture_source_prepared old=(' + HEX + ') new=(' + HEX + ')', logs['mail-selected-source5.log'])
    fifth_again, sixth = one(r'kit_fixture_source_prepared old=(' + HEX + ') new=(' + HEX + ')', logs['mail-selected-source6.log'])
    require(old != fifth and fifth == fifth_again and sixth != fifth, 'source continuity differs')
    for name in ['mail-selected-kill-observe.log', 'mail-selected-boot-kill.log']:
        text = logs[name]
        require('Result=signal' in text and 'ExecMainCode=2' in text and 'ExecMainStatus=9' in text and 'status=9/KILL' in text, 'actual SIGKILL absent')
    def selected(name, want_old, want_new):
        request, leaf, served = one(r'native_sigkill_selected_before_reload=verified request=([0-9a-f]{32}) selected=(' + HEX + r') served=(\{[^\n]+\}) queue=retained configuration=unchanged', logs[name])
        served = json.loads(served)
        require(leaf == want_new and set(served) == {'587', '993'} and served['993'] == want_old and all(v in [want_old, want_new] for v in served.values()), 'selected state mistaken for activation')
        return request
    first = selected('mail-selected-kill-proof2.log', old, fifth)
    second = selected('mail-selected-boot-kill.log', fifth, sixth)
    require(first != second, 'independent trials reused request')
    refused = logs['mail-selected-refuse.log']
    require('ExecMainStatus=1' in refused and 'Result=exit-code' in refused and 'independent_selected_recovery_stopped_service=refused ledger=unchanged queue=unchanged configuration=unchanged selected=preserved service=not_started request=' + first in refused, 'owner stop not preserved')
    def completed(name, prefix, request, leaf):
        text = logs[name]
        got, actual, served = one(prefix + r'=verified request=([0-9a-f]{32}) leaf=(' + HEX + r') served=(\{[^\n]+\}) queue=acknowledged foreign_history=preserved config_bytes_and_mtime=preserved management=absent', text)
        require(got == request and actual == leaf and json.loads(served) == {'587': leaf, '993': leaf}, 'wrong operation or listeners')
        require('Result=success' in text and 'ExecMainStatus=0' in text, 'native completion missing')
    completed('mail-selected-recover.log', 'independent_selected_recovery', first, fifth)
    completed('mail-selected-boot-proof2.log', 'automatic_boot_selected_recovery', second, sixth)
    before = one(r'(?m)^(' + UUID + ')$', logs['mail-selected-boot-kill.log'])
    boot = logs['mail-selected-boot-proof2.log']; after = one(r'(?m)^(' + UUID + ')$', boot)
    require(before != after and 'before_reboot=selected_intent_pending; timer=enabled_but_stopped; no_manual_recovery_start=yes' in logs['mail-selected-boot-kill.log'], 'retained intent reboot missing')
    require('LastTriggerUSec=' in boot and 'LastTriggerUSec=\n' not in boot and 'Started celikpanel-mail-renewal.timer' in boot and 'Starting celikpanel-mail-renewal.service' in boot, 'native timer did not recover')
    m = r['manifest']; require(m['schema'] == 'celikpanel-mail-renewal-runtime/v1', 'wrong kit schema')
    require(m['ledger_version'] == 1 and m['plan_version'] == 1 and m['receipt_schema'] == 'mail-host-certificate-receipt/v1', 'wrong consumer contracts')
    kit = Path(__file__).resolve().parents[3] / 'internal/mailrenewalkit'
    templates = {n: (kit / f).read_text() for n, f in [('service', 'renewal.service'), ('timer', 'renewal.timer'), ('hook', 'deploy-hook')]}
    require(re.fullmatch(HEX, m['binary_sha256']), 'helper digest missing')
    generation = digest((m['schema'] + '\n' + m['binary_sha256'] + '\n' + '\n'.join(digest(templates[n].encode()) for n in ('service', 'timer', 'hook')) + '\n').encode())
    require(m['generation'] == generation, 'generation mismatch')
    for n, text in templates.items():
        require(m[n + '_sha256'] == digest(text.replace('@RENEW@', '/usr/libexec/celikpanel/mail-renewal/' + generation + '/renew').encode()), 'native template differs')
    require('selected_recovery_fixture_kit=installed old_generation=retained generation=' + generation in logs['mail-selected-kit.log'], 'fixture enrollment absent')
    require('component=mail-renewal\nversion=dev\ncommit=' + source in logs['mail-selected-kit.log'], 'executed source differs')
    require(m['binary_sha256'] + '  /usr/libexec/celikpanel/mail-renewal/' + generation + '/renew' in boot, 'postboot helper differs')
    return {'selected_interruption': 'same-operation recovered', 'native_boot_timer': 'verified', 'request': second, 'production_enrollment': 'not established'}


if __name__ == '__main__': print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text())), sort_keys=True))
