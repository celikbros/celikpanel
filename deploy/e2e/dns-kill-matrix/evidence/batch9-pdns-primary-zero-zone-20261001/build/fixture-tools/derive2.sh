# Batch 9 (from 8r) derived, read-only files: status texts of all cells; zone lifecycle and catalog re-stamp timelines (c04, z04, z05; restamp timeline for every cell)
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b9
O=/var/tmp/cp-b9-1001/status-texts-all-cells.txt
: > $O
while read s c; do echo "######## $s $c" >> $O; python3 $SP/stat8r.py $s $c >> $O 2>&1; done < $SP/cells.txt
wc -l $O
for s in c04-pri-started-zl-rb z04-zero-committed-zl z05-zero-started-zl-rb; do
  E=/var/tmp/cp-b9-1001/evidence/$s
  python3 $SP/zl.py $s > $E/zone-lifecycle-summary.txt 2>&1
  python3 $SP/ledgerjobs.py $s >> $E/zone-lifecycle-summary.txt 2>&1
  { echo "== pdns daemon CATALOG-HASH lines (all boots)"; grep -h "new CATALOG-HASH" $E/raw/journald/pdns.service.txt; } >> $E/zone-lifecycle-summary.txt
  wc -l $E/zone-lifecycle-summary.txt
done
while read s c; do
  E=/var/tmp/cp-b9-1001/evidence/$s
  bash $SP/restamp.sh $s 00:00:00 23:59:59 > $E/catalog-restamp-timeline.txt 2>&1
  python3 $SP/samp.py $E/peer-dns-sampler.log > $E/peer-dns-sampler-changes.txt 2>&1
done < $SP/cells.txt
E=/var/tmp/cp-b9-1001/evidence/z04-zero-committed-zl/rec
python3 $SP/samp.py $E/peer-dns-sampler.log > $E/peer-dns-sampler-changes.txt 2>&1
python3 $SP/zl.py z04-zero-committed-zl/rec > $E/zone-lifecycle-summary.txt 2>&1
