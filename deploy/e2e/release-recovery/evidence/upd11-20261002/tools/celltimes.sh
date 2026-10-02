#!/bin/bash
cd /var/tmp/cp-upd11-run/logs
for s in cell-*.start; do n=${s%.start}
  printf '%s %s %s rc=%s %s\n' $n "$(cat $s)" "$(cat $n.end 2>/dev/null)" "$(cat $n.rc 2>/dev/null)" "$(grep -o 'cell [a-z0-9-]* [^ ]* upd11-[a-z0-9-]*' ../jobs/job-$n.sh | awk '{print $2, $4}')"
done
