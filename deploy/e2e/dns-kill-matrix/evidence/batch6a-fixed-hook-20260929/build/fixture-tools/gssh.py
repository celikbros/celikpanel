#!/usr/bin/env python3
# usage: gssh.py ROOT CELL NODE REMOTE_COMMAND  (stdout/stderr passthrough)
import sys, subprocess
from pathlib import Path
sys.path.insert(0, "/root/cp-b6a-src/deploy/e2e/dns-kill-matrix")
import fixture, guest_bootstrap
root, cell, node, remote = sys.argv[1:5]
plan = fixture.load_cell_plan(Path(root).resolve(), cell)
base = guest_bootstrap.ssh_base(plan["nodes"][node], Path(root) / "id_ed25519")
sys.exit(subprocess.run(base + [remote]).returncode)
