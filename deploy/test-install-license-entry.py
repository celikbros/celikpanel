#!/usr/bin/env python3
"""Exercise the real panel CLI through fresh and resumed installer handoffs.

Run as root in a disposable Linux environment with a built panel argument.
No license, account, database, service or network mutation is performed: each
case must reach the missing-terminal diagnostic before accepting any secret.
"""
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

repo = Path(__file__).resolve().parent.parent
assert os.geteuid() == 0, "run in a disposable root test environment"
assert len(sys.argv) == 2, "provide the real compiled panel binary"
binary = Path(sys.argv[1]).resolve(strict=True)
for state in ("/var/lib/celikpanel", "/var/lib/celikpanel-license"):
    assert not Path(state).exists(), "refusing to inspect an existing installation"

install = (repo / "install.sh").read_text()
match = re.search(r'^if \[\[ "\$APPLY_ONLY" -eq 0 && -x "\$SRC/bin/panel".*?^fi$',
                  install, re.M | re.S)
assert match, "installer license admission block missing"
entry = match.group()
bootstrap = (repo / "download-portal/get.sh").read_text()
match = re.search(r'^  CELIKPANEL_TRUSTED_RELEASE_ROOT="\$extracted_root" \\\n.*?    bash "\$installer"$',
                  bootstrap, re.M | re.S)
assert match, "download bootstrap installer handoff missing"
handoff = match.group()

with tempfile.TemporaryDirectory(prefix="celikpanel-license-entry-") as work:
    root = Path(work)
    (root / "bin").mkdir()
    (root / "bin/panel").symlink_to(binary)
    legacy = root / "legacy-install.sh"
    # Alpha58/59 candidates invoke the CLI without a data-directory assignment.
    legacy.write_text('#!/bin/bash\nset -eu\n"$CELIKPANEL_TRUSTED_RELEASE_ROOT/bin/panel" --activate-install-license\n')
    setup = '''set -eu
die() { printf '%s\\n' "$*" >&2; exit 1; }
SRC=$1
DATA_DIR=/var/lib/celikpanel
APPLY_ONLY=0
RESTORE_ARCHIVE_PATH=
extracted_root=$1
installer=$1/legacy-install.sh
signed_public_key_path=$1/public.pem
signed_release_sequence=59
version=v0.1.0-alpha.59
signed_commit=5af302f29c1de5ca6524971b8e6ab2890cb2d40e
'''
    for name, block in (("fresh", entry), ("resume-alpha59", handoff)):
        for inherited in (None, "./wrong-relative-data", str(root / "wrong-absolute-data")):
            env = os.environ.copy()
            env.pop("CELIKPANEL_DATA_DIR", None)
            if inherited is not None:
                env["CELIKPANEL_DATA_DIR"] = inherited
            result = subprocess.run(["bash", "-c", setup + block, "test", work],
                                    env=env, stdin=subprocess.DEVNULL,
                                    stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                    start_new_session=True, timeout=30, text=True)
            assert result.returncode != 0, (name, "accepted without license")
            assert "rerun installation in a terminal to enter your license" in result.stdout, (name, inherited, result.stdout)
            assert "invalid licensing configuration" not in result.stdout, result.stdout
            assert not (root / "wrong-absolute-data").exists()
            print(f"PASS {name}, inherited directory {inherited!r}: real CLI reached license entry")
    for state in ("/var/lib/celikpanel", "/var/lib/celikpanel-license"):
        assert not Path(state).exists(), "license entry unexpectedly wrote state"
print("Fresh and resumed license entry passed without state mutation.")
