#!/usr/bin/env python3
"""Arm or collect one bounded port conflict in a registered disposable QEMU lab."""
from __future__ import annotations
import argparse
import base64
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shlex
import stat
import sys
import tarfile
import time

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("port_fault_update_trial", HERE / "update_trial.py")
trial = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = trial
SPEC.loader.exec_module(trial)
lab = trial.lab
SCHEMA = "celikpanel/release-port-fault-intent/v1"
EVENT_SCHEMA = "celikpanel/release-port-fault/v1"
MAXIMUM = 1048576
SEQUENCE = 80
EXPECTED_COMMIT = "bd14d97efc5cfd19acd70ddf0edb9c6343317e2b"
EXPECTED_ARCHIVE = "a29bd22d72dfd70d19811994999fda9b5d7d8d85873e2a65dc190d8034c70f10"
EXPECTED_BINARIES = {"agent": "7f84ca2f03b82b51748a7a2c4f7bda8938f43a8e8bcac546171368d320f4c5ac", "panel": "88d607bcdf160ab1d296803a5632d19e72cc7351365761de5d6847191537ce67"}


def manifest_fields(raw):
    result = {}
    for line in raw.decode("ascii").splitlines():
        key, separator, value = line.partition("=")
        if not separator or key in result:
            raise ValueError("invalid release manifest fields")
        result[key] = value
    return result


def sha_stream(stream):
    digest = hashlib.sha256()
    while True:
        chunk = stream.read(1048576)
        if not chunk:
            return digest.hexdigest()
        digest.update(chunk)


def candidate_artifacts(intent, manifest_path=None):
    manifest_path = manifest_path or trial.asset_paths(SEQUENCE)[0]
    raw = manifest_path.read_bytes()
    if hashlib.sha256(raw).hexdigest() != intent["assets"]["manifest"]["sha256"]:
        raise ValueError("local manifest differs from the signed reviewed intent")
    fields = manifest_fields(raw)
    version = "v0.1.0-alpha.80"
    archive_name = "celikpanel-" + version + "-linux-amd64.tar.gz"
    if any(fields.get(key) != value for key, value in {
        "format": "celikpanel-release-manifest-v2", "sequence": "80", "version": version,
        "commit": EXPECTED_COMMIT, "os": "linux", "arch": "amd64", "archive": archive_name,
        "archive_sha256": EXPECTED_ARCHIVE, "archive_size": "23792005"}.items()):
        raise ValueError("candidate is not the pinned Alpha80 signed tuple")
    archive = manifest_path.parent / archive_name
    before = archive.lstat()
    if not stat.S_ISREG(before.st_mode) or before.st_size != int(fields["archive_size"]):
        raise ValueError("candidate archive type or size differs")
    with archive.open("rb") as stream:
        if sha_stream(stream) != fields["archive_sha256"]:
            raise ValueError("candidate archive checksum differs")
        stream.seek(0)
        with tarfile.open(fileobj=stream, mode="r:gz") as bundle:
            prefix = "celikpanel-" + version + "/"
            selected = {}
            names = {prefix + "SHA256SUMS", prefix + "bin/agent", prefix + "bin/panel"}
            for member in bundle:
                if member.name in names:
                    if member.name in selected or not member.isfile() or member.size > 40000000:
                        raise ValueError("unsafe or duplicate selected archive member")
                    selected[member.name] = member
            if set(selected) != names:
                raise ValueError("candidate payload evidence is incomplete")
            checksum_member = selected[prefix + "SHA256SUMS"]
            if checksum_member.size > 1048576:
                raise ValueError("candidate checksum inventory exceeds bound")
            checksums = {}
            for line in bundle.extractfile(checksum_member).read().decode("ascii").splitlines():
                match = re.fullmatch(r"([0-9a-f]{64}) [ *](.+)", line)
                if not match:
                    raise ValueError("invalid candidate checksum inventory")
                name = match.group(2).removeprefix("./")
                if name in checksums:
                    raise ValueError("duplicate candidate checksum entry")
                checksums[name] = match.group(1)
            result = {}
            for name in ("agent", "panel"):
                digest = sha_stream(bundle.extractfile(selected[prefix + "bin/" + name]))
                if digest != checksums.get("bin/" + name) or digest != EXPECTED_BINARIES[name]:
                    raise ValueError("candidate executable checksum differs")
                result[name] = digest
        after = os.fstat(stream.fileno())
    if (before.st_size, before.st_mtime_ns, before.st_ctime_ns) != (after.st_size, after.st_mtime_ns, after.st_ctime_ns):
        raise ValueError("candidate archive changed during verification")
    return result


