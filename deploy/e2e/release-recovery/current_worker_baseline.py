#!/usr/bin/env python3
"""Fresh unpublished current-feature baseline for actual worker recovery drills.

This never upgrades a pre-existing panel. The registered QEMU, SSH host key,
cloud-init nonce and DMI UUID are checked before touching its fresh guest.
The unchanged installer and one-time enrollment helper own all product state.
The enrolled key is an isolated fixture key, not published-release authority.
"""
from __future__ import annotations

import argparse
import base64
import hashlib
import importlib.util
import inspect
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import tarfile
import sys

HERE = Path(__file__).resolve().parent


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    value = importlib.util.module_from_spec(spec)
    sys.modules[name] = value
    spec.loader.exec_module(value)
    return value


lab = module("current_worker_baseline_lab", "lab.py")
archive_tools = module("current_worker_baseline_archive", "candidate_archive.py")
VERSION = "v0.1.0-alpha.81"
SEQUENCE = 81
RELEASE_POLICY = {"version": VERSION, "current": SEQUENCE, "previous": 80, "previous_version": "v0.1.0-alpha.80"}
COMMIT = "45dfc265bfd7997e9a0e0d39b0ebf60a57f58c00"
SCHEMA = "celikpanel/current-worker-baseline-intent/v1"
RESULT_SCHEMA = "celikpanel/current-worker-baseline-result/v1"
PRIVATE = "/root/celikpanel-release-recovery-lab"
PUBLIC_KEY = PRIVATE + "/worker-origin-public.pem"
UNIT = "celikpanel-lab-current-worker-baseline.service"
INTENT = "current-worker-baseline-intent.json"
DRIVER = "current-worker-baseline-driver.py"
ARCHIVE = "current-worker-baseline.tar.gz"
RESULT = "current-worker-baseline-result.json"
LOG = "current-worker-baseline.log"
FRESH_PATHS = (
    "/opt/celikpanel", "/etc/celikpanel", "/var/lib/celikpanel",
    "/var/lib/celikpanel-agent-private", "/var/lib/celikpanel-release-state",
    "/var/lib/celikpanel-release-transaction", "/var/backups/celikpanel",
    "/etc/systemd/system/celikpanel-agent.service",
    "/etc/systemd/system/celikpanel-panel.service",
    "/usr/libexec/celikpanel",
)


def encoded(value):
    return (json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n").encode()


def save_once(path, raw):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


def identity(record, node_name, node):
    return {"schema": lab.SCHEMA, "nonce": record["nonce"], "cell_id": record["cell_id"],
            "node": node_name, "vm_uuid": node["qemu_command"][node["qemu_command"].index("-uuid") + 1]}


def intent_path(root, node_name):
    if node_name not in ("debian13", "arch"):
        raise ValueError("unsupported registered guest")
    return root / ("current-worker-baseline-" + node_name + "-intent.json")


def load_intent(root, record, plan, node_name):
    value = lab.read_private(intent_path(root, node_name))
    if (value.get("schema") != SCHEMA or value.get("identity") != identity(record, node_name, plan["nodes"][node_name])
            or value.get("version") != VERSION or value.get("sequence") != SEQUENCE
            or value.get("public_key_path") != PUBLIC_KEY):
        raise ValueError("current baseline intent identity differs")
    return value


def extract_current_archive(source, packed, candidate, archive):
    """Preserve verified package metadata despite the private fixture umask.

    Only release-package 0755 directories and 0644/0755 regular files are
    admitted. The enclosing fixture directory remains 0700. No links or
    unlisted/implicit directories may acquire authority through extraction.
    """
    source.mkdir(mode=0o700)
    directories = {}
    with tarfile.open(packed, "r:gz") as bundle:
        for member in bundle:
            relative = archive.member_path(member.name, candidate["root_name"])
            path = source / relative
            if member.isdir():
                if member.mode != 0o755:
                    raise ValueError("baseline package directory mode differs")
                if relative:
                    path.mkdir(mode=0o700, parents=True, exist_ok=True)
                directories[relative] = member.mode
                continue
            if not member.isfile() or member.mode not in (0o644, 0o755):
                raise ValueError("unsafe baseline package file metadata")
            path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            with os.fdopen(fd, "wb") as target, bundle.extractfile(member) as origin:
                sha = hashlib.sha256()
                for block in iter(lambda: origin.read(1048576), b""):
                    target.write(block)
                    sha.update(block)
                if sha.hexdigest() != candidate["files"].get(relative):
                    raise ValueError("extracted baseline file digest differs")
                target.flush()
                os.fchmod(target.fileno(), member.mode)
                os.fsync(target.fileno())
    actual_directories = {""} | {path.relative_to(source).as_posix() for path in source.rglob("*") if path.is_dir()}
    if actual_directories != set(directories):
        raise ValueError("baseline archive directory inventory differs")
    actual_files = {path.relative_to(source).as_posix() for path in source.rglob("*") if path.is_file()}
    if actual_files != set(candidate["files"]):
        raise ValueError("baseline archive file inventory differs")
    for relative in sorted(directories, key=lambda value: value.count("/"), reverse=True):
        directory = source / relative
        directory.chmod(directories[relative])
        if stat.S_IMODE(directory.stat().st_mode) != directories[relative]:
            raise ValueError("baseline package directory metadata was not preserved")
        descriptor = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)


