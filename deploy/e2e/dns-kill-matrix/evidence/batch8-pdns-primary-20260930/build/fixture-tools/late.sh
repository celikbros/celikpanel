# Copies two late log files into the evidence directory and lists what is there (read-only otherwise)
D='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-kill-matrix/evidence/batch8-pdns-primary-20260930'
L=/var/tmp/cp-b8-0930/logs
cp $L/c01-pri-intent/run-prepared.rc.standalone-gate-probe-attempt "$D/c01-pri-intent/run-prepared.rc.standalone-gate-probe-attempt"
ls "$D/c01-pri-intent" | grep -E 'gate|rc'
ls "$D/c04-pri-started-zl" | grep dryrun
cat "$D/c04-pri-started-zl/run-prepared-dryrun-full-combination.log"
