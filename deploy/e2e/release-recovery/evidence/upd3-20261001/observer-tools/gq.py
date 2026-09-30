#!/usr/bin/env python3
"""Read-only guest query through the lab's guarded SSH (observer only).

usage: gq.py LABNAME NODE SCRIPTFILE OUTFILE
The script file must contain read-only commands only (journalctl, cat, ls, stat, systemctl show).
"""
import sys
from pathlib import Path
sys.path.insert(0, "/var/tmp/cp-upd3-run/harness/deploy/e2e/release-recovery")
import lab  # noqa: E402

name, node, script, outfile = sys.argv[1:5]
root = lab.checked_root(f"/var/tmp/cp-release-drill-{name}")
record, plan = lab.load(root)
body = "set +eu\nexec 2>&1\n" + Path(script).read_text()
res = lab.guarded_script(root, record, plan, node, body, timeout=120)
Path(outfile).parent.mkdir(parents=True, exist_ok=True)
Path(outfile).write_text(f"# read-only query {Path(script).name} lab={name} node={node} rc={res.returncode}\n" + res.stdout)
print(outfile)
