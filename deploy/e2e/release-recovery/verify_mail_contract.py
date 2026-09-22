#!/usr/bin/env python3
"""Check the bounded recorded AX native acceptance; not a host attestation."""
import hashlib
import json
from pathlib import Path
import re
import sys

HEX = r"[0-9a-f]{64}"
UUID = r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}"

def require(ok, message):
    if not ok: raise ValueError(message)

def one(pattern, text):
    matches = re.findall(pattern, text)
    require(len(matches) == 1, "missing or ambiguous evidence: " + pattern)
    return matches[0]

def verify(record):
    require(record.get('schema') == 'celikpanel/native-mail-contract/v1', 'schema differs')
    require(re.fullmatch(r'[0-9a-f]{40}', record.get('source_commit', '')), 'missing source identity')
    binary = record.get('test_binary_sha256', '')
    require(re.fullmatch(HEX, binary), 'missing binary identity')
    scope = record.get('scope', {})
    for key in ('real_daemons', 'real_host_readiness_and_package_probes', 'fixture_ca_and_certbot_source', 'orderly_reboot'):
        require(scope.get(key) is True, 'required native scope absent: ' + key)
    for key in ('external_acme_issuance', 'independent_renewal_helper', 'installed_owner_server', 'power_loss'):
        require(scope.get(key) is False, 'unsupported scope claim: ' + key)
    logs = {}
    for name, item in record['logs'].items():
        text = item['text']
        require(len(text) < 32768 and hashlib.sha256(text.encode()).hexdigest() == item['sha256'], 'log digest differs')
        require('PRIVATE KEY' not in text and 'nonce=' not in text, 'private evidence present')
        logs[name] = text
    cases = [('mail-convergence.log', 'ConvergenceAndRenewal'), ('mail-receipt-before-boot.log', 'RenewalReceipt'), ('mail-receipt-after-boot.log', 'RenewalReceipt'), ('mail-owner-drift.log', 'CompletedRenewalOwnerDrift')]
    for name, test in cases:
        text = logs[name]
        require('--- PASS: TestMailHostCertificateDisposableVM' + test + ' (' in text, 'test did not pass')
        require('Result=success' in text and 'ExecMainStatus=0' in text and '--- FAIL:' not in text, 'native service failed')
    initial = one(r'initial host publication and SMTP/IMAP system-trusted TLS passed; request=[0-9a-f]{32} leaf=(' + HEX + ')', logs['mail-convergence.log'])
    renewed = one(r'renewal exported/reloaded exact second generation without panel/license; leaf=(' + HEX + ')', logs['mail-convergence.log'])
    require(initial != renewed, 'renewal did not change certificate')
    receipt = r'exact durable renewal receipt verified; request=([0-9a-f]{32}) phase=(commit/mail-host-certificate/v1/published/[0-9a-f]{32}/mail[.]setup[.]celikpanel[.]test/mhc1:' + HEX + ')'
    before = one(receipt, logs['mail-receipt-before-boot.log'])
    after = one(receipt, logs['mail-receipt-after-boot.log'])
    drift = one(receipt, logs['mail-owner-drift.log'])
    require(before == after == drift and before[0] == before[1].split('/')[4], 'durable operation changed')
    before_boot = one(r'(?m)^(' + UUID + ')$', logs['mail-receipt-before-boot.log'])
    after_boot = one(r'(?m)^(' + UUID + ')$', logs['mail-receipt-after-boot.log'])
    require(before_boot != after_boot, 'no real reboot')
    require(one(r'(?m)^(' + UUID + ')$', logs['mail-native-boot.log']) == after_boot, 'native boot differs')
    require(logs['mail-native-boot.log'].splitlines()[1:4] == ['active', 'active', 'inactive'], 'native daemons or agent state differs')
    fingerprint = ':'.join(renewed[i:i+2].upper() for i in range(0, 64, 2))
    require(logs['mail-boot-native-handshakes.log'].splitlines() == ['sha256 Fingerprint=' + fingerprint] * 2, 'native postboot listeners differ')
    selected, source = one(r'owner selection preserved; pending retained; historical completion preserved; selected=(' + HEX + ') source=(' + HEX + ')', logs['mail-owner-drift.log'])
    require((selected, source) == (initial, renewed), 'owner drift lost or wrong source')
    metadata = logs['mail-fixture-metadata.log']
    require(metadata.startswith(binary + '  /root/celikpanel-release-recovery-lab/mail-agent.test\n'), 'executed binary differs')
    require('installed_panel_absent=yes' in metadata and 'installed_agent_absent=yes' in metadata, 'installed management present')
    return {'renewal': 'verified', 'retained_after_orderly_boot': 'verified', 'owner_drift_preserved': 'verified', 'independent_renewal': 'not established'}

