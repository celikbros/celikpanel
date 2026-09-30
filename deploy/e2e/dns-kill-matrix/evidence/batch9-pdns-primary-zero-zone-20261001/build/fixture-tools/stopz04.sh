SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b9
bash $SP/note.sh "z04 RUN_PREPARED_RC=2: parentless delete pending (dns_peer_enrollment_required); owner enrollment done (enroll.log); zone-lifecycle --recover-delete exit 2, trigger refused_not_pending before any mutation (harness phase check uses s1-kill.test); second collect in evidence/z04-zero-committed-zl/rec; guests stopped, overlays kept"
bash $SP/stoponly.sh z04-zero-committed-zl pdns-switch__committed__after-write__paired-primary__peer-reachable /var/tmp/cp-b9-1001/r2 >> /var/tmp/cp-b9-1001/logs/chain.log 2>&1
tail -12 /var/tmp/cp-b9-1001/logs/chain.log
