import copy
import json
from pathlib import Path
import unittest

from verify_firewall_generations import verify


class NativeFirewallGenerationEvidence(unittest.TestCase):
    def setUp(self):
        self.record = json.loads(Path(__file__).with_name('FIREWALL-GENERATIONS-AR.json').read_text())

    def test_actual_three_boot_record(self):
        self.assertTrue(verify(self.record))

    def test_incomplete_or_changed_evidence_is_not_success(self):
        changes = [
            lambda r: r['observations'].pop(),
            lambda r: r['observations'][1].update(boot_id=r['observations'][0]['boot_id']),
            lambda r: r['observations'][0].update(management_binaries_absent=False),
            lambda r: r['observations'][1]['units']['celikpanel-agent.service'].update(ActiveState='active'),
            lambda r: r['observations'][2]['units']['celikpanel-firewall-restore.service'].update(Result='exit-code'),
            lambda r: r['observations'][2]['installed_unit'].update(sha256='0'*64),
            lambda r: r['observations'][0]['policy'].update(sha256='0'*64),
            lambda r: r['observations'][1]['tables'].update(celikpanel_lab_other='missing'),
            lambda r: r['observations'][2]['generations']['b']['files']['restore'].update(mode=0o777),
            lambda r: r['observations'][2]['generations']['a']['files']['restore'].update(sha256='0'*64),
            lambda r: r['observations'][0]['identity'].update(vm_uuid='other'),
        ]
        for change in changes:
            with self.subTest(change=change):
                record = copy.deepcopy(self.record)
                change(record)
                with self.assertRaises(ValueError):
                    verify(record)


if __name__ == '__main__':
    unittest.main()
