#!/usr/bin/env bash
echo "--- dist sizes (newest 8)"; ls -dt /var/tmp/cp-pair-accept/dist/* | head -8 | xargs du -sh
echo "--- a build dir"; du -sh /var/tmp/cp-upd1-build/20261009t143202z/* 2>/dev/null
echo "--- set4b run"; ls /var/tmp/cp-set4b-run; du -sh /var/tmp/cp-set4b-run/* | sort -h | tail -8
cat /var/tmp/cp-set4b-run/progress.txt 2>/dev/null | tail -30
echo "--- set4b labs"; ls -d /var/tmp/cp-release-drill-s4b* 2>/dev/null; du -sh /var/tmp/cp-release-drill-s4b* 2>/dev/null
echo "--- artifacts of 143202z"; python3 -c "
import json;d=json.load(open('/var/tmp/cp-upd1-build/20261009t143202z/upd1-artifacts.json'))
print(d['source_head']);[print(r,d[r]['version'],d[r]['commit'],d[r]['sha256'][:16]) for r in ('baseline','good','defective','startcheck','realstart') if r in d]; print(d.get('baseline_ref'))"
