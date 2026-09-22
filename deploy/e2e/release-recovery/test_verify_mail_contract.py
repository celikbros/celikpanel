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

class MailCleanupTests(unittest.TestCase):
    def setUp(self):
        self.record=json.loads((HERE/'MAIL-CLEANUP-AY.json').read_text())
        self.base=(HERE/'MAIL-CONTRACT-AY.json').read_bytes()
    def test_native_cleanup_evidence(self):
        self.assertEqual(s.verify_cleanup(self.record,self.base)['owner_reviewed_native_cleanup'],'verified')
    def test_wrong_scope_binary_or_retained_fixture(self):
        for key,value in [('test_binary_sha256','0'*64),('base_record_sha256','0'*64),('scope',{})]:
            record=copy.deepcopy(self.record);record[key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):s.verify_cleanup(record,self.base)
    def test_rehashed_failure_or_changed_workload(self):
        for old,new in [('--- PASS:','--- FAIL:'),('retained failed status','reported success'),('7713abe26e69887a166cd5c6efc39bb96be0f19cf6ec55428af1673924466a11','0'*64)]:
            record=copy.deepcopy(self.record);item=record['logs']['mail-recovery-cleanup.log']
            self.assertIn(old,item['text']);item['text']=item['text'].replace(old,new)
            item['sha256']=hashlib.sha256(item['text'].encode()).hexdigest()
            with self.subTest(old=old),self.assertRaises(ValueError):s.verify_cleanup(record,self.base)

class MailDialectTests(unittest.TestCase):
    def setUp(self):
        self.record=json.loads((HERE/'MAIL-DIALECT-AY.json').read_text())
        self.base=(HERE/'MAIL-CONTRACT-AY.json').read_bytes()
    def test_native_dialect_evidence(self):
        self.assertEqual(s.verify_record(self.record,self.base)['unknown_dialect_preserves_native_state'],'verified')
    def test_wrong_scope_binary_or_fixture(self):
        for key,value in [('test_binary_sha256','0'*64),('base_record_sha256','0'*64),('source_commit',''),('scope',{}),('lab',{}),('schema','unrecognized')]:
            record=copy.deepcopy(self.record);record[key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):s.verify_record(record,self.base)
    def test_rehashed_failure_or_changed_workload(self):
        for old,new in [('--- PASS:','--- FAIL:'),('ExecMainStatus=0','ExecMainStatus=1'),('pending renewal','discarded renewal'),('7a3fbe8a-98b5-4b50-857e-f238ad4b22dc','00000000-0000-0000-0000-000000000000'),('7713abe26e69887a166cd5c6efc39bb96be0f19cf6ec55428af1673924466a11','0'*64)]:
            record=copy.deepcopy(self.record);item=record['logs']['mail-native-dialect-result.log']
            self.assertIn(old,item['text']);item['text']=item['text'].replace(old,new)
            item['sha256']=hashlib.sha256(item['text'].encode()).hexdigest()
            with self.subTest(old=old),self.assertRaises(ValueError):s.verify_record(record,self.base)
    def test_unhashed_edit(self):
        self.record['logs']['mail-native-dialect-result.log']['text']+='changed'
        with self.assertRaises(ValueError):s.verify_record(self.record,self.base)

class MailLedgerTests(unittest.TestCase):
    def setUp(self):
        self.record=json.loads((HERE/'MAIL-LEDGER-AY.json').read_text())
        self.base=(HERE/'MAIL-CONTRACT-AY.json').read_bytes()
    def test_native_ledger_evidence(self):
        self.assertEqual(s.verify_record(self.record,self.base)['host_lock_exclusion'],'verified')
    def test_wrong_binary_scope_or_base(self):
        for key,value in [('test_binary_sha256','0'*64),('base_record_sha256','0'*64),('scope',{})]:
            record=copy.deepcopy(self.record);record[key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):s.verify_record(record,self.base)
    def test_rehashed_missing_exclusion_or_changed_state(self):
        for old,new in [('held-lock-refused=verified','held-lock-accepted=verified'),('native-ledger-check=passed','native-ledger-check=failed'),('51e1704bc9a5d460678b0458e068e16614076ed9547309487f5a6b6051857bde','0'*64),('689a6c54-3150-43bd-b95b-277adf11d5e1','00000000-0000-0000-0000-000000000000'),('Fingerprint=77:13','Fingerprint=00:00')]:
            record=copy.deepcopy(self.record);item=record['logs']['mail-native-ledger-result.log']
            self.assertIn(old,item['text']);item['text']=item['text'].replace(old,new);item['sha256']=hashlib.sha256(item['text'].encode()).hexdigest()
            with self.subTest(old=old),self.assertRaises(ValueError):s.verify_record(record,self.base)

if __name__ == '__main__': unittest.main()
