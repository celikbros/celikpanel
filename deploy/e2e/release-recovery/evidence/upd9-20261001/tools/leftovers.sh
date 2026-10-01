#!/bin/bash
# read-only: what this run created on the host, with sizes
date -u +%FT%TZ
du -sh /var/tmp/cp-upd9-run /var/tmp/cp-release-drill-upd9-* /var/tmp/cp-upd1-build/20261001t174240z /var/tmp/cp-upd1-build/20261001t174610z 2>/dev/null
for a in /var/tmp/cp-upd1-build/20261001t174240z/upd1-artifacts.json /var/tmp/cp-upd1-build/20261001t174610z/upd1-artifacts.json; do
  python3 -c "import json,sys,os;d=json.load(open(sys.argv[1]));[print(r, os.path.dirname(d[r]['archive'])) for r in ('baseline','good','defective','startcheck','realstart') if r in d]" $a
done | while read r d; do du -sh $d 2>/dev/null; done | sort -u
ls -d /var/tmp/cp-release-drill-upd9-dry 2>/dev/null || echo "no dry-run lab"
pgrep -a qemu | cut -c1-100 || echo "no qemu process"
df -BG /var/tmp | tail -1
git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' config --list --local | sed 's/=.*//' | sort | md5sum
