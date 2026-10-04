#!/bin/bash
# upd13 read-only: what this run created on the host, with sizes; QEMU and git config state.
date -u +%FT%TZ
du -sh /var/tmp/cp-upd13-run /var/tmp/cp-upd13-run/harness /var/tmp/cp-upd13-run/harness-h22 /var/tmp/cp-release-drill-upd13-* /var/tmp/cp-upd1-build/20261002t203047z /var/tmp/cp-upd1-build/20261002t203514z 2>/dev/null
for a in /var/tmp/cp-upd1-build/20261002t203047z/upd1-artifacts.json /var/tmp/cp-upd1-build/20261002t203514z/upd1-artifacts.json; do
  python3 -c "import json,sys,os;d=json.load(open(sys.argv[1]));[print(os.path.dirname(d[r]['archive'])) for r in ('baseline','good','defective','startcheck','realstart') if r in d]" $a
done | sort -u | while read d; do du -sh $d 2>/dev/null; done
echo "overlay disks still present in upd13 labs:"; ls /var/tmp/cp-release-drill-upd13-*/cells/*/*/overlay.qcow2 2>/dev/null || echo none
ls -d /var/tmp/cp-release-drill-upd13-dry 2>/dev/null || echo "no dry-run lab"
pgrep -a qemu | cut -c1-100 || echo "no qemu process"
echo "git local config key list fingerprint: $(git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' config --list --local | sed 's/=.*//' | sort | md5sum)"
echo "HEAD $(git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse HEAD)"
find /var/tmp/cp-upd13-run -name __pycache__ -type d | head -3
