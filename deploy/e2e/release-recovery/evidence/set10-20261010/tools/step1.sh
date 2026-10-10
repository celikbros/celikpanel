#!/bin/bash
# set10: the run directory, a read-only host check, and run copy a (the pristine archive of cd46ca595).
R=/var/tmp/cp-set10-run
T=<scratchpad>/set10/tools
[ -e $R ] && { echo "refusing: $R exists"; exit 2; }
mkdir -p $R/logs $R/build $R/host
echo "$(date -u +%FT%TZ) set10 run directory created" >> $R/progress.txt
bash $T/hostcheck.sh > $R/host/hostcheck-before.txt 2>&1
sha256sum /var/tmp/cp-pair-accept/dist/a0beb7263d1f4ca72258f6b306f9111ba4e2a334-acceptance-license/*.tar.gz >> $R/host/hostcheck-before.txt 2>&1
ls -la /var/tmp/cp-pair-accept/dist/a0beb7263d1f4ca72258f6b306f9111ba4e2a334-acceptance-license/ >> $R/host/hostcheck-before.txt 2>&1
git -c safe.directory='*' -C '<repo>' rev-parse cd46ca595 'cd46ca595^{tree}' HEAD >> $R/host/hostcheck-before.txt
bash $T/mkov.sh a > $R/logs/mkov-a.txt 2>&1; echo "mkov a rc=$?"; head -n 8 $R/logs/mkov-a.txt
tail -n 30 $R/host/hostcheck-before.txt
