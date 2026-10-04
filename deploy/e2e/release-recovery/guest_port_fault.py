#!/usr/bin/env python3
"""Bounded panel-port fault inside one nonce/DMI-verified disposable guest only.

Never starts an update, stops a service, or alters a product file. The sole fault
is a temporary loopback listener while the exact update has stopped the old panel.
All evidence is private JSONL; absence of a verified checkpoint stays unknown.
"""
from __future__ import annotations
import argparse
import errno
import hashlib
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import re
import signal
import socket
import stat
import subprocess
import sys
import time

SPEC = importlib.util.spec_from_file_location("port_fault_probe", Path(__file__).with_name("guest_probe.py"))
probe = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(probe)
SCHEMA = "celikpanel/release-port-fault/v1"
PRIVATE_ROOT = Path("/root/celikpanel-release-recovery-lab")
TRANSACTIONS = Path("/var/lib/celikpanel-release-transaction")
SNAPSHOTS = Path("/var/backups/celikpanel/update-snapshots")
HEX32 = re.compile(r"[0-9a-f]{32}\Z")
HEX64 = re.compile(r"[0-9a-f]{64}\Z")
SNAPSHOT = re.compile(r"[0-9]{8}T[0-9]{6}Z-from-[A-Za-z0-9._-]+-to-[0-9a-f]{40}-[0-9a-f]{32}\Z")
ACTIVE = {"active", "activating", "reloading"}


class StopFault(Exception):
    pass


def digest_file(path, tick=lambda: None):
    before = path.lstat()
    if not stat.S_ISREG(before.st_mode) or before.st_size > 512 * 1024 * 1024:
        raise probe.ProbeError("not a bounded regular file")
    digest = hashlib.sha256()
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        opened = os.fstat(stream.fileno())
        if (before.st_dev, before.st_ino) != (opened.st_dev, opened.st_ino):
            raise probe.ProbeError("file replaced during observation")
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            tick()
            digest.update(chunk)
        if probe.metadata(opened) != probe.metadata(os.fstat(stream.fileno())):
            raise probe.ProbeError("file changed during hashing")
    return digest.hexdigest()


def verify_snapshot(name, tick=lambda: None, root=SNAPSHOTS):
    if not isinstance(name, str) or not SNAPSHOT.fullmatch(name):
        raise probe.ProbeError("snapshot name is not a canonical release snapshot")
    directory = root / name
    if not directory.exists():
        return None
    if directory.is_symlink() or directory.resolve() != directory or not directory.is_dir():
        raise probe.ProbeError("snapshot is not a final canonical directory")
    manifest = directory / "SHA256SUMS"
    if not manifest.exists():
        return None
    raw = probe.bounded_file(manifest, 8 * 1024 * 1024)
    rows = {}
    for line in raw.decode("utf-8").splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  (\./[^\x00\r\n]+)", line)
        if not match:
            raise probe.ProbeError("snapshot manifest contains an unsupported entry")
        relative = match[2][2:]
        parsed = PurePosixPath(relative)
        if parsed.is_absolute() or ".." in parsed.parts or str(parsed) != relative or relative == "SHA256SUMS" or relative in rows:
            raise probe.ProbeError("snapshot manifest path is ambiguous")
        rows[relative] = match[1]
    if not rows or len(rows) > 20000 or "snapshot.version" not in rows:
        raise probe.ProbeError("snapshot manifest is incomplete")
    actual = set()
    for parent, directories, files in os.walk(directory, followlinks=False):
        for entry in directories:
            if not stat.S_ISDIR((Path(parent) / entry).lstat().st_mode):
                raise probe.ProbeError("snapshot contains a symbolic or special directory")
        for filename in files:
            path = Path(parent) / filename
            if not stat.S_ISREG(path.lstat().st_mode):
                raise probe.ProbeError("snapshot contains a symbolic or special file")
            if path != manifest:
                actual.add(path.relative_to(directory).as_posix())
    if set(rows) != actual:
        raise probe.ProbeError("snapshot file inventory does not match manifest")
    for relative, expected in rows.items():
        path = directory / relative
        if path.resolve() != path or digest_file(path, tick) != expected:
            raise probe.ProbeError("snapshot payload checksum mismatch")
    if probe.bounded_file(manifest, 8 * 1024 * 1024) != raw:
        raise probe.ProbeError("snapshot manifest changed during verification")
    return {"snapshot": name, "manifest_sha256": hashlib.sha256(raw).hexdigest(), "verified_files": len(rows)}


def unit_state(unit, *, allow_not_found=False):
    result = subprocess.run(["/usr/bin/systemctl", "show", unit, "-p", "ActiveState", "-p", "MainPID", "-p", "LoadState"],
                            stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                            text=True, timeout=2, env={"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL": "C"})
    values = {}
    for line in result.stdout.splitlines():
        if "=" in line:
            key, value = line.split("=", 1)
            if key in values:
                raise probe.ProbeError("duplicate unit observation")
            values[key] = value
    if set(values) != {"ActiveState", "MainPID", "LoadState"} or not values["MainPID"].isdigit():
        raise probe.ProbeError("unit observation unavailable")
    absent = values == {"ActiveState": "inactive", "MainPID": "0", "LoadState": "not-found"}
    if absent and allow_not_found and result.returncode in (0, 1):
        return values
    if result.returncode or values["LoadState"] != "loaded":
        raise probe.ProbeError("unit observation unavailable")
    return values


