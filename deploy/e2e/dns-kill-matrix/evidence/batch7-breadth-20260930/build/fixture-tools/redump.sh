S=$1; C=$2; ROOT=$3
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch7
E=/var/tmp/cp-b7-0930/evidence/$S
: > $E/pdns-db-timeline.txt
for f in $(find $E/raw/watch -name "*.sqlite3" | sort); do echo "######## $f" >> $E/pdns-db-timeline.txt; python3 $SP/pdnsdb.py $f copy >> $E/pdns-db-timeline.txt 2>&1; done
if [ -n "$ROOT" ]; then python3 $SP/gssh.py $ROOT $C debian13 "sudo python3 - /var/lib/powerdns/pdns.sqlite3" < $SP/pdnsdb.py > $E/pdns-db-post-collect-redump.txt 2>&1; echo redump rc=$?; fi
grep -c . $E/pdns-db-timeline.txt
