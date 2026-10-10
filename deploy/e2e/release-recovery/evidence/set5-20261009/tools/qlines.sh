#!/bin/bash
# set5: the queue log lines that end a cell or the queue; a last line says whether a worker still runs
grep -E "staged|NOT started|did not complete|queue done|queue.stop|refusing" /var/tmp/cp-set5-run/logs/queue.log | cut -c1-230
pgrep -f "set5/queue.sh" > /dev/null || echo "WORKER-GONE"
