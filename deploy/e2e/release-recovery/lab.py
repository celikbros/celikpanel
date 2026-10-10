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
from urllib.parse import urlsplit


HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("release_lab_fixture", HERE.parent / "dns-kill-matrix/fixture.py")
fixture = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = fixture
SPEC.loader.exec_module(fixture)
SCHEMA = "celikpanel-release-recovery-lab/v1"
MARKER = "/etc/celikpanel-release-recovery-lab"
# upd8: a one-node Ubuntu 24.04 lab beside the default Debian 13 + Arch pair. The image is the
# official cloud image pinned by images-ubuntu.lock.json (SHA-256 from the same release's SHA256SUMS).
PLATFORMS = ("debian13-arch", "ubuntu")
UBUNTU_IMAGE_LOCK = HERE / "images-ubuntu.lock.json"
UBUNTU_LOCK_SCHEMA = "celikpanel-release-recovery-ubuntu-image-lock/v1"
UBUNTU_URL_RE = re.compile(r"/releases/noble/release-(?P<build>[0-9]{8}(?:\.[0-9]+)?)/ubuntu-24\.04-server-cloudimg-amd64\.img")
UBUNTU_NODE = fixture.NodeSpec(name="ubuntu", hostname="dns-ubuntu", image="ubuntu", mgmt_mac="52:54:00:13:00:12",
                               peer_mac="52:54:00:53:00:12", peer_address="192.0.2.12/24", ssh_service="ssh.service",
                               admin_group="sudo")


def load_ubuntu_pin(path=UBUNTU_IMAGE_LOCK):
    raw = fixture.read_json_regular(Path(path), "Ubuntu image lock")
    if not isinstance(raw, dict) or set(raw) != {"schema", "image"} or raw["schema"] != UBUNTU_LOCK_SCHEMA:
        raise ValueError("Ubuntu image lock schema is unsupported")
    item = raw["image"]
    fields = {"distribution", "release", "architecture", "url", "filename", "digest", "bytes"}
    if not isinstance(item, dict) or set(item) != fields:
        raise ValueError("Ubuntu image pin fields are incomplete or unexpected")
    if (item["distribution"], item["release"], item["architecture"]) != ("Ubuntu", "24.04", "x86_64"):
        raise ValueError("Ubuntu image identity is not the pinned 24.04 amd64 cloud image")
    url = urlsplit(item["url"])
    match = UBUNTU_URL_RE.fullmatch(url.path)
    if (url.scheme != "https" or url.hostname != "cloud-images.ubuntu.com" or url.port or url.query or url.fragment
            or not match):
        raise ValueError("Ubuntu image must be an immutable cloud-images.ubuntu.com release URL")
    if item["filename"] != "ubuntu-24.04-server-cloudimg-amd64-" + match["build"] + ".img":
        raise ValueError("Ubuntu cached image name must carry the release build")
    digest = item["digest"]
    if (not isinstance(digest, dict) or set(digest) != {"algorithm", "value"} or digest["algorithm"] != "sha256"
            or not re.fullmatch(r"[0-9a-f]{64}", str(digest["value"]))):
        raise ValueError("Ubuntu image digest must be the published SHA-256")
    if type(item["bytes"]) is not int or item["bytes"] <= 0:
        raise ValueError("Ubuntu image byte size must be a positive integer")
    return fixture.ImagePin(name="ubuntu", distribution="Ubuntu", release="24.04", architecture="x86_64",
                            url=item["url"], filename=item["filename"], digest_algorithm="sha256",
                            digest=digest["value"], size=item["bytes"])


