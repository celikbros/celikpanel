#!/usr/bin/env python3
"""Verify forward native firewall update and same-Agent startup re-observation."""
import importlib.util
import json
from pathlib import Path
import re
import sys

spec = importlib.util.spec_from_file_location('firewall_update', Path(__file__).with_name('verify_firewall_update.py'))
firewall = importlib.util.module_from_spec(spec)
spec.loader.exec_module(firewall)
require = firewall.require


def verify(record):
    require(record['schema'] == 'celikpanel/native-platform-update-acceptance/v1', 'schema')
    require(record['fixture_only_changes'] == ['deploy/release-sequence-policy'], 'fixture source delta')
    require(re.fullmatch('[a-f0-9]{40}', record['source_commit']) and re.fullmatch('[a-f0-9]{40}', record['fixture_commit']), 'source commits')
    require(firewall.SHA.fullmatch(record['archive_sha256']), 'archive hash')
    case = record['updated']
    require(case['before_enablement'] == case['before']['unit_enabled'] == 'enabled', 'owner enablement')
    require(case['after']['identity']['node'] == 'arch', 'native Arch')
    expected = record['candidate']
    require(expected['commit'] == record['fixture_commit'] and expected['archive_sha256'] == record['archive_sha256'], 'candidate binding')
    require(all(firewall.SHA.fullmatch(expected['installed_artifacts'][x]) for x in ('agent', 'panel')), 'candidate binaries')
    firewall.verify_case(record, 'updated', case, expected['firewall_unit'], expected)
    accepted = record['accepted']
    require(accepted['event'] == 'accepted' and accepted['request_id'] == case['after']['request_id'], 'accepted request')
    require(accepted['cell_id'] == case['after']['identity']['cell_id'] and accepted['node'] == 'arch', 'accepted guest')
    require(accepted['target_commit'] == record['fixture_commit'] and accepted['target_archive_sha256'] == record['archive_sha256'], 'accepted target')
    early, ready = record['platform']['starting'], record['platform']['ready']
    require(early['boot_id'] == ready['boot_id'] == case['boot']['boot_id'], 'same observed boot')
    require(early['agent'] == ready['agent'], 'same Agent process')
    require(type(early['agent']['pid']) is int and early['agent']['pid'] > 1 and re.fullmatch('[0-9]+', early['agent']['start_ticks']) and re.fullmatch('[a-f0-9]{32}', early['agent']['invocation_id']), 'process identity')
    require(early['agent']['executable_sha256'] == expected['installed_artifacts']['agent'], 'new Agent executed')
    require(early['systemd_state'] == 'starting' and ready['systemd_state'] == 'running', 'real native transition')
    for observation in (early, ready):
        require(observation['schema'] == 'celikpanel/native-platform-observation/v1', 'platform schema')
        check = observation['check']
        require(check['schema'] == 'celikpanel-release-recovery-update/v1' and check['event'] == 'support_observed', 'authenticated read')
        require(check['current_commit'] == record['fixture_commit'] and check['current_version'] == 'v0.1.0-alpha.82', 'observed candidate')
        require(check['node'] == 'arch' and check['cell_id'] == case['boot']['identity']['cell_id'], 'same guest')
    require(early['check']['supported'] is False and 'host is still starting' in early['check']['detail'], 'starting refusal retained')
    require(ready['check']['supported'] is True and ready['check']['detail'] == '', 'new ready observation')
    require(ready['check']['available'] is False, 'same target is not a new update')
    return {'arch_forward_update_and_boot': 'verified', 'same_process_starting_to_ready': 'verified', 'production_UI_admission': False}


if __name__ == '__main__':
    print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text())), sort_keys=True))
