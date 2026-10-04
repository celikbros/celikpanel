import copy
import importlib.util
import json
from pathlib import Path
import unittest

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('verify_platform_update', HERE / 'verify_platform_update.py')
s = importlib.util.module_from_spec(spec)
spec.loader.exec_module(s)

class PlatformUpdateTests(unittest.TestCase):
    def setUp(self):
        self.record = json.loads((HERE / 'PLATFORM-UPDATE-AV.json').read_text())

    def test_native_evidence(self):
        self.assertEqual(s.verify(self.record)['same_process_starting_to_ready'], 'verified')

    def test_restart_false_success_and_lost_owner_state_are_not_accepted(self):
        edits = [
            (('platform','ready','agent','pid'), 999999),
            (('platform','ready','agent','start_ticks'), '999999'),
            (('platform','ready','agent','invocation_id'), '0'*32),
            (('platform','ready','boot_id'), self.record['updated']['after']['boot_id']),
            (('platform','starting','check','supported'), True),
            (('platform','starting','systemd_state'), 'running'),
            (('platform','ready','check','supported'), False),
            (('platform','ready','check','detail'), 'origin unavailable'),
            (('platform','ready','check','current_commit'), '0'*40),
            (('accepted','request_id'), '0'*32),
            (('updated','after','operation_status','terminal_proof'), 'none'),
            (('updated','boot','tables','celikpanel_lab_other'), 'changed'),
            (('updated','boot','https_http_code'), '503'),
            (('updated','boot','units','celikpanel-firewall-restore.service','UnitFileState'), 'disabled'),
        ]
        for path, replacement in edits:
            with self.subTest(path=path):
                value=copy.deepcopy(self.record); destination=value
                for key in path[:-1]: destination=destination[key]
                destination[path[-1]]=replacement
                with self.assertRaises(ValueError): s.verify(value)

if __name__ == '__main__': unittest.main()
