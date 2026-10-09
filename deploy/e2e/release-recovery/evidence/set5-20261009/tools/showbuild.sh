#!/bin/bash
R=/var/tmp/cp-set5-run; ART=$(cat $R/artifacts.path); B=$(dirname $ART)
python3 -I -c "
import json,sys
d=json.load(open(sys.argv[1]))
print('source_head',d['source_head']); print(json.dumps(d.get('baseline_ref'))); print(d['provenance'])
for r in ('baseline','good','defective','startcheck'):
    i=d[r]; print(r,i['version'],i['sequence'],i['commit'],i['tree'],i['sha256'],i.get('parent'),i.get('reused_dist'),i.get('license_mode'))
" $ART
head -c 1500 $B/baseline-ref-proof.txt; echo; grep -c identical $B/baseline-ref-proof.txt; grep -c DIFFERENT $B/baseline-ref-proof.txt; tail -n 3 $B/baseline-ref-proof.txt
git -C $B/repo for-each-ref --format='%(refname) %(objectname) %(subject)' refs/upd1
git -C $B/repo diff --stat 67b62cc0f $(git -C $B/repo rev-parse refs/upd1/good)
git -C $B/repo diff 67b62cc0f $(git -C $B/repo rev-parse refs/upd1/good)
cat $R/build/a81-prove.json | python3 -I -c "import json,sys;d=json.load(sys.stdin);print(type(d).__name__, list(d)[:12] if isinstance(d,dict) else len(d)); print(json.dumps(d)[:1200])"
cat $R/logs/build.start $R/logs/build.end
grep -h 'panel_build_tags' /var/tmp/cp-pair-accept/dist/a0beb7263d1f4ca72258f6b306f9111ba4e2a334-acceptance-license/dist.json
sha256sum /var/tmp/cp-pair-accept/dist/a0beb7263d1f4ca72258f6b306f9111ba4e2a334-acceptance-license/*.tar.gz
