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

if __name__ == '__main__':
    import argparse
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('record');parser.add_argument('--base')
    args=parser.parse_args();record=json.loads(Path(args.record).read_text())
    result=verify_cleanup(record,Path(args.base).read_bytes()) if args.base else verify(record)
    print(json.dumps(result,sort_keys=True))
