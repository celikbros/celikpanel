E=/var/tmp/cp-b10-1001/evidence/z04-resume
tail -n 1 $E/catalog-serial-history.txt | cut -c250-900
cat $E/raw/watch/cp-b10-watch-db/timeline.log | cut -c1-200
