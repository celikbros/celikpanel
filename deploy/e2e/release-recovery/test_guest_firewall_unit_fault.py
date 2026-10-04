import copy
import hashlib
import importlib.util
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('fault',Path(__file__).with_name('guest_firewall_unit_fault.py'))
s=importlib.util.module_from_spec(spec);spec.loader.exec_module(s)

class FirewallFaultTests(unittest.TestCase):
    def setUp(self):
        digest=lambda raw:hashlib.sha256(raw).hexdigest()
        self.value={'schema':'celikpanel/lab-firewall-unit-fault/v1','operation_id':'a'*32,'generation':'b'*64,'unit_sha256':digest(b'unit'),'helper_sha256':digest(b'helper'),'manifest_sha256':digest(b'manifest')}
        self.native=object.__new__(s.Native);self.native.checkpoint=self.value
        self.native.args=SimpleNamespace(operation_id='a'*32)
        self.raw={'celikpanel-firewall-restore.service':b'unit','restore':b'helper','runtime.manifest':b'manifest'}

    def test_exact_checkpoint_identity(self):
        self.assertEqual(s.validate_checkpoint(self.value,'a'*32),self.value)
        for key,value in [('operation_id','c'*32),('generation','../other'),('helper_sha256',0),('schema','v2'),('extra','unknown')]:
            bad=copy.deepcopy(self.value);bad[key]=value
            with self.subTest(key=key),self.assertRaises(s.bound.probe.ProbeError):s.validate_checkpoint(bad,'a'*32)

    def test_candidate_without_unit_or_helper_never_reaches_kill(self):
        with patch.object(s.OriginalNative,'installed',return_value={'candidate':'ready'}),patch.object(s.OriginalNative,'kill') as kill:
            for bad in ('celikpanel-firewall-restore.service','restore','runtime.manifest'):
                def read(path,*args):return b'changed' if path.name==bad else self.raw[path.name]
                with self.subTest(bad=bad),patch.object(s.bound,'protected_read',side_effect=read):
                    self.assertIsNone(self.native.installed())
                    with self.assertRaises(s.bound.base.MissedCheckpoint):self.native.kill()
                    kill.assert_not_called()

    def test_exact_proof_preserves_original_kill_guard(self):
        with patch.object(s.bound,'protected_read',side_effect=lambda path,*args:self.raw[path.name]) as read,patch.object(s.OriginalNative,'kill') as kill:
            self.assertEqual(self.native.firewall_proof(),self.value)
            self.native.kill();kill.assert_called_once_with()
            self.assertEqual(len(read.call_args_list),8)
            self.assertEqual(str(read.call_args_list[0].args[0]),'/etc/systemd/system/celikpanel-firewall-restore.service')

if __name__=='__main__':unittest.main()
