SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b11
L=/var/tmp/cp-b11-1001/logs/z05-zero-started-zl-rb
grep -oE '"(status|combined_exit|guest_controller_exit|guest_controller_suspended_for_zone_lifecycle|catalog_serial|rndc_status_ok|verdict|outcome|job_error_code|next_step|step)": ("[^"]*"|[0-9a-z]+)' $L/run-prepared.log | tr '\n' ' ' | fold -w 300; echo
tail -n 3 $L/run-prepared.log | cut -c1-1500
grep -o '"owner_prepared_rndc_key": "[a-z]*"' $L/peer-prepare.log
bash $SP/held11.sh z05-zero-started-zl-rb pdns-switch__target-started__after-write__paired-primary__peer-reachable /var/tmp/cp-b11-1001/r2 before-enroll
