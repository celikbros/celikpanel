#!/bin/bash
# usage: snapshot-host.sh TAG -> names and mtimes of /var/tmp and /root top level into the scratchpad (host read-only)
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd6run
find /var/tmp -mindepth 1 -maxdepth 1 -printf '%f %TY-%Tm-%Td %TH:%TM\n' | LC_ALL=C sort > $P/host-vartmp-$1.txt
find /root -mindepth 1 -maxdepth 1 -printf '%f %TY-%Tm-%Td %TH:%TM\n' | LC_ALL=C sort > $P/host-root-$1.txt
ls /var/tmp/cp-pair-accept/dist | LC_ALL=C sort > $P/host-pairdist-$1.txt
ls /var/tmp/cp-upd1-build | LC_ALL=C sort > $P/host-upd1build-$1.txt
git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' config --list --show-origin > $P/host-gitconfig-$1.txt 2>&1
df -BG /var/tmp | tail -1 > $P/host-df-$1.txt
wc -l $P/host-*-$1.txt; cat $P/host-df-$1.txt
