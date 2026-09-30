end=$(( $(date +%s) + 560 ))
while [ "$(date +%s)" -lt "$end" ]; do grep -qE 'BUILD-OK|rror|FAIL' /var/tmp/cp-b11-build.log 2>/dev/null && break; sleep 5; done
tail -n 80 /var/tmp/cp-b11-build.log
