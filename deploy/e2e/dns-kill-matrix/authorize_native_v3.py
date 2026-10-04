#!/usr/bin/env python3
"""Authorize one exact, disposable Debian native V3 DNS switch trial.

This writes only the root-owned test receipt consumed by the tagged Go test.
It cannot be used on an installed panel or to open the product RPC gate.
"""
import hashlib
import json
import os
from pathlib import Path
import secrets
import stat
import subprocess

CELL = "pdns-switch__intent__after-write__paired-primary__peer-reachable"
ROOT = Path("/var/lib/celikpanel-dns-kill-matrix")
AUTH = ROOT / "v3-native-authorization.json"
SCENARIO = ROOT / "scenario.json"
MARKER = Path("/etc/celikpanel-dns-kill-matrix")


def read_exact(path: Path, mode: int) -> bytes:
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_nlink != 1 or stat.S_IMODE(info.st_mode) != mode:
        raise RuntimeError(f"unsafe fixture file: {path}")
    return path.read_bytes()


def main() -> None:
    if os.geteuid() != 0 or Path("/opt/celikpanel/bin/agent").exists() is False:
        raise RuntimeError("disposable primary root and fixture Agent are required")
    marker = read_exact(MARKER, 0o444).decode("ascii")
    if marker != f"schema=celikpanel/dns-kill-fixture-plan/v1\ncell_id={CELL}\nnode=debian13\n":
        raise RuntimeError("not the exact disposable Debian fixture")
    scenario_bytes = read_exact(SCENARIO, 0o600)
    scenario = json.loads(scenario_bytes)
    if (scenario.get("driver"), scenario.get("source_fixture"), scenario.get("target_engine"), scenario.get("topology"), scenario.get("pair_role"), scenario.get("local_ip"), scenario.get("peer_ip")) != ("pdns-switch", "uninitialized", "pdns", "paired", "primary", "192.0.2.10", "192.0.2.11"):
        raise RuntimeError("scenario does not match the native trial")
    for unit in ("celikpanel-agent.service", "celikpanel-panel.service"):
        state = subprocess.run(("/usr/bin/systemctl", "show", unit, "-p", "ActiveState", "--value"), capture_output=True, text=True, check=True).stdout.strip()
        if state != "inactive":
            raise RuntimeError(f"management unit is not inactive: {unit}")
    socket = Path("/run/celikpanel/agent.sock")
    if socket.exists() or socket.is_symlink():
        raise RuntimeError("Agent socket must be absent")
    if AUTH.exists() or AUTH.is_symlink():
        raise RuntimeError("test authorization already exists")
    for path in (Path("/var/lib/celikpanel-agent-private/dns-engine-state.json"), Path("/var/lib/celikpanel-agent-private/dns-engine-switch-journal.json")):
        if path.exists() or path.is_symlink():
            raise RuntimeError("DNS state is not fresh")
    record = {
        "schema": "celikpanel/pdns-v3-native-test-authorization/v1",
        "cell_id": CELL,
        "node": "debian13",
        "machine_id": Path("/etc/machine-id").read_text().strip(),
        "boot_id": Path("/proc/sys/kernel/random/boot_id").read_text().strip(),
        "request_id": secrets.token_hex(16),
        "owner_id": secrets.token_hex(16),
        "nonce": secrets.token_hex(32),
        "scenario_sha256": hashlib.sha256(scenario_bytes).hexdigest(),
    }
    payload = json.dumps(record, sort_keys=True, separators=(",", ":")).encode("ascii")
    fd = os.open(AUTH, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        os.fchmod(fd, 0o600)
        with os.fdopen(os.dup(fd), 'wb') as stream:
            stream.write(payload)
            stream.flush()
        os.fsync(fd)
    finally:
        os.close(fd)
    directory = os.open(ROOT, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)
    print(json.dumps({"state": "authorized", "cell_id": CELL, "request_id": record["request_id"]}, sort_keys=True))


if __name__ == "__main__":
    main()
