#!/usr/bin/env python3
"""Verify the bounded Debian native helper generation/reboot evidence."""
import json
import re
from pathlib import Path


def require(value, reason):
    if not value:
        raise ValueError(reason)


def verify(record):
    require(record['schema'] == 'celikpanel/native-firewall-generation-acceptance/v1', 'schema')
    before, observations, expected = record['before'], record['observations'], record['build']
    require(len(observations) == 3, 'three independent boots required')
    boot_ids = {before['boot_id']}
    expected_uuid = record['vm_uuid']
    for observation, label in zip(observations, ('a','b','a')):
        require(observation['schema'] == 'celikpanel/native-firewall-generations/v1', 'observation schema')
        require(observation['identity']['schema'] == 'celikpanel-release-recovery-lab/v1' and observation['identity']['vm_uuid'] == expected_uuid, 'guest identity')
        require(observation['boot_id'] not in boot_ids, 'stale boot observation')
        boot_ids.add(observation['boot_id'])
        require(observation['management_binaries_absent'] is True, 'management dependency')
        for name in ('celikpanel-agent.service','celikpanel-panel.service'):
            state = observation['units'][name]
            require(state['ActiveState'] == 'inactive' and state['UnitFileState'] == 'disabled', 'management service running')
        unit = observation['units']['celikpanel-firewall-restore.service']
        require(unit == {'ActiveState':'active','SubState':'exited','Result':'success','UnitFileState':'enabled','FragmentPath':'/etc/systemd/system/celikpanel-firewall-restore.service'}, 'native unit result')
        require(observation['installed_unit'] == {'sha256':expected[label]['celikpanel-firewall-restore.service'],'mode':0o644,'uid':0,'gid':0}, 'installed unit differs')
        require(observation['policy']['sha256'] == before['policy_sha256'] and observation['policy']['mode'] == 0o600 and observation['policy']['uid'] == 0, 'saved policy changed')
        require(observation['tables'] == before['tables'], 'kernel policy or unrelated table changed')
        for retained in ('a','b'):
            generation = observation['generations'][retained]
            require(re.fullmatch('[a-f0-9]{64}',generation['generation']), 'generation identity')
            for name, digest in expected[retained].items():
                require(generation['files'][name] == {'sha256':digest,'mode':0o755 if name == 'restore' else 0o644,'uid':0,'gid':0}, 'retained helper content or metadata changed')
            require(generation['generation'] == observations[0]['generations'][retained]['generation'], 'retained generation substituted')
    require(observations[0]['generations']['a']['generation'] != observations[0]['generations']['b']['generation'], 'no distinct generation')
    require(expected['a']['restore'] != expected['b']['restore'], 'same helper representation')
    return True


if __name__ == '__main__':
    record = json.loads(Path(__file__).with_name('FIREWALL-GENERATIONS-AR.json').read_text())
    verify(record)
    print('PASS: native independent generations A/B/A across three boots')
