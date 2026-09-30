cd /var/tmp/cp-b9-1001/evidence
for s in "$@"; do echo "######## $s"; sed -n '/== loaded catalog/,/== \/var\/named tree/p' $s/bind-secondary-state-post-collect.txt | cut -c1-200; head -3 $s/bind-secondary-state-post-collect.txt; done
