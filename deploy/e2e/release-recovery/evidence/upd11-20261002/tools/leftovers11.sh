#!/bin/bash
# upd11 read-only: what this run created on the host, with sizes; QEMU and git config state.
date -u +%FT%TZ
du -sh /var/tmp/cp-upd11-run /var/tmp/cp-release-drill-upd11-* /var/tmp/cp-upd1-build/20261001t221215z /var/tmp/cp-upd1-build/20261001t221533z 2>/dev/null
for a in /var/tmp/cp-upd1-build/20261001t221215z/upd1-artifacts.json /var/tmp/cp-upd1-build/20261001t221533z/upd1-artifacts.json; do
  python3 -c "import json,sys,os;d=json.load(open(sys.argv[1]));[print(os.path.dirname(d[r]['archive'])) for r in ('baseline','good','defective','startcheck','realstart') if r in d]" $a
done | sort -u | while read d; do du -sh $d 2>/dev/null; done
echo "overlay disks still present in upd11 labs:"; ls /var/tmp/cp-release-drill-upd11-*/cells/*/*/overlay.qcow2 2>/dev/null || echo none
ls -d /var/tmp/cp-release-drill-upd11-dry 2>/dev/null || echo "no dry-run lab"
pgrep -a qemu | cut -c1-100 || echo "no qemu process"
git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' config --list --local | sed 's/=.*//' | sort | md5sum
git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse HEAD
