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

if __name__ == '__main__':
    print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text())), sort_keys=True))
