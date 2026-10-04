import hashlib
import json
import re
import sys
from pathlib import Path
from verify_mail_contract import require, HEX


def digest(raw):
    return hashlib.sha256(raw.encode()).hexdigest()


def verify(r):
    require(r.get('schema') == 'celikpanel-native-mail-enrollment-composition/v1', 'schema differs')
    require(re.fullmatch('[0-9a-f]{40}', r.get('source_commit', '')), 'source missing')
    require(r.get('lab') == dict(cell_id='release-recovery__3688d130a0721786', node='arch', loopback=True), 'lab differs')
    require(r.get('scope') == dict(native_composition=True, actual_sigkill_count=2, management_absent=True, production_enrollment=False, real_mail_workload=False, reboot=False, power_loss=False, outer_fence='protected_test_intent'), 'scope inflated')
    require(r.get('production_enrollment') is False and r.get('native_mail_workload') is False and r.get('management_absent') is True, 'native scope differs')
    require(r.get('native_result') == 'forward_and_exact_inverse_verified', 'native result differs')
    op, target = r['operation'], r['target']
    require(re.fullmatch('[0-9a-f]{32}', op) and re.fullmatch(HEX, target), 'operation/target missing')
    expected_files = {'bin/recovery', 'bin/enrollment.test'} | {'bin/mail-renewal-runtime/' + name for name in ['renew', 'runtime.manifest', 'celikpanel-mail-host-cert', 'celikpanel-mail-renewal.service', 'celikpanel-mail-renewal.timer']}
    require(set(r['files']) == expected_files and all(re.fullmatch(HEX, h) for h in r['files'].values()), 'artifact inventory differs')
    require(re.fullmatch('[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}', r['boot_id']), 'boot identity missing')
    logs = r['logs']
    require(set(logs) == {'forward-cut', 'forward-verify', 'rollback-cut', 'rollback-verify'}, 'native phase missing')
    for side, point in [('forward', 'activity_forward_acted'), ('rollback', 'enable_rollback_moved')]:
        cut, complete = logs[side + '-cut'], logs[side + '-verify']
        require('native_enrollment_cut=' + point + ' operation=' + op + ' target=' + target in cut and 'PASS' not in cut, 'actual native cut missing')
        require('native_enrollment_verified=' + side + ' operation=' + op + ' target=' + target in complete and complete.rstrip().endswith('PASS'), 'same operation did not complete')
    raw, records = r['receipt_bytes'], r['receipts']
    require(set(raw) == set(records) and {k: json.loads(v) for k, v in raw.items()} == records, 'raw records differ')
    def record(suffix): return records[op + suffix]
    def sha(suffix): return digest(raw[op + suffix])
    capture, files, scope = record('.json'), record('.files.json'), record('.enrollment.json')
    require(capture['schema'] == 'celikpanel-mail-renewal-before-image/v1', 'capture schema differs')
    t = capture['contract']
    require(t['operation_id'] == op and t['target_generation'] == target and t['previous_generation'] == '' and t['before'] == {}, 'bootstrap capture differs')
    require(t['timer_before'] == dict(enablement='absent', activity='inactive') and t['timer_after'] == dict(enablement='enabled', activity='active'), 'timer scope differs')
    require(files['schema'] == 'celikpanel-mail-renewal-files/v1' and files['capture_sha256'] == sha('.json'), 'inode plan lost capture')
    require(scope == dict(schema='celikpanel-mail-renewal-enrollment/v1', operation=op, capture_sha256=sha('.json'), files_sha256=sha('.files.json'), target=target), 'accepted scope changed')
    enable = record('.timer-enable.json')
    require(enable['schema'] == 'celikpanel-mail-renewal-enable/v1' and enable['files_sha256'] == sha('.files.json'), 'enablement lost file plan')
    parent = record('.timer-parent.json')
    require(parent['schema'] == 'celikpanel-mail-renewal-parent/v1' and parent['capture_sha256'] == sha('.json') and parent['directory'] == enable['parent'], 'parent authority changed')
    require(record('.timer-parent-ready.json') == dict(schema='celikpanel-mail-renewal-parent/v1', plan_sha256=sha('.timer-parent.json'), direction='retained'), 'parent preparation missing')
    for side in ['forward', 'rollback']:
        expected = dict(schema=scope['schema'], scope_sha256=sha('.enrollment.json'), direction=side)
        require(record('.enrollment-' + side + '.json') == expected, 'terminal scope differs')
        if side == 'rollback': require(record('.enrollment-rollback-intent.json') == expected, 'monotonic inverse missing')
        require(record('.files-' + side + '.json') == dict(schema=files['schema'], plan_sha256=sha('.files.json'), direction=side), 'file completion differs')
        require(record('.timer-enable-' + side + '.json') == dict(schema=enable['schema'], plan_sha256=sha('.timer-enable.json'), direction=side), 'enablement completion differs')
        activity = dict(schema='celikpanel-mail-renewal-activity/v1', enable_sha256=sha('.timer-enable.json'), generation=target, direction=side)
        require(record('.timer-activity-' + side + '-intent.json') == record('.timer-activity-' + side + '.json') == activity, 'activity chain differs')
        attempt = record('.timer-activity-' + side + '-attempt-1.json')
        require(attempt == dict(schema=activity['schema'], intent_sha256=sha('.timer-activity-' + side + '-intent.json'), number=1, outcome='admitted'), 'native action not admitted durably')
        loaded = record('.loaded-' + side + '.json')
        require(record('.loaded-' + side + '-intent.json') == loaded == dict(schema='celikpanel-mail-renewal-bootstrap-loaded/v1', plan_sha256=sha('.files.json'), direction=side, generation=target if side == 'forward' else '', timer=dict(enablement='disabled' if side == 'forward' else 'absent', activity='inactive')), 'native load proof differs')
    require(r['wants_before'][:5] == r['wants_after'][:5] and len(r['wants_before']) == len(r['wants_after']) == 6, 'shared parent replaced')
    stage = '.celikpanel-mail-enable-' + enable['nonce']
    require(r['retained_enable_stage'] == stage and set(r['wants_after'][5]) == set(r['wants_before'][5]) | {stage}, 'unrelated inventory changed')
    require(set(r['loaded_inverse']) == {'celikpanel-mail-renewal.service', 'celikpanel-mail-renewal.timer'}, 'native inverse observation missing')
    for state in r['loaded_inverse'].values():
        require(state == dict(LoadState='not-found', ActiveState='inactive', UnitFileState='', FragmentPath='', DropInPaths='', NeedDaemonReload='no'), 'native inverse differs')
    require(r['absent_after'] == ['/etc/letsencrypt/renewal-hooks/deploy/celikpanel-mail-host-cert', '/etc/systemd/system/celikpanel-mail-renewal.service', '/etc/systemd/system/celikpanel-mail-renewal.timer', '/etc/systemd/system/timers.target.wants/celikpanel-mail-renewal.timer', '/opt/celikpanel/bin/agent', '/opt/celikpanel/bin/panel'], 'absence proof differs')
    failures = r['preserved_attempts']
    require([p['file'] for p in failures] == ['mail-enrollment-native.log', 'mail-enrollment-native-resume.log', 'mail-enrollment-native-resume2.log'], 'earlier attempts lost')
    for p in failures: require(p['sha256'] == digest(p['log']), 'earlier attempt changed')
    require('runtime_changed' in failures[1]['log'] and 'line 39' in failures[2]['log'], 'preparation/inventory failure missing')
    return dict(native_composition='verified', process_kills=2, production_enrollment='not established', real_mail_workload='not exercised')


if __name__ == '__main__':
    print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text())), sort_keys=True))