def verify_foundation_identity(raw, version, sequence, commit):
    """Read-only identity check after the real installer publishes foundation."""
    fields=("format","protocol","sequence","release-version","release-commit","runner-sha256","service-sha256","timer-sha256","start-guard-sha256","agent-dropin-sha256","panel-dropin-sha256","protocol-sha256")
    try:lines=raw.decode("ascii").splitlines(keepends=True)
    except UnicodeDecodeError as error:raise ValueError("installed foundation is not ASCII") from error
    if len(lines)!=len(fields) or any(not line.startswith(key+"=") or not line.endswith("\n") or "\r" in line for key,line in zip(fields,lines)):
        raise ValueError("installed foundation is noncanonical")
    value={key:line[len(key)+1:-1] for key,line in zip(fields,lines)}
    expected={"format":"celikpanel-release-recovery-foundation-v1","protocol":"1","sequence":str(sequence),"release-version":version,"release-commit":commit}
    if any(value[key]!=wanted for key,wanted in expected.items()) or any(len(value[key])!=64 or any(char not in "0123456789abcdef" for char in value[key]) for key in fields[5:]):
        raise ValueError("installed foundation differs from the verified baseline release")
    return {"version":version,"sequence":sequence,"commit":commit,"sha256":hashlib.sha256(raw).hexdigest()}


def guest_driver(record, node_name, node, intent_sha256):
    return lab.guest_guard(record, node_name, node) + "\n" + inspect.getsource(extract_current_archive) + "\n" + inspect.getsource(verify_foundation_identity) + "\n" + GUEST_DRIVER.replace(
        "INTENT_SHA256_LITERAL", repr(intent_sha256)).replace("FRESH_PATHS_LITERAL", repr(FRESH_PATHS))


