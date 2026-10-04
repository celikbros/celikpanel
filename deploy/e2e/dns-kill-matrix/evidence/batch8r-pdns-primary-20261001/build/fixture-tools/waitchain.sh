# usage: waitchain.sh PATTERN MAXSEC -- wait until chain.log matches PATTERN, then print its start/done lines
end=$(( $(date +%s) + $2 ))
while [ "$(date +%s)" -lt "$end" ]; do
  grep -qE "$1|CHAIN-DONE|chain stopped" /var/tmp/cp-b8r-1001/logs/chain.log && break
  sleep 10
done
grep -E 'start|done|rc=|QEMU' /var/tmp/cp-b8r-1001/logs/chain.log
