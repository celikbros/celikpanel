"""Fixed second-fault admission while the exact native exchange remains held.

Only a disposable registered guest may arm the existing recovery observer.
This module never resets a VM, restarts recovery or edits product evidence.
"""
import importlib.util
from pathlib import Path
from types import SimpleNamespace
import sys
import time

_spec = importlib.util.spec_from_file_location('exchange_recovery_existing_handoff', Path(__file__).with_name('guest_recovery_handoff.py'))
hand = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = hand
_spec.loader.exec_module(hand)
FAULT = {'action': 'reboot', 'checkpoint': 'payload_restored'}
BOUNDARY = 'database-exchange-recovery-reboot'


def cancel(operation, proof):
    try:
        return hand.cancel(operation, proof['unit'])
    except Exception:
        return 'unknown'  # preserve the original failure; never infer cleanup


def arm(args, plan, worker, checkpoint, revalidate, *, clock=time.monotonic):
    if (getattr(args, 'boundary', None) != BOUNDARY or plan.get('recovery_fault') != FAULT
            or checkpoint.get('status') != 'verified'
            or checkpoint.get('classification') != 'database-exchanged-before-receipt-held'
            or checkpoint.get('operation_id') != args.operation_id
            or checkpoint.get('identity') != plan.get('identity')):
        raise ValueError('fixed exchange recovery fault not authorized')
    # Use the already verified native snapshot, never an invented marker.
    snapshot = checkpoint['database']['snapshot']
    if snapshot['snapshot'] != checkpoint['transaction']['snapshot']:
        raise ValueError('exchange recovery snapshot differs')
    options = SimpleNamespace(**{**vars(args), 'recovery_action': FAULT['action'],
                                 'recovery_checkpoint': FAULT['checkpoint']})
    deadline = clock() + 45
    def tick():
        if clock() >= deadline:
            raise TimeoutError('exchange recovery handoff deadline')
    def exact(expected):
        tick()
        if expected != worker:
            raise ValueError('exchange recovery worker differs')
        revalidate()
    proof = None
    try:
        proof = hand.arm(options, worker, snapshot, tick, exact)
        exact(worker)
        intent = proof['intent']
        if (proof.get('operation_id') != args.operation_id or proof.get('identity') != plan['identity']
                or any(intent.get(k) != v for k, v in FAULT.items())
                or intent.get('snapshot') != checkpoint['transaction']['snapshot']
                or intent.get('transaction_token_sha256') != checkpoint['transaction']['transaction_token_sha256']
                or intent.get('runtime_manifest_sha256') != checkpoint['kit']['manifest_sha256']):
            raise ValueError('armed recovery observer differs from exchange')
        return proof
    except BaseException:
        if proof is not None:
            cancel(args.operation_id, proof)
        raise