def transaction():
    present = []
    for phase in ("active", "completion.pending"):
        path = TRANSACTIONS / phase
        if path.exists() or path.is_symlink():
            fields = probe.parse_transaction(probe.bounded_file(path, 8192))
            present.append(dict(fields, phase=phase))
    if len(present) > 1:
        raise probe.ProbeError("ambiguous release transaction markers")
    return present[0] if present else None


def observe(args):
    return {"update": unit_state("celikpanel-self-update-" + args.operation_id + ".service", allow_not_found=True),
            "panel": unit_state("celikpanel-panel.service"),
            "recovery": unit_state("celikpanel-release-recovery.service"),
            "transaction": transaction()}


def bind_port():
    listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    try:
        listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        listener.bind(("127.0.0.1", 2083))
        listener.listen(1)
        return listener
    except BaseException:
        listener.close()
        raise


def candidate_proof(args, snapshot, tick):
    expected = {"agent": args.candidate_agent, "panel": args.candidate_panel}
    actual = {}
    for name in expected:
        actual[name] = digest_file(Path("/opt/celikpanel/bin") / name, tick)
    if actual != expected:
        return None
    proof = verify_snapshot(snapshot, tick)
    if proof is None:
        return None
    return dict(proof, installed_artifacts=actual)


def run_fault(args, emit, *, observer=observe, binder=bind_port, prover=candidate_proof,
              clock=time.monotonic, pause=time.sleep, interrupted=lambda: False):
    deadline = clock() + 600
    listener = None
    checkpoint = None
    saw_update = False
    last_observation = clock()
    held_snapshot = None

    def stop_if_recovery(state):
        marker = state["transaction"]
        if state["update"]["ActiveState"] not in ACTIVE:
            raise StopFault("update-unit-exited")
        if state["recovery"]["ActiveState"] in ACTIVE:
            raise StopFault("recovery-unit-started")
        if marker and marker.get("operation") == "rollback":
            raise StopFault("transaction-entered-rollback")
        if marker is None:
            raise StopFault("transaction-disappeared")
        if marker.get("snapshot") != held_snapshot:
            raise StopFault("transaction-snapshot-changed")

    def tick():
        nonlocal last_observation
        if interrupted():
            raise StopFault("signal")
        if clock() >= deadline:
            raise StopFault("timeout")
        if listener is not None and clock() - last_observation >= 0.2:
            stop_if_recovery(observer(args))
            last_observation = clock()

    emit("armed", operation_id=args.operation_id, timeout_seconds=600)
    try:
        while True:
            tick()
            state = observer(args)
            last_observation = clock()
            active = state["update"]["ActiveState"] in ACTIVE
            marker = state["transaction"]
            if saw_update and not active:
                raise StopFault("update-unit-exited")
            if active:
                saw_update = True
            if listener is not None:
                stop_if_recovery(state)
            elif active and state["panel"]["MainPID"] == "0" and marker and marker.get("operation") == "update" and state["recovery"]["ActiveState"] not in ACTIVE:
                try:
                    listener = binder()
                except OSError as exc:
                    if exc.errno != errno.EADDRINUSE:
                        raise
                    pause(0.1)
                    continue
                held_snapshot = marker["snapshot"]
                emit("port_held", operation_id=args.operation_id, address="127.0.0.1:2083",
                     snapshot=marker["snapshot"], phase=marker["phase"])
            if listener is not None and checkpoint is None:
                checkpoint = prover(args, marker["snapshot"], tick)
                if checkpoint:
                    emit("candidate_installed_with_port_conflict", operation_id=args.operation_id, **checkpoint)
            pause(0.1)
    except StopFault as exc:
        reason = str(exc)
    except Exception as exc:
        reason = "observation-error:" + type(exc).__name__
    finally:
        if listener is not None:
            listener.close()
        emit("released", operation_id=args.operation_id, reason=locals().get("reason", "interrupted"),
             checkpoint_verified=checkpoint is not None, port_was_held=listener is not None)
    return 0 if checkpoint is not None else 2


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--lab-nonce", required=True)
    parser.add_argument("--vm-uuid", required=True)
    parser.add_argument("--cell-id", required=True)
    parser.add_argument("--node", choices=("debian13", "arch"), required=True)
    parser.add_argument("--operation-id", required=True)
    parser.add_argument("--candidate-agent", required=True)
    parser.add_argument("--candidate-panel", required=True)
    args = parser.parse_args(argv)
    if not HEX32.fullmatch(args.operation_id) or not HEX64.fullmatch(args.candidate_agent) or not HEX64.fullmatch(args.candidate_panel):
        parser.error("operation and candidate identities must be exact hexadecimal values")
    identity = probe.guard_guest(args)
    info = PRIVATE_ROOT.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or stat.S_IMODE(info.st_mode) != 0o700:
        raise probe.ProbeError("private fixture output root is unsafe")
    path = PRIVATE_ROOT / ("port-fault-" + args.operation_id + ".jsonl")
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
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
            return run_fault(args, emit, interrupted=lambda: stopped)
    finally:
        for sig, handler in previous.items():
            signal.signal(sig, handler)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, probe.ProbeError) as exc:
        print("port fault refused: " + type(exc).__name__, file=sys.stderr)
        sys.exit(2)
