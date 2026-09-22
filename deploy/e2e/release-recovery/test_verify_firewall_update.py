import copy
import importlib.util
import json
from pathlib import Path
import unittest

HERE=Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('verify_firewall_update',HERE/'verify_firewall_update.py')
s=importlib.util.module_from_spec(spec);spec.loader.exec_module(s)

class NativeFirewallUpdateTests(unittest.TestCase):
    def setUp(self):self.value=json.loads((HERE/'FIREWALL-UPDATE-AS-AT-AU.json').read_text())
    def test_committed_native_evidence(self):
        self.assertEqual(s.verify(self.value)['automatic_rollback'],'verified')
    def test_false_completion_dependency_loss_and_wrong_identity_refused(self):
        cases=[(('rollback','boot','boot_id'),self.value['rollback']['after']['boot_id']),
               (('updated','explicit_boot_enablement'),False),
               (('rollback','after','operation_status','previous_failure'),'none'),
               (('updated','after','operation_status','terminal_proof'),'none'),
               (('rollback','after','request_id'),'0'*32),
               (('rollback','after','policy','sha256'),'0'*64),
               (('updated','boot','https_http_code'),'503'),
               (('rollback','boot','tables','celikpanel_lab_other'),'changed'),
               (('rollback','fault','checkpoint','bound_worker','target_commit'),'0'*40),
               (('rollback','fault','released','kill_sent'),False),
               (('rollback','after','units',s.UNIT,'UnitFileState'),'disabled'),
               (('updated','after','unit','sha256'),self.value['updated']['before']['unit_sha256']),
               (('rollback','after','binaries','agent','sha256'),'0'*64),
               (('updated','boot','identity','vm_uuid'),self.value['rollback']['boot']['identity']['vm_uuid'])]
        cases.extend([( ('arch_rollback','boot','boot_id'),self.value['arch_rollback']['after']['boot_id']),
                      (('arch_rollback','fault','checkpoint','identity','node'),'debian13'),
                      (('arch_rollback','boot','operation_status','previous_failure'),'none')])
        gen=next(iter(self.value['rollback']['after']['generations']))
        cases.append((('rollback','after','generations',gen,'restore','mode'),0o777))
        for path,replacement in cases:
            with self.subTest(path=path):
                bad=copy.deepcopy(self.value);target=bad
                for key in path[:-1]:target=target[key]
                target[path[-1]]=replacement
                with self.assertRaises(ValueError):s.verify(bad)

if __name__=='__main__':unittest.main()
