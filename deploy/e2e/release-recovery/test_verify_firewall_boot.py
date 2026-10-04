import copy
import unittest
import verify_firewall_boot as v


class FirewallEvidenceTests(unittest.TestCase):
    def fixture(self):
        unit = {'ActiveState': 'inactive', 'MainPID': '0', 'UnitFileState': 'disabled'}
        before = {'schema': 'celikpanel/native-firewall-observation/v1',
                  'identity': {'schema': 'celikpanel-release-recovery-lab/v1', 'vm_uuid': 'one'},
                  'at': '2026-09-22T00:01:00+00:00', 'boot_id': 'one',
                  'management_binaries_absent': True,
                  'units': {'celikpanel-agent.service': dict(unit), 'celikpanel-panel.service': dict(unit),
                            'celikpanel-lab-native-firewall.service': {'ActiveState': 'active', 'MainPID': '0', 'Result': 'success', 'UnitFileState': 'enabled'}},
                  'helper': {'uid': 0, 'mode': 0o755, 'sha256': 'digest'},
                  'policy': {'uid': 0, 'gid': 989, 'mode': 0o600, 'size': 75},
                  'preflight': 'Saved firewall policy and current SSH access passed native preflight; nothing was applied.\n',
                  'tables': {'celikpanel_fw': 'policy drop; tcp dport { 22, 2083 } accept udp dport 53 accept ct state established,related accept',
                             'celikpanel_lab_other': 'chain marker'},
                  'unit_sha256': 'unit', 'systemd': 'native', 'nft': 'native'}
        after = copy.deepcopy(before); after.update(boot_id='two', at='2026-09-22T00:02:00+00:00')
        return before, after, {'binary_sha256': 'digest'}

    def test_accepts_bounded_native_proof(self):
        v.check(*self.fixture())

    def test_rejects_false_or_foreign_proof(self):
        changes = [lambda a: a.update(boot_id='one'), lambda a: a['identity'].update(vm_uuid='other'),
                   lambda a: a.update(management_binaries_absent=False),
                   lambda a: a['units']['celikpanel-agent.service'].update(ActiveState='active'),
                   lambda a: a['units']['celikpanel-panel.service'].update(UnitFileState='enabled'),
                   lambda a: a['units']['celikpanel-lab-native-firewall.service'].update(Result='exit-code'),
                   lambda a: a['helper'].update(sha256='other'), lambda a: a['policy'].update(mode=0o644),
                   lambda a: a['tables'].update(celikpanel_fw='policy accept;'),
                   lambda a: a['tables'].update(celikpanel_lab_other='absent'),
                   lambda a: a.update(preflight='unknown')]
        for change in changes:
            with self.subTest(change=change):
                before, after, source = self.fixture(); change(after)
                with self.assertRaises(ValueError): v.check(before, after, source)


if __name__ == '__main__':
    unittest.main()
