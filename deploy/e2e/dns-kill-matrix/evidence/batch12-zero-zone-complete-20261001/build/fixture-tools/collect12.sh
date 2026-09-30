# usage: collect12.sh SHORT CELL ROOT -- 150 s capture hold (samplers running, read-only), stop the ledger samplers, then the batch 10 collect (renamed)
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
SHORT=$1 CELL=$2 ROOT=$3
LOG=/var/tmp/cp-b12-1001/logs/$SHORT
echo "### $(date -u +%FT%T.%3NZ) capture hold 150s before collect (read-only; samplers running)" >> $LOG/driver.log
sleep 150
echo "### $(date -u +%FT%T.%3NZ) stop ledwatch; collect" >> $LOG/driver.log
python3 $SP/gssh.py $ROOT $CELL debian13 'sudo touch /var/tmp/cp-b12-ledwatch.stop; sleep 1; sudo sh -c "tail -n 2 /var/tmp/cp-b12-watch-led*/timeline.log" | cut -c1-200' < /dev/null >> $LOG/driver.log 2>&1
bash $SP/collect.sh $SHORT $CELL $ROOT
echo "### $(date -u +%FT%T.%3NZ) collect done" >> $LOG/driver.log
tail -n 40 $LOG/collect.log
