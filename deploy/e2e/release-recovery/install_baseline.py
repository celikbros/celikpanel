#!/usr/bin/env python3
"""Install the genuine signed Alpha75 baseline only inside registered fresh QEMU guests.

There is no arbitrary SSH target or installed-update mode. The lab controller
checks the QEMU process, loopback transport, host key, nonce and guest DMI UUID.
Administrator credentials are generated inside the guest, never returned, and
kept in a root-only fixture file. Installer output remains in a private log.
"""
from __future__ import annotations

import argparse
import base64
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import stat
import subprocess
import sys

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("release_baseline_lab", HERE / "lab.py")
lab = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = lab
SPEC.loader.exec_module(lab)

VERSION = "v0.1.0-alpha.75"
COMMIT = "5aa03fd5b6775b21834ff7b1ce0695d92f50ae93"
BOOTSTRAP_SHA256 = "82b2674c103e347df471ec7e3f2f091d50006c957c54ac946b7c021ed19e041d"
GUEST_ROOT = "/root/celikpanel-release-recovery-lab"
UNIT = "celikpanel-lab-alpha75-install.service"
FRESH_PATHS = (
    "/opt/celikpanel", "/etc/celikpanel", "/var/lib/celikpanel",
    "/var/lib/celikpanel-agent-private", "/var/lib/celikpanel-release-state",
    "/var/lib/celikpanel-release-transaction",
    "/etc/systemd/system/celikpanel-agent.service",
    "/etc/systemd/system/celikpanel-panel.service",
)


