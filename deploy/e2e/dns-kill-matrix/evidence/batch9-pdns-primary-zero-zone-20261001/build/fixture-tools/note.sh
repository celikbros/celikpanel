# usage: note.sh TEXT... -- append a timestamped line to the chain log
echo "$(date -u +%FT%TZ) $*" >> /var/tmp/cp-b9-1001/logs/chain.log
tail -3 /var/tmp/cp-b9-1001/logs/chain.log
