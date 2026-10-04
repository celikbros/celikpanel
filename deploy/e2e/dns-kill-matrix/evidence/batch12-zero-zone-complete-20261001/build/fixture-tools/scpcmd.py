#!/usr/bin/env python3
# usage: scpcmd.py ROOT CELL NODE  -> prints the fixture's scp base command
import sys, shlex
from pathlib import Path
sys.path.insert(0, "/root/cp-b12-src/deploy/e2e/dns-kill-matrix")
import fixture, guest_bootstrap
root, cell, node = sys.argv[1:4]
plan = fixture.load_cell_plan(Path(root).resolve(), cell)
print(shlex.join(guest_bootstrap.scp_base(plan["nodes"][node], Path(root) / "id_ed25519")))
