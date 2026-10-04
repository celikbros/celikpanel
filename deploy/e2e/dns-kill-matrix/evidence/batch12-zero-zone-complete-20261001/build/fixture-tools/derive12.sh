# Batch 11 derived, read-only files for both cells
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
for p in "z04-zero-committed-zl pdns-switch__committed__after-write__paired-primary__peer-reachable" "z05-zero-started-zl-rb pdns-switch__target-started__after-write__paired-primary__peer-reachable"; do
  set -- $p; s=$1; c=$2
  E=/var/tmp/cp-b12-1001/evidence/$s
  python3 $SP/boundcheck10.py $E $c > $E/boundary-window.txt 2>&1; head -3 $E/boundary-window.txt
  python3 $SP/zlsum.py $E > $E/zone-lifecycle-summary.txt 2>&1
  { echo "== pdns daemon CATALOG-HASH lines (all boots)"; grep -h "new CATALOG-HASH" $E/raw/journald/pdns.service.txt; } >> $E/zone-lifecycle-summary.txt
  bash $SP/restamp.sh $s 00:00:00 23:59:59 > $E/catalog-restamp-timeline.txt 2>&1
  python3 $SP/samp.py $E/peer-dns-sampler.log > $E/peer-dns-sampler-changes.txt 2>&1
  python3 $SP/tlsum.py $E > $E/timeline-summary.txt 2>&1
  wc -l $E/zone-lifecycle-summary.txt $E/catalog-restamp-timeline.txt $E/peer-dns-sampler-changes.txt $E/timeline-summary.txt
  cat $E/zone-lifecycle-summary.txt | cut -c1-330
done
