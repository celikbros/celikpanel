E=/var/tmp/cp-b6b-0930/evidence/$1
awk "{for(i=1;i<=NF;i++) if(\$i ~ /^jphase=/) print \$1, \$i}" $E/raw/watch/cp-b6b-watch/timeline.log | uniq -f1 | head -30
grep "CHANGE pdnsdb" $E/raw/watch/cp-b6b-watch/timeline.log
grep -E "^########|WAL|domains:|records per domain:|domainmetadata rows|domainmetadata:" $E/pdns-db-timeline.txt | cut -c60-360
