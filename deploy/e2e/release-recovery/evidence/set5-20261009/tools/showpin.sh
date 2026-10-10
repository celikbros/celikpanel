#!/bin/bash
# set5 read-only: the pinning reading of a lab. usage: showpin.sh LAB NODE
d=$(ls -d /var/tmp/cp-release-drill-$1/evidence/$2/upd1/*/ | tail -1)
python3 -I -c "
import json,sys,glob
for f in sorted(glob.glob(sys.argv[1]+'steps/*set5-name-pinning*/*.json')):
    v=json.load(open(f)); print(f[len(sys.argv[1]):])
    print(json.dumps(v,sort_keys=True)[:2600])
" "$d"
