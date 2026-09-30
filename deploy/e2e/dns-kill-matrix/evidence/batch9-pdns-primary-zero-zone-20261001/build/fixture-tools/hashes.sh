cd /var/tmp/cp-b9-1001/evidence
for s in "$@"; do echo "######## $s"; grep -hE 'CATALOG-HASH|Started pdns|Stopped pdns|Stopping pdns' $s/raw/journald/pdns.service.txt | cut -c1-200; echo "last pdns journal line:"; tail -1 $s/raw/journald/pdns.service.txt | cut -c1-200; done
