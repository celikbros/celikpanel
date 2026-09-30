# usage: brief.sh SHORT CELL -- compact read-only summary for the operator log
S=$1; C=$2
D=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b9
E=/var/tmp/cp-b9-1001/evidence/$S
grep -v '^/var/tmp' /var/tmp/cp-b9-1001/logs/$S/collect.log | grep -E 'rc=|request id' | tr '\n' ' '; echo
cat /var/tmp/cp-b9-1001/logs/$S/run-prepared.rc
python3 $D/boundcheck.py $E $C 2>&1 | head -6
python3 $D/digest.py $E $C 2>&1 | grep -vE '^  (Restored|BIND units|Removed|Intentionally|Left in|Local port)' | cut -c1-500
if [ -d $E/paired-secondary-peer ]; then bash $D/peerv.sh $S 2>&1 | cut -c1-500; fi
