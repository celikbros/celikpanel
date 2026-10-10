#!/bin/bash
# set8 step 1: progress file, run copies a and b, then the build in the background.
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
R=/var/tmp/cp-set8-run
mkdir -p $R/logs
[ -e $R/progress.txt ] || echo "$(date -u +%FT%TZ) set8 run directory created by this session (resumable progress file)" > $R/progress.txt
echo "set8-$(date -u +%Y%m%d)" > $R/evidence-name.txt
git -c safe.directory='*' -C "/mnt/c/CELIKBROS PROJECTS/celikpanel" rev-parse HEAD 2a0af8866 'v0.1.0-alpha.82^{commit}' 2a0af8866^{tree} > $R/logs/heads.txt
cat $R/logs/heads.txt
bash $J/mkcopy-a.sh || exit 1
bash $J/mkcopy-b.sh || exit 1
bash $J/bg.sh build $J/job-build.sh