GUEST_DRIVER = r'''import datetime,fcntl,hashlib,importlib.util,secrets,subprocess,sys,tarfile,time
ROOT=Path("/root/celikpanel-release-recovery-lab")
EXPECTED_INTENT=INTENT_SHA256_LITERAL
os.umask(0o077)

def private_read(path,limit=4*1024*1024,modes=(0o600,)):
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    with os.fdopen(fd,"rb") as stream:
        before=os.fstat(stream.fileno())
        if not (stat.S_ISREG(before.st_mode) and before.st_uid==0 and before.st_gid==0 and before.st_nlink==1 and stat.S_IMODE(before.st_mode) in modes and before.st_size<=limit):
            raise ValueError("unsafe private baseline input")
        raw=stream.read(limit+1);after=os.fstat(stream.fileno())
    if len(raw)>limit or (before.st_dev,before.st_ino,before.st_size,before.st_mtime_ns,before.st_ctime_ns)!=(after.st_dev,after.st_ino,after.st_size,after.st_mtime_ns,after.st_ctime_ns):
        raise ValueError("private baseline input changed")
    return raw

def save(path,value):
    fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
    with os.fdopen(fd,"w") as stream:
        json.dump(value,stream,sort_keys=True);stream.write("\n");stream.flush();os.fsync(stream.fileno())
    fd=os.open(path.parent,os.O_RDONLY|os.O_DIRECTORY)
    try:os.fsync(fd)
    finally:os.close(fd)

def digest(path,proc=False):
    fd=os.open(path,os.O_RDONLY|os.O_CLOEXEC|(0 if proc else os.O_NOFOLLOW))
    with os.fdopen(fd,"rb") as stream:
        before=os.fstat(stream.fileno())
        if not stat.S_ISREG(before.st_mode) or before.st_size>128*1024*1024:raise ValueError("unbounded baseline artifact")
        sha=hashlib.sha256()
        for block in iter(lambda:stream.read(1048576),b""):sha.update(block)
        after=os.fstat(stream.fileno())
    if (before.st_dev,before.st_ino,before.st_size,before.st_mtime_ns,before.st_ctime_ns)!=(after.st_dev,after.st_ino,after.st_size,after.st_mtime_ns,after.st_ctime_ns):raise ValueError("baseline artifact changed")
    return sha.hexdigest()

for item in (ROOT,*ROOT.parents):
    info=item.lstat()
    if not (stat.S_ISDIR(info.st_mode) and info.st_uid==0 and info.st_gid==0 and not stat.S_IMODE(info.st_mode)&0o022):raise ValueError("unsafe private baseline ancestry")
if stat.S_IMODE(ROOT.stat().st_mode)!=0o700:raise ValueError("baseline root must be private")
raw=private_read(ROOT/"current-worker-baseline-intent.json")
if hashlib.sha256(raw).hexdigest()!=EXPECTED_INTENT:raise ValueError("baseline intent changed")
intent=json.loads(raw)
if intent.get("schema")!="celikpanel/current-worker-baseline-intent/v1" or intent.get("identity")!=expected or intent.get("version")!="v0.1.0-alpha.81" or intent.get("sequence")!=81:
    raise ValueError("baseline intent is not for this guest")
key=ROOT/"worker-origin-public.pem"
if intent.get("public_key_path")!=str(key) or hashlib.sha256(private_read(key,16384,(0o600,0o644))).hexdigest()!=intent["public_key_sha256"]:
    raise ValueError("isolated fixture public key changed")
lock=os.open(ROOT/"current-worker-baseline.started",os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB);os.fsync(lock)
for name in FRESH_PATHS_LITERAL:
    path=Path(name)
    if path.exists() or path.is_symlink():raise ValueError("fresh baseline refuses existing CelikPanel state")
log_fd=os.open(ROOT/"current-worker-baseline.log",os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
proof={"schema":"celikpanel/current-worker-baseline-result/v1","identity":expected,"version":intent["version"],"sequence":intent["sequence"],
       "source_commit":intent["candidate"]["commit"],"archive_sha256":intent["candidate"]["archive_sha256"],
       "intent_sha256":EXPECTED_INTENT,"started_at":datetime.datetime.now(datetime.timezone.utc).isoformat(),
       "provenance":"unpublished-local-build-with-isolated-fixture-trust-not-production-release-admission",
       "credentials":"guest-root-only","verified":False,"error_type":None,"install_exit":None,"enrollment_exit":None}
with os.fdopen(log_fd,"wb") as output:
    try:
        helper_path=ROOT/"candidate_archive.py"
        helper_raw=private_read(helper_path,524288)
        if hashlib.sha256(helper_raw).hexdigest()!=intent["archive_helper_sha256"]:raise ValueError("archive inspection helper changed")
        spec=importlib.util.spec_from_file_location("current_baseline_archive",helper_path)
        archive=importlib.util.module_from_spec(spec);sys.modules[spec.name]=archive;spec.loader.exec_module(archive)
        packed=ROOT/"current-worker-baseline.tar.gz"
        candidate=archive.inspect_archive(packed,intent["candidate"]["archive_sha256"],release_policy={"version":"v0.1.0-alpha.81","current":81,"previous":80,"previous_version":"v0.1.0-alpha.80"})
        if candidate!=intent["candidate"]:raise ValueError("baseline archive identity differs")
        source=ROOT/"current-worker-baseline-source"
        extract_current_archive(source,packed,candidate,archive)
        actual={str(path.relative_to(source)):digest(path) for path in source.rglob("*") if path.is_file()}
        if actual!=candidate["files"]:raise ValueError("extracted baseline inventory differs")
        credentials={"username":"labadmin","email":"labadmin@example.invalid","password":secrets.token_urlsafe(32)}
        for name in ("admin-install.json","admin-login.json"):save(ROOT/name,credentials)
        del credentials
        environment={"PATH":"/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin","HOME":"/root","LANG":"C.UTF-8", "CELIKPANEL_ADMIN_CREDENTIALS_FILE":str(ROOT/"admin-install.json")}
        completed=subprocess.run(["/bin/bash",str(source/"install.sh")],cwd=source,env=environment,stdin=subprocess.DEVNULL,stdout=output,stderr=subprocess.STDOUT,timeout=2400)
        proof["install_exit"]=completed.returncode
        if completed.returncode:raise ValueError("native baseline installer failed")
        if (ROOT/"admin-install.json").exists():raise ValueError("installer did not consume credential input")
        environment.pop("CELIKPANEL_ADMIN_CREDENTIALS_FILE")
        enrolled=subprocess.run(["/bin/bash",str(source/"deploy/enroll-signed-release-trust.sh"),"--sequence","81","--version",intent["version"],"--commit",candidate["commit"],"--public-key-file",str(key)],cwd=source,env=environment,stdin=subprocess.DEVNULL,stdout=output,stderr=subprocess.STDOUT,timeout=120)
        proof["enrollment_exit"]=enrolled.returncode
        if enrolled.returncode:raise ValueError("native baseline trust enrollment failed")
        floor=private_read(Path("/var/lib/celikpanel-release-state/sequence.floor"),512)
        if floor!=b"format=celikpanel-release-sequence-floor-v1\nsequence=81\nversion=v0.1.0-alpha.81\n":raise ValueError("enrolled release floor differs")
        trusted_key=private_read(Path("/etc/celikpanel/release-signing-ed25519.pem"),16384,(0o644,))
        if hashlib.sha256(trusted_key).hexdigest()!=intent["public_key_sha256"]:raise ValueError("enrolled fixture key differs")
        proof["sequence_floor_sha256"]=hashlib.sha256(floor).hexdigest();proof["public_key_sha256"]=hashlib.sha256(trusted_key).hexdigest()
        foundation=private_read(Path("/var/lib/celikpanel-release-state/recovery-foundation.v1"),2048)
        proof["foundation"]=verify_foundation_identity(foundation,intent["version"],intent["sequence"],candidate["commit"])
        services={};installed={};running={}
        for name in ("agent","panel"):
            def observe():
                value=subprocess.run(["systemctl","show","celikpanel-"+name+".service","-p","ActiveState","-p","SubState","-p","MainPID","-p","InvocationID"],capture_output=True,text=True,timeout=20,check=True)
                return dict(line.split("=",1) for line in value.stdout.splitlines() if "=" in line)
            services[name]=observe();pid=services[name].get("MainPID","0")
            if services[name].get("ActiveState")!="active" or services[name].get("SubState")!="running" or not pid.isdigit() or int(pid)<2:raise ValueError("baseline service not active")
            installed[name]=digest(Path("/opt/celikpanel/bin")/name)
            running[name]=digest(Path("/proc")/pid/"exe",proc=True)
            if observe()!=services[name]:raise ValueError("baseline service changed during observation")
            if installed[name]!=candidate["files"]["bin/"+name] or running[name]!=installed[name]:raise ValueError("running baseline artifact differs")
        proof.update({"services":services,"installed_artifacts":installed,"running_artifacts":running})
        request=subprocess.run(["curl","--silent","--insecure","--max-time","15","--output","/dev/null","--write-out","%{http_code}","https://127.0.0.1:2083/login"],capture_output=True,text=True,timeout=20)
        proof.update({"https_curl_exit":request.returncode,"https_http_code":request.stdout.strip(),"https_trust_validation":"not-claimed-self-signed-bootstrap"})
        if request.returncode or request.stdout.strip()!="200":raise ValueError("baseline login listener unavailable")
        proof["verified"]=True
    except Exception as exc:
        proof["error_type"]=type(exc).__name__
        output.write(("\nFixture verification failed: "+str(exc)+"\n").encode())
    output.flush();os.fsync(output.fileno())
proof.update({"finished_at":datetime.datetime.now(datetime.timezone.utc).isoformat(),"installer_log_sha256":digest(ROOT/"current-worker-baseline.log"),"installer_log_bytes":(ROOT/"current-worker-baseline.log").stat().st_size})
save(ROOT/"current-worker-baseline-result.json",proof)
raise SystemExit(0 if proof["verified"] else 1)
'''


