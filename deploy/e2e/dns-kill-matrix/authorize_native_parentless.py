#!/usr/bin/env python3
"""Authorize exactly one parentless V3 deletion on the disposable Debian DNS VM."""
import hashlib
import json
import os
from pathlib import Path
import secrets
import stat

CELL = "pdns-switch__intent__after-write__paired-primary__peer-reachable"
ROOT = Path("/var/lib/celikpanel-dns-kill-matrix")
TARGET = ROOT / "v3-parentless-native-authorization.json"


def read_root(path: Path, mode: int) -> bytes:
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_nlink != 1 or stat.S_IMODE(info.st_mode) != mode:
        raise RuntimeError(f"unsafe fixture file: {path}")
    return path.read_bytes()


def main() -> None:
    if os.geteuid() != 0:
        raise RuntimeError("root required on the disposable fixture")
    marker = read_root(Path("/etc/celikpanel-dns-kill-matrix"), 0o444).decode("ascii")
    if marker != f"schema=celikpanel/dns-kill-fixture-plan/v1\ncell_id={CELL}\nnode=debian13\n":
        raise RuntimeError("not the exact disposable Debian DNS fixture")
    source = json.loads(read_root(ROOT / "v3-native-authorization.json", 0o600))
    scenario = read_root(ROOT / "scenario.json", 0o600)
    machine = Path("/etc/machine-id").read_text().strip()
    boot = Path("/proc/sys/kernel/random/boot_id").read_text().strip()
    if source.get("cell_id") != CELL or source.get("node") != "debian13" or source.get("machine_id") != machine or source.get("boot_id") != boot or source.get("scenario_sha256") != hashlib.sha256(scenario).hexdigest():
        raise RuntimeError("source authorization is not bound to this boot and scenario")
    state = json.loads(read_root(Path("/var/lib/celikpanel-agent-private/dns-engine-state.json"), 0o600))
    acquisition = state.get("acquisition", {})
    if state.get("schema") != "celikpanel-dns-engine-state/v3" or acquisition.get("engine") != "pdns" or acquisition.get("pair_role") != "primary" or acquisition.get("pair_local_ip") != "192.0.2.10" or acquisition.get("pair_peer_ip") != "192.0.2.11":
        raise RuntimeError("the expected paired PowerDNS primary is not active")
    if TARGET.exists() or TARGET.is_symlink():
        raise RuntimeError("parentless authorization already exists")
    record = {
        "schema": "celikpanel/pdns-v3-parentless-native-test-authorization/v1",
        "cell_id": CELL, "machine_id": machine, "boot_id": boot,
        "scenario_sha256": source["scenario_sha256"], "source_request_id": source["request_id"],
        "request_id": secrets.token_hex(16), "owner_id": secrets.token_hex(16),
        "domain": "s1-kill.test", "generation": 2,
    }
    payload = json.dumps(record, sort_keys=True, separators=(",", ":")).encode("ascii")
    fd = os.open(TARGET, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        os.fchmod(fd, 0o600)
        with os.fdopen(os.dup(fd), "wb", closefd=True) as output:
            output.write(payload)
            output.flush()
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