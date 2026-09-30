# z04: read-only pre-state, then (only if the Agent's code is dns_peer_enrollment_required) the owner enrollment and the README resume command
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
L=/var/tmp/cp-b12-1001/logs/z04-zero-committed-zl
C=pdns-switch__committed__after-write__paired-primary__peer-reachable; R=/var/tmp/cp-b12-1001/r1
grep -oE '"(job_error_code|job_error_detail|next_step|lifecycle_status|status|step)": ("[^"]*"|null)' $L/run-prepared.log | tail -12 | cut -c1-600
if ! grep -q '"job_error_code": "dns_peer_enrollment_required"' $L/run-prepared.log; then echo "NOT dns_peer_enrollment_required: stop for review"; exit 1; fi
bash $SP/z04pre.sh > $L/z04pre.out 2>&1
echo "### $(date -u +%FT%T.%3NZ) enrollment start" >> $L/driver.log
bash $SP/enroll12.sh z04-zero-committed-zl $C $R
grep -E 'DIGEST|ENROLL-DONE|"status"|rc=' $L/enroll.log | cut -c1-200 | tail -12
bash $SP/recover12.sh z04-zero-committed-zl $C $R
cat $L/zone-lifecycle-recover.rc
