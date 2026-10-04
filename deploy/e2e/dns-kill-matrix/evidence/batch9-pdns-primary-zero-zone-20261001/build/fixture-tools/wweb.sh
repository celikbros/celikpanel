end=$(( $(date +%s) + 560 ))
while [ "$(date +%s)" -lt "$end" ]; do grep -q 'WEB-DONE' /var/tmp/cp-b9-webbuild.log 2>/dev/null && break; sleep 5; done
tail -n 40 /var/tmp/cp-b9-webbuild.log
