# Cell 1: stop the read-only ledger sampler, then the batch 9 collect (renamed cp-b10) into /var/tmp/cp-b10-1001/evidence/z04-resume
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b10
ROOT=/var/tmp/cp-b9-1001/r2; CELL=pdns-switch__committed__after-write__paired-primary__peer-reachable
LOG=/var/tmp/cp-b10-1001/logs/z04-resume
echo "### $(date -u +%FT%T.%3NZ) stop ledwatch; collect" >> $LOG/driver.log
python3 $SP/gssh.py $ROOT $CELL debian13 'sudo touch /var/tmp/cp-b10-ledwatch.stop; sleep 1; sudo tail -n 2 /var/tmp/cp-b10-watch-led/timeline.log | cut -c1-200' < /dev/null >> $LOG/driver.log 2>&1
bash $SP/collect.sh z04-resume $CELL $ROOT
echo "### $(date -u +%FT%T.%3NZ) collect done" >> $LOG/driver.log
tail -n 30 $LOG/collect.log
