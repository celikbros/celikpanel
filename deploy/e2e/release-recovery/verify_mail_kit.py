#!/usr/bin/env python3
"""Check bounded native scheduling evidence, not host or release attestation."""
import hashlib
import json
from pathlib import Path
import re
import sys
from verify_mail_contract import HEX, UUID, one, require


def digest(raw): return hashlib.sha256(raw).hexdigest()


def verify(r):
    require(r.get('schema') == 'celikpanel/native-mail-kit/v1', 'kit evidence schema differs')
    source = r.get('source_commit', '')
    require(re.fullmatch(r'[0-9a-f]{40}', source), 'source identity missing')
    require(r.get('scope') == {'native_hook_and_timer': True, 'automatic_boot_new_leaf': True, 'real_native_mail': True, 'installed_management_absent': True, 'fixture_ca': True, 'production_enrollment': False, 'public_acme': False, 'power_loss': False, 'independent_interrupted_recovery': False}, 'unsupported kit scope')
    m = r['manifest']; generation = m['generation']; binary = m['binary_sha256']
    require(m['schema'] == 'celikpanel-mail-renewal-runtime/v1' and m['ledger_version'] == 1 and m['plan_version'] == 1 and m['receipt_schema'] == 'mail-host-certificate-receipt/v1', 'unsupported artifact contract')
    require(re.fullmatch(HEX, binary) and re.fullmatch(HEX, generation), 'artifact identity absent')
    kit = Path(__file__).resolve().parents[3] / 'internal/mailrenewalkit'
    templates = {name: (kit / filename).read_text() for name, filename in [('service', 'renewal.service'), ('timer', 'renewal.timer'), ('hook', 'deploy-hook')]}
    path = '/usr/libexec/celikpanel/mail-renewal/' + generation + '/renew'
    for name, template in templates.items():
        require(r[name] == template.replace('@RENEW@', path), 'unsupported native template')
        require(digest(r[name].encode()) == m[name + '_sha256'], 'native file digest differs')
    expected = digest((m['schema'] + '\n' + binary + '\n' + '\n'.join(digest(templates[n].encode()) for n in ('service', 'timer', 'hook')) + '\n').encode())
    require(generation == expected, 'generation binding differs')
    names = {'mail-kit-install.log', 'mail-kit-source3.log', 'mail-kit-timer3.log', 'mail-kit-boot-prepare.log', 'mail-kit-boot-result.log'}
    require(set(r['logs']) == names, 'native kit logs differ')
    logs = {}
    for name, item in r['logs'].items():
        text = item['text']; require(len(text) < 32768 and digest(text.encode()) == item['sha256'], 'kit log digest differs')
        require('PRIVATE KEY' not in text and 'nonce=' not in text, 'private kit evidence present'); logs[name] = text
    installed = logs['mail-kit-install.log']
    require('fixture_kit_installed generation=' + generation + ' previous_hook=retained; installed_management=absent' in installed, 'fixture installation differs')
    require('component=mail-renewal\nversion=dev\ncommit=' + source in installed, 'executed source differs')
    require('Result=success' in installed and 'ExecMainStatus=0' in installed and 'native_sandbox_same_leaf_ack=verified; production_enrollment=not_exercised' in installed, 'native sandbox not verified')
    old, third = one(r'kit_fixture_source_prepared old=(' + HEX + ') new=(' + HEX + ')', logs['mail-kit-source3.log'])
    require(old != third and 'native_hook=queued; helper_service_not_started_by_hook=yes' in logs['mail-kit-source3.log'], 'native queue source missing')
    third_log = logs['mail-kit-timer3.log']
    before = one(r'(?m)^(' + UUID + ')$', third_log)
    request3, leaf3 = one(r'native_timer_new_leaf_completed request=([a-f0-9]{32}) leaf=(' + HEX + ') queue=absent; config=unchanged', third_log)
    require(leaf3 == third, 'timer selected another source')
    previous, fourth, expected_boot = one(r'next_boot_source_prepared old=(' + HEX + ') new=(' + HEX + ') before_boot=(' + UUID + ')', logs['mail-kit-boot-prepare.log'])
    require(previous == third and fourth != third and expected_boot == before, 'boot source continuity differs')
    require('before_reboot=pending; timer=enabled_but_stopped; no_manual_renewal_start=yes' in logs['mail-kit-boot-prepare.log'], 'boot queue not retained')
    boot = logs['mail-kit-boot-result.log']
    after = one(r'(?m)^(' + UUID + ')$', boot)
    require(after != before and 'installed_management=absent' in boot, 'independent reboot absent')
    require('Started celikpanel-mail-renewal.timer' in boot and 'Starting celikpanel-mail-renewal.service' in boot, 'native boot activation missing')
    request4, leaf4 = one(r'automatic_boot_renewal_completed request=([a-f0-9]{32}) leaf=(' + HEX + ') queue=absent; config=unchanged; runtime=root:celikpanel:0750', boot)
    require(leaf4 == fourth and request3 != request4, 'boot completion identity differs')
    for text, leaf in [(third_log, third), (boot, fourth)]:
        require('LastTriggerUSec=' in text and 'Result=success' in text and 'ExecMainStatus=0' in text, 'timer/service outcome missing')
        fingerprint = ':'.join(leaf[i:i+2].upper() for i in range(0, 64, 2))
        require(re.findall(r'(?m)^sha256 Fingerprint=(.*)$', text) == [fingerprint] * 2, 'native listeners differ')
    return {'automatic_boot_new_renewal': 'verified', 'operation': request4, 'production_enrollment': 'not established'}


if __name__ == '__main__': print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text())), sort_keys=True))
