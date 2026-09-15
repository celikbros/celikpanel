"""Offline causal proof for the closed exchange-cut then recovery-reboot fixture.

Inputs must already have their capture hashes and registered guest identity checked.
A reset submission is not reboot success; this check requires a different boot and
same transaction's native rollback completion. It does not prove storage durability.
"""
import hashlib
import json

UNIT = 'celikpanel-release-recovery.service'


def require(value, reason):
    if not value: raise ValueError(reason)


def verify(rows_before, rows_after, exchange, recovery, reset, final):
    intent = recovery['intent']; proof = recovery['reboot_proof']; held = proof['checkpoint']
    worker = proof['worker']; last = final['terminal_checkpoint']; tx = exchange['transaction']
    identity = exchange['identity']; operation = exchange['operation_id']
    require(reset.get('identity') == recovery.get('identity') == final.get('identity') == identity
            and reset.get('operation_id') == recovery.get('operation_id') == final.get('operation_id') == operation,
            'two-fault identity differs')
    require(reset.get('command') == 'system_reset' and reset.get('action') == 'registered-QEMU-reset-submitted-once'
            and reset.get('scope') == 'registered-disposable-QEMU-only', 'reset submission not established')
    require(intent.get('action') == 'reboot' and intent.get('checkpoint') == held.get('checkpoint') == 'payload_restored'
            and held.get('schema') == last.get('schema') == 'celikpanel/recovery-checkpoint/v1', 'recovery checkpoint differs')
    require(reset.get('before_boot_id') == worker['boot_id'] == held['boot_id'] == exchange['worker']['boot_id']
            and reset.get('checkpoint_sha256') == proof['checkpoint_sha256'], 'reset checkpoint binding differs')
    for key in ('snapshot','transaction_token_sha256'):
        require(intent[key] == held[key] == last[key] == tx[key] == reset['native_cut'][key], 'transaction changed across reboot')
    require(intent['runtime_manifest_sha256'] == held['runtime_manifest_sha256'] == last['runtime_manifest_sha256']
            == exchange['kit']['manifest_sha256'], 'recovery runtime changed')
    require(held['transaction_operation'] == last['transaction_operation'] == 'rollback'
            and held['recovery_unit'] == last['recovery_unit'] == worker['unit'] == UNIT
            and held['invocation_id'] == worker['invocation_id'], 'native recovery invocation differs')
    require(final['boot_id'] == last['boot_id'] and final['boot_id'] != held['boot_id']
            and last['invocation_id'] != held['invocation_id'] and last['checkpoint'] == 'schedulers_restored'
            and final['transaction_markers'] == [] and final['expected_version'] == 38, 'same-operation recovery did not finish in new boot')
    require(reset['native_cut']['checkpoint_sha256'] == hashlib.sha256(json.dumps(exchange,sort_keys=True,separators=(',',':')).encode()).hexdigest(), 'native exchange binding differs')
    before_boot = held['boot_id'].replace('-',''); after_boot = last['boot_id'].replace('-','')
    stamp=lambda row:int(row.get('__REALTIME_TIMESTAMP','0'))
    def same(row, boot):return row.get('_BOOT_ID','').replace('-','') == boot
    def native(row, boot, invocation):
        return same(row,boot) and row.get('_SYSTEMD_UNIT') == UNIT and row.get('_SYSTEMD_INVOCATION_ID') == invocation
    updater = exchange['worker']['unit']
    killed=[r for r in rows_before if same(r,before_boot) and r.get('UNIT',r.get('_SYSTEMD_UNIT'))==updater and ('status=9/KILL' in str(r.get('MESSAGE','')) or 'signal=KILL' in str(r.get('MESSAGE','')))]
    triggered=[r for r in rows_before if same(r,before_boot) and r.get('UNIT',r.get('_SYSTEMD_UNIT'))==updater and 'Triggering OnFailure=' in str(r.get('MESSAGE',''))]
    snapshots_before=[r for r in rows_before if native(r,before_boot,held['invocation_id']) and tx['snapshot'] in str(r.get('MESSAGE','')) and 'Verified snapshot /' in str(r.get('MESSAGE',''))]
    snapshots_after=[r for r in rows_after if native(r,after_boot,last['invocation_id']) and tx['snapshot'] in str(r.get('MESSAGE','')) and 'Verified snapshot /' in str(r.get('MESSAGE',''))]
    completed=[r for r in rows_after if native(r,after_boot,last['invocation_id']) and str(r.get('MESSAGE','')).strip().startswith('==> Rollback complete /')]
    require(killed and triggered and snapshots_before and snapshots_after and completed, 'native journal chain incomplete')
    require(min(map(stamp,killed)) <= min(map(stamp,triggered)) <= min(map(stamp,snapshots_before))
            and max(map(stamp,snapshots_before)) < min(map(stamp,snapshots_after)) <= max(map(stamp,completed)), 'native journal order differs')
    return {'status':'verified','operation_id':operation,'snapshot':tx['snapshot'],
            'before_boot_id':held['boot_id'],'after_boot_id':last['boot_id'],
            'before_invocation_id':held['invocation_id'],'after_invocation_id':last['invocation_id'],
            'kill_records':killed,'onfailure_records':triggered,'before_snapshot_records':snapshots_before,
            'after_snapshot_records':snapshots_after,'terminal_records':completed,
            'intermediate_failure_records':[r for r in rows_after if same(r,after_boot) and (r.get('UNIT',r.get('_SYSTEMD_UNIT'))==UNIT) and any(text in str(r.get('MESSAGE','')) for text in ('systemd is not ready','failed with status','Failed with result','Failed to start'))],
            'scope':'native-exchange-cut-OnFailure-rollback-QMP-reset-new-boot-same-transaction'}
