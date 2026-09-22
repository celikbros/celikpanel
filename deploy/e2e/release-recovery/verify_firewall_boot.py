#!/usr/bin/env python3
"""Verify the bounded Agent-absent native firewall reboot observation."""
import argparse
import hashlib
import json
from pathlib import Path
from datetime import datetime


def require(ok, message):
    if not ok:
        raise ValueError(message)


def check(before, after, source):
    for value in (before, after):
        require(value['schema'] == 'celikpanel/native-firewall-observation/v1', 'wrong observation schema')
        require(value['identity']['schema'] == 'celikpanel-release-recovery-lab/v1', 'not a disposable fixture')
        require(value['management_binaries_absent'] is True, 'management executable still available')
        for name in ('celikpanel-agent.service', 'celikpanel-panel.service'):
            unit = value['units'][name]
            require(unit['ActiveState'] == 'inactive' and unit['MainPID'] == '0' and unit['UnitFileState'] == 'disabled', 'management still running/enabled')
        unit = value['units']['celikpanel-lab-native-firewall.service']
        require(unit == {'ActiveState': 'active', 'MainPID': '0', 'Result': 'success', 'UnitFileState': 'enabled'}, 'native boot consumer not successful')
        require(value['helper']['uid'] == 0 and value['helper']['mode'] == 0o755, 'unsafe helper')
        require(value['helper']['sha256'] == source['binary_sha256'], 'wrong tested helper')
        require(value['policy']['uid'] == 0 and value['policy']['mode'] == 0o600 and value['policy']['size'] > 0, 'unsafe policy')
        require(value['preflight'] == 'Saved firewall policy and current SSH access passed native preflight; nothing was applied.\n', 'no fresh preflight')
        table = value['tables']['celikpanel_fw']
        for rule in ('policy drop;', 'tcp dport { 22, 2083 } accept', 'udp dport 53 accept', 'ct state established,related accept'):
            require(rule in table, 'expected tested policy missing')
        require('chain marker' in value['tables']['celikpanel_lab_other'], 'unrelated table absent')
    require(before['identity'] == after['identity'], 'foreign guest observation')
    require(before['boot_id'] != after['boot_id'], 'no real reboot')
    require(datetime.fromisoformat(before['at']) < datetime.fromisoformat(after['at']), 'observations out of order')
    for key in ('helper', 'policy', 'tables', 'unit_sha256', 'systemd', 'nft'):
        require(before[key] == after[key], 'changed ' + key)


def verify(directory):
    root = Path(directory)
    raw = {name: (root / (name + '.json')).read_bytes() for name in ('before', 'after', 'source')}
    values = {name: json.loads(data) for name, data in raw.items()}
    check(values['before'], values['after'], values['source'])
    after = values['after']
    return {'schema': 'celikpanel/native-firewall-boot-acceptance/v1',
            'node': after['identity']['node'], 'vm_uuid': after['identity']['vm_uuid'],
            'at': after['at'], 'systemd': after['systemd'], 'nft': after['nft'],
            'helper_sha256': after['helper']['sha256'], 'source': values['source'],
            'policy_sha256': after['policy']['sha256'], 'unit_sha256': after['unit_sha256'],
            'boot_ids': [values['before']['boot_id'], after['boot_id']],
            'management_binaries_absent': True, 'management_services_disabled': True,
            'native_firewall_after_reboot': True, 'fresh_guarded_ssh_after_reboot': True,
            'other_native_table_retained': True,
            'evidence_sha256': {name + '.json': hashlib.sha256(data).hexdigest() for name, data in raw.items()},
            'limitations': ['Fixture-seeded canonical v2 policy, not Agent RPC production or installation migration',
                            'Separate fixture unit; production boot unit still invokes Agent',
                            'Orderly Debian reboot, not power loss, Arch or emergency-target recovery',
                            'No shared Agent exclusion or concurrent owner nft mutation acceptance',
                            'Not panel removal or complete P0.5 acceptance']}


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--evidence-dir', required=True)
    a = p.parse_args()
    print(json.dumps(verify(a.evidence_dir), sort_keys=True, indent=2))
