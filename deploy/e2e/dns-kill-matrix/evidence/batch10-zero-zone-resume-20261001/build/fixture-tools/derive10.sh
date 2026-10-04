# Batch 10 derived, read-only files for both cells
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b10
C5=pdns-switch__target-started__after-write__paired-primary__peer-reachable
E=/var/tmp/cp-b10-1001/evidence/z05-held-resume
python3 $SP/boundcheck10.py $E $C5 > $E/boundary-window.txt 2>&1; head -3 $E/boundary-window.txt
for s in z04-resume z05-held-resume; do
  E=/var/tmp/cp-b10-1001/evidence/$s
  python3 $SP/zlsum.py $E > $E/zone-lifecycle-summary.txt 2>&1
  { echo "== pdns daemon CATALOG-HASH lines (all boots)"; grep -h "new CATALOG-HASH" $E/raw/journald/pdns.service.txt; } >> $E/zone-lifecycle-summary.txt
  bash $SP/restamp.sh $s 00:00:00 23:59:59 > $E/catalog-restamp-timeline.txt 2>&1
  python3 $SP/samp.py $E/peer-dns-sampler.log > $E/peer-dns-sampler-changes.txt 2>&1
  python3 $SP/tlsum.py $E > $E/timeline-summary.txt 2>&1
  wc -l $E/zone-lifecycle-summary.txt $E/catalog-restamp-timeline.txt $E/peer-dns-sampler-changes.txt $E/timeline-summary.txt
done
cat /var/tmp/cp-b10-1001/evidence/z05-held-resume/peer-dns-sampler-changes.txt | cut -c1-260 | head -40
