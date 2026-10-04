#!/usr/bin/env python3
"""One real signed update in a registered disposable QEMU recovery lab.

This fixture has no production destination and never retries a start or repairs
the guest. An ambiguous response is preserved and queried under the same ID.
"""
from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import shlex
import stat
import subprocess
import sys

HERE = Path(__file__).resolve().parent
REPOSITORY = HERE.parents[2]
SPEC = importlib.util.spec_from_file_location("release_update_exercise", HERE / "exercise.py")
exercise = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = exercise
SPEC.loader.exec_module(exercise)
PROFILE_SPEC = importlib.util.spec_from_file_location("update_baseline_profiles", HERE / "baseline_profiles.py")
profiles = importlib.util.module_from_spec(PROFILE_SPEC)
sys.modules[PROFILE_SPEC.name] = profiles
PROFILE_SPEC.loader.exec_module(profiles)
lab = exercise.lab
HEX32 = re.compile(r"[0-9a-f]{32}\Z")
HEX64 = re.compile(r"[0-9a-f]{64}\Z")
SCHEMA = "celikpanel-release-recovery-update-intent/v1"


def now():
    return dt.datetime.now(dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def read_private(path, maximum=131072):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    with os.fdopen(fd, "rb") as stream:
        before = os.fstat(stream.fileno())
        if not (stat.S_ISREG(before.st_mode) and before.st_uid == os.getuid()
                and before.st_nlink == 1 and stat.S_IMODE(before.st_mode) == 0o600
                and before.st_size <= maximum):
            raise ValueError("unsafe private fixture evidence")
        raw = stream.read(maximum + 1)
        after = os.fstat(stream.fileno())
        if (len(raw) != before.st_size or before.st_mtime_ns != after.st_mtime_ns
                or before.st_ctime_ns != after.st_ctime_ns):
            raise ValueError("fixture evidence changed during read")
        return raw


def save(root, node, name, content):
    result = exercise.save(root, node, name, content)
    fd = os.open(Path(result["path"]).parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)
    return result


def validate_seed(raw, record, node):
    if len(raw) > 131072:
        raise ValueError("seed proof exceeds bound")
    events = [json.loads(line) for line in raw.splitlines() if line.strip()]
    expected = ["switch_start", "switch_complete", "publication_start", "publication_complete",
                "publication_start", "publication_complete", "seed_complete"]
    if [item.get("event") for item in events] != expected:
        raise ValueError("seed does not contain the exact genuine producer sequence")
    for item in events:
        if (item.get("schema") != "celikpanel-release-recovery-seed/v1"
                or item.get("cell_id") != record["cell_id"] or item.get("node") != node
                or item.get("zone") != exercise.ZONE):
            raise ValueError("seed identity differs from the registered node")
    completed = [events[index] for index in (1, 3, 5, 6)]
    ownership = completed[0].get("ownership_sha256", "")
    if not HEX64.fullmatch(ownership):
        raise ValueError("seed ownership hash missing")
    for item in completed:
        if (item.get("ownership_unchanged") is not True
                or item.get("ownership_sha256") != ownership
                or not HEX64.fullmatch(item.get("generation", ""))
                or type(item.get("catalog_serial")) is not int or item["catalog_serial"] < 0):
            raise ValueError("seed publication preservation proof is incomplete")
    if len({item["generation"] for item in completed[:3]}) != 3 or completed[2]["generation"] != completed[3]["generation"]:
        raise ValueError("seed did not prove two genuine generation advances")
    requests = set()
    for index in (0, 2, 4):
        first, final = events[index:index + 2]
        request = first.get("request_id", "")
        if (not HEX32.fullmatch(request) or request in requests
                or final.get("request_id") != request or first.get("owner_id") != final.get("owner_id")
                or not HEX32.fullmatch(first.get("owner_id", ""))):
            raise ValueError("seed producer operation identity differs")
        requests.add(request)
    return events[-1]


def validate_baseline(value, profile_name=profiles.DEFAULT):
    profile = profiles.get_profile(profile_name)
    profiles.validate_result(value, profile_name)
    if (value.get("schema") != "celikpanel/release-baseline-install-result/v1"
            or value.get("version") != profile.version or value.get("exit_code") != 0
            or value.get("error_type") is not None or value.get("https_curl_exit") != 0
            or value.get("https_http_code") != "200"):
        raise ValueError("genuine baseline installation is not confirmed")
    for name in ("agent", "panel"):
        digest = value.get("installed_artifacts", {}).get(name, "")
        if (not HEX64.fullmatch(digest) or value.get("running_artifacts", {}).get(name) != digest
                or value.get("services", {}).get(name, {}).get("ActiveState") != "active"):
            raise ValueError("baseline service executable proof is incomplete")


def asset_paths(sequence):
    base = REPOSITORY / (".tmp-release" + str(sequence)) / "assets"
    manifest = base / ("celikpanel-v0.1.0-alpha." + str(sequence) + "-linux-amd64.release-manifest-v2")
    return manifest, Path(str(manifest) + ".sig")


def identity(record, plan, node):
    command = plan["nodes"][node]["qemu_command"]
    return {"nonce": record["nonce"], "cell_id": record["cell_id"], "node": node,
            "vm_uuid": command[command.index("-uuid") + 1]}


def validate_intent(value, record, plan, node, sequence):
    if (value.get("schema") != SCHEMA or value.get("identity") != identity(record, plan, node)
            or value.get("sequence") != sequence or not HEX32.fullmatch(value.get("request_id", ""))):
        raise ValueError("saved update intent differs from this exact registered trial")
    return value


def stage(root, record, plan, node, sequence, binary):
    manifest, signature = asset_paths(sequence)
    items = (("driver", binary, "update-driver", 0o700),
             ("manifest", manifest, "target-manifest-v2", 0o600),
             ("signature", signature, "target-manifest-v2.sig", 0o600))
    result = {}
    for key, source, destination, mode in items:
        path, digest = lab.put_file(root, record, plan, node, source, destination, mode=mode)
        result[key] = {"guest_path": path, "sha256": digest}
    return result


def driver(root, record, plan, node, intent, mode, label):
    argv = [intent["assets"]["driver"]["guest_path"], "--nonce", record["nonce"],
            "--manifest", intent["assets"]["manifest"]["guest_path"],
            "--signature", intent["assets"]["signature"]["guest_path"],
            "--request-id", intent["request_id"], "--mode", mode]
    code = None
    problem = None
    try:
        result = lab.guarded_script(root, record, plan, node, shlex.join(argv), timeout=180)
        stdout, stderr, code = result.stdout, result.stderr, result.returncode
    except subprocess.CalledProcessError as exc:
        stdout, stderr, code = exc.stdout or "", exc.stderr or "", exc.returncode
    except subprocess.TimeoutExpired as exc:
        stdout, stderr = exc.stdout or b"", exc.stderr or b""
        problem = "transport-timeout-outcome-unknown"
    stdout = stdout.encode() if isinstance(stdout, str) else stdout
    stderr = stderr.encode() if isinstance(stderr, str) else stderr
    oversized = len(stdout) > 131072 or len(stderr) > 131072
    saved = {"stdout": save(root, node, label + ".stdout.jsonl", stdout[:131072]),
             "stderr": save(root, node, label + ".stderr.txt", stderr[:131072])}
    outcome = {"request_id": intent["request_id"], "mode": mode, "exit_code": code,
               "error": problem, "output_truncated": oversized, "evidence": saved, "time": now()}
    save(root, node, label + ".result.json", (json.dumps(outcome, sort_keys=True) + "\n").encode())
    print(json.dumps({"action": "driver-result", "node": node, **outcome}), flush=True)
    return code == 0 and not oversized and problem is None


def validate_preview(root, record, plan, node, intent):
    outcome = json.loads(read_private(root / "evidence" / node / "update-preview.result.json"))
    if (outcome.get("request_id") != intent["request_id"] or outcome.get("mode") != "preview"
            or outcome.get("exit_code") != 0 or outcome.get("error") is not None
            or outcome.get("output_truncated") is not False):
        raise ValueError("saved native preview is not confirmed")
    raw = read_private(root / "evidence" / node / "update-preview.stdout.jsonl")
    if hashlib.sha256(raw).hexdigest() != outcome["evidence"]["stdout"]["sha256"]:
        raise ValueError("saved preview bytes changed")
    events = [json.loads(line) for line in raw.splitlines() if line.strip()]
    if len(events) != 1:
        raise ValueError("preview must have exactly one reviewed event")
    event = events[0]
    request = event.get("request", {})
    if (event.get("schema") != "celikpanel-release-recovery-update/v1"
            or event.get("event") != "reviewed" or event.get("cell_id") != record["cell_id"]
            or event.get("node") != node or event.get("request_id") != intent["request_id"]
            or event.get("manifest_sha256") != intent["assets"]["manifest"]["sha256"]
            or event.get("target_version") != "v0.1.0-alpha." + str(intent["sequence"])
            or request.get("request_id") != intent["request_id"]
            or request.get("target_version") != event["target_version"]
            or request.get("target_sequence") != str(intent["sequence"])
            or request.get("target_commit") != event.get("target_commit")
            or request.get("target_archive_sha256") != event.get("target_archive_sha256")
            or request.get("expected_current_version") != "v0.1.0-alpha.75"
            or request.get("expected_current_commit") != "5aa03fd5b6775b21834ff7b1ce0695d92f50ae93"):
        raise ValueError("saved preview identity or signed target tuple differs")
    validate_intent(intent, record, plan, node, intent["sequence"])


def prepare(root, record, plan, node, sequence, binary):
    evidence = root / "evidence" / node
    intent_path = evidence / "update-intent.json"
    if intent_path.exists() or intent_path.is_symlink():
        raise ValueError("an update intent already exists; never prepare a second request")
    seed_raw = read_private(evidence / "seed.stdout.jsonl")
    seed = validate_seed(seed_raw, record, node)
    baseline_raw = read_private(root / ("baseline-" + node + "-baseline-install-result.json"))
    validate_baseline(json.loads(baseline_raw))
    baseline_label = "before-update"
    if not (evidence / (baseline_label + ".json")).exists():
        exercise.capture(root, record, plan, node, baseline_label, now())
    before = json.loads(read_private(evidence / (baseline_label + ".json"), 4194304))
    if before.get("schema") != "celikpanel/release-recovery-observation/v1" or before.get("status") != "observed" or before.get("identity") != identity(record, plan, node):
        raise ValueError("baseline observation identity is not confirmed")
    assets = stage(root, record, plan, node, sequence, binary)
    intent = {"schema": SCHEMA, "identity": identity(record, plan, node),
              "request_id": secrets.token_hex(16), "sequence": sequence, "started_at": now(),
              "assets": assets, "seed_sha256": hashlib.sha256(seed_raw).hexdigest(),
              "seed_generation": seed["generation"], "baseline_sha256": hashlib.sha256(baseline_raw).hexdigest()}
    save(root, node, "update-intent.json", (json.dumps(intent, sort_keys=True) + "\n").encode())
    print(json.dumps({"action": "intent-persisted", "node": node, "request_id": intent["request_id"], "sequence": sequence}), flush=True)
    if not driver(root, record, plan, node, intent, "preview", "update-preview"):
        raise ValueError("preview unconfirmed; no update start was invoked")
    validate_preview(root, record, plan, node, intent)
    return intent


def native(root, record, plan, node, intent, label):
    unit = "celikpanel-self-update-" + intent["request_id"] + ".service"
    body = "python3 -I - <<'CP_NATIVE_UPDATE_OBSERVE'\n" + r'''
import json,subprocess
from pathlib import Path
units=[UNIT_LITERAL,'celikpanel-release-recovery.service','celikpanel-release-recovery.timer','celikpanel-agent.service','celikpanel-panel.service']
result={}
for unit in units:
    observed=subprocess.run(['systemctl','show',unit,'-p','Id','-p','LoadState','-p','ActiveState','-p','SubState','-p','Result','-p','ExecMainStatus','-p','MainPID','-p','OnFailure'],capture_output=True,text=True,timeout=15)
    result[unit]=dict(line.split('=',1) for line in observed.stdout.splitlines() if '=' in line)
journal=subprocess.run(['journalctl','--no-pager','--output=short-iso','-n','160','--since',SINCE_LITERAL,'-u',UNIT_LITERAL,'-u','celikpanel-release-recovery.service'],capture_output=True,text=True,timeout=20)
result['journal']={'exit_code':journal.returncode,'text':journal.stdout[:65536],'truncated':len(journal.stdout)>65536}
p=Path('/var/lib/celikpanel-release-transaction/active')
result['active_transaction']=None
if p.exists():
    result['active_transaction']={line.split('=',1)[0]:line.split('=',1)[1] for line in p.read_text()[:16384].splitlines() if line.startswith(('operation=','snapshot='))}
print(json.dumps(result,sort_keys=True))
'''.replace("UNIT_LITERAL", repr(unit)).replace("SINCE_LITERAL", repr(intent["started_at"])) + "\nCP_NATIVE_UPDATE_OBSERVE\n"
    result = lab.guarded_script(root, record, plan, node, body, timeout=120)
    saved = save(root, node, label + ".native.json", result.stdout.encode())
    value = json.loads(result.stdout)
    summary = {key: details for key, details in value.items() if key != "journal"}
    journal = value["journal"]
    summary["journal"] = {"exit_code": journal["exit_code"], "truncated": journal["truncated"],
                          "bytes": len(journal["text"].encode("utf-8"))}
    print(json.dumps({"action": "native-observed", "node": node, "request_id": intent["request_id"],
                      "evidence": saved, "observation": summary}, sort_keys=True), flush=True)
    return value


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--work-root", required=True)
    parser.add_argument("--node", choices=("debian13", "arch"), required=True)
    parser.add_argument("--sequence", type=int, choices=(79, 80), required=True)
    parser.add_argument("--mode", choices=("prepare", "start", "status", "observe"), required=True)
    parser.add_argument("--execute", action="store_true")
    parser.add_argument("--binary", type=Path, default=REPOSITORY / ".tmp-release-recovery-update")
    args = parser.parse_args()
    root = lab.checked_root(args.work_root)
    record, plan = lab.load(root)
    lab.process_guard(plan["nodes"][args.node])
    evidence = root / "evidence" / args.node
    intent_path = evidence / "update-intent.json"
    if args.mode in ("prepare", "start"):
        if not args.execute:
            raise ValueError("explicit --execute is required for the disposable guest update")
        attempt = evidence / "update-start-attempt.json"
        if attempt.exists() or attempt.is_symlink():
            raise ValueError("a start was already attempted; only status or observe are allowed")
        if args.mode == "prepare" or not intent_path.exists():
            intent = prepare(root, record, plan, args.node, args.sequence, args.binary)
        else:
            intent = validate_intent(json.loads(read_private(intent_path)), record, plan, args.node, args.sequence)
            validate_preview(root, record, plan, args.node, intent)
        if args.mode == "prepare":
            print(json.dumps({"action": "prepared-not-started", "node": args.node, "request_id": intent["request_id"]}), flush=True)
            return
        save(root, args.node, "update-start-attempt.json", (json.dumps({"request_id": intent["request_id"], "started_at": now()}) + "\n").encode())
        confirmed = driver(root, record, plan, args.node, intent, "start", "update-start")
        native(root, record, plan, args.node, intent, "update-start")
        if not confirmed:
            raise ValueError("update start outcome unconfirmed; only query this persisted request ID")
    else:
        intent = validate_intent(json.loads(read_private(intent_path)), record, plan, args.node, args.sequence)
        label = "update-" + args.mode + "-" + dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
        if args.mode == "status":
            driver(root, record, plan, args.node, intent, "status", label)
        native(root, record, plan, args.node, intent, label)
        exercise.capture(root, record, plan, args.node, label, intent["started_at"], intent["request_id"])


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, subprocess.SubprocessError, lab.fixture.FixtureError) as exc:
        print(json.dumps({"action": "stopped", "error_type": type(exc).__name__,
                          "detail": str(exc) if isinstance(exc, ValueError) else "fixture operation unavailable; retain exact intent and evidence"}), file=sys.stderr)
        sys.exit(1)