def start(root, record, plan, node_name, execute, *, archive_path, archive_sha256,
          public_key_path=PUBLIC_KEY, public_key_sha256):
    """Seal one fresh baseline intent, stage it, and launch a bounded native unit."""
    intent_path(root, node_name)
    if public_key_path != PUBLIC_KEY or not archive_tools.SHA.fullmatch(public_key_sha256):
        raise ValueError("only the fixed isolated fixture public key is accepted")
    candidate = archive_tools.inspect_archive(Path(archive_path), archive_sha256, release_policy=RELEASE_POLICY)
    if candidate["version"] != VERSION or candidate["commit"] != COMMIT or "deploy/enroll-signed-release-trust.sh" not in candidate["files"]:
        raise ValueError("baseline must be the complete Alpha81 current-feature archive")
    source = archive_tools.verify_committed_source(candidate, HERE.parents[2])
    if not execute:
        return {"node": node_name, "action": "fresh-current-worker-baseline", "version": VERSION,
                "archive_sha256": archive_sha256, "source_commit": candidate["commit"], "execute": False}
    check = "python3 -I - <<'CP_FRESH_CURRENT'\nfrom pathlib import Path\nfor name in " + repr(FRESH_PATHS + (PRIVATE + "/" + INTENT, PRIVATE + "/current-worker-baseline.started")) + ":\n    p=Path(name)\n    if p.exists() or p.is_symlink():raise ValueError('fresh current baseline already has state')\nCP_FRESH_CURRENT\n"
    lab.guarded_script(root, record, plan, node_name, check)
    intent = {"schema": SCHEMA, "identity": identity(record, node_name, plan["nodes"][node_name]),
              "version": VERSION, "sequence": SEQUENCE, "candidate": candidate, "source": source,
              "public_key_path": public_key_path, "public_key_sha256": public_key_sha256,
              "archive_helper_sha256": hashlib.sha256((HERE / "candidate_archive.py").read_bytes()).hexdigest(),
              "provenance": "unpublished-local-build-with-isolated-fixture-trust-not-production-release-admission"}
    raw = encoded(intent)
    intent_file = intent_path(root, node_name)
    save_once(intent_file, raw)
    driver = root / ("current-worker-baseline-" + node_name + "-driver.py")
    save_once(driver, guest_driver(record, node_name, plan["nodes"][node_name], hashlib.sha256(raw).hexdigest()).encode())
    for path, basename in ((Path(archive_path), ARCHIVE), (HERE / "candidate_archive.py", "candidate_archive.py"),
                           (intent_file, INTENT), (driver, DRIVER)):
        lab.put_file(root, record, plan, node_name, path, basename)
    body = "systemd-run --quiet --no-block --unit=" + UNIT + " --property=Type=oneshot --property=TimeoutStartSec=2600 --property=UMask=0077 /usr/bin/python3 -I " + PRIVATE + "/" + DRIVER + "\n"
    lab.guarded_script(root, record, plan, node_name, body, timeout=60)
    return {"node": node_name, "action": "started", "version": VERSION, "unit": UNIT,
            "source_commit": candidate["commit"], "archive_sha256": archive_sha256, "credentials": "guest-root-only"}