def build_ubuntu_plan(root, pin, cell_id, ssh_public_key, *, ssh_port, memory_mb, cpus, disk_gb):
    """The fixture's node layout for one Ubuntu guest; its peer NIC listens and stays unconnected."""
    fixture.validate_cell_id(cell_id)
    peer_port = ssh_port + 2
    cell = fixture.cell_directory(root, cell_id, must_exist=False)
    node = UBUNTU_NODE
    paths = fixture._node_paths(cell, node)
    if len(os.fsencode(paths["qmp"])) > 100:
        raise ValueError("work root is too long for a Unix QMP socket")
    base = fixture.image_path(root, pin)
    qemu = fixture._qemu_command(cell_id, node, paths, ssh_port, peer_port, "kvm", memory_mb, cpus)
    connect = f"socket,id=peer,connect=127.0.0.1:{peer_port}"
    qemu[qemu.index(connect)] = f"socket,id=peer,listen=127.0.0.1:{peer_port}"
    entry = {
        "hostname": node.hostname,
        "management": {"ssh_host": "127.0.0.1", "ssh_port": ssh_port, "mac": node.mgmt_mac, "mode": "qemu-user-nat"},
        "peer": {"address": node.peer_address, "mac": node.peer_mac, "device_id": "peer-link",
                 "transport": "loopback-socket", "transport_port": peer_port},
        "paths": {key: str(value) for key, value in paths.items()},
        "cloud_init": fixture.cloud_init_files(cell_id, node, ssh_public_key),
        "overlay_command": ["qemu-img", "create", "-f", "qcow2", "-F", "qcow2", "-b", str(base),
                            str(paths["overlay"]), f"{disk_gb}G"],
        "seed_command": fixture._seed_command("genisoimage", paths),
        "qemu_command": qemu,
        "base": {"path": str(base), "digest": {"algorithm": pin.digest_algorithm, "value": pin.digest},
                 "bytes": pin.size},
    }
    return {"schema": fixture.PLAN_SCHEMA, "cell_id": cell_id, "work_root": str(root), "cell_directory": str(cell),
            "host_requirements": {"os": "linux", "qmp_transport": "unix", "daemonization": "qemu-daemonize",
                                  "accelerator": "kvm"},
            "start_order": ["ubuntu"], "peer_link_policy": {"initial": "up", "qmp_device": "peer-link"},
            "nodes": {"ubuntu": entry}}


def load_plan(root, cell_id):
    try:
        return fixture.load_cell_plan(root, cell_id)
    except fixture.FixtureError:
        cell = fixture.cell_directory(root, cell_id, must_exist=True)
        plan = fixture.read_json_regular(cell / "fixture-plan.json", "fixture plan")
        if not isinstance(plan, dict) or set(plan.get("nodes", {})) != {"ubuntu"}:
            raise
    if (plan.get("schema") != fixture.PLAN_SCHEMA or plan.get("cell_id") != cell_id
            or plan.get("work_root") != str(root) or plan.get("cell_directory") != str(cell)
            or plan.get("start_order") != ["ubuntu"]
            or plan["nodes"]["ubuntu"].get("paths") != {k: str(v) for k, v in fixture._node_paths(cell, UBUNTU_NODE).items()}):
        raise ValueError("Ubuntu lab plan identity is invalid")
    return plan


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
    plan = load_plan(root, record["cell_id"])
    data =(Path(plan["cell_directory"]) / "fixture-plan.json").read_bytes()
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
    ubuntu = getattr(args, "platform", PLATFORMS[0]) == "ubuntu"
    pins = {"ubuntu": load_ubuntu_pin()} if ubuntu else fixture.load_image_lock(fixture.DEFAULT_IMAGE_LOCK)
    cache = Path(args.image_cache).resolve(strict=True)
    for pin in pins.values():
        source = cache / pin.filename
        info = source.lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_size != pin.size or fixture.digest_file(source, pin.digest_algorithm) != pin.digest:
            raise ValueError("cached base image does not match the reviewed image pin")
    if not args.execute:
        print(json.dumps({"action": "prepare", "root": str(root), "nodes": ["ubuntu"] if ubuntu else ["debian13", "arch"],
                          "execute": False}))
        return
    fixture.initialize_work_root(root)
    for pin in pins.values():
        target = root / "images" / pin.filename
        if os.environ.get("CELIKPANEL_LAB_LINK_BASE_IMAGES") == "1":
            # set5 (opt-in): a hard link to the cached base image instead of a copy of it. The cache and the lab are
            # on one filesystem, the cached file is read-only, QEMU opens it only as a backing file, and it is
            # verified against the reviewed pin just below exactly as a copy is. Nothing is chmod'ed here: the
            # verification refuses a writable image.
            os.link(cache / pin.filename, target)
        else:
            run(["cp", "--reflink=auto", "--", str(cache / pin.filename), str(target)])
            target.chmod(0o444)
    if ubuntu:
        fixture.verify_image(fixture.validate_work_root(root), pins["ubuntu"])
    else:
        fixture.verify_images(root, pins)
    # A fixed comment: the default would be user@host of the operator's machine, and that
    # name ended up in every published fixture plan (scrubbed on 2026-10-10).
    run(["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "celikpanel-lab", "-f", str(root / "key")])
    if ubuntu:
        plan = build_ubuntu_plan(root, pins["ubuntu"], cell_id, fixture.read_ssh_public_key(root / "key.pub"),
                                 ssh_port=args.ssh_port, memory_mb=3072, cpus=2, disk_gb=24)
    else:
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
    parser.add_argument("--platform", choices=PLATFORMS, default=PLATFORMS[0])
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
