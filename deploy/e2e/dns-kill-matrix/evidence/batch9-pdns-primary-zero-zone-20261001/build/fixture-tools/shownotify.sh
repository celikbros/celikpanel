cd /var/tmp/cp-b9-1001/evidence
for s in "$@"; do echo "######## $s"; cut -c1-330 $s/pdns-notify-journal-and-also-notify.txt | grep -v '^   \[' ; done
