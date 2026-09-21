"""Fault admission negatives, not substitutes for native acceptance."""
import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch
spec=importlib.util.spec_from_file_location('tested_budget',Path(__file__).with_name('guest_dispatch_budget.py'))
f=importlib.util.module_from_spec(spec);spec.loader.exec_module(f)

class BudgetFaultTests(unittest.TestCase):
    def setUp(self):
        self.snapshot='20260921T210000Z-from-unknown-to-'+'a'*40+'-'+'b'*32
        self.raw=('schema=celikpanel-recovery-dispatch/v1\nsnapshot='+self.snapshot+
                  '\nattempt=2\ntoken_sha256='+'c'*64+'\noperation=update\nphase=active\n').encode()

    def test_only_exact_canonical_reservation_is_accepted(self):
        self.assertEqual(f.validate_receipt(self.raw,self.snapshot,2)['attempt'],'2')
        for raw in (self.raw[:-1],self.raw+b'x\n',self.raw.replace(b'attempt=2',b'attempt=3'),
                    self.raw.replace(b'phase=active',b'phase=unknown'),
                    self.raw.replace(b'operation=update',b'operation=install'),
                    self.raw.replace(b'c'*64,b'x'*64),self.raw.replace(b'/v1',b'/v2')):
            with self.subTest(raw=raw),self.assertRaises(ValueError):f.validate_receipt(raw,self.snapshot,2)
        with self.assertRaises(ValueError):f.validate_receipt(self.raw,self.snapshot+'x',2)

    def test_native_checkpoint_does_not_authorize_cut_without_reservation(self):
        native=f.BudgetNative({'snapshot':self.snapshot},2)
        with patch.object(f.fault.Native,'observe',return_value={'worker':'verified'}),patch.object(f,'receipts',side_effect=FileNotFoundError):
            with self.assertRaises(f.fault.Unavailable):native.observe()

    def test_unit_never_starts_product_recovery_or_updates(self):
        raw=f.unit_bytes('a'*32).decode()
        self.assertIn('Type=exec\n',raw);self.assertIn('RuntimeMaxSec=650\n',raw)
        self.assertNotIn('Requires=',raw);self.assertNotIn('Wants=',raw)
        self.assertEqual(raw.count('ExecStart='),1)
        for operation in ('../host','a'*32+'\nExecStart=/bin/false'):
            with self.assertRaises(ValueError):f.unit_bytes(operation)

if __name__=='__main__':unittest.main()
