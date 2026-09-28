#!/usr/bin/env python3
"""Remove only the fixture's managed-block comment in a disposable Arch guest."""

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
EDITED_SHA256 = "4d5515f54996890e6d9f3430f22cfaed0e62b8257b4d47a0c1f953b81186cdb9"
CANONICAL_SHA256 = "d16ee58e0f99bf245c7994fbe767a3aa4da1ba4e39b5083cdaa30fdbe0214270"
ANCHOR = b"// BEGIN CELIKPANEL MANAGED BIND ZONES\n"
COMMENT = b"// owner-edit-stage2-managed-block-20260927\n"


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args()
    if not args.execute or os.geteuid() != 0:
        parser.error("disposable root execution is required")
    if Path("/etc/celikpanel-dns-kill-matrix").read_text() != MARKER:
        parser.error("disposable Arch guest marker mismatch")
    ledger = json.loads(LEDGER.read_bytes())
    jobs = ledger.get("jobs", {})
    job = jobs.get(REQUEST)
    if (len(jobs) != 2 or ledger.get("active_request_id") is not None or
            not isinstance(job, dict) or job.get("owner_id") != OWNER or
            job.get("status") != "pending" or
            job.get("error_code") != "dns_peer_owner_edit_unknown" or
            "/propagation-pending/" not in job.get("phase", "")):
        parser.error("exact reviewed owner-edit job is not idle and pending")
    fd = os.open(CONFIG, os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(fd)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or
                stat.S_IMODE(info.st_mode) != 0o640 or info.st_nlink != 1 or
                info.st_size > 1 << 20):
            parser.error("managed config identity is unsafe")
        original = os.read(fd, 1 << 20)
        if (hashlib.sha256(original).hexdigest() != EDITED_SHA256 or
                original.count(ANCHOR + COMMENT) != 1 or
                original.count(COMMENT) != 1):
            parser.error("managed config edited preimage mismatch")
        restored = original.replace(ANCHOR + COMMENT, ANCHOR, 1)
        if hashlib.sha256(restored).hexdigest() != CANONICAL_SHA256:
            parser.error("reconciliation would change bytes outside the one injected comment")
        os.lseek(fd, 0, os.SEEK_SET)
        if os.write(fd, restored) != len(restored):
            parser.error("short reconciliation write")
        os.ftruncate(fd, len(restored))
        os.fsync(fd)
        os.lseek(fd, 0, os.SEEK_SET)
        if os.read(fd, 1 << 20) != restored:
            parser.error("reconciled config readback differs")
    finally:
        os.close(fd)
    print(json.dumps({"path": str(CONFIG), "inode": info.st_ino,
                      "before_sha256": EDITED_SHA256,
                      "after_sha256": CANONICAL_SHA256,
                      "removed": COMMENT.decode("ascii").strip()}, sort_keys=True))


if __name__ == "__main__":
    main()
