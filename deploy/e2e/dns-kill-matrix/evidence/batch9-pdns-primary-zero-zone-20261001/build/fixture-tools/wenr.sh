end=$(( $(date +%s) + 500 ))
while [ "$(date +%s)" -lt "$end" ]; do grep -qE 'owner-enrollment done|FAILED|MISMATCH' /var/tmp/cp-b9-1001/logs/z04-zero-committed-zl/enroll.log 2>/dev/null && break; sleep 5; done
cut -c1-1500 /var/tmp/cp-b9-1001/logs/z04-zero-committed-zl/enroll.log
