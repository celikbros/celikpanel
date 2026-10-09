#!/bin/bash
# set4 read-only: what this run created on the host, with sizes; QEMU, job and git state.
date -u +%FT%TZ
du -sh /var/tmp/cp-set4-run /var/tmp/cp-release-drill-s4-* /var/tmp/cp-upd1-build/20261009t101529z /var/tmp/cp-upd1-build/20261009t102025z 2>/dev/null
echo "dist directories of this run's builds (the reused one is marked):"
for ART in /var/tmp/cp-upd1-build/20261009t101529z/upd1-artifacts.json /var/tmp/cp-upd1-build/20261009t102025z/upd1-artifacts.json; do
  python3 -c "import json,sys,os;d=json.load(open(sys.argv[1]));[print(os.path.dirname(d[r]['archive']) + ('   (built by set3, reused, not of this run)' if d[r].get('reused_dist') else '')) for r in ('baseline','good','defective','startcheck','realstart') if r in d]" $ART
done | sort -u | while read d rest; do echo "$(du -sh $d 2>/dev/null | cut -f1) $d $rest"; done
echo "overlay disks still present in this run's labs:"
ls -la /var/tmp/cp-release-drill-s4-*/cells/*/*/overlay.qcow2 2>/dev/null || echo none
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null || echo "no dry-run lab"
pgrep -a qemu | cut -c1-100 || echo "no qemu process"
pgrep -af 'set4_trial|settings_writes_trial|request_identity_trial|owner_update_trial|run-set4|run-upd1|lab.py|set4/queue.sh' | grep -v pgrep | cut -c1-120 || echo "no job process of this run"
echo "git local config key list fingerprint: $(git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' config --list --local | sed 's/=.*//' | sort | md5sum)"
echo "HEAD $(git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse HEAD)"
echo "__pycache__ under the run directory: $(find /var/tmp/cp-set4-run -name __pycache__ -type d | wc -l)"
echo "directories of earlier runs (not touched): $(ls -d /var/tmp/cp-set1-run /var/tmp/cp-set2-run /var/tmp/cp-set3-run 2>/dev/null | tr '\n' ' ')"
