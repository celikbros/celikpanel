#!/usr/bin/env python3
"""Disposable QEMU lab; never accepts an existing server or SSH destination.

Geçici QEMU laboratuvarı; mevcut sunucu veya SSH hedefi kabul etmez.
"""

from __future__ import annotations

import argparse
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
SPEC = importlib.util.spec_from_file_location("release_lab_fixture", HERE.parent / "dns-kill-matrix/fixture.py")
fixture = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = fixture
SPEC.loader.exec_module(fixture)
SCHEMA = "celikpanel-release-recovery-lab/v1"
MARKER = "/etc/celikpanel-release-recovery-lab"


def run(argv, **kwargs):
    return subprocess.run(argv, check=True, **kwargs)


def checked_root(value):
    root = Path(value).absolute()
    if root.parent != Path("/var/tmp") or not re.fullmatch(r"cp-release-drill-[a-z0-9-]{1,50}", root.name):
        raise ValueError("lab root must be a dedicated /var/tmp/cp-release-drill-NAME")
    if root.resolve() != root or root.is_symlink():
        raise ValueError("lab root must not traverse a symlink")
    fixture.require_linux_qemu_host()
    return root


def read_private(path):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1:
        raise ValueError("invalid private lab record")
    return json.loads(path.read_text())


def load(root):
    fixture.validate_work_root(root)
    record = read_private(root / "lab.json")
    if record.get("schema") != SCHEMA or not re.fullmatch(r"[0-9a-f]{64}", record.get("nonce", "")):
        raise ValueError("invalid lab identity")
    plan = fixture.load_cell_plan(root, record["cell_id"])
    data = (Path(plan["cell_directory"]) / "fixture-plan.json").read_bytes()
    if hashlib.sha256(data).hexdigest() != record["plan_sha256"]:
        raise ValueError("lab plan changed")
    return record, plan


def process_guard(node, *, allow_dead=False):
    try:
        pid = int(Path(node["paths"]["pid"]).read_text())
    except FileNotFoundError:
        if allow_dead and not Path(node["paths"]["qmp"]).exists():
            return False
        raise ValueError("QEMU identity is unavailable")
    if pid < 2:
        raise ValueError("invalid QEMU PID")
    try:
        actual = Path(f"/proc/{pid}/cmdline").read_bytes().rstrip(b"\0").split(b"\0")
    except FileNotFoundError:
        if allow_dead:
            return False
        raise ValueError("registered QEMU is not running")
    expected = [os.fsencode(x) for x in node["qemu_command"]]
    if Path(os.fsdecode(actual[0])).name != "qemu-system-x86_64" or actual[1:] != expected[1:]:
        raise ValueError("registered QEMU process does not own this lab")
    if node["management"]["ssh_host"] != "127.0.0.1":
        raise ValueError("non-loopback lab destination")
    return True


def ssh(root, record, node, *, readiness=False):
    process_guard(node)
    argv = fixture.ssh_command(node, root / "key")[:-1]
    if not readiness:
        argv[argv.index("StrictHostKeyChecking=accept-new")] = "StrictHostKeyChecking=yes"
    return argv


def guest_guard(record, node_name, node):
    identity = {"schema": SCHEMA, "nonce": record["nonce"], "vm_uuid": node["qemu_command"][node["qemu_command"].index("-uuid") + 1],
                "cell_id": record["cell_id"], "node": node_name}
    # Exact marker is supplied by this fresh VM's cloud-init, not by an SSH repair.
    # Tam kimlik dosyasını SSH onarımı değil, bu yeni VM'nin cloud-init'i oluşturur.
    return """import json,os,stat
from pathlib import Path
p=Path('/etc/celikpanel-release-recovery-lab')
i=p.lstat()
if not (stat.S_ISREG(i.st_mode) and i.st_uid==0 and i.st_gid==0 and stat.S_IMODE(i.st_mode)==0o444 and i.st_nlink==1 and i.st_size<=2048):
    raise ValueError('invalid lab marker metadata')
expected=""" + repr(identity) + """
if json.loads(p.read_text())!=expected:
    raise ValueError('lab marker identity differs')
if Path('/sys/class/dmi/id/product_uuid').read_text().strip().lower()!=expected['vm_uuid']:
    raise ValueError('guest UUID differs')
if Path('/proc/1/comm').read_text().strip()!='systemd':
    raise ValueError('native systemd is required')
"""


