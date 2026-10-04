# Batch 8r derived, read-only files: status texts of all cells; zone lifecycle and catalog re-stamp timelines (c04, c06)
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch8r
O=/var/tmp/cp-b8r-1001/status-texts-all-cells.txt
: > $O
while read s c; do echo "######## $s $c" >> $O; python3 $SP/stat8r.py $s $c >> $O 2>&1; done < $SP/cells.txt
wc -l $O
for s in c04-pri-started-zl-rb c06-pri-committed-zl; do
  E=/var/tmp/cp-b8r-1001/evidence/$s
  python3 $SP/zl.py $s > $E/zone-lifecycle-summary.txt 2>&1
  python3 $SP/ledgerjobs.py $s >> $E/zone-lifecycle-summary.txt 2>&1
  { echo "== pdns daemon CATALOG-HASH lines (all boots)"; grep -h "new CATALOG-HASH" $E/raw/journald/pdns.service.txt; } >> $E/zone-lifecycle-summary.txt
  bash $SP/restamp.sh $s 00:00:00 23:59:59 > $E/catalog-restamp-timeline.txt 2>&1
  wc -l $E/zone-lifecycle-summary.txt $E/catalog-restamp-timeline.txt
done
