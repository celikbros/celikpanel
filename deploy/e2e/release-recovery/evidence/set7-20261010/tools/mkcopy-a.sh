#!/bin/bash
# set7: run copy a = the pristine archive of 2a0af8866 (no overlay file); the builds run from it.
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set7-run; L=$R/logs
mkdir -p $L
bash $J/mkov.sh a > $L/mkov-a.txt 2>&1 || { tail -n 5 $L/mkov-a.txt; exit 1; }
head -n 8 $L/mkov-a.txt
du -sh $R/harness-a | cut -f1
bash $J/bg.sh build $J/job-build.sh
