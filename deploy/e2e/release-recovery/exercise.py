#!/usr/bin/env python3
"""Run fixture producers and read-only probes on registered disposable guests."""
from __future__ import annotations
import argparse
import datetime as dt
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shlex
import sys

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("release_recovery_lab", HERE / "lab.py")
lab = importlib.util.module_from_spec(spec)
spec.loader.exec_module(lab)
ZONE = "recovery-fixture.test"


def save(root, node, name, content):
    directory = root / "evidence" / node
    directory.mkdir(parents=True, mode=0o700, exist_ok=True)
    path = directory / name
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "wb") as out:
        out.write(content)
        out.flush()
        os.fsync(out.fileno())
    return {"path": str(path), "sha256": hashlib.sha256(content).hexdigest()}


def capture(root, record, plan, node, label, since, operation_id=None):
    guest, _ = lab.put_file(root, record, plan, node, HERE / "guest_probe.py", "guest_probe.py")
    argv = ["python3", "-I", guest, "--lab-nonce", record["nonce"],
            "--vm-uuid", plan["nodes"][node]["qemu_command"][plan["nodes"][node]["qemu_command"].index("-uuid") + 1],
            "--cell-id", record["cell_id"], "--node", node, "--zone", ZONE, "--name", ZONE, "--since", since]
    if operation_id:
        argv += ["--operation-id", operation_id]
    result = lab.guarded_script(root, record, plan, node, shlex.join(argv), timeout=180)
    saved = save(root, node, label + ".json", result.stdout.encode())
    observation = json.loads(result.stdout)
    expected = {"nonce": record["nonce"], "cell_id": record["cell_id"], "node": node,
                "vm_uuid": plan["nodes"][node]["qemu_command"][plan["nodes"][node]["qemu_command"].index("-uuid") + 1]}
    if not isinstance(observation, dict) or observation.get("schema") != "celikpanel/release-recovery-observation/v1" or observation.get("status") != "observed" or observation.get("identity") != expected:
        raise ValueError("probe lacks exact guest identity; raw evidence retained")
    print(json.dumps({"action": "observed", "node": node, "label": label,
                      "status": observation.get("status"), "evidence": saved}), flush=True)
    return observation


def seed(root, record, plan, node, binary):
    directory = root / "evidence" / node
    if (directory / "seed-intent.json").exists():
        raise ValueError("this fixture's seed was already attempted; inspect its exact evidence")
    guest, digest = lab.put_file(root, record, plan, node, binary, "seed", mode=0o700)
    save(root, node, "seed-intent.json", json.dumps({"binary_sha256": digest, "zone": ZONE,
         "started_at": dt.datetime.now(dt.timezone.utc).isoformat()}).encode())
    command = shlex.join([guest, "--nonce", record["nonce"], "--zone", ZONE, "--timeout", "15m"])
    try:
        result = lab.guarded_script(root, record, plan, node, command, timeout=930)
    except lab.subprocess.TimeoutExpired as exc:
        for suffix, data in (("stdout.jsonl", exc.stdout), ("stderr.txt", exc.stderr)):
            save(root, node, "seed." + suffix, data if isinstance(data, bytes) else (data or "").encode())
        save(root, node, "seed-outcome.json", json.dumps({"status": "unknown", "error_type": "TimeoutExpired"}).encode())
        raise ValueError("seed timed out; result unknown, inspect the same operation") from None
    except lab.subprocess.CalledProcessError as exc:
        save(root, node, "seed.stdout.jsonl", (exc.stdout or "").encode())
        save(root, node, "seed.stderr.txt", (exc.stderr or "").encode())
        raise ValueError("fixture seed failed; inspect private seed evidence") from None
    saved = save(root, node, "seed.stdout.jsonl", result.stdout.encode())
    save(root, node, "seed.stderr.txt", result.stderr.encode())
    events = [json.loads(line) for line in result.stdout.splitlines() if line.strip()]
    final = events[-1] if events else None
    if not isinstance(final, dict) or any(final.get(key) != value for key, value in {
        "schema": "celikpanel-release-recovery-seed/v1", "event": "seed_complete", "cell_id": record["cell_id"],
        "node": node, "zone": ZONE, "ownership_unchanged": True}.items()):
        raise ValueError("fixture seed lacks authoritative terminal proof")
    if final.get("ownership_unchanged") is not True or type(final.get("catalog_serial")) is not int or final["catalog_serial"] < 0 or any(not isinstance(final.get(key), str) or not re.fullmatch(r"[0-9a-f]{64}", final[key]) for key in ("generation", "ownership_sha256")):
        raise ValueError("fixture seed lacks publication and acquisition proof")
    print(json.dumps({"action": "seeded", "node": node, "evidence": saved}), flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("seed", "observe"))
    parser.add_argument("--work-root", required=True)
    parser.add_argument("--node", choices=("debian13", "arch"), required=True)
    parser.add_argument("--binary", type=Path)
    parser.add_argument("--label", default="observation")
    parser.add_argument("--since", default=dt.datetime.now(dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"))
    parser.add_argument("--operation-id")
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args()
    if not re.fullmatch(r"[a-z][a-z0-9-]{0,79}", args.label):
        parser.error("invalid evidence label")
    if args.operation_id and not re.fullmatch(r"[0-9a-f]{32}", args.operation_id):
        parser.error("invalid operation ID")
    root = lab.checked_root(args.work_root)
    record, plan = lab.load(root)
    if args.command == "seed":
        if args.binary is None:
            parser.error("seed requires --binary")
        if not args.execute:
            print(json.dumps({"action": "seed", "node": args.node, "execute": False}))
            return
        seed(root, record, plan, args.node, args.binary)
    else:
        capture(root, record, plan, args.node, args.label, args.since, args.operation_id)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, lab.subprocess.SubprocessError, lab.fixture.FixtureError) as exc:
        print("exercise refused: " + str(exc), file=sys.stderr)
        sys.exit(1)
