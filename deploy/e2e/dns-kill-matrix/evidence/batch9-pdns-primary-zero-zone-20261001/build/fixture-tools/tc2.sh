SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b9
{ echo "Offline tests of the kill-matrix harness in /root/cp-b9-src (git archive 3cceb29a), $(date -u +%FT%TZ)";
  echo "unittest = python3 -m unittest MODULE (from deploy/e2e/dns-kill-matrix); direct = python3 FILE (from the tree root)";
  bash $SP/tcount.sh;
  echo "python3 -m unittest discover -s deploy/e2e/dns-kill-matrix -p 'test_*.py' (build.log): Ran 456 tests, OK";
  echo "go test -count=1 -v ./cmd/dns-kill-matrix-trigger (trigger-tests.log): top-level PASS $(grep -cE '^--- PASS' /var/tmp/cp-b9-triggertest.log), FAIL $(grep -cE '^--- FAIL' /var/tmp/cp-b9-triggertest.log), rc 0"; } > $SP/offline-test-counts.txt 2>&1
cat $SP/offline-test-counts.txt
