cd /var/tmp/cp-b9-1001/evidence
for s in c04-pri-started-zl-rb z01-zero-staged z02-zero-started z03-zero-committed z04-zero-committed-zl z05-zero-started-zl-rb z06-zero-owner-sql; do echo "######## $s"; cat $s/boundary-window.txt | head -5; cut -c1-400 $s/cell-digest.txt; cat $s/run-prepared.rc; done
