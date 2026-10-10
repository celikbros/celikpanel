#!/bin/bash
# set4b read-only: what this run created on the host, with sizes; QEMU, job and git state.
date -u +%FT%TZ
du -sh /var/tmp/cp-set4b-run /var/tmp/cp-release-drill-s4b-* /var/tmp/cp-upd1-build/20261009t143202z 2>/dev/null
echo "dist directories this run's build made:"
python3 -c "import json,sys,os;d=json.load(open(sys.argv[1]));[print(os.path.dirname(d[r]['archive'])) for r in ('baseline','good','defective','startcheck','realstart') if r in d]" /var/tmp/cp-upd1-build/20261009t143202z/upd1-artifacts.json | sort -u | while read d; do echo "$(du -sh $d 2>/dev/null | cut -f1) $d"; done
echo "overlay disks still present in this run's labs:"
ls -la /var/tmp/cp-release-drill-s4b-*/cells/*/*/overlay.qcow2 2>/dev/null || echo none
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null || echo "no dry-run lab"
pgrep -a qemu | cut -c1-100 || echo "no qemu process"
pgrep -af 'set4b_trial|set4_trial|run-set4b|run-upd1|lab.py' | grep -v pgrep | cut -c1-120 || echo "no job process of this run"
echo "working repository HEAD $(git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse HEAD)"
echo "candidate commit present in the working repository: $(git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' cat-file -e de34e44859d49039972ae2e1f9f42ba12d9e1595 2>/dev/null && echo yes || echo no)"
echo "__pycache__ under the run directory: $(find /var/tmp/cp-set4b-run -name __pycache__ -type d | wc -l)"
echo "directories of earlier runs (not touched): $(ls -d /var/tmp/cp-set1-run /var/tmp/cp-set2-run /var/tmp/cp-set3-run /var/tmp/cp-set4-run 2>/dev/null | tr '\n' ' ')"
