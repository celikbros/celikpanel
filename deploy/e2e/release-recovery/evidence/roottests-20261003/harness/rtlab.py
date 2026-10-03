#!/usr/bin/env python3
"""One-node disposable Debian 13 QEMU guest for the root contract-test run.

Built from the release-recovery lab (lab.py) and the DNS kill-matrix fixture of the
candidate's own run copy: same pinned Debian 13 image, same cloud-init user, same
/etc/celikpanel-release-recovery-lab identity marker and guest guard before every
privileged command. Differences: one node only, overlay virtual size 20 GiB, and a
'restrict' step that restarts the same guest with QEMU user networking restrict=on
(no outbound network; the loopback SSH forward keeps working).
"""
import hashlib, importlib.util, json, os, secrets, subprocess, sys, time
from pathlib import Path

SRC = Path("/var/tmp/cp-roottests-20261003/src")
ROOT = Path("/var/tmp/cp-release-drill-roottests-20261003")
CACHE = Path("/var/tmp/cp-install-vm/images")
SSH_PORT = 2271

spec = importlib.util.spec_from_file_location("rt_lab", SRC / "deploy/e2e/release-recovery/lab.py")
lab = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = lab
spec.loader.exec_module(lab)
fixture = lab.fixture


def state():
    record = lab.read_private(ROOT / "rt-lab.json")
    plan = json.loads((ROOT / "rt-plan.json").read_text())
    return record, plan


def save_plan(plan):
    fixture.atomic_write_text(ROOT / "rt-plan.json", json.dumps(plan, indent=2, sort_keys=True) + "\n", 0o600)


def prepare():
    root = lab.checked_root(str(ROOT))
    if root.exists():
        raise ValueError("fresh lab root already exists")
    nonce = secrets.token_hex(32)
    cell_id = fixture.validate_cell_id("release-recovery__" + nonce[:16])
    pins = fixture.load_image_lock(fixture.DEFAULT_IMAGE_LOCK)
    pin = pins["debian13"]
    src = CACHE / pin.filename
    if src.stat().st_size != pin.size or fixture.digest_file(src, pin.digest_algorithm) != pin.digest:
        raise ValueError("cached Debian image does not match the pin")
    fixture.initialize_work_root(root)
    target = root / "images" / pin.filename
    subprocess.run(["cp", "--reflink=auto", "--", str(src), str(target)], check=True)
    target.chmod(0o444)
    fixture.verify_image(root, pin)
    subprocess.run(["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(root / "key")], check=True)
    plan = fixture.build_cell_plan(root, pins, cell_id, fixture.read_ssh_public_key(root / "key.pub"),
                                   debian_ssh_port=SSH_PORT, arch_ssh_port=SSH_PORT + 1, peer_port=SSH_PORT + 2,
                                   memory_mb=8192, cpus=8, disk_gb=20)
    del plan["nodes"]["arch"]
    plan["start_order"] = ["debian13"]
    node = plan["nodes"]["debian13"]
    identity = {"schema": lab.SCHEMA, "nonce": nonce,
                "vm_uuid": node["qemu_command"][node["qemu_command"].index("-uuid") + 1],
                "cell_id": cell_id, "node": "debian13"}
    extra = ("  - path: " + lab.MARKER + "\n    owner: root:root\n    permissions: '0444'\n    content: |\n      "
             + json.dumps(identity) + "\n")
    text = node["cloud_init"]["user-data"]
    assert text.count("runcmd:\n") == 1
    node["cloud_init"]["user-data"] = text.replace("runcmd:\n", extra + "runcmd:\n")
    fixture.execute_prepare(plan)
    save_plan(plan)
    fixture.atomic_write_text(ROOT / "rt-lab.json", json.dumps({"schema": lab.SCHEMA, "nonce": nonce,
                              "cell_id": cell_id}, indent=2) + "\n", 0o600)
    print(json.dumps({"action": "prepared", "root": str(root), "cell_id": cell_id, "vm_uuid": identity["vm_uuid"]}))


def start():
    record, plan = state()
    fixture.execute_start(plan)
    lab.process_guard(plan["nodes"]["debian13"])
    fixture.wait_for_ssh(plan, ROOT / "key", 600)
    print(lab.guarded_script(ROOT, record, plan, "debian13",
                             "printf 'verified-disposable-systemd-guest\\n'\n").stdout.strip())


def run(script_path, timeout):
    record, plan = state()
    body = Path(script_path).read_text()
    try:
        lab.guarded_script(ROOT, record, plan, "debian13", body, timeout=timeout, capture=False)
    except subprocess.CalledProcessError as exc:
        return exc.returncode
    return 0


def put(source, dest):
    record, plan = state()
    node = plan["nodes"]["debian13"]
    data = Path(source).read_bytes()
    digest = hashlib.sha256(data).hexdigest()
    lab.guarded_script(ROOT, record, plan, "debian13", "true\n")
    if not all(ch.isalnum() or ch in "/._-" for ch in dest):
        raise ValueError("unsafe destination")
    cmd = f"sudo sh -c 'umask 077; cat > {dest}.part && mv {dest}.part {dest} && sha256sum {dest}'"
    out = subprocess.run(lab.ssh(ROOT, record, node) + [cmd],
                         input=data, capture_output=True, check=True, timeout=1800)
    got = out.stdout.decode().split()[0]
    if got != digest:
        raise ValueError("digest mismatch after upload")
    print(digest, dest)


def get(source, dest):
    record, plan = state()
    node = plan["nodes"]["debian13"]
    lab.guarded_script(ROOT, record, plan, "debian13", "true\n")
    with open(dest, "wb") as out:
        subprocess.run(lab.ssh(ROOT, record, node) + ["sudo cat " + source], stdout=out, check=True, timeout=1800)
    print(hashlib.sha256(Path(dest).read_bytes()).hexdigest(), dest)


def poweroff_and_wait():
    record, plan = state()
    node = plan["nodes"]["debian13"]
    lab.guarded_script(ROOT, record, plan, "debian13", "sync\n")
    subprocess.run(lab.ssh(ROOT, record, node) + ["sudo systemctl poweroff"], check=False, timeout=60)
    pid = Path(node["paths"]["pid"])
    deadline = time.monotonic() + 180
    while fixture._pid_alive(pid) and time.monotonic() < deadline:
        time.sleep(1)
    if fixture._pid_alive(pid):
        fixture.stop_vms(plan)
    for name in ("qmp", "pid"):
        p = Path(node["paths"][name])
        if p.exists() and not fixture._pid_alive(pid):
            p.unlink()
    return plan


def restrict():
    plan = poweroff_and_wait()
    node = plan["nodes"]["debian13"]
    cmd = node["qemu_command"]
    i = [k for k, v in enumerate(cmd) if v.startswith("user,id=mgmt,")][0]
    if ",restrict=on" not in cmd[i]:
        cmd[i] = cmd[i].replace("user,id=mgmt,", "user,id=mgmt,restrict=on,")
    save_plan(plan)
    print(json.dumps({"netdev": cmd[i]}))
    start()


def stop():
    poweroff_and_wait()
    print("stopped")


if __name__ == "__main__":
    c = sys.argv[1]
    if c == "prepare":
        prepare()
    elif c == "start":
        start()
    elif c == "run":
        sys.exit(run(sys.argv[2], int(sys.argv[3]) if len(sys.argv) > 3 else 600))
    elif c == "put":
        put(sys.argv[2], sys.argv[3])
    elif c == "get":
        get(sys.argv[2], sys.argv[3])
    elif c == "restrict":
        restrict()
    elif c == "stop":
        stop()
    else:
        raise SystemExit("unknown command")
