#!/usr/bin/env python3
"""Validate bounded runtime transcripts; this is not host attestation."""
import hashlib
import json
from pathlib import Path
import re
import sys
from verify_mail_contract import HEX, UUID, one, require


def verify(record):
    require(record.get('schema') == 'celikpanel/native-mail-runtime/v1', 'runtime schema differs')
    require(re.fullmatch(r'[0-9a-f]{40}', record.get('source_commit', '')), 'source missing')
    helper, test = record.get('helper_binary_sha256', ''), record.get('test_binary_sha256', '')
    require(re.fullmatch(HEX, helper) and re.fullmatch(HEX, test) and helper != test, 'binary identities differ')
    require(re.fullmatch(HEX, record.get('predecessor_evidence_sha256', '')), 'predecessor missing')
    require(record.get('scope') == {'postboot_same_leaf_ack': True, 'postboot_new_leaf_renewal': True, 'owner_runtime_preservation': True, 'empty_queue_no_initialization': True, 'installed_management_absent': True, 'fixture_ca': True, 'production_enrollment': False, 'automatic_boot_scheduling': False, 'public_acme': False, 'power_loss': False}, 'unsupported runtime scope')
    labs = record.get('labs', {})
    require(set(labs) == {'bd', 'be'} and len(set(labs.values())) == 2 and all(re.fullmatch(r'release-recovery__[a-f0-9]{16}', v) for v in labs.values()), 'separate guarded labs missing')
    require(set(record.get('logs', {})) == {'bd-postboot', 'bd-owner', 'be-prepare', 'be-renewal'}, 'runtime logs differ')
    logs = {}
    for name, item in record['logs'].items():
        text = item['text']
        require(len(text) < 32768 and hashlib.sha256(text.encode()).hexdigest() == item['sha256'], 'runtime log digest differs')
        require('PRIVATE KEY' not in text and 'nonce=' not in text, 'private runtime evidence present')
        binary = test if name == 'be-prepare' else helper
        basename = 'mail-agent.test' if name == 'be-prepare' else 'mail-renewal-runtime'
        require(text.startswith(binary + '  /root/celikpanel-release-recovery-lab/' + basename + '\n'), 'runtime executable differs')
        logs[name] = text
    prepared = logs['be-prepare']
    require('--- PASS: TestMailHostCertificateDisposableVMPrepareIndependentRenewal (' in prepared and '--- FAIL:' not in prepared and 'Result=success' in prepared, 'fixture preparation failed')
    lineage, old, new = one(r'independent renewal source prepared; lineage=(celikpanel-mail-[a-f0-9]{24}) selected=(' + HEX + ') source=(' + HEX + ')', prepared)
    require(old != new, 'new source absent')
    before = one(r'(?m)^(' + UUID + ')$', prepared)
    renewed = logs['be-renewal']
    after = one(r'(?m)^(' + UUID + ')$', renewed)
    require(before != after, 'native reboot absent')
    require('installed_management=absent; volatile_runtime=absent; pending=absent' in renewed and 'empty_queue=no_runtime_initialization' in renewed, 'absence/no-work observations missing')
    require('initial_selected_leaf=' + old in renewed, 'initial selection differs')
    require('/mail-renewal-runtime --queue ' + lineage in renewed and '/mail-renewal-runtime --process-pending' in renewed, 'helper entry not exercised')
    require(renewed.count('Result=success') == 2 and renewed.count('ExecMainStatus=0') == 2 and renewed.count('ActiveState=inactive') == 2, 'native helper units failed')
    request, leaf = one(r'postboot_new_renewal_completed request=([0-9a-f]{32}) leaf=(' + HEX + ') queue=absent', renewed)
    require(leaf == new, 'published wrong source')
    require('runtime=root:celikpanel:0750; native_configuration_bytes_and_mtimes=unchanged; exact_receipt_and_ledger=matched' in renewed, 'durable/native checks missing')
    fingerprint = ':'.join(new[i:i+2].upper() for i in range(0, 64, 2))
    require(re.findall(r'(?m)^sha256 Fingerprint=(.*)$', renewed) == [fingerprint] * 2, 'new listeners differ')
    same = logs['bd-postboot']
    require('installed_management=absent; volatile_runtime=absent; pending=present' in same and 'Result=success' in same and 'ExecMainStatus=0' in same, 'same-leaf boot processing failed')
    require('runtime=root:celikpanel:0750; retained_ledger_and_native_config=unchanged; selection=unchanged; same_leaf_pending=acknowledged' in same, 'same-leaf evidence changed')
    same_listeners = re.findall(r'(?m)^sha256 Fingerprint=(.*)$', same)
    require(len(same_listeners) == 2 and same_listeners[0] == same_listeners[1], 'same-leaf listeners differ')
    owner = logs['bd-owner']
    require(one(r'(?m)^(' + UUID + ')$', owner) == one(r'(?m)^(' + UUID + ')$', same), 'owner check boot differs')
    require('owner_runtime_mode=0755; refusal_exit=1; pending_ledger_native_config=unchanged; owner_content=preserved' in owner, 'owner override not preserved')
    require('explicit_fixture_owner_resolution=0750; same_leaf_pending=acknowledged; existing_runtime_inode_and_content=preserved' in owner, 'owner resolution missing')
    return {'postboot_new_renewal': 'verified', 'operation': request, 'runtime_owner_preservation': 'verified', 'automatic_scheduling': 'not established'}


if __name__ == '__main__':
    path = Path(sys.argv[1])
    record = json.loads(path.read_text())
    predecessor = path.with_name('MAIL-EXECUTOR-BD.json')
    require(hashlib.sha256(predecessor.read_bytes()).hexdigest() == record['predecessor_evidence_sha256'], 'predecessor bytes differ')
    print(json.dumps(verify(record), sort_keys=True))
