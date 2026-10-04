# Read-only: condensed watcher timeline (every change of journal phase, pdns state/MainPID, agent MainPID, killpid) and
# every CHANGE/POINTER line, per watcher directory. usage: tlsum.py EVIDENCE_DIR
import sys, glob, os, re
E = sys.argv[1]
for t in sorted(glob.glob(f"{E}/raw/watch/*/timeline.log")):
    print(f"######## {os.path.relpath(t, E)}")
    last = None
    for line in open(t, errors="replace"):
        line = line.rstrip("\n")
        if " CHANGE " in line or " POINTER " in line or line == "done":
            print(line[:300]); continue
        m = re.match(r"(\S+) pdns=(\S*) named=(\S*) agent=(\S*) jphase=(\S*) killpid=(\S*)", line)
        if not m:
            continue
        key = (m.group(2), m.group(4), m.group(5), m.group(6))
        if key != last:
            print(f"{m.group(1)} pdns(MainPID/NRestarts/ActiveState/SubState)={m.group(2)} agent(MainPID/ActiveState)={m.group(4)} jphase={m.group(5)} killpid={m.group(6)}")
            last = key