def historical_bootstrap(repository):
    raw = subprocess.run(["git", "-C", str(repository), "show", COMMIT + ":download-portal/get.sh"],
                         check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout
    if hashlib.sha256(raw).hexdigest() != BOOTSTRAP_SHA256:
        raise ValueError("historical bootstrap does not match the pinned released bytes")
    if b"bootstrap_release_sequence=75\n" not in raw or b"bootstrap_release_version=" + VERSION.encode() + b"\n" not in raw:
        raise ValueError("historical bootstrap release selection changed")
    return raw


def private_file(path, contents):
    try:
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    except FileExistsError:
        info = path.lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1 or path.read_bytes() != contents:
            raise ValueError("existing baseline artifact changed")
        return
    with os.fdopen(fd, "wb") as stream:
        stream.write(contents)
        stream.flush()
        os.fsync(stream.fileno())


def fresh_check():
    return "python3 -I - <<'CP_BASELINE_FRESH'\nfrom pathlib import Path\n" + \
        "for path in " + repr(FRESH_PATHS) + ":\n" + \
        "    p=Path(path)\n    if p.exists() or p.is_symlink(): raise RuntimeError('guest already contains CelikPanel state')\n" + \
        "CP_BASELINE_FRESH\n"


def guest_driver(record, node_name, node):
    guard = lab.guest_guard(record, node_name, node)
    return guard + "\n" + r'''
import datetime,fcntl,hashlib,secrets,subprocess,time
ROOT=Path("/root/celikpanel-release-recovery-lab")
VERSION="v0.1.0-alpha.75"
EXPECTED_BOOTSTRAP="82b2674c103e347df471ec7e3f2f091d50006c957c54ac946b7c021ed19e041d"
os.umask(0o077)
info=ROOT.lstat()
if not (stat.S_ISDIR(info.st_mode) and info.st_uid==0 and stat.S_IMODE(info.st_mode)==0o700): raise RuntimeError("unsafe private lab directory")
lock=os.open(ROOT/"baseline-install.lock",os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
for path in FRESH_PATHS_LITERAL:
    p=Path(path)
    if p.exists() or p.is_symlink(): raise RuntimeError("guest already contains CelikPanel state")
bootstrap=ROOT/"get-alpha75.sh"
if hashlib.sha256(bootstrap.read_bytes()).hexdigest()!=EXPECTED_BOOTSTRAP: raise RuntimeError("bootstrap changed")
credentials={"username":"labadmin","email":"labadmin@example.invalid","password":secrets.token_urlsafe(32)}
raw=json.dumps(credentials,separators=(",",":")).encode()+b"\n"
for name in ("admin-install.json","admin-login.json"):
    fd=os.open(ROOT/name,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    with os.fdopen(fd,"wb") as target:
        target.write(raw)
        target.flush()
        os.fsync(target.fileno())
del raw,credentials
log_path=ROOT/"baseline-install.log"
started=datetime.datetime.now(datetime.timezone.utc).isoformat()
log_fd=os.open(log_path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
environment={"PATH":"/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin","HOME":"/root","LANG":"C.UTF-8",
             "CELIKPANEL_ADMIN_CREDENTIALS_FILE":str(ROOT/"admin-install.json")}
code=None
problem=None
with os.fdopen(log_fd,"wb") as output:
    try:
        result=subprocess.run(["/bin/bash",str(bootstrap),"--install","--version",VERSION],
                              stdin=subprocess.DEVNULL,stdout=output,stderr=subprocess.STDOUT,
                              env=environment,timeout=2400)
        code=result.returncode
    except Exception as exc:
        problem=type(exc).__name__
    output.flush()
    os.fsync(output.fileno())
def digest(path):
    p=Path(path)
    if not p.is_file() or p.is_symlink():
        return None
    sha=hashlib.sha256()
    with p.open("rb") as stream:
        for block in iter(lambda:stream.read(1<<20),b""):
            sha.update(block)
    return sha.hexdigest()
services={}
for name in ("agent","panel"):
    observed=subprocess.run(["systemctl","show","celikpanel-"+name+".service",
                             "-p","ActiveState","-p","SubState","-p","MainPID"],
                             stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True,timeout=20)
    services[name]=dict(line.split("=",1) for line in observed.stdout.splitlines() if "=" in line)
installed={name:digest("/opt/celikpanel/bin/"+name) for name in ("agent","panel")}
running={}
for name in services:
    pid=services[name].get("MainPID","0")
    running[name]=digest("/proc/"+pid+"/exe") if pid.isdigit() and pid!="0" else None
    if pid.isdigit() and pid!="0":
        try:
            sha=hashlib.sha256()
            with open("/proc/"+pid+"/exe","rb") as source:
                for block in iter(lambda:source.read(1<<20),b""):
                    sha.update(block)
            running[name]=sha.hexdigest()
        except OSError:
            running[name]=None
request=subprocess.run(["curl","--silent","--insecure","--max-time","15","--output","/dev/null",
                        "--write-out","%{http_code}","https://127.0.0.1:2083/login"],
                       stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True,timeout=20)
proof={"schema":"celikpanel/release-baseline-install-result/v1","version":VERSION,"started_at":started,
       "finished_at":datetime.datetime.now(datetime.timezone.utc).isoformat(),"exit_code":code,"error_type":problem,
       "bootstrap_sha256":EXPECTED_BOOTSTRAP,"installer_log_sha256":digest(log_path),
       "installer_log_bytes":log_path.stat().st_size,"installed_artifacts":installed,
       "running_artifacts":running,"services":services,"https_curl_exit":request.returncode,
       "https_http_code":request.stdout.strip(),"https_trust_validation":"not-claimed-self-signed-bootstrap",
       "credentials":"guest-root-only"}
fd=os.open(ROOT/"baseline-install-result.json",os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
with os.fdopen(fd,"w") as stream:
    json.dump(proof,stream,sort_keys=True)
    stream.write("\n")
    stream.flush()
    os.fsync(stream.fileno())
sys_exit=0 if code==0 else 1
raise SystemExit(sys_exit)
'''.replace("FRESH_PATHS_LITERAL", repr(FRESH_PATHS))


def start(root, record, plan, node_name, execute):
    if not execute:
        return {"node": node_name, "action": "fresh-signed-install", "version": VERSION, "execute": False}
    lab.guarded_script(root, record, plan, node_name, fresh_check())
    bootstrap = root / "get-alpha75.sh"
    private_file(bootstrap, historical_bootstrap(HERE.parents[2]))
    driver = root / ("baseline-driver-" + node_name + ".py")
    private_file(driver, guest_driver(record, node_name, plan["nodes"][node_name]).encode())
    lab.put_file(root, record, plan, node_name, bootstrap, "get-alpha75.sh")
    lab.put_file(root, record, plan, node_name, driver, "baseline-install-driver.py")
    body = fresh_check() + (
        "systemd-run --quiet --no-block --unit=" + UNIT +
        " --property=Type=oneshot --property=TimeoutStartSec=2500 --property=UMask=0077"
        " /usr/bin/python3 -I " + GUEST_ROOT + "/baseline-install-driver.py\n"
    )
    lab.guarded_script(root, record, plan, node_name, body, timeout=60)
    return {"node": node_name, "action": "started", "version": VERSION, "unit": UNIT,
            "bootstrap_sha256": BOOTSTRAP_SHA256, "credentials": "guest-root-only"}


def status(root, record, plan, node_name):
    body = """python3 -I - <<'CP_BASELINE_STATUS'
import json,stat,subprocess
from pathlib import Path
p=Path('/root/celikpanel-release-recovery-lab/baseline-install-result.json')
service=subprocess.run(['systemctl','show','celikpanel-lab-alpha75-install.service','-p','ActiveState','-p','SubState','-p','Result','-p','ExecMainStatus'],capture_output=True,text=True,timeout=20)
result={'unit':dict(line.split('=',1) for line in service.stdout.splitlines() if '=' in line)}
if p.exists():
    info=p.lstat()
    if not (stat.S_ISREG(info.st_mode) and info.st_uid==0 and stat.S_IMODE(info.st_mode)==0o600 and info.st_nlink==1 and info.st_size<16384): raise RuntimeError("unsafe baseline result")
    result['installation']=json.loads(p.read_text())
else:
    result['installation']=None
print(json.dumps(result,sort_keys=True))
CP_BASELINE_STATUS
"""
    result = lab.guarded_script(root, record, plan, node_name, body, timeout=45)
    data = json.loads(result.stdout)
    data["node"] = node_name
    return data



def collect(root, record, plan, node_name):
    """Copy only private installation log/result evidence; never credentials."""
    observed = status(root, record, plan, node_name)
    if observed["installation"] is None:
        raise ValueError("installation has no terminal result; private guest log is retained")
    result = {"node": node_name, "artifacts": {}}
    for name in ("baseline-install-result.json", "baseline-install.log"):
        body = "python3 -I - <<'CP_BASELINE_COLLECT'\n" + r"""
import base64,stat
from pathlib import Path
p=Path("/root/celikpanel-release-recovery-lab")/NAME_LITERAL
info=p.lstat()
if not (stat.S_ISREG(info.st_mode) and info.st_uid==0 and stat.S_IMODE(info.st_mode)==0o600 and info.st_nlink==1 and info.st_size<=16777216):
    raise RuntimeError("unsafe private baseline evidence")
print(base64.b64encode(p.read_bytes()).decode())
""" .replace("NAME_LITERAL", repr(name)) + "\nCP_BASELINE_COLLECT\n"
        fetched = lab.guarded_script(root, record, plan, node_name, body, timeout=60)
        raw = base64.b64decode(fetched.stdout.strip(), validate=True)
        digest = hashlib.sha256(raw).hexdigest()
        if name.endswith(".log") and digest != observed["installation"]["installer_log_sha256"]:
            raise ValueError("terminal installer log changed")
        destination = root / ("baseline-" + node_name + "-" + name)
        private_file(destination, raw)
        result["artifacts"][name] = {"path": str(destination), "sha256": digest, "bytes": len(raw)}
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("start", "status", "collect"))
    parser.add_argument("--work-root", required=True)
    parser.add_argument("--node", choices=("debian13", "arch", "all"), default="all")
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args()
    root = lab.checked_root(args.work_root)
    record, plan = lab.load(root)
    names = list(plan["nodes"]) if args.node == "all" else [args.node]
    for name in names:
        result = start(root, record, plan, name, args.execute) if args.command == "start" else (collect(root, record, plan, name) if args.command == "collect" else status(root, record, plan, name))
        print(json.dumps(result, sort_keys=True), flush=True)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, subprocess.SubprocessError, lab.fixture.FixtureError) as exc:
        print("baseline installation refused: " + type(exc).__name__, file=sys.stderr)
        sys.exit(1)
