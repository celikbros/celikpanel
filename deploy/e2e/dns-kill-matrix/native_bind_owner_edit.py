#!/usr/bin/env python3
"""Inject one bounded owner comment into a disposable managed BIND block.

This is a guest-only fault fixture. It refuses to run without the exact QEMU
marker, expected preimage hash, root-owned regular config and pending V3 job.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import stat
from pathlib import Path


CELL = "bind__intent__after-write__paired-primary__peer-reachable"
MARKER = f"schema=celikpanel/dns-kill-fixture-plan/v1\ncell_id={CELL}\nnode=arch\n"
CONFIG = Path("/etc/named.conf")
LEDGER = Path("/var/lib/celikpanel-agent-private/service-mutations.json")
REQUEST = "14b0d5712bca0f77e7556b215e9c2bb2"
OWNER = "09b5c4771c3831471d1357609f48217f"
BEFORE = b"// BEGIN CELIKPANEL MANAGED BIND ZONES\n"
INSERT = b"// owner-edit-stage2-managed-block-20260927\n"


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--expected-sha256", required=True)
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args()
    if not args.execute or os.geteuid() != 0:
        parser.error("disposable root execution is required")
    if Path("/etc/celikpanel-dns-kill-matrix").read_text() != MARKER:
        parser.error("disposable Arch guest marker mismatch")
    ledger = json.loads(LEDGER.read_bytes())
    jobs = ledger.get("jobs", {})
    job = jobs.get(REQUEST)
    if (len(jobs) != 2 or not isinstance(job, dict) or
            job.get("owner_id") != OWNER or job.get("status") != "pending" or
            "/propagation-pending/" not in job.get("phase", "")):
        parser.error("exact V3 operation is not the sole pending job")
    fd = os.open(CONFIG, os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(fd)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or
                stat.S_IMODE(info.st_mode) != 0o640 or info.st_nlink != 1):
            parser.error("managed config identity is unsafe")
        original = os.read(fd, 1 << 20)
        before_hash = hashlib.sha256(original).hexdigest()
        if before_hash != args.expected_sha256 or original.count(BEFORE) != 1 or INSERT in original:
            parser.error("managed config preimage or anchor mismatch")
        updated = original.replace(BEFORE, BEFORE + INSERT, 1)
        os.lseek(fd, 0, os.SEEK_SET)
        os.write(fd, updated)
        os.ftruncate(fd, len(updated))
        os.fsync(fd)
        os.lseek(fd, 0, os.SEEK_SET)
        if os.read(fd, 1 << 20) != updated:
            parser.error("owner edit readback differs")
    finally:
        os.close(fd)
    print(json.dumps({"path": str(CONFIG), "inode": info.st_ino,
                      "before_sha256": before_hash,
                      "after_sha256": hashlib.sha256(updated).hexdigest(),
                      "insert": INSERT.decode("ascii").strip()}, sort_keys=True))


if __name__ == "__main__":
    main()
