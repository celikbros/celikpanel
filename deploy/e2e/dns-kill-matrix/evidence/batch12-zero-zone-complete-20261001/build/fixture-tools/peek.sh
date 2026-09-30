date -u +%FT%T.%3NZ
tail -n 25 /var/tmp/cp-b12-1001/logs/z05-zero-started-zl-rb/enroll.log | cut -c1-250
pgrep -af 'enroll11|gssh|ssh ' | cut -c1-250
