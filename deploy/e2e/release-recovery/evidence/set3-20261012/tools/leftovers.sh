#!/bin/bash
# set3 read-only: what this run created on the host, with sizes; QEMU, job and git state.
date -u +%FT%TZ
du -sh /var/tmp/cp-set3-run /var/tmp/cp-release-drill-set3-* /var/tmp/cp-release-drill-rid3-* /var/tmp/cp-release-drill-u14-* /var/tmp/cp-upd1-build/20261009t061555z /var/tmp/cp-upd1-build/20261009t064831z /var/tmp/cp-upd1-build/20261009t070418z 2>/dev/null
for ART in /var/tmp/cp-upd1-build/20261009t061555z/upd1-artifacts.json /var/tmp/cp-upd1-build/20261009t064831z/upd1-artifacts.json /var/tmp/cp-upd1-build/20261009t070418z/upd1-artifacts.json; do
  python3 -c "import json,sys,os;d=json.load(open(sys.argv[1]));[print(os.path.dirname(d[r]['archive'])) for r in ('baseline','good','defective','startcheck','realstart') if r in d]" $ART
done | sort -u | while read d; do du -sh $d 2>/dev/null; done
echo "overlay disks still present in this run's labs:"
ls -la /var/tmp/cp-release-drill-set3-*/cells/*/*/overlay.qcow2 /var/tmp/cp-release-drill-rid3-*/cells/*/*/overlay.qcow2 /var/tmp/cp-release-drill-u14-*/cells/*/*/overlay.qcow2 2>/dev/null || echo none
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null || echo "no dry-run lab"
pgrep -a qemu | cut -c1-100 || echo "no qemu process"
pgrep -af 'settings_writes_trial|request_identity_trial|owner_update_trial|run-set2|run-upd1|lab.py|queue.sh|hold.sh' | cut -c1-120 || echo "no job process of this run"
echo "git local config key list fingerprint: $(git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' config --list --local | sed 's/=.*//' | sort | md5sum)"
echo "HEAD $(git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse HEAD)"
find /var/tmp/cp-set3-run -name __pycache__ -type d | head -3
