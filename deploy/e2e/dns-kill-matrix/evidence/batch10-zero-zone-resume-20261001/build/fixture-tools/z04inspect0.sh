# Read-only: on-disk state of the kept batch 9 z04 cell before booting
C=/var/tmp/cp-b9-1001/r2/cells/7392457812ac089a866de6ec
date -u +%FT%T.%NZ
ls -la --time-style=full-iso $C $C/debian13 $C/arch $C/fresh-primary-peer $C/debian13/* $C/arch/* 2>&1
python3 -c 'import json,sys;p=json.load(open(sys.argv[1]));print(p.get("cell_id"));print(json.dumps({k:{kk:(vv if kk!="qemu_command" else " ".join(vv)) for kk,vv in v.items() if kk in ("paths","management","qemu_command")} for k,v in p["nodes"].items()},indent=1)[:6000])' $C/fixture-plan.json
pgrep -a qemu || echo "no qemu"
