SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
L=/var/tmp/cp-b12-1001/logs/z05-zero-started-zl-rb
C=pdns-switch__target-started__after-write__paired-primary__peer-reachable; R=/var/tmp/cp-b12-1001/r2
bash $SP/z05post0.sh > $L/z05post0.out 2>&1
cut -c1-400 $L/z05post0.out | head -30
if ! grep -q '"job_error_code": "dns_peer_enrollment_required"' $L/run-prepared.log; then echo "NOT dns_peer_enrollment_required: stop for review"; exit 1; fi
echo "### $(date -u +%FT%T.%3NZ) enrollment start (no reboot)" >> $L/driver.log
timeout 300 bash $SP/enroll12.sh z05-zero-started-zl-rb $C $R < /dev/null
grep -E 'DIGEST|ENROLL-DONE' $L/enroll.log
