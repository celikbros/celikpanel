# usage: waitrc.sh SHORT FILE [SECONDS] -- wait for FILE (or a FAILED line in the driver log), then show the driver log tail
L=/var/tmp/cp-b11-1001/logs/$1
end=$(( $(date +%s) + ${3:-570} ))
while [ "$(date +%s)" -lt "$end" ]; do [ -e "$2" ] && break; grep -q 'FAILED' $L/driver.log 2>/dev/null && break; sleep 5; done
date -u +%FT%TZ
grep '^###' $L/driver.log | tail -n 14
ls "$2" 2>&1 && cat "$2"
