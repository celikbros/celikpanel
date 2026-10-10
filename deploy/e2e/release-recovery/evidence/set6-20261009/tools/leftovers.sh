#!/bin/bash
# set6 read-only: what this run created on the WSL host, with sizes; QEMU, job, mount and git state.
date -u +%FT%TZ
R=/var/tmp/cp-set6-run
echo "--- created by this run and left on the WSL host (du -sh)"
du -sh $R /var/tmp/cp-release-drill-s6-* 2>/dev/null
for b in cur a81; do du -sh "$(dirname "$(cat $R/artifacts-$b.path 2>/dev/null)")" 2>/dev/null; done
echo "--- parts of the run directory"
du -sh $R/harness-* $R/overlay-* $R/logs $R/build 2>/dev/null
echo "--- dist directories of this run's builds (the reused one is marked)"
for b in cur a81; do
python3 -I -c "import json,sys,os;d=json.load(open(sys.argv[1]));[print(os.path.dirname(d[r]['archive']) + ('   (built by set3, reused read-only, not of this run)' if d[r].get('reused_dist') else '   (built by this run, build ' + sys.argv[2] + ', role ' + r + ')')) for r in ('baseline','good','defective','startcheck','realstart') if r in d]" "$(cat $R/artifacts-$b.path)" $b
done | sort -u | while read -r d rest; do echo "$(du -sh "$d" 2>/dev/null | cut -f1) $d $rest; src directory present: $([ -d "$d/src" ] && echo yes || echo no)"; done
echo "--- every entry of /var/tmp/cp-pair-accept/dist and /var/tmp/cp-upd1-build"
ls -la /var/tmp/cp-pair-accept/dist /var/tmp/cp-upd1-build
echo "--- overlay disks still present in this run's labs"
ls -la /var/tmp/cp-release-drill-s6-*/cells/*/*/overlay.qcow2 2>/dev/null || echo none
echo "--- base images still present in this run's labs"
ls -la /var/tmp/cp-release-drill-s6-*/images/* 2>/dev/null || echo none
echo "--- tmpfs mounts under /var/tmp"
findmnt -rn -t tmpfs -o TARGET,SIZE,USED | grep '^/var/tmp/' || echo none
echo "--- image cache (not of this run; link counts)"
stat -c '%h links %s bytes %n' /var/tmp/cp-v3n28/images/*
echo "--- dry-run labs"
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null || echo "no dry-run lab"
echo "--- processes"
q=$(pgrep -a qemu | cut -c1-100); echo "${q:-no qemu process}"
p=$(pgrep -af 'set6_trial|owner_update_trial|run-set6|run-upd1|lab.py|set6/queue.sh|build-upd1|build-dist' | grep -v pgrep | cut -c1-140); echo "${p:-no job process of this run}"
h=$(pgrep -af 'set6/hold.sh' | grep -v pgrep | cut -c1-140); echo "${h:-no held session of this run}"
echo "--- listening ports of the labs (46xx-52xx / 186xx-192xx)"
ss -ltn 2>/dev/null | grep -E ':(4[6-9][0-9]{2}|5[0-2][0-9]{2}|18[6-9][0-9]{2}|19[0-2][0-9]{2})\b' || echo none
echo "--- repository"
echo "HEAD $(git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse HEAD) branch $(git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse --abbrev-ref HEAD)"
echo "__pycache__ under the run directory: $(find $R -name __pycache__ -type d | wc -l)"
echo "--- WSL disk"; df -h /var/tmp | tail -1
echo "--- memory"; free -m | sed -n 1,3p
echo "--- kept, not of this run (listed only)"
ls -d /var/tmp/cp-install-vm /var/tmp/cp-v3n28 /var/tmp/cp-pair-accept/work /var/tmp/cp-pair-accept/dist/a0beb7263d1f4ca72258f6b306f9111ba4e2a334-acceptance-license 2>&1
echo "labs under /var/tmp: $(ls -d /var/tmp/cp-release-drill-* 2>/dev/null | wc -l) (of this run: $(ls -d /var/tmp/cp-release-drill-s6-* 2>/dev/null | wc -l)); entries in /var/tmp: $(ls /var/tmp | wc -l)"
