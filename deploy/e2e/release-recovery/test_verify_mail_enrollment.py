import copy
import json
from pathlib import Path
import unittest
from verify_mail_enrollment import verify

class MailEnrollmentEvidence(unittest.TestCase):
    def setUp(self): self.e = json.loads(Path(__file__).with_name('MAIL-ENROLLMENT-BE.json').read_text())
    def test_native_record(self): self.assertEqual(verify(self.e)['process_kills'], 2)
    def test_reject_inflated_or_broken_evidence(self):
        op = self.e['operation']
        edits = [
            lambda r: r['scope'].update(production_enrollment=True),
            lambda r: r['scope'].update(reboot=True),
            lambda r: r.update(native_mail_workload=True),
            lambda r: r.update(operation='b'*32),
            lambda r: r['logs'].update(**{'forward-cut':'PASS'}),
            lambda r: r['logs'].update(**{'rollback-verify':'PASS'}),
            lambda r: r['receipt_bytes'].update({op+'.enrollment.json':'{}\n'}),
            lambda r: r['receipts'][op+'.enrollment.json'].update(target='b'*64),
            lambda r: r['wants_after'][5].append('owner.timer'),
            lambda r: r['wants_after'].__setitem__(1,1),
            lambda r: r['loaded_inverse']['celikpanel-mail-renewal.timer'].update(ActiveState='active'),
            lambda r: r['absent_after'].pop(),
            lambda r: r['preserved_attempts'].clear(),
            lambda r: r['preserved_attempts'][1].update(log='passed'),
        ]
        for i, edit in enumerate(edits):
            with self.subTest(case=i):
                changed=copy.deepcopy(self.e);edit(changed)
                with self.assertRaises((ValueError,AssertionError,KeyError,IndexError)): verify(changed)

if __name__=='__main__': unittest.main()
