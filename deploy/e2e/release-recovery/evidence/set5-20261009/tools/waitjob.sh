#!/bin/bash
# set5: exit 0 when the named job has ended and been staged, or when no queue worker runs any more
[ -e /var/tmp/cp-set5-run/logs/stage-$1.txt ] && ! pgrep -f "stage.sh $1 " > /dev/null && exit 0
pgrep -f "set5/queue.sh" > /dev/null || exit 0
exit 1
