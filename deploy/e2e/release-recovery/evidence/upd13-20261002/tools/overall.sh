#!/bin/bash
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd13-20261002"
cd "$S" && for r in upd1-*/run-* part2-alpha80/*/run-*; do [ -f "$r/result.json" ] || continue; python3 -c "
import json,sys;d=json.load(open(sys.argv[1]+'/result.json'));o=d.get('outcome') or {};k=((d.get('kind') or {}).get('judged') or {})
bad=[(s['name'],s['verdict'],(s.get('reason') or '')[:160]) for s in d['steps'] if s['verdict'] in ('failed','inconclusive')]
print(sys.argv[1],d.get('overall'),o.get('classification'),'kind=',k.get('verdict'),'native=',d.get('native_evidence'),bad)" "$r"; done
