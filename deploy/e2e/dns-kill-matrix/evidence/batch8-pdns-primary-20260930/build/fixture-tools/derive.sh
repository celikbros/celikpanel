# Derived, read-only files per cell inside /var/tmp/cp-b8-0930/evidence/<short>: boundary-window.txt, cell-digest.txt,
# gate-probe.json (verbatim result.json sub-object), timeline-summary.txt
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch8
while read s c; do
  E=/var/tmp/cp-b8-0930/evidence/$s
  [ -d $E ] || { echo "$s: no evidence"; continue; }
  R=$E/raw/results/$c/result.json
  if [ -f $R ]; then
    python3 $SP/boundcheck.py $E $c > $E/boundary-window.txt 2>&1
    python3 $SP/table8.py $s > $E/cell-digest.txt 2>&1
    python3 -c 'import json,sys;r=json.load(open(sys.argv[1]));print(json.dumps({"source":"result.json gate_probe (verbatim)","gate_probe":r.get("gate_probe")},indent=2,sort_keys=True))' $R > $E/gate-probe.json
  fi
  python3 $SP/tlsum.py $E > $E/timeline-summary.txt 2>&1
  echo "$s: $(head -1 $E/boundary-window.txt)"
done < $SP/cells.txt
