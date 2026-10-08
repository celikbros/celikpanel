#!/bin/bash
# set1: harness run copy from the pinned commit (never the working tree); the overlay is added by mkov.sh.
set -euo pipefail
R=/var/tmp/cp-set1-run
C=c4cf7fd9
[ -e $R ] && { echo "refusing: $R exists"; exit 2; }
mkdir -p $R/harness $R/logs $R/jobs $R/build
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
git -c safe.directory='*' -C "$REPO" rev-parse "$C^{commit}" > $R/harness-commit.txt
git -c safe.directory='*' -C "$REPO" archive $C | tar -x -C $R/harness
cd $R/harness && find . -type f -print0 | sort -z | xargs -0 sha256sum > $R/runcopy-$C-files.sha256
wc -l $R/runcopy-$C-files.sha256; cat $R/harness-commit.txt
