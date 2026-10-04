L=/var/tmp/cp-b12-1001/logs/z05-zero-started-zl-rb
cat $L/smstatus.stderr | cut -c1-300; head -c 400 /var/tmp/cp-b12-1001/evidence/z05-zero-started-zl-rb/service-mutation-status-post-collect.json; echo
grep -E 'celikpanel-agent|celikpanel-panel' $L/post-state.txt | head -6
