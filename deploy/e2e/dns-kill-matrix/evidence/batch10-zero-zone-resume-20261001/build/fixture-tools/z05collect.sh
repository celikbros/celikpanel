# Cell 2: 150 s capture hold (samplers running, read-only), stop the ledger sampler, then the batch 9 collect (renamed) into evidence/z05-held-resume. Guests stay up (run held).
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b10
ROOT=/var/tmp/cp-b10-1001/r1; CELL=pdns-switch__target-started__after-write__paired-primary__peer-reachable
LOG=/var/tmp/cp-b10-1001/logs/z05-held-resume
echo "### $(date -u +%FT%T.%3NZ) capture hold 150s before collect (read-only; samplers running; run still held)" >> $LOG/driver.log
sleep 150
echo "### $(date -u +%FT%T.%3NZ) stop ledwatch; collect" >> $LOG/driver.log
python3 $SP/gssh.py $ROOT $CELL debian13 'sudo touch /var/tmp/cp-b10-ledwatch.stop; sleep 1; sudo tail -n 2 /var/tmp/cp-b10-watch-led/timeline.log | cut -c1-200' < /dev/null >> $LOG/driver.log 2>&1
bash $SP/collect.sh z05-held-resume $CELL $ROOT
bash $SP/z05held.sh after-collect
echo "### $(date -u +%FT%T.%3NZ) collect done (guests left running; run held)" >> $LOG/driver.log
tail -n 5 $LOG/collect.log
grep -E 'result.json present|^[0-9a-f]{64}  /var/lib|boot_id=' $LOG/held-state-after-collect.txt
