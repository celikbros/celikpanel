# usage: trig.sh SHORT CELL -- read-only: trigger/transcript events of a cell that stopped early
E=/var/tmp/cp-b8r-1001/evidence/$1
R=$E/raw/results/$2
ls -la $R
python3 - $R <<'PY'
import json, sys, glob
R = sys.argv[1]
for f in sorted(glob.glob(R + "/transcript*.jsonl")):
    for line in open(f):
        try: e = json.loads(line)
        except Exception: continue
        ev = e.get("event") or e.get("type")
        s = json.dumps(e, sort_keys=True)
        if any(x in s for x in ("trigger", "gate", "error", "refus", "exit")):
            print(s[:1500])
PY
grep -iE 'trigger|refus|error' $E/run-prepared.log | cut -c1-1500 | head -20
