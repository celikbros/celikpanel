#!/bin/bash
# set5 read-only: what this run created on the WSL host, with sizes; QEMU, job, mount and git state.
date -u +%FT%TZ
R=/var/tmp/cp-set5-run
ART=$(cat $R/artifacts.path 2>/dev/null)
echo "--- created by this run and left on the WSL host (du -sh)"
du -sh $R /var/tmp/cp-release-drill-s5-* "$(dirname "$ART")" 2>/dev/null
echo "--- parts of the run directory"
du -sh $R/harness-* $R/overlay-* $R/logs $R/build $R/ramstage 2>/dev/null
echo "--- dist directories of this run's build (the reused one is marked)"
python3 -I -c "import json,sys,os;d=json.load(open(sys.argv[1]));[print(os.path.dirname(d[r]['archive']) + ('   (built by set3, reused read-only, not of this run)' if d[r].get('reused_dist') else '   (built by this run; its src directory was removed after the proof)')) for r in ('baseline','good','defective','startcheck','realstart') if r in d]" "$ART" | sort -u | while read -r d rest; do echo "$(du -sh "$d" 2>/dev/null | cut -f1) $d $rest"; done
echo "--- overlay disks still present in this run's labs"
ls -la /var/tmp/cp-release-drill-s5-*/cells/*/*/overlay.qcow2 2>/dev/null || echo none
echo "--- base images still present in this run's labs"
ls -la /var/tmp/cp-release-drill-s5-*/images/* 2>/dev/null || echo none
echo "--- tmpfs mounts of this run's labs"
findmnt -rn -t tmpfs -o TARGET,SIZE,USED | grep '^/var/tmp/cp-' || echo none
echo "--- image cache (not of this run; link counts)"
stat -c '%h links %s bytes %n' /var/tmp/cp-v3n28/images/*
echo "--- dry-run labs"
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null || echo "no dry-run lab"
echo "--- processes"
q=$(pgrep -a qemu | cut -c1-100); echo "${q:-no qemu process}"
p=$(pgrep -af 'set5_trial|owner_update_trial|run-set5|run-upd1|lab.py|set5/queue.sh|set5/hold.sh|job-memwatch' | grep -v pgrep | cut -c1-140); echo "${p:-no job process of this run}"
echo "--- listening ports of the labs (46xx / 186xx)"
ss -ltn 2>/dev/null | grep -E ':(46[0-9]{2}|47[0-9]{2}|48[0-9]{2}|186[0-9]{2}|187[0-9]{2}|188[0-9]{2})\b' || echo none
echo "--- repository"
echo "git local config key list fingerprint: $(git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' config --list --local | sed 's/=.*//' | sort | md5sum)"
echo "HEAD $(git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse HEAD) branch $(git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse --abbrev-ref HEAD)"
echo "__pycache__ under the run directory: $(find $R -name __pycache__ -type d | wc -l)"
echo "--- WSL disk"; df -h /var/tmp | tail -1
echo "--- memory"; free -m | sed -n 1,3p
echo "--- directories of earlier runs (not touched; listed only)"
ls -d /var/tmp/cp-set1-run /var/tmp/cp-set2-run /var/tmp/cp-set3-run /var/tmp/cp-set4-run /var/tmp/cp-set4b-run 2>/dev/null | tr '\n' ' '; echo
echo "labs under /var/tmp: $(ls -d /var/tmp/cp-release-drill-* 2>/dev/null | wc -l) (of this run: $(ls -d /var/tmp/cp-release-drill-s5-* 2>/dev/null | wc -l)); entries in /var/tmp: $(ls /var/tmp | wc -l)"
