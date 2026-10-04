#!/usr/bin/env python3
"""Observe one owner-started update worker; optionally arm one recovery fault.

Disposable registered QEMU guest only. The owner starts the update through the
Panel; this observer never starts, retries or signals it. In ``checkpoint``
mode it waits for the genuine worker's candidate-installed checkpoint, freezes
only that worker unit (at most 30 seconds), proves the complete snapshot and the
worker/request/target/snapshot binding with the existing bound-worker proof,
arms the selected recovery fault through the existing recovery handoff, and
thaws the worker again. **No signal is sent.** The candidate then fails by
itself. ``watch`` mode only records the timeline.

It reuses guest_bound_worker.Native (proofs), guest_update_kill (freeze/thaw,
snapshot verification) and guest_recovery_handoff/guest_recovery_fault (the
second fault). Events: ``owner-update-observer-<operation>.jsonl``.
"""
from __future__ import annotations

import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import stat
import sys
import time

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("owner_observer_bound", HERE / "guest_bound_worker.py")
bound = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(bound)
base, probe, files = bound.base, bound.probe, bound.files
INTENT_SCHEMA = "celikpanel/owner-update-observer-intent/v1"
EVENT_SCHEMA = "celikpanel/owner-update-observer/v1"
FAULTS = ({"action": "reboot", "checkpoint": "payload_restored"},
          {"action": "kill", "checkpoint": "runtime_verified"})
FREEZE_BUDGET = 30.0
OVERALL_LIMIT = 3600.0


def names(operation):
    if not files.HEX32.fullmatch(operation):
        raise probe.ProbeError("invalid observer operation")
    return {"intent": files.PRIVATE_ROOT / ("owner-update-observer-" + operation + ".json"),
            "events": files.PRIVATE_ROOT / ("owner-update-observer-" + operation + ".jsonl"),
            "unit": "celikpanel-lab-owner-update-observer-" + operation + ".service"}


def validate_intent(value, identity, operation):
    """Artifact identities use the bound-worker rules; the fault set is upd1's."""
    if (not isinstance(value, dict)
            or set(value) != {"schema", "identity", "operation_id", "mode", "baseline", "target", "recovery_fault"}
            or value["schema"] != INTENT_SCHEMA or value["identity"] != identity
            or value["operation_id"] != operation or value["mode"] not in ("checkpoint", "watch")):
        raise probe.ProbeError("owner observer intent identity differs")
    if value["mode"] == "watch" and value["recovery_fault"] is not None:
        raise probe.ProbeError("watch mode never arms a fault")
    if value["mode"] == "checkpoint" and value["recovery_fault"] not in FAULTS:
        raise probe.ProbeError("unsupported upd1 recovery fault")
    bound.validate_intent({"schema": bound.SCHEMA, "identity": identity, "operation_id": operation,
                           "baseline": value["baseline"], "target": value["target"], "recovery_fault": None},
                          identity, operation)
    return value


def compact(state):
    worker = state.get("worker") or {}
    marker = state.get("transaction") or {}
    recovery = state.get("recovery") or {}
    return {"worker": worker.get("ActiveState"), "worker_freezer": worker.get("FreezerState"),
            "transaction_phase": marker.get("phase"), "transaction_operation": marker.get("operation"),
            "snapshot": marker.get("snapshot"), "recovery": recovery.get("ActiveState")}