def status(root, record, plan, node_name):
    """Read status for the sealed unit; never restart or create another install."""
    intent = load_intent(root, record, plan, node_name)
    body = "python3 -I - <<'CP_CURRENT_STATUS'\n" + r'''
import json,stat,subprocess
from pathlib import Path
p=Path("/root/celikpanel-release-recovery-lab/current-worker-baseline-result.json")
service=subprocess.run(["systemctl","show","celikpanel-lab-current-worker-baseline.service","-p","ActiveState","-p","SubState","-p","Result","-p","ExecMainStatus"],capture_output=True,text=True,timeout=20,check=True)
result={"unit":dict(line.split("=",1) for line in service.stdout.splitlines() if "=" in line),"installation":None}
if p.exists() or p.is_symlink():
    info=p.lstat()
    if not (stat.S_ISREG(info.st_mode) and info.st_uid==0 and info.st_gid==0 and stat.S_IMODE(info.st_mode)==0o600 and info.st_nlink==1 and info.st_size<16384):raise ValueError("unsafe current baseline result")
    result["installation"]=json.loads(p.read_text())
print(json.dumps(result,sort_keys=True))
''' + "\nCP_CURRENT_STATUS\n"
    result = lab.guarded_script(root, record, plan, node_name, body, timeout=45)
    data = json.loads(result.stdout)
    data["node"] = node_name
    observed = data.get("installation")
    if observed is not None and (observed.get("schema") != RESULT_SCHEMA or observed.get("identity") != intent["identity"]
            or observed.get("archive_sha256") != intent["candidate"]["archive_sha256"]
            or observed.get("intent_sha256") != hashlib.sha256(encoded(intent)).hexdigest()):
        raise ValueError("baseline result is not for the sealed intent")
    return data


