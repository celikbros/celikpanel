# usage: waitrc.sh FILE [SECONDS] -- wait for FILE (or a FAILED line in the z05 driver log), then show the driver log tail
L=/var/tmp/cp-b10-1001/logs/z05-held-resume
end=$(( $(date +%s) + ${2:-570} ))
while [ "$(date +%s)" -lt "$end" ]; do [ -e "$1" ] && break; grep -q 'FAILED' $L/driver.log 2>/dev/null && break; sleep 5; done
grep '^###' $L/driver.log | tail -n 12
ls "$1" 2>&1
