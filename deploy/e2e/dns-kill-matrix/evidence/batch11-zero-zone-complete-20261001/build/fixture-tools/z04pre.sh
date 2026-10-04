# Cell 1, read-only after run-prepared (pending delete), before the owner enrollment
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b11
ROOT=/var/tmp/cp-b11-1001/r1; CELL=pdns-switch__committed__after-write__paired-primary__peer-reachable
LOG=/var/tmp/cp-b11-1001/logs/z04-zero-committed-zl
echo "### $(date -u +%FT%T.%3NZ) read-only state after run-prepared (pending delete), before enrollment" >> $LOG/driver.log
python3 $SP/gssh.py $ROOT $CELL debian13 'sudo bash -s' < $SP/state_remote.sh > $LOG/primary-state-after-run.txt 2>&1
bash $SP/rndcfacts.sh $ROOT $CELL $LOG/secondary-rndc-prerequisite-before-enroll.txt > /dev/null
sed -n '/zone-sync ledger/,/agent-private/p' $LOG/primary-state-after-run.txt | cut -c1-400
grep -iE 'zone|peer|pending|reason' $LOG/primary-state-after-run.txt | grep agent | cut -c1-600 | tail -8
grep -E 'rndc.key|953' $LOG/secondary-rndc-prerequisite-before-enroll.txt
