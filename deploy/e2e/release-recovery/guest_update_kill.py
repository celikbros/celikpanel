#!/usr/bin/env python3
"""Kill only one verified disposable update worker at a frozen active checkpoint.

This is a fixture fault injector, never a product recovery path. It does not
start updates, edit product state, or signal ordinary panel/agent services.
"""
from __future__ import annotations
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import sys
import time

SPEC = importlib.util.spec_from_file_location("kill_fault_shared", Path(__file__).with_name("guest_port_fault.py"))
shared = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(shared)
probe = shared.probe
SCHEMA = "celikpanel/release-update-kill/v1"
BASELINE_AGENT = "e8e69c520cf4112a5c60380378b8347021aaa035cdaacd820c9927436beff078"
ENV = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL": "C"}
FIELDS = ("Id", "LoadState", "ActiveState", "MainPID", "ControlGroup", "InvocationID", "FreezerState")


class MissedCheckpoint(Exception):
    pass


def unit_name(operation):
    if not shared.HEX32.fullmatch(operation):
        raise probe.ProbeError("operation identity is invalid")
    return "celikpanel-self-update-" + operation + ".service"


def process_start(raw):
    tail = raw.rsplit(")", 1)[1].split()
    if len(tail) < 20 or not tail[19].isdigit():
        raise probe.ProbeError("worker process identity unavailable")
    return tail[19]


def validate_worker_fields(properties, operation):
    unit = unit_name(operation)
    if (properties.get("Id") != unit or properties.get("LoadState") != "loaded"
            or properties.get("ActiveState") != "active"
            or not properties.get("MainPID", "").isdigit() or int(properties["MainPID"]) <= 1
            or properties.get("ControlGroup") != "/system.slice/" + unit
            or not shared.HEX32.fullmatch(properties.get("InvocationID", ""))):
        raise MissedCheckpoint("exact-worker-not-active")


def active_snapshot(state, expected=None):
    marker = state["transaction"]
    if state["worker"]["ActiveState"] not in shared.ACTIVE:
        raise MissedCheckpoint("update-unit-exited")
    if (not marker or marker.get("phase") != "active" or marker.get("operation") != "update"):
        raise MissedCheckpoint("active-update-checkpoint-missed")
    if expected is not None and marker.get("snapshot") != expected:
        raise MissedCheckpoint("snapshot-identity-changed")
    if state["recovery"]["ActiveState"] in shared.ACTIVE:
        raise MissedCheckpoint("recovery-already-started")
    return marker["snapshot"]