def guarded_script(root, record, plan, node_name, body, *, timeout=120, capture=True):
    node = plan["nodes"][node_name]
    prefix = "set -eu\npython3 -I - <<'CP_LAB_GUARD'\n" + guest_guard(record, node_name, node) + "\nCP_LAB_GUARD\n"
    return run(ssh(root, record, node) + ["sudo /bin/bash -s"], input=prefix + body, text=True,
               capture_output=capture, timeout=timeout)



def put_file(root, record, plan, node_name, source, basename, *, mode=0o600):
    """Upload only to the private directory of a proven disposable guest."""
    if not re.fullmatch(r"[a-z][a-z0-9_.-]{0,79}", basename) or mode not in (0o600, 0o700):
        raise ValueError("invalid private lab payload destination")
    source = Path(source)
    descriptor = os.open(source, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, "rb") as payload:
        info = os.fstat(payload.fileno())
        if not stat.S_ISREG(info.st_mode) or not 0 < info.st_size <= 100 * 1024 * 1024:
            raise ValueError("lab payload must be a bounded regular file")
        data = payload.read(info.st_size + 1)
        if len(data) != info.st_size:
            raise ValueError("lab payload changed while reading")
    digest = hashlib.sha256(data).hexdigest()
    node = plan["nodes"][node_name]
    script = guest_guard(record, node_name, node) + """
import hashlib,sys,tempfile
root=Path('/root/celikpanel-release-recovery-lab')
try:
    root.mkdir(mode=0o700)
except FileExistsError:
    pass
i=root.lstat()
if not (stat.S_ISDIR(i.st_mode) and i.st_uid==0 and i.st_gid==0 and stat.S_IMODE(i.st_mode)==0o700):
    raise ValueError('invalid private guest payload directory')
""" + "expected_digest=" + repr(digest) + "\nexpected_size=" + str(len(data)) + "\nbasename=" + repr(basename) + "\nmode=" + str(mode) + """
payload=sys.stdin.buffer.read(expected_size+1)
if len(payload)!=expected_size or hashlib.sha256(payload).hexdigest()!=expected_digest:
    raise ValueError('payload digest differs')
target=root/basename
if target.exists() or target.is_symlink():
    i=target.lstat()
    if not (stat.S_ISREG(i.st_mode) and i.st_uid==0 and i.st_gid==0 and i.st_nlink==1):
        raise ValueError('invalid existing guest payload')
fd,temporary=tempfile.mkstemp(prefix='.incoming-',dir=root)
try:
    with os.fdopen(fd,'wb') as out:
        out.write(payload)
        out.flush()
        os.fchmod(out.fileno(),mode)
        os.fsync(out.fileno())
    os.replace(temporary,target)
    directory=os.open(root,os.O_RDONLY|os.O_DIRECTORY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)
finally:
    if os.path.exists(temporary):
        os.unlink(temporary)
print(expected_digest)
"""
    result = run(ssh(root, record, node) + ["sudo python3 -I -c " + shlex.quote(script)],
                 input=data, capture_output=True, timeout=120)
    if result.stdout.decode().strip() != digest:
        raise ValueError("guest did not confirm uploaded payload digest")
    return "/root/celikpanel-release-recovery-lab/" + basename, digest