def run_observer(args, intent, emit, native, *, clock=time.monotonic, pause=time.sleep, interrupted=lambda: False):
    deadline = clock() + OVERALL_LIMIT
    frozen = thawed = False
    proof = None
    snapshot = None
    freeze_deadline = None
    last = None
    saw_worker = False
    reason = "unknown"
    last_observation = clock()
    fault = intent["recovery_fault"]

    def tick():
        nonlocal last_observation
        if interrupted():
            raise base.MissedCheckpoint("signal")
        if clock() >= deadline or (freeze_deadline is not None and not thawed and clock() >= freeze_deadline):
            raise base.MissedCheckpoint("timeout")
        if frozen and not thawed and clock() - last_observation >= 0.2:
            base.active_snapshot(native.observe(), snapshot)
            last_observation = clock()

    emit("armed", operation_id=args.operation_id, mode=intent["mode"], recovery_fault=fault,
         timeout_seconds=OVERALL_LIMIT)
    try:
        while True:
            tick()
            state = native.observe()
            now = compact(state)
            if now != last:
                emit("timeline", operation_id=args.operation_id, **now)
                last = now
            running = state["worker"]["ActiveState"] in files.ACTIVE
            if saw_worker and not running:
                reason = "worker-exited" + ("" if proof is not None or intent["mode"] == "watch" else "-before-checkpoint")
                break
            saw_worker = saw_worker or running
            marker = state["transaction"]
            if (intent["mode"] == "checkpoint" and proof is None and running and marker
                    and marker.get("phase") == "completion.pending"):
                emit("checkpoint_missed", operation_id=args.operation_id, reason="completion-pending-before-proof")
                intent = dict(intent, mode="watch")
            if (intent["mode"] == "checkpoint" and proof is None and running and marker
                    and marker.get("phase") == "active" and marker.get("operation") == "update"):
                snapshot = base.active_snapshot(state)
                if native.installed() is not None:
                    identity = native.worker_identity()
                    base.active_snapshot(native.observe(), snapshot)
                    frozen = True
                    freeze_deadline = clock() + FREEZE_BUDGET
                    native.freeze()
                    base.active_snapshot(native.observe(), snapshot)
                    native.revalidate(identity)
                    emit("worker_frozen", operation_id=args.operation_id, worker=identity, snapshot=snapshot)
                    proof = native.full_proof(snapshot, tick)
                    tick()
                    native.revalidate(identity)
                    base.active_snapshot(native.observe(), snapshot)
                    emit("candidate_installed_checkpoint", operation_id=args.operation_id, phase="active",
                         worker=identity, **proof)
                    if fault is not None:
                        handoff = native.recovery_handoff(identity, proof, tick)
                        emit("recovery_fault_armed", operation_id=args.operation_id, handoff=handoff)
                    tick()
                    native.revalidate(identity)
                    thaw_exit = native.thaw()
                    thawed = True
                    emit("worker_released", operation_id=args.operation_id, worker=identity, snapshot=snapshot,
                         thaw_exit=thaw_exit, signal_sent=False)
            pause(0.03 if proof is None else 0.5)
    except base.MissedCheckpoint as exc:
        reason = str(exc)
    except Exception as exc:  # noqa: BLE001 - reported as evidence, never retried
        reason = "observation-error:" + type(exc).__name__
    finally:
        thaw_exit = None
        if proof is None and fault is not None:
            try:
                native.cancel_recovery_handoff()
            except Exception:  # noqa: BLE001 - cleanup uncertainty authorizes nothing
                pass
        if frozen and not thawed:
            try:
                thaw_exit = native.thaw()
            except Exception:  # noqa: BLE001
                thaw_exit = "unknown"
        emit("released", operation_id=args.operation_id, reason=reason, checkpoint_verified=proof is not None,
             recovery_fault_armed=proof is not None and fault is not None, kill_sent=False, thaw_exit=thaw_exit)
    return 0 if (proof is not None or intent["mode"] == "watch") else 2


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    for name in ("lab-nonce", "vm-uuid", "cell-id", "node", "operation-id"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args(argv)
    if not args.execute or not files.HEX32.fullmatch(args.operation_id):
        parser.error("exact disposable operation and --execute required")
    identity = probe.guard_guest(args)
    root = files.PRIVATE_ROOT
    info = root.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or stat.S_IMODE(info.st_mode) != 0o700:
        raise probe.ProbeError("unsafe observer fixture root")
    paths = names(args.operation_id)
    intent = validate_intent(probe.strict_object(bound.protected_read(paths["intent"], 8192)), identity, args.operation_id)
    args.candidate_agent, args.candidate_panel = intent["target"]["agent_sha256"], intent["target"]["panel_sha256"]
    fault = intent["recovery_fault"]
    args.recovery_action = fault["action"] if fault else None
    args.recovery_checkpoint = fault["checkpoint"] if fault else None
    native = bound.Native(args, {"baseline": intent["baseline"], "target": intent["target"],
                                 "operation_id": args.operation_id})
    fd = os.open(paths["events"], os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    stopped = False

    def stop(signum, frame):
        nonlocal stopped
        stopped = True
    previous = {sig: signal.signal(sig, stop) for sig in (signal.SIGINT, signal.SIGTERM)}
    try:
        with os.fdopen(fd, "w") as stream:
            def emit(event, **fields):
                stream.write(json.dumps({"schema": EVENT_SCHEMA, "event": event, "at": probe.utc_now(),
                                         "identity": identity, **fields}, sort_keys=True) + "\n")
                stream.flush()
                os.fsync(stream.fileno())
            return run_observer(args, intent, emit, native, interrupted=lambda: stopped)
    finally:
        for sig, handler in previous.items():
            signal.signal(sig, handler)


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, probe.ProbeError) as exc:
        print("owner update observer refused: " + type(exc).__name__, file=sys.stderr)
        raise SystemExit(2)
