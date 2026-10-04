S=$1; C=$2
D=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch6b
bash $D/check.sh $S $C
echo ==== digest
bash $D/dg.sh $S $C 2>&1 | cut -c1-900
E=/var/tmp/cp-b6b-0930/evidence/$S
if [ -d $E/paired-secondary-peer ]; then echo ==== peer; bash $D/peerv.sh $S 2>&1 | cut -c1-900; echo ==== options; cat $E/pdns-secondary-options.json | head -80; echo ==== catalog lines; cat $E/catalog-format-lines-transcript.txt $E/catalog-format-lines-agent-journal.txt | cut -c1-400; fi
echo ==== collect.log; grep -v "^/var/tmp" /var/tmp/cp-b6b-0930/logs/$S/collect.log | head -30