def prepare(args):
    root = checked_root(args.work_root)
    if not 1024 <= args.ssh_port <= 65533:
        raise ValueError("SSH and peer ports must be unprivileged and within range")
    if root.exists():
        raise ValueError("fresh lab root already exists; choose a new name")
    nonce = secrets.token_hex(32)
    cell_id = fixture.validate_cell_id("release-recovery__" + nonce[:16])
    pins = fixture.load_image_lock(fixture.DEFAULT_IMAGE_LOCK)
    cache = Path(args.image_cache).resolve(strict=True)
    for pin in pins.values():
        source = cache / pin.filename
        info = source.lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_size != pin.size or fixture.digest_file(source, pin.digest_algorithm) != pin.digest:
            raise ValueError("cached base image does not match the reviewed image pin")
    if not args.execute:
        print(json.dumps({"action": "prepare", "root": str(root), "nodes": ["debian13", "arch"], "execute": False}))
        return
    fixture.initialize_work_root(root)
    for pin in pins.values():
        target = root / "images" / pin.filename
        run(["cp", "--reflink=auto", "--", str(cache / pin.filename), str(target)])
        target.chmod(0o444)
    fixture.verify_images(root, pins)
    run(["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(root / "key")])
    plan = fixture.build_cell_plan(root, pins, cell_id, fixture.read_ssh_public_key(root / "key.pub"),
                                   debian_ssh_port=args.ssh_port, arch_ssh_port=args.ssh_port + 1,
                                   peer_port=args.ssh_port + 2, memory_mb=3072, cpus=2, disk_gb=24)
    for name, node in plan["nodes"].items():
        identity = {"schema": SCHEMA, "nonce": nonce, "vm_uuid": node["qemu_command"][node["qemu_command"].index("-uuid") + 1],
                    "cell_id": cell_id, "node": name}
        extra = "  - path: " + MARKER + "\n    owner: root:root\n    permissions: '0444'\n    content: |\n      " + json.dumps(identity) + "\n"
        text = node["cloud_init"]["user-data"]
        assert text.count("runcmd:\n") == 1
        node["cloud_init"]["user-data"] = text.replace("runcmd:\n", extra + "runcmd:\n")
    fixture.execute_prepare(plan)
    raw = (Path(plan["cell_directory"]) / "fixture-plan.json").read_bytes()
    fixture.atomic_write_text(root / "lab.json", json.dumps({"schema": SCHEMA, "nonce": nonce, "cell_id": cell_id,
                              "plan_sha256": hashlib.sha256(raw).hexdigest()}, indent=2) + "\n", 0o600)
    print(json.dumps({"action": "prepared", "root": str(root), "cell_id": cell_id, "ssh_ports": [args.ssh_port, args.ssh_port + 1]}))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("prepare", "start", "status", "stop"))
    parser.add_argument("--work-root", required=True)
    parser.add_argument("--image-cache", default="/var/tmp/cp-install-vm/images")
    parser.add_argument("--ssh-port", type=int, default=2261)
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args()
    if args.command == "prepare":
        prepare(args)
        return
    root = checked_root(args.work_root)
    record, plan = load(root)
    if args.command != "status" and not args.execute:
        print(json.dumps({"action": args.command, "root": str(root), "execute": False}))
        return
    if args.command == "start":
        fixture.execute_start(plan)
        for node in plan["nodes"].values():
            process_guard(node)
        fixture.wait_for_ssh(plan, root / "key", 300)
    if args.command == "stop":
        live = {name: node for name, node in plan["nodes"].items()
                if process_guard(node, allow_dead=True)}
        stopping = dict(plan, nodes=live, start_order=[name for name in plan["start_order"] if name in live])
        fixture.stop_vms(stopping)
        print(json.dumps({"action": "stopped", "root": str(root), "evidence_retained": True}))
        return
    result = {}
    for name in plan["nodes"]:
        observation = guarded_script(root, record, plan, name, "printf 'verified-disposable-systemd-guest\\n'\n")
        result[name] = observation.stdout.strip()
    print(json.dumps({"action": "ready", "root": str(root), "nodes": result}))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, subprocess.SubprocessError, fixture.FixtureError) as exc:
        print("lab refused: " + str(exc), file=sys.stderr)
        sys.exit(1)
