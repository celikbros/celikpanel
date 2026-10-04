SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
L=/var/tmp/cp-b12-1001/logs/z05-zero-started-zl-rb
C=pdns-switch__target-started__after-write__paired-primary__peer-reachable; R=/var/tmp/cp-b12-1001/r2
grep -oE '"(verdict|step|outcome|job_status|job_error_code|job_error_detail|combined_exit|guest_controller_exit|zone_lifecycle_before_reboot|resumed|status|lifecycle_status)": ("[^"]*"|null|true|false|[0-9]+|\{[^}]*\})' $L/resume.log | uniq | cut -c1-300 | head -60
echo ----; tail -c 2500 $L/resume.log; echo
echo ----; cat $L/boot-monitor.log | cut -c1-200
CD=$(python3 -c 'import sys;sys.path.insert(0,"/root/cp-b12-src/deploy/e2e/dns-kill-matrix");import fixture;from pathlib import Path;print(fixture.load_cell_plan(Path(sys.argv[1]).resolve(),sys.argv[2])["cell_directory"])' $R $C)
ls -la --time-style=full-iso $CD/fresh-primary-peer
