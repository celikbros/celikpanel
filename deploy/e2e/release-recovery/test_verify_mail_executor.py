import copy
import hashlib
import json
from pathlib import Path
import unittest
import verify_mail_executor as s
HERE=Path(__file__).resolve().parent

class ExecutorEvidenceTests(unittest.TestCase):
    def setUp(self):self.record=json.loads((HERE/'MAIL-EXECUTOR-BD.json').read_text())
    def test_native_separate_executor(self):self.assertEqual(s.verify(self.record)['separate_executable_renewal'],'verified')
    def test_wrong_scope_and_binary(self):
        for key,value in [('schema','unknown'),('source_commit',''),('helper_binary_sha256','0'*64),('test_binary_sha256','0'*64),('scope',{})]:
            record=copy.deepcopy(self.record);record[key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):s.verify(record)
        for key in ('production_enrollment','public_acme','automatic_boot_renewal','power_loss','independent_interrupted_recovery'):
            record=copy.deepcopy(self.record);record['scope'][key]=True
            with self.subTest(key=key),self.assertRaises(ValueError):s.verify(record)
    def test_rehashed_false_results(self):
        edits=[('mail-helper-native-result.log','ExecMainStatus=0','ExecMainStatus=1'),('mail-helper-native-result.log','installed_agent_absent=yes','installed_agent_absent=no'),('mail-helper-negative.log','"exit": 125','"exit": 0'),('mail-helper-native-result.log','E2:40:F3','00:00:00'),('mail-helper-native-boot.log','2ba12c6e-41fe-4848-9d9d-15d6703b3117','00294b15-afea-4bce-8634-163a26dda861'),('mail-helper-runtime-gap.log','pending=retained','pending=deleted'),('mail-helper-replay.log','canonical_ledger=unchanged','canonical_ledger=rewritten')]
        for name,old,new in edits:
            record=copy.deepcopy(self.record);item=record['logs'][name];self.assertIn(old,item['text']);item['text']=item['text'].replace(old,new);item['sha256']=hashlib.sha256(item['text'].encode()).hexdigest()
            with self.subTest(name=name,old=old),self.assertRaises(ValueError):s.verify(record)
    def test_unhashed_edit(self):
        self.record['logs']['mail-helper-native-result.log']['text']+='changed'
        with self.assertRaises(ValueError):s.verify(self.record)
