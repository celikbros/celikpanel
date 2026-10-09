#!/bin/bash
# set4b: the build job's result and the artifacts document it wrote.
R=/var/tmp/cp-set4b-run; L=$R/logs
echo "rc=$(cat $L/build.rc 2>/dev/null) start=$(cat $L/build.start) end=$(cat $L/build.end 2>/dev/null)"
tail -n 3 $L/build.out
tail -n 2 $L/build-c1.out
grep -c . $L/build-c1.err
grep -E "BUILD-UPD1|refus|error" $L/build-c1.err | head -5
ART=$(grep -E '^/var/tmp/cp-upd1-build/.*upd1-artifacts.json$' $L/build-c1.out | tail -n 1)
echo "ART=$ART"
[ -n "$ART" ] && echo "$ART" > $R/artifacts-c1.path && python3 -c "
import json,sys
d=json.load(open(sys.argv[1]))
print('source_head', d['source_head'])
for r in ('baseline','good','defective','startcheck','realstart'):
    if r in d: print(r, d[r]['version'], d[r]['sequence'], d[r]['commit'], d[r]['tree'], d[r]['sha256'], d[r]['license_mode'])
" $ART
