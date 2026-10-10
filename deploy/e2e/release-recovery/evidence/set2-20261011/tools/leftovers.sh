#!/bin/bash
# set2 read-only: what this run created on the host, with sizes; QEMU and git config state.
date -u +%FT%TZ
du -sh /var/tmp/cp-set2-run /var/tmp/cp-set2-run/harness* /var/tmp/cp-release-drill-set2-* /var/tmp/cp-release-drill-rid-* /var/tmp/cp-upd1-build/20261009t030455z 2>/dev/null
ART=/var/tmp/cp-upd1-build/20261009t030455z/upd1-artifacts.json
python3 -c "import json,sys,os;d=json.load(open(sys.argv[1]));[print(os.path.dirname(d[r]['archive'])) for r in ('baseline','good','defective','startcheck','realstart') if r in d]" $ART | sort -u | while read d; do du -sh $d 2>/dev/null; done
echo "overlay disks still present in set2 labs:"; ls -la /var/tmp/cp-release-drill-set2-*/cells/*/*/overlay.qcow2 /var/tmp/cp-release-drill-rid-*/cells/*/*/overlay.qcow2 2>/dev/null || echo none
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null || echo "no dry-run lab"
pgrep -a qemu | cut -c1-100 || echo "no qemu process"
pgrep -af 'settings_writes_trial|request_identity_trial|run-set2|run-upd1|lab.py|watch.sh|tail -n 0 -F' | cut -c1-120 || echo "no set2 job process"
echo "git local config key list fingerprint: $(git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' config --list --local | sed 's/=.*//' | sort | md5sum)"
echo "HEAD $(git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse HEAD)"
find /var/tmp/cp-set2-run -name __pycache__ -type d | head -3