poll = status


def collect(root, record, plan, node_name):
    """Retain private result/log evidence; never copy or print credentials."""
    observed = status(root, record, plan, node_name)
    if observed["installation"] is None:
        raise ValueError("current baseline has no terminal evidence yet")
    artifacts = {}
    for name in (RESULT, LOG):
        body = "python3 -I - <<'CP_CURRENT_COLLECT'\n" + r'''
import base64,os,stat
from pathlib import Path
p=Path("/root/celikpanel-release-recovery-lab")/NAME_LITERAL
fd=os.open(p,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
with os.fdopen(fd,"rb") as source:
    info=os.fstat(source.fileno())
    if not (stat.S_ISREG(info.st_mode) and info.st_uid==0 and info.st_gid==0 and stat.S_IMODE(info.st_mode)==0o600 and info.st_nlink==1 and info.st_size<=16777216):raise ValueError("unsafe private baseline evidence")
    raw=source.read(16777217)
print(base64.b64encode(raw).decode())
'''.replace("NAME_LITERAL", repr(name)) + "\nCP_CURRENT_COLLECT\n"
        fetched = lab.guarded_script(root, record, plan, node_name, body, timeout=60)
        raw = base64.b64decode(fetched.stdout.strip(), validate=True)
        digest = hashlib.sha256(raw).hexdigest()
        if name == LOG and digest != observed["installation"]["installer_log_sha256"]:
            raise ValueError("terminal baseline log changed")
        destination = root / ("current-worker-baseline-" + node_name + "-" + name)
        if destination.exists():
            info = destination.lstat()
            if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1 or destination.read_bytes() != raw:
                raise ValueError("previous baseline evidence differs")
        else:
            save_once(destination, raw)
        artifacts[name] = {"path": str(destination), "sha256": digest, "bytes": len(raw)}
    return {"node": node_name, "artifacts": artifacts}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("start", "status", "collect"))
    parser.add_argument("--work-root", required=True)
    parser.add_argument("--node", choices=("debian13", "arch"), default="debian13")
    parser.add_argument("--archive")
    parser.add_argument("--archive-sha256")
    parser.add_argument("--public-key-sha256")
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args()
    root = lab.checked_root(args.work_root)
    record, plan = lab.load(root)
    if args.command == "start":
        if not all((args.archive, args.archive_sha256, args.public_key_sha256)):
            parser.error("start requires exact archive and fixture public key hashes")
        result = start(root, record, plan, args.node, args.execute, archive_path=args.archive,
                       archive_sha256=args.archive_sha256, public_key_sha256=args.public_key_sha256)
    else:
        result = (status if args.command == "status" else collect)(root, record, plan, args.node)
    print(json.dumps(result, sort_keys=True), flush=True)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, subprocess.SubprocessError, lab.fixture.FixtureError) as exc:
        print("current baseline refused: " + type(exc).__name__, file=sys.stderr)
        sys.exit(1)