def unit_name(operation, kind="port-fault"):
    if not trial.HEX32.fullmatch(operation) or kind not in ("port-fault", "update-kill"):
        raise ValueError("invalid operation identifier or fixture kind")
    return "celikpanel-lab-" + kind + "-" + operation + ".service"


def guest_collection_script(operation, kind="port-fault"):
    unit = unit_name(operation, kind)
    path = "/root/celikpanel-release-recovery-lab/" + kind + "-" + operation + ".jsonl"
    program = '''import base64,json,os,stat,subprocess
path=PATH_LITERAL
unit=UNIT_LITERAL
raw=b""
present=False
try:
 fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_CLOEXEC)
except FileNotFoundError:
 pass
else:
 with os.fdopen(fd,"rb") as stream:
  info=os.fstat(stream.fileno())
  if not stat.S_ISREG(info.st_mode) or info.st_uid!=0 or info.st_nlink!=1 or stat.S_IMODE(info.st_mode)!=0o600 or info.st_size>1048576:
   raise ValueError("unsafe fault evidence file")
  raw=stream.read(1048577)
  if len(raw)>1048576:
   raise ValueError("fault evidence exceeds bound")
  present=True
result=subprocess.run(["systemctl","show",unit,"-p","LoadState","-p","ActiveState","-p","SubState","-p","MainPID","-p","Result","-p","ExecMainStatus"],capture_output=True,text=True,timeout=10)
properties=dict(line.split("=",1) for line in result.stdout.splitlines() if "=" in line)
if result.returncode not in (0,1) or (result.returncode==1 and properties.get("LoadState")!="not-found"):
 raise ValueError("cannot query exact fault unit")
print(json.dumps({"unit":unit,"unit_query_exit":result.returncode,"properties":properties,"present":present,"jsonl_base64":base64.b64encode(raw).decode("ascii")},sort_keys=True))
'''.replace("PATH_LITERAL", repr(path)).replace("UNIT_LITERAL", repr(unit))
    return "python3 -I - <<'CELIKPANEL_FAULT_COLLECT'\n" + program + "CELIKPANEL_FAULT_COLLECT\n"


def read_guest(root, record, plan, node, operation, kind="port-fault"):
    result = lab.guarded_script(root, record, plan, node, guest_collection_script(operation, kind), timeout=30)
    if len(result.stdout) > 1500000:
        raise ValueError("fault collection transport exceeds bound")
    value = json.loads(result.stdout)
    if value.get("unit") != unit_name(operation, kind):
        raise ValueError("fault unit identity differs")
    raw = base64.b64decode(value.pop("jsonl_base64"), validate=True)
    if len(raw) > MAXIMUM:
        raise ValueError("fault evidence exceeds bound")
    return value, raw


def validate_events(raw, expected_identity, operation):
    if len(raw) > MAXIMUM or (raw and not raw.endswith(b"\n")):
        raise ValueError("fault event stream is incomplete or oversized")
    events = [json.loads(line) for line in raw.splitlines()]
    allowed = {"armed", "port_held", "candidate_installed_with_port_conflict", "released"}
    previous = -1
    order = {"armed": 0, "port_held": 1, "candidate_installed_with_port_conflict": 2, "released": 3}
    for event in events:
        kind = event.get("event")
        if (event.get("schema") != EVENT_SCHEMA or event.get("identity") != expected_identity
                or event.get("operation_id") != operation or kind not in allowed
                or not isinstance(event.get("at"), str) or order[kind] <= previous):
            raise ValueError("fault event identity or sequence differs")
        previous = order[kind]
    if events and events[0]["event"] != "armed":
        raise ValueError("fault stream lacks the first armed event")
    return events


def load_intent(root, record, plan, node):
    value = json.loads(trial.read_private(root / "evidence" / node / "update-intent.json"))
    return trial.validate_intent(value, record, plan, node, SEQUENCE)


def assert_not_started(root, node):
    path = root / "evidence" / node / "update-start-attempt.json"
    if path.exists() or path.is_symlink():
        raise ValueError("update start was already attempted; fault cannot be armed afterwards")