class Native:
    def __init__(self, args):
        self.args = args
        self.unit = unit_name(args.operation_id)

    def properties(self):
        argv = ["/usr/bin/systemctl", "show", self.unit]
        for field in FIELDS:
            argv.extend(("-p", field))
        result = subprocess.run(argv, stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=2, env=ENV)
        values = {}
        for line in result.stdout.splitlines():
            key, separator, value = line.partition("=")
            if not separator or key in values:
                raise probe.ProbeError("worker unit properties malformed")
            values[key] = value
        if set(values) != set(FIELDS) or result.returncode not in (0, 1):
            raise probe.ProbeError("worker unit properties unavailable")
        if result.returncode and values.get("LoadState") != "not-found":
            raise probe.ProbeError("worker unit query failed")
        return values

    def observe(self):
        return {"worker": self.properties(), "transaction": shared.transaction(),
                "recovery": shared.unit_state("celikpanel-release-recovery.service")}

    def installed(self, tick=lambda: None):
        try:
            actual = {name: shared.digest_file(Path("/opt/celikpanel/bin") / name, tick) for name in ("agent", "panel")}
        except (FileNotFoundError, probe.ProbeError):
            return None  # an in-flight atomic replacement is not a stable checkpoint
        return actual if actual == {"agent": self.args.candidate_agent, "panel": self.args.candidate_panel} else None

    def worker_identity(self):
        properties = self.properties()
        validate_worker_fields(properties, self.args.operation_id)
        pid = properties["MainPID"]
        proc = Path("/proc") / pid
        start = process_start((proc / "stat").read_text())
        command = (proc / "cmdline").read_bytes()
        expected_command = b"/opt/celikpanel/bin/agent\0--self-update-worker\0" + self.args.operation_id.encode("ascii") + b"\0"
        if command != expected_command:
            raise MissedCheckpoint("worker-command-differs")
        cgroup = (proc / "cgroup").read_text()
        if cgroup != "0::" + properties["ControlGroup"] + "\n":
            raise MissedCheckpoint("worker-cgroup-differs")
        digest = hashlib.sha256()
        with (proc / "exe").open("rb") as stream:
            info = os.fstat(stream.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_size > 100000000:
                raise probe.ProbeError("worker executable is not bounded regular data")
            for chunk in iter(lambda: stream.read(1048576), b""):
                digest.update(chunk)
        if digest.hexdigest() != BASELINE_AGENT:
            raise MissedCheckpoint("worker-is-not-original-alpha75-agent")
        after = self.properties()
        if any(after.get(key) != properties[key] for key in ("Id", "MainPID", "InvocationID", "ControlGroup", "ActiveState")) or process_start((proc / "stat").read_text()) != start:
            raise MissedCheckpoint("worker-identity-changed")
        return {"unit": self.unit, "pid": int(pid), "start_ticks": start,
                "invocation_id": properties["InvocationID"], "cgroup": properties["ControlGroup"],
                "running_executable_sha256": digest.hexdigest(),
                "boot_id": Path("/proc/sys/kernel/random/boot_id").read_text().strip()}

    def action(self, action):
        result = subprocess.run(["/usr/bin/systemctl", action, self.unit], stdin=subprocess.DEVNULL,
                                capture_output=True, text=True, timeout=5, env=ENV)
        if result.returncode:
            raise probe.ProbeError("exact worker " + action + " failed")

    def freeze(self):
        self.action("freeze")
        if self.properties().get("FreezerState") != "frozen":
            raise MissedCheckpoint("worker-freeze-not-confirmed")

    def thaw(self):
        result = subprocess.run(["/usr/bin/systemctl", "thaw", self.unit], stdin=subprocess.DEVNULL,
                                capture_output=True, text=True, timeout=5, env=ENV)
        return result.returncode

    def full_proof(self, snapshot, tick):
        installed = self.installed(tick)
        if installed is None:
            raise MissedCheckpoint("candidate-changed-under-freeze")
        proof = shared.verify_snapshot(snapshot, tick)
        if proof is None:
            raise MissedCheckpoint("complete-snapshot-not-confirmed")
        return dict(proof, installed_artifacts=installed)

    def revalidate(self, identity):
        if self.properties().get("FreezerState") != "frozen" or self.worker_identity() != identity:
            raise MissedCheckpoint("frozen-worker-identity-changed")

    def kill(self):
        result = subprocess.run(["/usr/bin/systemctl", "kill", "--kill-whom=all", "--signal=KILL", self.unit],
                                stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=5, env=ENV)
        if result.returncode:
            raise probe.ProbeError("exact frozen update unit kill result unknown")


def run_kill(args, emit, native, *, clock=time.monotonic, pause=time.sleep, interrupted=lambda: False):
    deadline = clock() + 600
    frozen = False
    killed = False
    proof = None
    saw_worker = False
    snapshot = None
    freeze_deadline = None
    last_observation = clock()

    def tick():
        nonlocal last_observation
        if interrupted():
            raise MissedCheckpoint("signal")
        if clock() >= deadline or (freeze_deadline is not None and clock() >= freeze_deadline):
            raise MissedCheckpoint("timeout")
        if frozen and clock() - last_observation >= 0.2:
            active_snapshot(native.observe(), snapshot)
            last_observation = clock()

    emit("armed", operation_id=args.operation_id, timeout_seconds=600)
    try:
        while True:
            tick()
            state = native.observe()
            running = state["worker"]["ActiveState"] in shared.ACTIVE
            if saw_worker and not running:
                raise MissedCheckpoint("update-unit-exited-before-checkpoint")
            saw_worker = saw_worker or running
            marker = state["transaction"]
            if running and marker and marker.get("phase") == "completion.pending":
                raise MissedCheckpoint("completion-pending-checkpoint-missed")
            if running and marker and marker.get("phase") == "active" and marker.get("operation") == "update":
                snapshot = active_snapshot(state)
                if native.installed() is not None:
                    identity = native.worker_identity()
                    active_snapshot(native.observe(), snapshot)
                    frozen = True  # cleanup also runs when freeze partially succeeds
                    freeze_deadline = clock() + 30
                    native.freeze()
                    active_snapshot(native.observe(), snapshot)
                    native.revalidate(identity)
                    emit("worker_frozen", operation_id=args.operation_id, worker=identity, snapshot=snapshot)
                    proof = native.full_proof(snapshot, tick)
                    tick()
                    native.revalidate(identity)
                    active_snapshot(native.observe(), snapshot)
                    emit("candidate_installed_checkpoint", operation_id=args.operation_id, phase="active", worker=identity, **proof)
                    tick()
                    native.revalidate(identity)
                    emit("kill_requested", operation_id=args.operation_id, worker=identity, snapshot=snapshot, signal="SIGKILL", scope="exact-update-unit-cgroup")
                    native.kill()
                    killed = True
                    emit("kill_sent", operation_id=args.operation_id, worker=identity, snapshot=snapshot)
                    reason = "exact-update-unit-killed"
                    break
            pause(0.03)
    except MissedCheckpoint as exc:
        reason = str(exc)
    except Exception as exc:
        reason = "observation-error:" + type(exc).__name__
    finally:
        thaw_exit = None
        if frozen:
            try:
                thaw_exit = native.thaw()
            except Exception:
                thaw_exit = "unknown"
        emit("released", operation_id=args.operation_id, reason=locals().get("reason", "unknown"),
             checkpoint_verified=proof is not None, kill_sent=killed, thaw_exit=thaw_exit)
    return 0 if killed and proof is not None else 2


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("lab-nonce", "vm-uuid", "cell-id", "node", "operation-id", "candidate-agent", "candidate-panel"):
        parser.add_argument("--" + name, required=True)
    args = parser.parse_args(argv)
    if (not shared.HEX32.fullmatch(args.operation_id) or not shared.HEX64.fullmatch(args.candidate_agent)
            or not shared.HEX64.fullmatch(args.candidate_panel) or args.node not in ("arch", "debian13")):
        parser.error("invalid exact fixture identities")
    identity = probe.guard_guest(args)
    directory = shared.PRIVATE_ROOT.lstat()
    if not stat.S_ISDIR(directory.st_mode) or directory.st_uid != 0 or stat.S_IMODE(directory.st_mode) != 0o700:
        raise probe.ProbeError("unsafe private fixture evidence root")
    path = shared.PRIVATE_ROOT / ("update-kill-" + args.operation_id + ".jsonl")
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    stopped = False
    def stop(signum, frame):
        nonlocal stopped
        stopped = True
    previous = {sig: signal.signal(sig, stop) for sig in (signal.SIGINT, signal.SIGTERM)}
    try:
        with os.fdopen(fd, "w") as stream:
            def emit(event, **fields):
                stream.write(json.dumps({"schema": SCHEMA, "event": event, "at": probe.utc_now(), "identity": identity, **fields}, sort_keys=True) + "\n")
                stream.flush()
                os.fsync(stream.fileno())
            return run_kill(args, emit, Native(args), interrupted=lambda: stopped)
    finally:
        for sig, handler in previous.items():
            signal.signal(sig, handler)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, probe.ProbeError) as exc:
        print("update kill fault refused: " + type(exc).__name__, file=sys.stderr)
        sys.exit(2)
