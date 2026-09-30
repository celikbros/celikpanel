# usage: collect2.sh SHORT CELL ROOT SUB -- second read-only collect of a cell after the owner steps, into evidence/SHORT/SUB
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b9
L=/var/tmp/cp-b9-1001/logs/$1
mkdir -p $L/$4
cp $L/boot-monitor.log $L/$4/boot-monitor.log
for f in enroll.log zone-lifecycle-recover.log zone-lifecycle-recover.rc; do [ -e $L/$f ] && cp $L/$f $L/$4/; done
bash $SP/collect.sh "$1/$4" "$2" "$3"
echo "COLLECT2-DONE $(date -u +%FT%TZ)" >> $L/$4/collect.log
