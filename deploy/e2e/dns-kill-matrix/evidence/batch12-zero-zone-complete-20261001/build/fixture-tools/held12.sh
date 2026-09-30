# usage: held12.sh SHORT CELL ROOT TAG -- read-only capture of a held run (primary checkpoint/ledger, secondary boot, rndc facts, host hold record)
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
SHORT=$1 CELL=$2 ROOT=$3 TAG=$4
LOG=/var/tmp/cp-b12-1001/logs/$SHORT
python3 $SP/gssh.py $ROOT $CELL debian13 "sudo bash -s $CELL" < $SP/z05held_remote.sh > $LOG/held-state-$TAG.txt 2>&1
python3 $SP/gssh.py $ROOT $CELL arch 'echo "wall=$(date -u +%FT%T.%NZ) boot_id=$(cat /proc/sys/kernel/random/boot_id)"' < /dev/null >> $LOG/held-state-$TAG.txt 2>&1
bash $SP/rndcfacts.sh $ROOT $CELL $LOG/secondary-rndc-prerequisite-$TAG.txt > /dev/null
CD=$(python3 -c 'import sys;sys.path.insert(0,"/root/cp-b12-src/deploy/e2e/dns-kill-matrix");import fixture;from pathlib import Path;print(fixture.load_cell_plan(Path(sys.argv[1]).resolve(),sys.argv[2])["cell_directory"])' $ROOT $CELL)
{ echo "== host cell dir $CD/fresh-primary-peer"; ls -la --time-style=full-iso $CD/fresh-primary-peer; sha256sum $CD/fresh-primary-peer/*; for f in $CD/fresh-primary-peer/zone-lifecycle-held.json; do echo "== $f"; cat $f; echo; done; } >> $LOG/held-state-$TAG.txt 2>&1
grep -E 'wall=|result.json present|reboot-checkpoint-1.json$|^[0-9a-f]{64}  /var/lib' $LOG/held-state-$TAG.txt
