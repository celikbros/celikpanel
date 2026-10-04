# Read-only: agent journal and ledger around the zone lifecycle; usage: restamp.sh SHORT FROM TO (HH:MM:SS)
E=/var/tmp/cp-b11-1001/evidence/$1
echo "== dbwatch timeline"; cat $E/raw/watch/cp-b11-watch-db*/timeline.log | cut -c1-200
echo "== agent journal $2..$3"; awk -v a="$2" -v b="$3" '{t=substr($1,12,8); if (t>=a && t<=b) print}' $E/raw/journald/celikpanel-agent.service.txt | cut -c1-400
echo "== pdns journal $2..$3"; awk -v a="$2" -v b="$3" '{t=substr($1,12,8); if (t>=a && t<=b) print}' $E/raw/journald/pdns.service.txt | cut -c1-300
echo "== peer sampler cat serial transitions"; grep -o '^[^ ]* \|10:cat:udp=[^ ]*\|11:cat:udp=[^ ]*' $E/peer-dns-sampler.log | paste - - - | awk '{k=$2" "$3; if (k!=last) print; last=k}'
