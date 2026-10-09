#!/bin/bash
L=/var/tmp/cp-set2-run/logs
echo "end=$(cat $L/cell-rid-arch.end 2>/dev/null) rc=$(cat $L/cell-rid-arch.rc 2>/dev/null)"
tail -n 4 $L/cell-rid-arch.out | cut -c1-300; tail -n 4 $L/cell-rid-arch.err | cut -c1-300
ev=$(ls -d /var/tmp/cp-release-drill-rid-arch-a/evidence/arch/upd1/*/ | tail -1)
ls $ev $ev/steps | tr '\n' ' '; echo
python3 - "$ev" <<'PY'
import json, sys, glob, os
for path in sorted(glob.glob(os.path.join(sys.argv[1], "steps", "*c0-*", "section.json"))):
    d = json.load(open(path))
    print(json.dumps(d.get("acme_isolation"))[:600])
for path in sorted(glob.glob(os.path.join(sys.argv[1], "steps", "*c0-*", "native", "*isolate*"))):
    print(open(path).read()[:900])
PY
pgrep -a qemu | cut -c1-60
