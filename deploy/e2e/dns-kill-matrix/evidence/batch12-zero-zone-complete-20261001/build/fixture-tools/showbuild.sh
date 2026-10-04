grep -v -E '^(=== RUN|--- PASS|    --- PASS|PASS$)' /var/tmp/cp-b12-build.log | cut -c1-220