def collect(root, record, plan, node, intent):
    path = root / "evidence" / node / "port-fault-intent.json"
    armed_intent = json.loads(trial.read_private(path))
    if (armed_intent.get("schema") != SCHEMA or armed_intent.get("identity") != intent["identity"]
            or armed_intent.get("operation_id") != intent["request_id"]):
        raise ValueError("saved fault intent identity differs")
    state, raw = read_guest(root, record, plan, node, intent["request_id"])
    events = validate_events(raw, intent["identity"], intent["request_id"])
    label = "port-fault-collection-" + str(time.time_ns())
    evidence = {"events": trial.save(root, node, label + ".jsonl", raw),
                "state": trial.save(root, node, label + ".state.json", (json.dumps(state, sort_keys=True) + "\n").encode())}
    summary = {"action": "port-fault-collected", "node": node, "operation_id": intent["request_id"],
               "events": [event["event"] for event in events], "properties": state["properties"], "evidence": evidence}
    print(json.dumps(summary, sort_keys=True), flush=True)
    return summary


def arm(root, record, plan, node, intent):
    assert_not_started(root, node)
    trial.validate_preview(root, record, plan, node, intent)
    candidates = candidate_artifacts(intent)
    state, raw = read_guest(root, record, plan, node, intent["request_id"])
    if raw or state.get("present") or state["properties"].get("LoadState") != "not-found":
        raise ValueError("fault unit or evidence already exists; never arm a second fault")
    assets = {}
    for name in ("guest_probe.py", "guest_port_fault.py"):
        guest, digest = lab.put_file(root, record, plan, node, HERE / name, name)
        assets[name] = {"guest_path": guest, "sha256": digest}
    assert_not_started(root, node)
    fault_intent = {"schema": SCHEMA, "identity": intent["identity"], "operation_id": intent["request_id"],
                    "created_at": trial.now(), "candidate_artifacts": candidates, "assets": assets,
                    "update_intent_sha256": hashlib.sha256(trial.read_private(root / "evidence" / node / "update-intent.json")).hexdigest()}
    trial.save(root, node, "port-fault-intent.json", (json.dumps(fault_intent, sort_keys=True) + "\n").encode())
    ident = intent["identity"]
    argv = ["systemd-run", "--unit=" + unit_name(intent["request_id"]), "--no-block", "--property=RuntimeMaxSec=650", "--property=UMask=0077",
            "python3", "-I", assets["guest_port_fault.py"]["guest_path"], "--lab-nonce", ident["nonce"], "--vm-uuid", ident["vm_uuid"],
            "--cell-id", ident["cell_id"], "--node", node, "--operation-id", intent["request_id"],
            "--candidate-agent", candidates["agent"], "--candidate-panel", candidates["panel"]]
    lab.guarded_script(root, record, plan, node, shlex.join(argv), timeout=30)
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        state, raw = read_guest(root, record, plan, node, intent["request_id"])
        events = validate_events(raw, intent["identity"], intent["request_id"])
        if events:
            if events[-1]["event"] == "released" or state["properties"].get("ActiveState") != "active":
                raise ValueError("fault released or unit stopped before update start")
            collect(root, record, plan, node, intent)
            print(json.dumps({"action": "port-fault-armed", "node": node, "operation_id": intent["request_id"], "candidate_artifacts": candidates}, sort_keys=True), flush=True)
            return
        if state["properties"].get("ActiveState") == "failed":
            raise ValueError("fault unit failed before the armed event")
        time.sleep(0.2)
    raise TimeoutError("armed event is not confirmed; preserve this intent and collect without rearming")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--work-root", required=True)
    parser.add_argument("--node", choices=("arch", "debian13"), default="arch")
    parser.add_argument("--mode", choices=("arm", "collect"), required=True)
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args(argv)
    if args.mode == "arm" and not args.execute:
        parser.error("arming requires --execute and the registered disposable VM")
    root = lab.checked_root(args.work_root)
    record, plan = lab.load(root)
    intent = load_intent(root, record, plan, args.node)
    (arm if args.mode == "arm" else collect)(root, record, plan, args.node, intent)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, TimeoutError) as exc:
        print("port fault controller refused: " + type(exc).__name__ + ": " + str(exc), file=sys.stderr)
        sys.exit(2)
