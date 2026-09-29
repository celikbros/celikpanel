# Read-only: verbatim owner-facing texts per cell (status reads, release messages, owner command output)
import json, os, re
ROOT = "/var/tmp/cp-b8r-1001/evidence"
for line in open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "cells.txt")):
    s, c = line.split()
    E = f"{ROOT}/{s}"
    rp = f"{E}/raw/results/{c}/result.json"
    print(f"######## {s}")
    if os.path.exists(rp):
        r = json.load(open(rp))
        for st, v in (r.get("recovery_status_reads") or {}).items():
            cm = v.get("command") or {}
            print(f"--- status read {st} rc={cm.get('returncode')}\n{cm.get('output')}")
    op = f"{E}/owner-post-state.txt"
    if os.path.exists(op):
        t = open(op, errors="replace").read()
        m = re.search(r"== recovery dns-switch-status --quiesced.*?\n(.*?)STATUS_RC=(\d+)", t, re.S)
        if m:
            print(f"--- status read post-collect rc={m.group(2)}\n{m.group(1)}")
