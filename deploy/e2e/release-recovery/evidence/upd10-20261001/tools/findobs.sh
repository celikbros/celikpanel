#!/bin/bash
# usage: findobs.sh LAB LABEL -> evidence files that carry the observation label (read-only)
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/ | tail -1)
grep -rl -- "$2" $ev | sed "s#$ev##"
grep -h -E "daemon (start|quit)" $ev/steps/*collect/journal-packagekit.txt 2>/dev/null | cut -c1-120
