cd /var/tmp/cp-b9-1001/evidence
for s in "$@"; do echo "######## $s"; sed -E 's/ 1[01]:soa:udp=[^ ]*//g' $s/peer-dns-sampler-changes.txt | cut -c1-420; done
