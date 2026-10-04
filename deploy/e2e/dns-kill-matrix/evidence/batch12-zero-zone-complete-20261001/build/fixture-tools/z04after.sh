SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
L=/var/tmp/cp-b12-1001/logs/z04-zero-committed-zl
C=pdns-switch__committed__after-write__paired-primary__peer-reachable; R=/var/tmp/cp-b12-1001/r1
grep -oE '"(verdict|status|step|lifecycle_status|outcome|job_status|job_error_code|job_error_detail|phase|catalog_serial|members|rc|exit)": ("[^"]*"|null|[0-9]+|\[[^]]*\])' $L/zone-lifecycle-recover.log | uniq | cut -c1-300 | head -60
echo ----
tail -c 1500 $L/zone-lifecycle-recover.log
echo ----
bash $SP/post12.sh z04-zero-committed-zl $C $R "2026-09-30 08:42:30" "2026-09-30 08:43:10" after-recover 2>&1 | cut -c1-600
