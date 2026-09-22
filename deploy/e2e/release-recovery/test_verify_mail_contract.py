import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import unittest
HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('verify_mail_contract', HERE / 'verify_mail_contract.py')
s = importlib.util.module_from_spec(spec)
spec.loader.exec_module(s)

class MailContractTests(unittest.TestCase):
    def setUp(self): self.record = json.loads((HERE / 'MAIL-CONTRACT-AX.json').read_text())
    def test_recorded_native_evidence(self):
        self.assertEqual(s.verify(self.record)['owner_drift_preserved'], 'verified')
        ay = json.loads((HERE / 'MAIL-CONTRACT-AY.json').read_text())
        self.assertEqual(s.verify(ay)['retained_after_orderly_boot'], 'verified')
    def test_false_scope_claim(self):
        for key in ('external_acme_issuance', 'independent_renewal_helper', 'installed_owner_server', 'power_loss'):
            value = copy.deepcopy(self.record); value['scope'][key] = True
            with self.subTest(key=key), self.assertRaises(ValueError): s.verify(value)
    def test_rehashed_but_semantically_wrong_logs(self):
        edits = [
          ('mail-convergence.log', '--- PASS:', '--- FAIL:'),
          ('mail-receipt-after-boot.log', 'f451e282864eb1147cde1e0f66d546cd', '0'*32),
          ('mail-receipt-after-boot.log', 'a43077db-2632-4e34-bb21-026df39f45e3', '610448ff-be9d-4812-ab52-19664469b0f0'),
          ('mail-receipt-after-boot.log', 'ExecMainStatus=0', 'ExecMainStatus=1'),
          ('mail-native-boot.log', 'active\nactive', 'failed\nactive'),
          ('mail-owner-drift.log', 'pending retained', 'pending removed'),
          ('mail-boot-native-handshakes.log', '0D:D7', 'FF:FF'),
          ('mail-fixture-metadata.log', 'installed_agent_absent=yes', 'installed_agent_absent=no'),
        ]
        for name, old, new in edits:
            value = copy.deepcopy(self.record); item = value['logs'][name]
            self.assertIn(old, item['text']); item['text'] = item['text'].replace(old, new)
            item['sha256'] = hashlib.sha256(item['text'].encode()).hexdigest()
            with self.subTest(name=name, old=old), self.assertRaises(ValueError): s.verify(value)
    def test_unhashed_edit(self):
        self.record['logs']['mail-owner-drift.log']['text'] += 'changed'
        with self.assertRaises(ValueError): s.verify(self.record)

if __name__ == '__main__': unittest.main()
