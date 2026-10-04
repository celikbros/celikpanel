# usage: cpoll.sh SHORT [N] -- tail of the cell's driver log and run-prepared log
L=/var/tmp/cp-b9-1001/logs/$1
echo "== driver.log"; tail -n ${2:-12} $L/driver.log 2>&1
echo "== run-prepared.log"; tail -n ${3:-8} $L/run-prepared.log 2>&1 | cut -c1-400
cat $L/run-prepared.rc 2>/dev/null