def verify_cleanup(record, base_bytes):
    require(record.get('schema') == 'celikpanel/native-mail-cleanup/v1', 'cleanup schema differs')
    require(record.get('base_record_sha256') == hashlib.sha256(base_bytes).hexdigest(), 'retained fixture evidence differs')
    base = json.loads(base_bytes); verify(base)
    require(record.get('lab') == {key:base['lab'][key] for key in ('cell_id','node')}, 'cleanup fixture differs')
    require(re.fullmatch(r'[0-9a-f]{40}',record.get('source_commit','')), 'cleanup source missing')
    binary = record.get('test_binary_sha256',''); require(re.fullmatch(HEX,binary), 'cleanup binary missing')
    expected_scope={'controlled_uncommitted_stage':True,'actual_agent_cleanup_path':True,'owner_explicitly_resolved_conflict':True,'automatic_crash_recovery':False,'independent_renewal':False}
    require(record.get('scope') == expected_scope, 'unsupported cleanup scope')
    logs={}
    for name,item in record['logs'].items():
        text=item['text']
        require(len(text)<32768 and hashlib.sha256(text.encode()).hexdigest()==item['sha256'], 'cleanup log digest differs')
        require('PRIVATE KEY' not in text and 'nonce=' not in text, 'private cleanup evidence present')
        logs[name]=text
    start=logs['mail-cleanup-start.log']; result=logs['mail-recovery-cleanup.log']
    require(start.startswith(binary+'  /root/celikpanel-release-recovery-lab/mail-cleanup.test\n'), 'executed cleanup binary differs')
    require(one(r'(?m)^('+UUID+')$',start) == one(r'(?m)^('+UUID+')$',result), 'cleanup boot changed')
    require('--- PASS: TestMailHostCertificateDisposableVMRecoveryCleanupOwnerEdit (' in result and '--- FAIL:' not in result and 'Result=success' in result and 'ExecMainStatus=0' in result, 'native cleanup did not pass')
    selected=one(r'owner selection preserved; pending retained; historical completion preserved; selected=('+HEX+') source='+HEX,base['logs']['mail-owner-drift.log']['text'])
    request,served=one(r'native cleanup refused owner files; same-operation owner-resolved cleanup retained failed status, queue and served leaf; request=([0-9a-f]{32}) selected=('+HEX+')',result)
    require(served==selected, 'cleanup changed served certificate')
    return {'owner_reviewed_native_cleanup':'verified','operation':request,'automatic_crash_recovery':'not established'}

def verify_dialect(record, base_bytes):
    require(record.get('schema') == 'celikpanel/native-mail-dialect/v1', 'dialect schema differs')
    require(record.get('base_record_sha256') == hashlib.sha256(base_bytes).hexdigest(), 'retained fixture evidence differs')
    base = json.loads(base_bytes); verify(base)
    require(record.get('lab') == {key:base['lab'][key] for key in ('cell_id','node')}, 'dialect fixture differs')
    require(re.fullmatch(r'[0-9a-f]{40}',record.get('source_commit','')), 'dialect source missing')
    binary = record.get('test_binary_sha256',''); require(re.fullmatch(HEX,binary), 'dialect binary missing')
    expected_scope={'real_native_config_readback':True,'real_executable_version_failure':True,'shared_host_mutation_lock':True,'configuration_and_pending_preserved':True,'native_daemon_failure':False,'independent_renewal':False,'crash_recovery':False}
    require(record.get('scope') == expected_scope, 'unsupported dialect scope')
    logs={}
    for name,item in record['logs'].items():
        text=item['text']
        require(len(text)<32768 and hashlib.sha256(text.encode()).hexdigest()==item['sha256'], 'dialect log digest differs')
        require('PRIVATE KEY' not in text and 'nonce=' not in text, 'private dialect evidence present')
        logs[name]=text
    start=logs['mail-native-dialect-start.log']; result=logs['mail-native-dialect-result.log']
    require(start.startswith(binary+'  /root/celikpanel-release-recovery-lab/mail-dialect.test\n'), 'executed dialect binary differs')
    require(one(r'(?m)^('+UUID+')$',start) == one(r'(?m)^('+UUID+')$',result), 'dialect boot changed')
    require('--- PASS: TestMailHostCertificateDisposableVMNativeDialectReadback (' in result and '--- FAIL:' not in result and 'Result=success' in result and 'ExecMainStatus=0' in result, 'native dialect did not pass')
    selected=one(r'owner selection preserved; pending retained; historical completion preserved; selected=('+HEX+') source='+HEX,base['logs']['mail-owner-drift.log']['text'])
    served=one(r'native version failure refused before configuration; native 2[.]4 retained-plan readback passed; configuration, ledger, pending renewal and trusted SMTP/IMAP leaf unchanged: ('+HEX+')',result)
    require(served==selected, 'dialect test changed served certificate')
    return {'unknown_dialect_preserves_native_state':'verified','native_retained_plan_readback':'verified','independent_renewal':'not established'}


