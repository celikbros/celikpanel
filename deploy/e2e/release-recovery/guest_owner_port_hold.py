#!/usr/bin/env python3
"""Owner-fixable panel-port hold for the upd1 owner-continuation cell (disposable guest only).

The cause the owner can fix: something else holds the panel's port. This helper
reuses the guarded primitives of ``guest_port_fault.py`` (the exact update unit,
the panel and recovery units, the release transaction marker and the loopback
listener on 127.0.0.1:2083). It binds only after the exact updater has stopped
the old panel, as that fault does.

Unlike that fault, the hold is NOT released when the updater exits or native
recovery starts: it must outlast forward completion's retry budget, so the new
panel's stability wait fails (``panel_start_unverified``) in the update and in
every automatic forward attempt, and completion pauses at
``paused_retry_limit``. It is released only by:

* the owner's action: ``systemctl stop`` of this lab unit (SIGTERM), reported as
  ``owner-released`` - the expected end;
* the operation leaving the forward update path: a rollback marker, another
  snapshot, or no transaction marker for ``MARKER_GONE_SECONDS``;
* an observation that stays unavailable for ``OBSERVATION_GRACE_SECONDS``
  (one ambiguous read during a marker rename is tolerated);
* its bound (``--hold-seconds``, sized by the driver from the product's
  stability wait, recovery timer and retry budget), plus a systemd
  ``RuntimeMaxSec`` set by the driver.

It never starts, stops or edits a product unit or file, and never starts an
update or a retry. Evidence is private JSONL under the lab root.
"""
from __future__ import annotations

import argparse
import errno
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import stat
import sys
import time

SPEC = importlib.util.spec_from_file_location("owner_port_hold_fault", Path(__file__).with_name("guest_port_fault.py"))
fault = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(fault)
probe = fault.probe
SCHEMA = "celikpanel/upd1-owner-port-hold/v1"
PRIVATE_ROOT = Path("/root/celikpanel-release-recovery-lab")
HEX32 = re.compile(r"[0-9a-f]{32}\Z")
ACTIVE = fault.ACTIVE
ADDRESS = "127.0.0.1:2083"
MIN_HOLD_SECONDS, MAX_HOLD_SECONDS = 60, 3600
MARKER_GONE_SECONDS = 5.0
OBSERVATION_GRACE_SECONDS = 10.0
TICK_SECONDS = 0.2
OWNER_RELEASED = "owner-released"


class StopHold(Exception):
    pass


def events_path(operation_id: str) -> Path:
    if not HEX32.fullmatch(operation_id or ""):
        raise ValueError("invalid operation id")
    return PRIVATE_ROOT / ("owner-port-hold-" + operation_id + ".jsonl")


