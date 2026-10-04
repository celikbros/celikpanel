#!/bin/bash
# upd6: harness run copy from the pinned commit (never the working tree).
set -euo pipefail
R=/var/tmp/cp-upd6-run
[ -e $R ] && { echo "refusing: $R exists"; exit 2; }
mkdir -p $R/harness $R/logs $R/jobs $R/build
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
git -c safe.directory='*' -C "$REPO" rev-parse '6cda60b8^{commit}' > $R/harness-commit.txt
git -c safe.directory='*' -C "$REPO" archive 6cda60b8 | tar -x -C $R/harness
cd $R/harness && find . -type f -print0 | sort -z | xargs -0 sha256sum > $R/runcopy-files.sha256
wc -l $R/runcopy-files.sha256; cat $R/harness-commit.txt
