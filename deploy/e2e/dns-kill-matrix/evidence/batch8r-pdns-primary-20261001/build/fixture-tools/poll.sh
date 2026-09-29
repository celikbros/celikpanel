L=/var/tmp/cp-b8r-1001/logs/$1
date -u +%FT%TZ
tail -n ${2:-15} $L/driver.log
echo "--- run-prepared.log tail"; tail -n ${3:-8} $L/run-prepared.log 2>/dev/null | cut -c1-400
cat $L/run-prepared.rc 2>/dev/null
pgrep -a qemu | cut -c1-80