def verify_ledger(record, base_bytes):
    require(record.get('schema') == 'celikpanel/native-mail-ledger/v1', 'ledger schema differs')
    require(record.get('base_record_sha256') == hashlib.sha256(base_bytes).hexdigest(), 'retained fixture evidence differs')
    base=json.loads(base_bytes);verify(base)
    require(record.get('lab') == {key:base['lab'][key] for key in ('cell_id','node')}, 'ledger fixture differs')
    require(re.fullmatch(r'[0-9a-f]{40}',record.get('source_commit','')), 'ledger source missing')
    binary=record.get('test_binary_sha256','');require(re.fullmatch(HEX,binary), 'ledger binary missing')
    require(record.get('scope') == {'standalone_production_checker':True,'retained_native_ledger':True,'host_lock_contention_and_inheritance':True,'managed_state_unchanged':True,'installed_management_absent':True,'independent_renewal':False,'crash_recovery':False}, 'unsupported ledger scope')
    logs={}
    for name,item in record['logs'].items():
        text=item['text']
        require(len(text)<32768 and hashlib.sha256(text.encode()).hexdigest()==item['sha256'], 'ledger log digest differs')
        require('PRIVATE KEY' not in text and 'nonce=' not in text, 'private ledger evidence present')
        logs[name]=text
    before=logs['mail-native-ledger-before.log'];result=logs['mail-native-ledger-result.log']
    require(result.startswith(binary+'  /root/celikpanel-release-recovery-lab/ledger-checker\n'), 'executed ledger binary differs')
    require(one(r'(?m)^('+UUID+')$',before)==one(r'(?m)^('+UUID+')$',result), 'ledger boot changed')
    require(before.startswith('active\nactive\n') and '\nactive\nactive\n' in result, 'native mail not active')
    for marker in ('retained-ledger-idle=verified','held-lock-refused=verified','inherited-lock-idle=verified','managed-state-unchanged=verified','native-ledger-check=passed'):
        require(result.splitlines().count(marker)==1, 'missing ledger proof: '+marker)
    require('Recovery agent check: service mutation state is not idle: the host package manager or mutation lock is busy' in result,'missing contention refusal')
    for filename in ('service-mutations.json','mail-host-certificate-renewal.pending'):
        pattern=r'(?m)^('+HEX+r')  /var/lib/celikpanel-agent-private/'+re.escape(filename)+r'$'
        require(one(pattern,before)==one(pattern,result), 'retained state changed: '+filename)
    selected=one(r'owner selection preserved; pending retained; historical completion preserved; selected=('+HEX+') source='+HEX,base['logs']['mail-owner-drift.log']['text'])
    fingerprint=':'.join(selected[i:i+2].upper() for i in range(0,64,2))
    require(re.findall(r'(?m)^sha256 Fingerprint=(.*)$',result)==[fingerprint,fingerprint], 'native ledger listeners changed')
    return {'standalone_retained_ledger':'verified','host_lock_exclusion':'verified','native_mail_preserved':'verified','independent_renewal':'not established'}


def verify_record(record, base_bytes=None):
    if base_bytes is None:
        return verify(record)
    schema=record.get('schema')
    if schema == 'celikpanel/native-mail-cleanup/v1':
        return verify_cleanup(record,base_bytes)
    if schema == 'celikpanel/native-mail-dialect/v1':
        return verify_dialect(record,base_bytes)
    if schema == 'celikpanel/native-mail-ledger/v1':
        return verify_ledger(record,base_bytes)
    raise ValueError('unsupported dependent evidence schema')

if __name__ == '__main__':
    import argparse
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('record');parser.add_argument('--base')
    args=parser.parse_args();record=json.loads(Path(args.record).read_text())
    result=verify_record(record,Path(args.base).read_bytes() if args.base else None)
    print(json.dumps(result,sort_keys=True))
