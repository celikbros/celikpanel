#!/bin/bash
set -euo pipefail
R=/var/tmp/cp-upd3-run
[ -e $R ] && { echo "refusing: $R exists"; exit 2; }
mkdir -p $R/harness $R/logs $R/side $R/tools
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
git -c safe.directory='*' -C "$REPO" rev-parse 94be6b6e^{commit} > $R/harness-commit.txt
git -c safe.directory='*' -C "$REPO" archive 94be6b6e | tar -x -C $R/harness
cd $R/harness && find . -type f -print0 | sort -z | xargs -0 sha256sum > $R/runcopy-files.sha256
wc -l $R/runcopy-files.sha256; cat $R/harness-commit.txt
