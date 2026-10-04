SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch8r
for r in /var/tmp/cp-b8r-1001 /var/tmp/cp-b8r-1001/r2 /var/tmp/cp-b8r-1001/r3; do bash $SP/setup.sh $r; echo "setup $r rc=$?"; done
grep -E 'SETUP-OK|rc=|Error|error' /var/tmp/cp-b8r-setup.log | head -20
tail -5 /var/tmp/cp-b8r-setup.log
mkdir -p /var/tmp/cp-b8r-1001/logs