def run_hold(args, emit, *, hold_seconds, observer=fault.observe, binder=fault.bind_port,
             clock=time.monotonic, pause=time.sleep, interrupted=lambda: False) -> int:
    """0 only when the port was held and the owner released it; 2 otherwise (never retried here)."""
    if not MIN_HOLD_SECONDS <= hold_seconds <= MAX_HOLD_SECONDS:
        raise ValueError("hold bound outside the reviewed range")
    started = clock()
    deadline = started + hold_seconds
    listener = None
    held_at = None
    held_snapshot = None
    saw_update = False
    gone_since = None
    error_since = None
    phases_seen: list[str] = []
    reason = "interrupted"
    emit("armed", operation_id=args.operation_id, hold_seconds=hold_seconds, address=ADDRESS)
    try:
        while True:
            if interrupted():
                raise StopHold(OWNER_RELEASED if listener is not None else "stopped-before-hold")
            if clock() >= deadline:
                raise StopHold("timeout")
            try:
                state = observer(args)
            except Exception as exc:  # noqa: BLE001 - a transient unreadable state is tolerated briefly
                error_since = clock() if error_since is None else error_since
                if clock() - error_since >= OBSERVATION_GRACE_SECONDS:
                    raise StopHold("observation-error:" + type(exc).__name__)
                pause(TICK_SECONDS)
                continue
            error_since = None
            marker = state["transaction"]
            if listener is None:
                active = state["update"]["ActiveState"] in ACTIVE
                if saw_update and not active:
                    raise StopHold("update-unit-exited-before-hold")
                saw_update = saw_update or active
                if (active and state["panel"]["MainPID"] == "0" and marker and marker.get("operation") == "update"
                        and state["recovery"]["ActiveState"] not in ACTIVE):
                    try:
                        listener = binder()
                    except OSError as exc:
                        if exc.errno != errno.EADDRINUSE:
                            raise
                        pause(0.1)
                        continue
                    held_at = clock()
                    held_snapshot = marker.get("snapshot")
                    phases_seen.append(marker.get("phase"))
                    emit("port_held", operation_id=args.operation_id, address=ADDRESS, snapshot=held_snapshot,
                         phase=marker.get("phase"))
            elif marker is None:
                gone_since = clock() if gone_since is None else gone_since
                if clock() - gone_since >= MARKER_GONE_SECONDS:
                    raise StopHold("transaction-finished")
            else:
                gone_since = None
                if marker.get("operation") == "rollback":
                    raise StopHold("transaction-entered-rollback")
                if marker.get("snapshot") != held_snapshot:
                    raise StopHold("transaction-snapshot-changed")
                if marker.get("phase") not in phases_seen:
                    phases_seen.append(marker.get("phase"))
                    emit("phase_seen", operation_id=args.operation_id, phase=marker.get("phase"),
                         held_seconds=round(clock() - held_at, 1),
                         update_unit=state["update"]["ActiveState"], recovery_unit=state["recovery"]["ActiveState"])
            pause(TICK_SECONDS)
    except StopHold as exc:
        reason = str(exc)
    except Exception as exc:  # noqa: BLE001 - recorded; the port is always released below
        reason = "error:" + type(exc).__name__
    finally:
        if listener is not None:
            listener.close()
        emit("released", operation_id=args.operation_id, reason=reason, port_was_held=listener is not None,
             held_seconds=None if held_at is None else round(clock() - held_at, 1), snapshot=held_snapshot,
             phases_seen=phases_seen)
    return 0 if reason == OWNER_RELEASED else 2


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--lab-nonce", required=True)
    parser.add_argument("--vm-uuid", required=True)
    parser.add_argument("--cell-id", required=True)
    parser.add_argument("--node", choices=("debian13", "arch", "ubuntu"), required=True)
    parser.add_argument("--operation-id", required=True)
    parser.add_argument("--hold-seconds", type=int, required=True)
    args = parser.parse_args(argv)
    if not HEX32.fullmatch(args.operation_id):
        parser.error("operation id must be 32 lowercase hexadecimal characters")
    if not MIN_HOLD_SECONDS <= args.hold_seconds <= MAX_HOLD_SECONDS:
        parser.error(f"--hold-seconds must be {MIN_HOLD_SECONDS}..{MAX_HOLD_SECONDS}")
    identity = probe.guard_guest(args)
    info = PRIVATE_ROOT.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or stat.S_IMODE(info.st_mode) != 0o700:
        raise probe.ProbeError("private fixture output root is unsafe")
    fd = os.open(events_path(args.operation_id), os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    stopped = False

    def stop(signum, frame):
        nonlocal stopped
        stopped = True

    previous = {sig: signal.signal(sig, stop) for sig in (signal.SIGINT, signal.SIGTERM)}
    try:
        with os.fdopen(fd, "w") as stream:
            def emit(event, **fields):
                stream.write(json.dumps({"schema": SCHEMA, "event": event, "at": probe.utc_now(),
                                         "identity": identity, **fields}, sort_keys=True) + "\n")
                stream.flush()
                os.fsync(stream.fileno())
            return run_hold(args, emit, hold_seconds=args.hold_seconds, interrupted=lambda: stopped)
    finally:
        for sig, handler in previous.items():
            signal.signal(sig, handler)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, probe.ProbeError) as exc:
        print("owner port hold refused: " + type(exc).__name__, file=sys.stderr)
        sys.exit(2)
