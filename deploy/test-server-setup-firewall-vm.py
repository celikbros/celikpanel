#!/usr/bin/env python3
"""Real reboot gate for three explicitly registered disposable localhost VMs.

This installs only an agent/test fixture on proven-empty VM images. It refuses
existing panels and never invokes CelikPanel's installer or updater. The caller
provides freshly built `agent` and `panel-test` files in the fixture directory.
"""

from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
import argparse
import hashlib
import json
import subprocess
import time

BASE = Path("/var/tmp/cp-server-setup-20260911")
KEY = Path("/var/tmp/cp-install-vm/key")
PORTS = {"debian13": 2241, "arch": 2242, "ubuntu24": 2243}
ROOT = Path(__file__).resolve().parent.parent


def command(args, *, text=None, timeout=120, check=True):
    result = subprocess.run(args, input=text, text=True, capture_output=True, timeout=timeout)
    if check and result.returncode:
        raise RuntimeError(f"command exited {result.returncode}: {result.stdout}\n{result.stderr}")
    return result


def ssh_args(port):
    return ["ssh", "-i", str(KEY), "-p", str(port), "-o", "BatchMode=yes",
            "-o", "ConnectTimeout=8", "-o", "StrictHostKeyChecking=yes",
            "-o", "UserKnownHostsFile=" + str(BASE / "ssh-known-hosts"), "celik@127.0.0.1"]


def verify_registry(name, row):
    if row["port"] != PORTS[name]:
        raise RuntimeError("fixture port changed")
    pid = int((BASE / name / "qemu.pid").read_text().strip())
    argv = Path(f"/proc/{pid}/cmdline").read_bytes().split(b"\0")
    if b"cp-setup65-" + name.encode() not in argv or not argv[0].endswith(b"qemu-system-x86_64"):
        raise RuntimeError("registered VM process identity changed")


def install_fixture(name, port):
    guard = f"""set -eu
test "$(cat /etc/hostname)" = setup65-{name}
test ! -e /opt/celikpanel
test ! -e /usr/local/bin/celikpanel
test ! -e /etc/celikpanel
test ! -e /var/lib/celikpanel-setup-vm
command -v nft
"""
    command(ssh_args(port) + ["sudo sh -s"], text=guard)
    files = [BASE / "agent", BASE / "panel-test", ROOT / "deploy/systemd/celikpanel-agent.service",
             ROOT / "deploy/systemd/celikpanel-firewall-restore.service"]
    for file in files:
        if not file.is_file():
            raise RuntimeError(f"missing candidate fixture artifact: {file}")
        scp = ["scp", "-i", str(KEY), "-P", str(port), "-o", "BatchMode=yes",
               "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + str(BASE / "ssh-known-hosts"),
               str(file), "celik@127.0.0.1:/tmp/" + file.name]
        command(scp)
    script = guard + """
getent group celikpanel >/dev/null || groupadd --system celikpanel
install -d -m 0755 /opt/celikpanel/bin
install -d -m 0700 /var/lib/celikpanel-setup-vm
install -m 0755 /tmp/agent /opt/celikpanel/bin/agent
install -m 0755 /tmp/panel-test /var/lib/celikpanel-setup-vm/panel-test
install -m 0644 /tmp/celikpanel-agent.service /etc/systemd/system/celikpanel-agent.service
install -m 0644 /tmp/celikpanel-firewall-restore.service /etc/systemd/system/celikpanel-firewall-restore.service
printf 'fresh-setup-firewall-acceptance-v1\\n' > /var/lib/celikpanel-setup-vm/fixture
systemctl daemon-reload
install -d -m 0700 /var/lib/celikpanel-agent-private
/opt/celikpanel/bin/agent --initialize-service-mutation-ledger
systemctl enable --now celikpanel-agent.service
test ! -e /opt/celikpanel/bin/panel
"""
    command(ssh_args(port) + ["sudo sh -s"], text=script)
    for _ in range(30):
        result = command(ssh_args(port) + ["sudo test -S /run/celikpanel/agent.sock"], check=False)
        if result.returncode == 0:
            return
        time.sleep(1)
    raise RuntimeError("candidate fixture agent did not become ready")


def stage(name, port, label):
    result = command(ssh_args(port) + [f"sudo env CELIKPANEL_SETUP_VM_STAGE={label} /var/lib/celikpanel-setup-vm/panel-test -test.run '^TestServerSetupDisposableVMFirewall$' -test.v"], timeout=180, check=False)
    (BASE / name / f"firewall-{label}.log").write_text(result.stdout + result.stderr)
    if result.returncode:
        raise RuntimeError(f"{name} {label} failed: {result.stdout}\n{result.stderr}")


def reboot(name, port, label):
    before = command(ssh_args(port) + ["cat /proc/sys/kernel/random/boot_id"]).stdout.strip()
    command(ssh_args(port) + ["sudo systemctl reboot"], check=False)
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        time.sleep(3)
        result = command(ssh_args(port) + ["cat /proc/sys/kernel/random/boot_id"], timeout=12, check=False)
        after = result.stdout.strip()
        if result.returncode == 0 and after and after != before:
            (BASE / name / f"reboot-{label}.json").write_text(json.dumps({"before": before, "after": after, "ssh_reconnected": True}, indent=2))
            return
    raise RuntimeError(f"{name} did not complete a verified reboot with SSH reachable")


def run_vm(item, install, resume):
    name, row = item
    verify_registry(name, row)
    port = row["port"]
    if install:
        install_fixture(name, port)
    if resume:
        if not (BASE / name / "reboot-no-snapshot.json").is_file():
            raise RuntimeError("cannot resume without the no-snapshot reboot evidence")
    else:
        stage(name, port, "baseline")
        reboot(name, port, "no-snapshot")
        stage(name, port, "baseline")
    stage(name, port, "setup")
    saved = command(ssh_args(port) + ["sudo sha256sum /etc/celikpanel/firewall.nft"]).stdout.split()[0]
    reboot(name, port, "saved")
    stage(name, port, "verify_saved")
    restored = command(ssh_args(port) + ["sudo sha256sum /etc/celikpanel/firewall.nft"]).stdout.split()[0]
    if saved != restored:
        raise RuntimeError("saved firewall bytes changed across reboot")
    stage(name, port, "off")
    reboot(name, port, "off")
    stage(name, port, "verify_off")
    evidence = {"os": name, "port": port, "ssh_preserved": True, "actual_reboots": 3,
                "saved_policy_sha256": saved, "candidate_agent_sha256": hashlib.sha256((BASE / "agent").read_bytes()).hexdigest(),
                "candidate_test_sha256": hashlib.sha256((BASE / "panel-test").read_bytes()).hexdigest()}
    (BASE / name / "firewall-acceptance.json").write_text(json.dumps(evidence, indent=2))
    print(json.dumps(evidence), flush=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--install-fresh-fixture", action="store_true")
    parser.add_argument("--resume-at-setup", action="store_true")
    args = parser.parse_args()
    plan = json.loads((BASE / "plan.json").read_text())
    if set(plan) != set(PORTS):
        raise RuntimeError("unexpected VM fixture registry")
    with ThreadPoolExecutor(max_workers=3) as pool:
        list(pool.map(lambda item: run_vm(item, args.install_fresh_fixture, args.resume_at_setup), plan.items()))


if __name__ == "__main__":
    main()
